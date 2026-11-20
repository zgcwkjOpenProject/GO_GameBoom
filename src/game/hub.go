// Package game 提供房间与对战服务。房间状态只在 Hub 的单个循环里被读写，
// WebSocket 连接的收发各自在独立 goroutine 上，通过 channel 与 Hub 通信。
package game

import (
	"context"
	"encoding/json"
	"time"
)

// DemoState 是首页演示需要的初始棋盘数据
type DemoState struct {
	Cols int `json:"cols"` // 演示棋盘列数
	Rows int `json:"rows"` // 演示棋盘行数
	Snapshot
}

// DemoSnapshot 返回首页本地演示所需的随机初始对局快照，不创建房间也不启动服务端机器人
func DemoSnapshot() DemoState {
	g := New([]Seat{
		{ID: "demo-red", Name: "红方电脑", Robot: true},
		{ID: "demo-blue", Name: "蓝方电脑", Robot: true},
	}, "normal")
	return DemoState{Cols: g.Cols, Rows: g.Rows, Snapshot: g.Serialize()}
}

const (
	tickInterval   = 33 * time.Millisecond // 服务端规则判定帧间隔
	actionInterval = tickInterval          // 移动坐标随每个规则帧同步，避免画面卡步
	mapInterval    = 5 * time.Second       // 向浏览器发送完整地图与权威校准状态的间隔
	maxDT          = 120.0                 // 单帧最多推进这么多毫秒，防止卡顿后瞬移
)

type command struct {
	client *Client // 消息来源连接
	msg    inbound // 已解析的浏览器消息
}

// Hub 管理所有连接与房间
type Hub struct {
	clients map[string]*Client // 所有在线连接
	rooms   map[string]*Room   // 仍然存在的房间
	order   []string           // 房间创建顺序，让大厅列表保持稳定
	seq     int                // 下一个连接标识序号

	register   chan *Client  // WebSocket 握手完成后注册的连接
	unregister chan *Client  // 已断开连接的注销请求
	commands   chan command  // 读协程提交的浏览器消息
	done       chan struct{} // Hub 主循环退出通知
	stats      *playerStats  // 首页游玩人数统计
}

// NewHub 创建一个空的 Hub
func NewHub() *Hub {
	return NewHubWithStats(defaultStatsPath())
}

// NewHubWithStats 创建一个使用指定 JSON 文件保存人数统计的 Hub
func NewHubWithStats(statsPath string) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		rooms:      make(map[string]*Room),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		commands:   make(chan command, 256),
		done:       make(chan struct{}),
		stats:      newPlayerStats(statsPath),
	}
}

// Run 跑主循环，直到 ctx 结束。所有房间逻辑都在这个 goroutine 里推进。
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-h.register:
			h.seq++
			c.id = itoa(h.seq)
			h.clients[c.id] = c
			h.send(c, outStats{T: "stats", Stats: h.stats.view()})
		case c := <-h.unregister:
			h.leaveRoom(c)
			delete(h.clients, c.id)
		case cmd := <-h.commands:
			h.dispatch(cmd.client, cmd.msg)
		case now := <-ticker.C:
			h.step(now)
		}
	}
}

// step 推进所有正在对战的房间
func (h *Hub) step(now time.Time) {
	for _, id := range append([]string(nil), h.order...) {
		if room, ok := h.rooms[id]; ok && room.game != nil {
			h.stepRoom(room, now)
		}
	}
}

// stepRoom 每帧推进规则，动作即时广播，完整地图与校准状态每五秒发送一次
func (h *Hub) stepRoom(room *Room, now time.Time) {
	dt := float64(now.Sub(room.last).Milliseconds())
	if dt > maxDT {
		dt = maxDT
	}
	if dt < 0 {
		dt = 0
	}
	room.last = now

	g := room.game
	g.Update(dt)
	if actionDue(room, now) {
		h.flushActions(room)
		room.lastActions = now
	}
	if mapDue(room, now) {
		h.broadcast(room, h.stateOf(room, room.lastMap.IsZero()))
		room.lastMap = now
	} else if g.HasEvents() {
		h.broadcast(room, h.eventStateOf(room))
	}

	if !g.Over {
		return
	}
	room.Playing = false
	h.broadcast(room, outOver{T: "over", Result: g.Result})
	room.game = nil
	h.pushRoom(room)
	h.pushLobby()
}

// eventStateOf 生成离散事件的增量状态，减少混战时重复发送的玩家数据
func (h *Hub) eventStateOf(room *Room) outState {
	snapshot := room.game.SerializeEvents()
	snapshot.Map = ""
	snapshot.MapVersion = 0
	snapshot.Time = 0
	snapshot.Sudden = false
	return outState{T: "state", S: snapshot}
}

