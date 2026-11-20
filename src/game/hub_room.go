// Package game 管理房间状态、房间视图与开局流程
package game

import (
	"encoding/json"
	"math/rand"
	"time"
)

const (
	maxSeats    = 8 // 一局最多人数，含电脑
	defaultBots = 1 // 新建房间默认电脑数量
	boardThemes = 3 // 随机棋局主题数量
)

// 电脑玩家使用固定名字，避免创建房间时生成无意义状态
var botNames = []string{
	"电脑·阿呆", "电脑·小炸", "电脑·老六", "电脑·雷霆",
	"电脑·闪电", "电脑·铁头", "电脑·追风", "电脑·火花",
	"电脑·方块", "电脑·雷鸣", "电脑·疾行", "电脑·石墙",
	"电脑·爆破", "电脑·夜影", "电脑·星火", "电脑·冠军"}

// RoomSeat 是房间里的一个真人座位
type RoomSeat struct {
	ID    string // 玩家连接标识
	Name  string // 已清洗的玩家昵称
	Ready bool   // 是否已准备开局
}

// Room 是 Hub 单循环持有的房间及当前对局
type Room struct {
	ID      string      // 四位房间号
	Name    string      // 已清洗的房间名称
	HostID  string      // 当前房主连接标识
	Seats   []*RoomSeat // 真人座位
	Bots    int         // 电脑玩家数量
	Playing bool        // 是否正在对局
	Theme   int         // 当前棋局主题编号
	Maze    bool        // 是否启用参战者局部视野

	game        *Game           // 当前对局规则状态
	last        time.Time       // 上次规则推进的时刻
	lastMap     time.Time       // 上次发送完整地图与权威校准状态的时刻
	lastActions time.Time       // 上次广播合并动作的时刻
	actions     map[string]bool // 本服务端帧待合并广播的动作玩家
}

// isSeat 判断客户端是否占用该房间的真人座位
func isSeat(room *Room, id string) bool {
	for _, s := range room.Seats {
		if s.ID == id {
			return true
		}
	}
	return false
}

// gameSeats 生成当前对局中的真人和电脑座位视图
func gameSeats(g *Game) []seatView {
	seats := make([]seatView, 0, len(g.Players))
	for _, p := range g.Players {
		seats = append(seats, seatView{ID: p.ID, Name: p.Name, Seat: p.Seat, Robot: p.Robot})
	}
	return seats
}

// spectatorViews 生成当前房间中不占真人座位的观战者列表
func (h *Hub) spectatorViews(room *Room) []seatView {
	viewers := make([]seatView, 0)
	for _, c := range h.clients {
		if c.roomID == room.ID && !isSeat(room, c.id) {
			viewers = append(viewers, seatView{ID: c.id, Name: c.name})
		}
	}
	return viewers
}

// pushViewers 把最新观战者列表同步给当前对局的所有浏览器
func (h *Hub) pushViewers(room *Room) {
	if room.game != nil {
		h.broadcast(room, outViewers{T: "viewers", Viewers: h.spectatorViews(room)})
	}
}

// briefOf 生成大厅使用的精简房间视图
func (h *Hub) briefOf(room *Room) roomBrief {
	host := ""
	if c, ok := h.clients[room.HostID]; ok {
		host = c.name
	}
	return roomBrief{ID: room.ID, Name: room.Name, Count: len(room.Seats) + room.Bots, Max: maxSeats, Playing: room.Playing, Host: host}
}

// viewOf 生成房内使用的完整房间视图
func viewOf(room *Room) roomView {
	seats := make([]seatView, 0, len(room.Seats))
	for i, s := range room.Seats {
		seats = append(seats, seatView{ID: s.ID, Name: s.Name, Ready: s.Ready, Seat: i})
	}
	count := max(0, min(room.Bots, len(botNames)))
	names := append([]string(nil), botNames[:count]...)
	return roomView{ID: room.ID, Name: room.Name, HostID: room.HostID, Playing: room.Playing, Bots: room.Bots, BotNames: names, Max: maxSeats, Maze: room.Maze, Seats: seats}
}

// lobbyList 按创建顺序生成大厅房间列表
func (h *Hub) lobbyList() []roomBrief {
	list := make([]roomBrief, 0, len(h.order))
	for _, id := range h.order {
		if room, ok := h.rooms[id]; ok {
			list = append(list, h.briefOf(room))
		}
	}
	return list
}

// pushLobby 把房间列表推给所有还在大厅的客户端
func (h *Hub) pushLobby() {
	raw, err := json.Marshal(outRooms{T: "rooms", List: h.lobbyList()})
	if err != nil {
		return
	}
	for _, c := range h.clients {
		if c.roomID == "" {
			c.push(raw)
		}
	}
}

// pushRoom 把房间状态推给房内所有人
func (h *Hub) pushRoom(room *Room) {
	h.broadcast(room, outRoom{T: "room", Room: viewOf(room)})
}

// pushStats 把更新后的人数统计推给所有在线浏览器
func (h *Hub) pushStats() {
	message := outStats{T: "stats", Stats: h.stats.view()}
	for _, c := range h.clients {
		h.send(c, message)
	}
}

// leaveRoom 让客户端退出所在房间，人走空了就解散
func (h *Hub) leaveRoom(c *Client) {
	room, ok := h.rooms[c.roomID]
	c.roomID = ""
	if !ok {
		return
	}
	for i, s := range room.Seats {
		if s.ID == c.id {
			room.Seats = append(room.Seats[:i], room.Seats[i+1:]...)
			break
		}
	}
	if room.game != nil {
		for _, p := range room.game.Players {
			if p.ID == c.id {
				room.game.Kill(p, "")
				p.Lives = 0
				p.RespawnTimer = 0
				break
			}
		}
	}
	if len(room.Seats) == 0 {
		h.endRoom(room)
		return
	} else {
		if room.HostID == c.id {
			room.HostID = room.Seats[0].ID
		}
		if room.Bots+len(room.Seats) > maxSeats {
			room.Bots = maxSeats - len(room.Seats)
		}
		h.pushRoom(room)
	}
	h.pushViewers(room)
	h.pushLobby()
}

// endRoom 结束没有真人参战者的房间，并让留下的观战者返回大厅
func (h *Hub) endRoom(room *Room) {
	for _, c := range h.clients {
		if c.roomID != room.ID {
			continue
		}
		c.roomID = ""
		h.send(c, outSimple{T: "left"})
	}
	h.removeRoom(room.ID)
	h.pushLobby()
}

// removeRoom 删除房间及其创建顺序记录
func (h *Hub) removeRoom(id string) {
	delete(h.rooms, id)
	for i, v := range h.order {
		if v == id {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
}

// newRoomID 生成当前未被占用的四位房号
func (h *Hub) newRoomID() string {
	for {
		id := itoa(1000 + rand.Intn(9000))
		if _, ok := h.rooms[id]; !ok {
			return id
		}
	}
}

// startGame 开一局：真人先入座，电脑补在后面，四角分配出生点
func (h *Hub) startGame(room *Room) {
	seats := make([]Seat, 0, maxSeats)
	for _, s := range room.Seats {
		seats = append(seats, Seat{ID: s.ID, Name: s.Name})
	}
	for i := 0; i < room.Bots && len(seats) < maxSeats; i++ {
		seats = append(seats, Seat{ID: "bot" + itoa(i+1), Name: botNames[i], Robot: true})
	}
	room.game = New(seats, "")
	room.Theme = rand.Intn(boardThemes)
	room.Playing = true
	room.last = time.Now()
	room.lastMap = time.Time{}
	room.lastActions = time.Time{}
	h.stats.recordGame(len(room.Seats), room.last)
	for _, s := range room.Seats {
		s.Ready = false
	}
	view := gameSeats(room.game)
	viewers := h.spectatorViews(room)
	for _, c := range h.clients {
		if c.roomID == room.ID {
			h.send(c, outStart{T: "start", Seats: view, Viewers: viewers, You: c.id, Cols: room.game.Cols, Rows: room.game.Rows, Theme: room.Theme, Maze: room.Maze, Spectator: !isSeat(room, c.id)})
		}
	}
	h.pushStats()
	h.pushLobby()
}