// stateOf 生成一条完整校准状态消息
func (h *Hub) stateOf(room *Room, fullMap bool) outState {
	snapshot := room.game.Serialize()
	if !fullMap {
		snapshot.Map = ""
	}
	return outState{T: "state", S: snapshot}
}

// actionDue 判断当前时刻是否应广播移动玩家的动作校准
func actionDue(room *Room, now time.Time) bool {
	return !now.Before(room.lastActions.Add(actionInterval))
}

// flushActions 广播移动玩家的输入确认，并附带当前位置校准
func (h *Hub) flushActions(room *Room) {
	actions := make([]actionView, 0, len(room.actions))
	for _, p := range room.game.Players {
		input, ok := room.game.InputOf(p.ID)
		if !ok || (input.X == 0 && input.Y == 0 && !room.actions[p.ID]) {
			continue
		}
		actions = append(actions, actionView{Seat: p.Seat, X: input.X, Y: input.Y, PX: int(round2(p.X) * 100), PY: int(round2(p.Y) * 100)})
	}
	room.actions = nil
	if len(actions) > 0 {
		h.broadcast(room, outActions{T: "acts", Actions: actions})
	}
}

// queueAction 记录下一次服务端帧需要确认的玩家输入
func (h *Hub) queueAction(room *Room, id string) {
	if room.actions == nil {
		room.actions = make(map[string]bool)
	}
	room.actions[id] = true
}

// mapDue 判断当前时刻是否应发送完整地图和权威校准状态
func mapDue(room *Room, now time.Time) bool {
	return room.lastMap.IsZero() || !now.Before(room.lastMap.Add(mapInterval))
}

// send 把一条消息发给单个客户端
func (h *Hub) send(c *Client, v any) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.push(raw)
}

// broadcast 只序列化一次，再发给房内的真人和观众
func (h *Hub) broadcast(room *Room, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	for _, c := range h.clients {
		if c.roomID == room.ID {
			c.push(raw)
		}
	}
}

// dispatch 按消息类型分发，未知类型直接忽略
func (h *Hub) dispatch(c *Client, m inbound) {
	if _, ok := h.clients[c.id]; !ok {
		return // 已经断开的连接，残留消息丢掉
	}
	switch m.T {
	case "hello":
		h.onHello(c, m)
	case "rooms":
		h.send(c, outRooms{T: "rooms", List: h.lobbyList()})
	case "create":
		h.onCreate(c, m)
	case "join":
		h.onJoin(c, m)
	case "leave":
		h.leaveRoom(c)
		h.send(c, outSimple{T: "left"})
		h.send(c, outRooms{T: "rooms", List: h.lobbyList()})
	case "ready":
		h.onReady(c, m)
	case "bots":
		h.onBots(c, m)
	case "maze":
		h.onMaze(c, m)
	case "start":
		h.onStart(c)
	case "input":
		h.onInput(c, m)
	case "bomb":
		if room, ok := h.rooms[c.roomID]; ok && room.game != nil {
			room.game.RequestBomb(c.id)
		}
	case "bot":
		h.onBot(c, m)
	case "chat":
		h.onChat(c, m)
	case "ping":
		h.send(c, outSimple{T: "pong"})
	}
}

// onInput 让服务端校验并保存真人动作，再把实际接受的方向广播给同局玩家
func (h *Hub) onInput(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok || room.game == nil || !room.game.SetInput(c.id, signOf(m.X), signOf(m.Y)) {
		return
	}
	h.queueAction(room, c.id)
}

// onBot 只接收房主为机器人发出的意图，位置、碰撞、炸弹与伤害仍由服务端计算
func (h *Hub) onBot(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok || room.game == nil || room.HostID != c.id {
		return
	}
	inputs := m.Bots
	if len(inputs) == 0 {
		inputs = []botInput{{ID: m.ID, X: m.X, Y: m.Y, Bomb: m.Bomb}}
	}
	if len(inputs) > maxSeats {
		return
	}
	seen := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		if seen[input.ID] {
			continue
		}
		seen[input.ID] = true
		h.applyBotInput(room, input)
	}
}

// applyBotInput 校验并应用一名机器人的输入意图
func (h *Hub) applyBotInput(room *Room, input botInput) {
	for _, p := range room.game.Players {
		if p.ID != input.ID || !p.Robot {
			continue
		}
		if input.Bomb {
			room.game.SetInput(p.ID, signOf(input.X), signOf(input.Y))
			room.game.RequestBomb(p.ID)
			h.queueAction(room, p.ID)
			return
		}
		if room.game.SetInput(p.ID, signOf(input.X), signOf(input.Y)) {
			h.queueAction(room, p.ID)
		}
		return
	}
}

// onHello 清洗昵称并返回当前连接身份
func (h *Hub) onHello(c *Client, m inbound) {
	c.name = clean(m.Name, 10)
	if c.name == "" {
		c.name = "玩家" + c.id
	}
	h.send(c, outWelcome{T: "welcome", ID: c.id, Name: c.name})
	h.pushLobby()
}

// onChat 清洗并广播房间成员发出的短消息
func (h *Hub) onChat(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok {
		return
	}
	text := clean(m.Text, 80)
	if text == "" {
		return
	}
	h.broadcast(room, outChat{T: "chat", Chat: chatView{Name: c.name, Text: text}})
}

// onCreate 创建房间并让发起者成为房主
func (h *Hub) onCreate(c *Client, m inbound) {
	if c.roomID != "" {
		h.leaveRoom(c)
	}
	name := clean(m.Name, 12)
	if name == "" {
		name = c.name + " 的房间"
	}
	room := &Room{
		ID:     h.newRoomID(),
		Name:   name,
		HostID: c.id,
		Seats:  []*RoomSeat{{ID: c.id, Name: c.name, Ready: true}},
		Bots:   defaultBots,
	}
	h.rooms[room.ID] = room
	h.order = append(h.order, room.ID)
	c.roomID = room.ID
	h.send(c, outRoom{T: "joined", Room: viewOf(room)})
	h.pushRoom(room)
	h.pushLobby()
}

// onJoin 将客户端加入候场房间，或以观众身份进入正在对战的房间
func (h *Hub) onJoin(c *Client, m inbound) {
	room, ok := h.rooms[m.RoomID.String()]
	if !ok {
		h.send(c, outSimple{T: "error", Msg: "房间不存在"})
		return
	}
	if room.Playing {
		if c.roomID != room.ID && c.roomID != "" {
			h.leaveRoom(c)
		}
		c.roomID = room.ID
		h.send(c, outRoom{T: "joined", Room: viewOf(room)})
		h.send(c, outStart{T: "start", Seats: gameSeats(room.game), Viewers: h.spectatorViews(room), You: c.id, Cols: room.game.Cols, Rows: room.game.Rows, Theme: room.Theme, Maze: room.Maze, Spectator: !isSeat(room, c.id)})
		h.send(c, h.stateOf(room, true))
		h.pushViewers(room)
		room.lastMap = time.Now()
		return
	}
	if len(room.Seats)+room.Bots >= maxSeats {
		h.send(c, outSimple{T: "error", Msg: "房间已满"})
		return
	}
	if c.roomID == room.ID {
		h.send(c, outRoom{T: "joined", Room: viewOf(room)}) // 已经在这个房间了，补一份视图就行
		return
	}
	if c.roomID != "" {
		h.leaveRoom(c)
	}
	room.Seats = append(room.Seats, &RoomSeat{ID: c.id, Name: c.name})
	c.roomID = room.ID
	h.send(c, outRoom{T: "joined", Room: viewOf(room)})
	h.pushRoom(room)
	h.pushLobby()
}

// onReady 更新真人玩家的准备状态
func (h *Hub) onReady(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok {
		return
	}
	for _, s := range room.Seats {
		if s.ID == c.id {
			s.Ready = m.V
			break
		}
	}
	h.pushRoom(room)
}

// onBots 房主调整电脑玩家数量与难度
func (h *Hub) onBots(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok || room.HostID != c.id || room.Playing {
		return
	}
	if m.N != nil {
		n := int(roundHalfUp(*m.N))
		room.Bots = clampInt(n, 0, maxSeats-len(room.Seats))
	}
	h.pushRoom(room)
}

// onMaze 更新房主设置的迷踪模式，开启后参战者只看局部视野
func (h *Hub) onMaze(c *Client, m inbound) {
	room, ok := h.rooms[c.roomID]
	if !ok || room.HostID != c.id || room.Playing {
		return
	}
	room.Maze = m.V
	h.pushRoom(room)
}

// onStart 在全部真人准备后由房主启动对局
func (h *Hub) onStart(c *Client) {
	room, ok := h.rooms[c.roomID]
	if !ok || room.HostID != c.id || room.Playing {
		return
	}
	// 除房主以外的人都得准备好
	for _, s := range room.Seats {
		if s.ID != c.id && !s.Ready {
			h.send(c, outSimple{T: "error", Msg: "还有玩家没准备好"})
			return
		}
	}
	h.startGame(room)
}
