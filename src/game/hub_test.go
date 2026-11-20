// 大厅与对局服务的集成测试，用真实 WebSocket 客户端跑完整协议
package game

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testMessageTimeout = 3 * time.Second // 联机测试单次等消息的最长时间

// testClient 是一条测试用的 WebSocket 连接，带上按类型等消息的辅助方法
type testClient struct {
	t     *testing.T
	conn  *websocket.Conn
	id    string
	state Snapshot // 最近收到的完整状态，用于合并后续增量帧
}

// startHub 起一个 Hub 和 httptest 服务器，返回拨号地址
func startHub(t *testing.T) string {
	t.Helper()
	hub := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	srv := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dial 建立一条测试用 WebSocket 连接
func dial(t *testing.T, url string) *testClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("握手失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &testClient{t: t, conn: conn}
}

// send 向测试服务发送一条 JSON 消息
func (c *testClient) send(v map[string]any) {
	c.t.Helper()
	if err := c.conn.WriteJSON(v); err != nil {
		c.t.Fatalf("发送失败: %v", err)
	}
}

// next 读下一条消息，超时就失败
func (c *testClient) next(timeout time.Duration) map[string]any {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(timeout))
	var m map[string]any
	if err := c.conn.ReadJSON(&m); err != nil {
		c.t.Fatalf("读取失败: %v", err)
	}
	return m
}

// waitFor 一直读到指定类型的消息
func (c *testClient) waitFor(kind string, timeout time.Duration) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			c.t.Fatalf("等 %s 超时", kind)
		}
		m := c.next(left)
		if m["t"] == kind {
			return m
		}
	}
}

// hello 打招呼并记下服务端分配的 id
func (c *testClient) hello(name string) {
	c.t.Helper()
	c.send(map[string]any{"t": "hello", "name": name})
	m := c.waitFor("welcome", testMessageTimeout)
	id, _ := m["id"].(string)
	if id == "" {
		c.t.Fatalf("welcome 没带 id: %v", m)
	}
	c.id = id
}

// waitForState 读到下一帧 state，合并增量后返回当前完整快照
func (c *testClient) waitForState(timeout time.Duration) Snapshot {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			c.t.Fatal("等 state 超时")
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(left))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("读取失败: %v", err)
		}
		var frame struct {
			T string   `json:"t"`
			S Snapshot `json:"s"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		if frame.T == "state" {
			c.state = mergeTestState(c.state, frame.S)
			return c.state
		}
	}
}

// mergeTestState 用与浏览器相同的规则合并服务端增量状态
func mergeTestState(previous, next Snapshot) Snapshot {
	if !next.Delta {
		return next
	}
	players := make(map[string]wirePlayer, len(previous.Players)+len(next.Players))
	for _, player := range previous.Players {
		players[player.ID] = player
	}
	for _, player := range next.Players {
		players[player.ID] = player
	}
	next.Players = make([]wirePlayer, 0, len(players))
	for _, player := range previous.Players {
		next.Players = append(next.Players, players[player.ID])
		delete(players, player.ID)
	}
	for _, player := range next.Players {
		delete(players, player.ID)
	}
	for _, player := range players {
		next.Players = append(next.Players, player)
	}
	if next.Map == "" {
		next.Map = previous.Map
	}
	if next.Bombs == nil {
		next.Bombs = mergeTestBombs(previous.Bombs, next.BombChanges)
	}
	if next.Flames == nil {
		next.Flames = mergeTestFlames(previous.Flames, next.FlameChanges)
	}
	if next.Items == nil {
		next.Items = mergeTestItems(previous.Items, next.ItemChanges)
	}
	if next.Time == 0 {
		next.Time = previous.Time
	}
	if !next.Sudden {
		next.Sudden = previous.Sudden
	}
	return next
}

// testEntityKey 生成测试合并实体使用的坐标键
func testEntityKey(col, row int) string {
	return itoa(col) + ":" + itoa(row)
}

// mergeTestBombs 合并测试客户端收到的炸弹增量
func mergeTestBombs(previous []wireBomb, changes []wireBombChange) []wireBomb {
	byID := make(map[int]wireBomb, len(previous))
	for _, bomb := range previous {
		byID[bomb.ID] = bomb
	}
	for _, change := range changes {
		if change.Remove {
			delete(byID, change.ID)
		} else if change.Bomb != nil {
			byID[change.ID] = *change.Bomb
		}
	}
	result := make([]wireBomb, 0, len(byID))
	for _, bomb := range byID {
		result = append(result, bomb)
	}
	return result
}

// mergeTestFlames 合并测试客户端收到的火焰增量
func mergeTestFlames(previous []wireFlame, changes []wireFlameChange) []wireFlame {
	byCell := make(map[string]wireFlame, len(previous))
	for _, flame := range previous {
		byCell[testEntityKey(flame.Col, flame.Row)] = flame
	}
	for _, change := range changes {
		key := testEntityKey(change.Col, change.Row)
		if change.Remove {
			delete(byCell, key)
		} else if change.Flame != nil {
			byCell[key] = *change.Flame
		}
	}
	result := make([]wireFlame, 0, len(byCell))
	for _, flame := range byCell {
		result = append(result, flame)
	}
	return result
}

// mergeTestItems 合并测试客户端收到的道具增量
func mergeTestItems(previous []wireItem, changes []wireItemChange) []wireItem {
	byCell := make(map[string]wireItem, len(previous))
	for _, item := range previous {
		byCell[testEntityKey(item.Col, item.Row)] = item
	}
	for _, change := range changes {
		key := testEntityKey(change.Col, change.Row)
		if change.Remove {
			delete(byCell, key)
		} else if change.Item != nil {
			byCell[key] = *change.Item
		}
	}
	result := make([]wireItem, 0, len(byCell))
	for _, item := range byCell {
		result = append(result, item)
	}
	return result
}

// waitForAction 读到服务端确认并广播的玩家动作
func (c *testClient) waitForAction(timeout time.Duration) map[string]any {
	c.t.Helper()
	m := c.waitFor("acts", timeout)
	actions, ok := m["a"].([]any)
	if !ok || len(actions) == 0 {
		c.t.Fatalf("动作包为空: %v", m)
	}
	values, ok := actions[0].([]any)
	if !ok || len(values) != 5 {
		c.t.Fatalf("动作格式不正确: %v", m)
	}
	return map[string]any{"seat": values[0], "x": values[1], "y": values[2], "px": values[3], "py": values[4]}
}

// waitRoom 读到满足条件的房间视图为止。房间广播是流水式的，
// 队列里可能还压着上一次的旧视图，所以要按内容筛而不是按类型筛。
func (c *testClient) waitRoom(want func(map[string]any) bool, timeout time.Duration) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			c.t.Fatal("等房间视图超时")
		}
		m := c.next(left)
		if m["t"] != "room" && m["t"] != "joined" {
			continue
		}
		room := roomOf(c.t, m)
		if want(room) {
			return room
		}
	}
}

// roomOf 取出消息里的房间视图
func roomOf(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	room, ok := m["room"].(map[string]any)
	if !ok {
		t.Fatalf("消息里没有 room: %v", m)
	}
	return room
}

// seatsOf 读取房间视图中的座位列表
func seatsOf(t *testing.T, room map[string]any) []any {
	t.Helper()
	seats, ok := room["seats"].([]any)
	if !ok {
		t.Fatalf("房间里没有 seats: %v", room)
	}
	return seats
}

// createRoom 建房并返回房号
func (c *testClient) createRoom(name string) string {
	c.t.Helper()
	c.send(map[string]any{"t": "create", "name": name})
	m := c.waitFor("joined", testMessageTimeout)
	id, _ := roomOf(c.t, m)["id"].(string)
	if len(id) != 4 {
		c.t.Fatalf("房号应该是 4 位，实际 %q", id)
	}
	return id
}

// TestCreateRoomDefaults 检查建房后的房主、默认电脑设置与自动房名
func TestCreateRoomDefaults(t *testing.T) {
	url := startHub(t)
	a := dial(t, url)
	a.hello("阿甲")

	a.send(map[string]any{"t": "create", "name": ""})
	room := roomOf(t, a.waitFor("joined", testMessageTimeout))

	if room["hostId"] != a.id {
		t.Fatalf("房主应该是自己: %v", room)
	}
	if room["bots"] != float64(defaultBots) {
		t.Fatalf("默认电脑数量不对: %v", room)
	}
	botNames, ok := room["botNames"].([]any)
	if !ok || len(botNames) != defaultBots || botNames[0] != "电脑·阿呆" {
		t.Fatalf("电脑名称没有随房间下发: %v", room)
	}
	if room["playing"] != false {
		t.Fatalf("刚建的房间不该在对战中: %v", room)
	}
	if name, _ := room["name"].(string); name != "阿甲 的房间" {
		t.Fatalf("空房名应该自动补全，实际 %q", name)
	}
	seats := seatsOf(t, room)
	if len(seats) != 1 {
		t.Fatalf("房间里应该只有一个人: %v", seats)
	}
	seat := seats[0].(map[string]any)
	if seat["id"] != a.id || seat["ready"] != true || seat["seat"] != float64(0) {
		t.Fatalf("房主座位不对: %v", seat)
	}
}

// TestBriefIncludesBots 检查大厅人数包含真人和电脑
func TestBriefIncludesBots(t *testing.T) {
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	room := &Room{ID: "1234", Seats: []*RoomSeat{{ID: "host"}, {ID: "guest"}}, Bots: 2}
	if brief := h.briefOf(room); brief.Count != 4 {
		t.Fatalf("大厅人数应包含电脑，实际 %d", brief.Count)
	}
}

// TestJoinAndStartMatch 检查加入、准备与开局流程，以及首帧快照内容
func TestJoinAndStartMatch(t *testing.T) {
	url := startHub(t)
	a, b := dial(t, url), dial(t, url)
	realPlayers := len([]*testClient{a, b})
	mapCells := 0
	a.hello("阿甲")
	b.hello("阿乙")
	roomID := a.createRoom("测试房")

	b.send(map[string]any{"t": "join", "roomId": roomID})
	joined := roomOf(t, b.waitFor("joined", testMessageTimeout))
	if len(seatsOf(t, joined)) != 2 {
		t.Fatalf("加入后应该有两个座位: %v", joined)
	}
	// 房主也要收到房间变化
	a.waitRoom(func(r map[string]any) bool { return len(seatsOf(t, r)) == 2 }, testMessageTimeout)

	// 没准备好就开局要被拦下
	a.send(map[string]any{"t": "start"})
	if msg := a.waitFor("error", testMessageTimeout); msg["msg"] == "" {
		t.Fatalf("应该返回未准备好的提示: %v", msg)
	}

	b.send(map[string]any{"t": "ready", "v": true})
	a.waitRoom(func(r map[string]any) bool {
		seats := seatsOf(t, r)
		return len(seats) == 2 && seats[1].(map[string]any)["ready"] == true
	}, testMessageTimeout)
	a.send(map[string]any{"t": "start"})

	for _, c := range []*testClient{a, b} {
		start := c.waitFor("start", testMessageTimeout)
		if start["you"] != c.id {
			t.Fatalf("start 里的 you 应该是自己: %v", start)
		}
		if start["cols"].(float64) <= 0 || start["rows"].(float64) <= 0 {
			t.Fatalf("场地尺寸不对: %v", start)
		}
		mapCells = int(start["cols"].(float64) * start["rows"].(float64))
		if theme, ok := start["theme"].(float64); !ok || theme < 0 || theme >= boardThemes {
			t.Fatalf("棋局主题不合法: %v", start)
		}
		seats, _ := start["seats"].([]any)
		if len(seats) != realPlayers+defaultBots {
			t.Fatalf("开局座位数量不对: %v", seats)
		}
		if defaultBots > 0 {
			last := seats[len(seats)-1].(map[string]any)
			if last["robot"] != true {
				t.Fatalf("最后一位应该是电脑: %v", last)
			}
		}
	}

	s := a.waitForState(testMessageTimeout)
	if len(s.Players) != realPlayers+defaultBots {
		t.Fatalf("快照玩家数量不对，实际 %d", len(s.Players))
	}
	if len(s.Map) != mapCells {
		t.Fatalf("地图串长度不对: %d", len(s.Map))
	}
	if s.Time <= 0 || s.Time > int(math.Ceil(roundMS/1000)) {
		t.Fatalf("剩余时间不合理: %d", s.Time)
	}
}

// TestBotSettingsClamped 检查电脑数量按空位钳制与非房主改动被忽略
func TestBotSettingsClamped(t *testing.T) {
	url := startHub(t)
	a, b := dial(t, url), dial(t, url)
	a.hello("房主")
	b.hello("路人")
	roomID := a.createRoom("设置房")

	wantBots := maxSeats - 1
	a.send(map[string]any{"t": "bots", "n": maxSeats + 1})
	room := a.waitRoom(func(r map[string]any) bool { return r["bots"] == float64(wantBots) }, testMessageTimeout)
	if room["bots"] != float64(wantBots) {
		t.Fatalf("一个真人时应允许 %d 个电脑，实际 %v", wantBots, room["bots"])
	}

	// 腾出位置给真人，再看看加入后电脑数会不会被挤掉
	a.send(map[string]any{"t": "bots", "n": 0})
	a.waitRoom(func(r map[string]any) bool { return r["bots"] == float64(0) }, testMessageTimeout)

	b.send(map[string]any{"t": "join", "roomId": roomID})
	b.waitFor("joined", testMessageTimeout)
	wantBots = max(0, maxSeats-len([]*testClient{a, b}))
	b.send(map[string]any{"t": "bots", "n": maxSeats + 1}) // 非房主，应当被忽略
	a.send(map[string]any{"t": "bots", "n": maxSeats + 1})
	room = a.waitRoom(func(r map[string]any) bool {
		return len(seatsOf(t, r)) == len([]*testClient{a, b}) && r["bots"] == float64(wantBots)
	}, testMessageTimeout)
}

// TestMazeSetting 检查迷踪模式默认关闭且仅房主可在开局前修改
func TestMazeSetting(t *testing.T) {
	url := startHub(t)
	host, guest := dial(t, url), dial(t, url)
	host.hello("房主")
	guest.hello("路人")
	roomID := host.createRoom("迷踪房")
	if room := roomOf(t, host.waitFor("room", testMessageTimeout)); room["maze"] != false {
		t.Fatalf("迷踪模式默认应该关闭: %v", room)
	}
	guest.send(map[string]any{"t": "join", "roomId": roomID})
	guest.waitFor("joined", testMessageTimeout)
	guest.send(map[string]any{"t": "maze", "v": true})
	host.waitRoom(func(r map[string]any) bool { return r["maze"] == false }, testMessageTimeout)
	host.send(map[string]any{"t": "maze", "v": true})
	room := host.waitRoom(func(r map[string]any) bool { return r["maze"] == true }, testMessageTimeout)
	if room["maze"] != true {
		t.Fatalf("房主应该能开启迷踪模式: %v", room)
	}
}

// TestJoinErrors 检查加入不存在的房间会报错，数字房号同样能识别
func TestJoinErrors(t *testing.T) {
	url := startHub(t)
	a := dial(t, url)
	a.hello("阿甲")

	a.send(map[string]any{"t": "join", "roomId": "9999"})
	if msg := a.waitFor("error", 3*time.Second); msg["msg"] != "房间不存在" {
		t.Fatalf("应该提示房间不存在: %v", msg)
	}

	// 房号用数字发也要能认出来
	roomID := a.createRoom("数字房")
	n := 0
	for _, r := range roomID {
		n = n*10 + int(r-'0')
	}
	a.send(map[string]any{"t": "join", "roomId": n})
	if got := roomOf(t, a.waitFor("joined", 3*time.Second))["id"]; got != roomID {
		t.Fatalf("数字房号应该等价，实际 %v", got)
	}
}

// TestLeaveAndPing 检查房主离开后房间解散，心跳与未知消息不影响连接
func TestLeaveAndPing(t *testing.T) {
	url := startHub(t)
	a := dial(t, url)
	a.hello("阿甲")
	a.createRoom("待解散")

	a.send(map[string]any{"t": "leave"})
	a.waitFor("left", 3*time.Second)
	list := a.waitFor("rooms", 3*time.Second)
	if rooms, _ := list["list"].([]any); len(rooms) != 0 {
		t.Fatalf("房主走光后房间应该解散: %v", rooms)
	}

	a.send(map[string]any{"t": "ping"})
	a.waitFor("pong", 3*time.Second)

	// 未知类型直接忽略，连接不能因此断开
	a.send(map[string]any{"t": "什么鬼"})
	a.send(map[string]any{"t": "rooms"})
	a.waitFor("rooms", 3*time.Second)
}

// startSolo 开一局只有自己的对局，方便验证输入回路
func startSolo(t *testing.T, url string) *testClient {
	t.Helper()
	c := dial(t, url)
	c.hello("独狼")
	c.createRoom("单人房")
	c.send(map[string]any{"t": "bots", "n": 0})
	c.waitRoom(func(r map[string]any) bool { return r["bots"] == float64(0) }, 3*time.Second)
	c.send(map[string]any{"t": "start"})
	if got := len(c.waitFor("start", 3*time.Second)["seats"].([]any)); got != 1 {
		t.Fatalf("单人局应该只有一个座位，实际 %d", got)
	}
	return c
}

// TestInputAndBombRoundTrip 检查方向动作即时广播，放弹与爆炸由事件状态同步
func TestInputAndBombRoundTrip(t *testing.T) {
	c := startSolo(t, startHub(t))
	first := c.waitForState(3 * time.Second)
	if len(first.Players) != 1 {
		t.Fatalf("快照里应该只有自己: %+v", first.Players)
	}

	c.send(map[string]any{"t": "input", "x": 1, "y": 0})
	if action := c.waitForAction(3 * time.Second); action["seat"] != float64(0) || action["x"] != float64(1) || action["y"] != float64(0) || action["px"] == nil || action["py"] == nil {
		t.Fatalf("服务端确认的方向不对: %v", action)
	}
	if action := c.waitForAction(3 * time.Second); action["seat"] != float64(0) || action["x"] != float64(1) || action["y"] != float64(0) {
		t.Fatalf("持续移动时应继续同步动作: %v", action)
	}

	c.send(map[string]any{"t": "input", "x": 0, "y": 0})
	if action := c.waitForAction(3 * time.Second); action["seat"] != float64(0) || action["x"] != float64(0) || action["y"] != float64(0) {
		t.Fatalf("服务端确认的停下动作不对: %v", action)
	}
	c.send(map[string]any{"t": "bomb"})
	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("放弹请求没有出现在快照里")
		}
		s := c.waitForState(3 * time.Second)
		if len(s.Bombs) == 0 {
			continue
		}
		b := s.Bombs[0]
		if b.Timer <= 0 || b.Timer > 1 {
			t.Fatalf("炸弹计时应该归一化: %+v", b)
		}
		break
	}

	// 引信烧完应该出现火焰
	deadline = time.Now().Add(time.Duration(bombFuse)*time.Millisecond + testMessageTimeout)
	for {
		if time.Now().After(deadline) {
			t.Fatal("炸弹没有炸开")
		}
		s := c.waitForState(3 * time.Second)
		if len(s.Flames) > 0 {
			break
		}
	}
}

// TestSpectatePlayingRoom 检查观众可进入进行中的房间并收到完整对局状态
func TestSpectatePlayingRoom(t *testing.T) {
	url := startHub(t)
	host := startSolo(t, url)
	host.waitForState(3 * time.Second)
	viewer := dial(t, url)
	viewer.hello("观众")
	host.send(map[string]any{"t": "rooms"})
	rooms := host.waitFor("rooms", 3*time.Second)["list"].([]any)
	if len(rooms) != 1 {
		t.Fatalf("应该有一个对局房间: %v", rooms)
	}
	roomID := rooms[0].(map[string]any)["id"]
	viewer.send(map[string]any{"t": "join", "roomId": roomID})
	viewer.waitFor("joined", 3*time.Second)
	start := viewer.waitFor("start", 3*time.Second)
	if start["spectator"] != true || start["you"] != viewer.id {
		t.Fatalf("观战开局消息不正确: %v", start)
	}
	viewers, ok := start["viewers"].([]any)
	if !ok || len(viewers) != 1 || viewers[0].(map[string]any)["id"] != viewer.id {
		t.Fatalf("观战者应该出现在玩家列表: %v", start)
	}
	s := viewer.waitForState(3 * time.Second)
	if len(s.Map) != Cols*Rows || len(s.Players) != 1 {
		t.Fatalf("观众应该立即收到完整对局状态: %+v", s)
	}
}

// TestHostLeaveEndsEmptyGame 检查房主离开且没有其他真人参战者时结束对局
func TestHostLeaveEndsEmptyGame(t *testing.T) {
	url := startHub(t)
	host := startSolo(t, url)
	host.waitForState(3 * time.Second)
	viewer := dial(t, url)
	viewer.hello("观众")
	host.send(map[string]any{"t": "rooms"})
	roomID := host.waitFor("rooms", 3*time.Second)["list"].([]any)[0].(map[string]any)["id"]
	viewer.send(map[string]any{"t": "join", "roomId": roomID})
	viewer.waitFor("start", 3*time.Second)
	host.send(map[string]any{"t": "leave"})
	viewer.waitFor("left", 3*time.Second)
	viewer.send(map[string]any{"t": "rooms"})
	if rooms := viewer.waitFor("rooms", 3*time.Second)["list"].([]any); len(rooms) != 0 {
		t.Fatalf("没有真人后房间应该结束并移除: %v", rooms)
	}
}

// TestSoloDeathRespawns 检查单人自爆会扣命并自动复活
func TestSoloDeathRespawns(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过需要等引信的对局")
	}
	c := startSolo(t, startHub(t))
	c.waitForState(3 * time.Second)
	c.send(map[string]any{"t": "bomb"}) // 站在原地引爆，自己被炸死

	deadline := time.Now().Add(time.Duration(bombFuse+respawnDelay)*time.Millisecond + testMessageTimeout)
	for time.Now().Before(deadline) {
		s := c.waitForState(3 * time.Second)
		p := s.Players[0]
		if p.Alive == 1 && p.Lives == defaultLives-1 {
			return
		}
	}
	t.Fatal("单人自爆后应该扣一条命并自动复活")
}

// TestOnlyHostControlsBots 检查机器人输入只接受房主浏览器提交
func TestOnlyHostControlsBots(t *testing.T) {
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	host := &Client{id: "host", roomID: "room"}
	guest := &Client{id: "guest", roomID: "room"}
	g := New([]Seat{{ID: "host", Name: "房主"}, {ID: "bot1", Name: "电脑", Robot: true}}, "normal")
	h.rooms["room"] = &Room{HostID: host.id, game: g}
	bot := g.Players[1]

	h.onBot(guest, inbound{ID: bot.ID, X: 1})
	if bot.Input.X != 0 {
		t.Fatal("非房主不应控制机器人")
	}
	h.onBot(host, inbound{ID: bot.ID, X: 1})
	if bot.Input.X != 1 || bot.Input.Y != 0 {
		t.Fatalf("房主机器人方向没有生效: %+v", bot.Input)
	}
	h.onBot(host, inbound{ID: bot.ID, Bomb: true})
	if !bot.WantBomb {
		t.Fatal("房主机器人放弹意图没有生效")
	}
	h.onBot(host, inbound{ID: bot.ID, X: 1, Bomb: true})
	if bot.Input.X != 1 || bot.Input.Y != 0 {
		t.Fatalf("机器人放弹后应该保留撤离方向: %+v", bot.Input)
	}
}

// TestBotBatchInputs 检查房主可在一条消息中提交多个机器人的输入意图
func TestBotBatchInputs(t *testing.T) {
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	host := &Client{id: "host", roomID: "room"}
	g := New([]Seat{{ID: "host", Name: "房主"}, {ID: "bot1", Name: "电脑一", Robot: true}, {ID: "bot2", Name: "电脑二", Robot: true}}, "normal")
	h.rooms["room"] = &Room{HostID: host.id, game: g}
	h.onBot(host, inbound{Bots: []botInput{{ID: "bot1", X: 1}, {ID: "bot2", Y: -1}}})
	if g.Players[1].Input.X != 1 || g.Players[1].Input.Y != 0 {
		t.Fatalf("第一名机器人的批量输入未生效: %+v", g.Players[1].Input)
	}
	if g.Players[2].Input.X != 0 || g.Players[2].Input.Y != -1 {
		t.Fatalf("第二名机器人的批量输入未生效: %+v", g.Players[2].Input)
	}
}

// TestCompactActions 检查多个动作合并为一条紧凑协议帧
func TestCompactActions(t *testing.T) {
	raw, err := json.Marshal(outActions{T: "acts", Actions: []actionView{{Seat: 1, X: 1, Y: 0, PX: 150, PY: 250}, {Seat: 2, X: 0, Y: -1, PX: 350, PY: 450}}})
	if err != nil {
		t.Fatalf("动作包编码失败: %v", err)
	}
	want := "{\"t\":\"acts\",\"a\":[[1,1,0,150,250],[2,0,-1,350,450]]}"
	if string(raw) != want {
		t.Fatalf("动作包格式不紧凑: %s", raw)
	}
}

// TestStateOnlyIncludesFullMapAtCalibration 检查完整地图只在校准帧发送
func TestStateOnlyIncludesFullMapAtCalibration(t *testing.T) {
	g := New([]Seat{{ID: "player", Name: "玩家"}}, "normal")
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	room := &Room{game: g}

	if state := h.stateOf(room, true); state.S.Map == "" {
		t.Fatal("首帧必须带地图")
	}
	if state := h.stateOf(room, false); state.S.Map != "" {
		t.Fatal("非校准帧不应重复发送完整地图")
	}
}

// TestEventStateUsesDelta 检查离散事件使用增量状态，避免重复发送全部玩家
func TestEventStateUsesDelta(t *testing.T) {
	g := New([]Seat{{ID: "one", Name: "甲"}, {ID: "two", Name: "乙"}}, "normal")
	g.items = append(g.items, &Item{Col: floor(g.Players[0].X), Row: floor(g.Players[0].Y), Kind: ItemBomb})
	g.Update(16)
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	state := h.eventStateOf(&Room{game: g})
	if !state.S.Delta || len(state.S.Players) != 1 || state.S.Players[0].ID != "one" {
		t.Fatalf("事件状态应该只发送受影响玩家: %+v", state.S)
	}
	if state.S.Map != "" {
		t.Fatal("事件增量状态不应重复发送完整地图")
	}
}

// TestEventStateCarriesBombs 确保放弹事件立即同步完整炸弹列表供机器人重新规划逃生路线
func TestEventStateCarriesBombs(t *testing.T) {
	g := New([]Seat{{ID: "one", Name: "甲"}}, "normal")
	p := g.Players[0]
	g.RequestBomb(p.ID)
	g.Update(16)
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	state := h.eventStateOf(&Room{game: g})
	if !state.S.Delta || len(state.S.Bombs) != 1 {
		t.Fatalf("放弹事件应带上当前炸弹: %+v", state.S)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(raw, &frame); err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(frame["s"], &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot["ps"]; ok {
		t.Fatalf("未影响玩家的放弹事件不应携带玩家列表: %s", raw)
	}
}

// TestEventStateCarriesEmptyEntities 确保炸弹消失后浏览器能收到明确的空列表
func TestEventStateCarriesEmptyEntities(t *testing.T) {
	g := New([]Seat{{ID: "one", Name: "甲"}}, "normal")
	g.events = append(g.events, Event{K: "boom"})
	h := NewHubWithStats(filepath.Join(t.TempDir(), "players.json"))
	state := h.eventStateOf(&Room{game: g})
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"bs":[]`, `"fs":[]`, `"it":[]`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("事件状态缺少空实体列表 %s: %s", key, raw)
		}
	}
}

// TestMapDue 检查首帧立即校准，之后按五秒间隔校准
func TestMapDue(t *testing.T) {
	now := time.Now()
	room := &Room{}
	if !mapDue(room, now) {
		t.Fatal("首帧应该立即发送")
	}
	room.lastMap = now
	if mapDue(room, now.Add(mapInterval-time.Millisecond)) {
		t.Fatal("同步间隔未到不应发送")
	}
	if !mapDue(room, now.Add(mapInterval)) {
		t.Fatal("同步间隔到达时应该发送")
	}
}

// TestActionDue 检查动作广播按短窗口合并，首帧仍然立即发送
func TestActionDue(t *testing.T) {
	now := time.Now()
	room := &Room{}
	if !actionDue(room, now) {
		t.Fatal("首次动作应该立即发送")
	}
	room.lastActions = now
	if actionDue(room, now.Add(actionInterval-time.Millisecond)) {
		t.Fatal("动作窗口未到时不应重复发送")
	}
	if !actionDue(room, now.Add(actionInterval)) {
		t.Fatal("动作窗口到期后应该发送")
	}
}

// TestChatBroadcast 检查房间成员的消息会被清洗后广播给全部房内玩家
func TestChatBroadcast(t *testing.T) {
	url := startHub(t)
	a, b := dial(t, url), dial(t, url)
	a.hello("阿甲")
	b.hello("阿乙")
	roomID := a.createRoom("聊天房")
	b.send(map[string]any{"t": "join", "roomId": roomID})
	b.waitFor("joined", 3*time.Second)
	a.waitRoom(func(r map[string]any) bool { return len(seatsOf(t, r)) == 2 }, 3*time.Second)

	a.send(map[string]any{"t": "chat", "text": " <你好> \u0001  "})
	for _, c := range []*testClient{a, b} {
		m := c.waitFor("chat", 3*time.Second)
		chat, ok := m["chat"].(map[string]any)
		if !ok || chat["name"] != "阿甲" || chat["text"] != "你好" {
			t.Fatalf("聊天广播或清洗不对: %v", m)
		}
	}
}

// TestStatsCountEachGame 检查每次正式开局按真人数量累计且写入 JSON
func TestStatsCountEachGame(t *testing.T) {
	path := filepath.Join(t.TempDir(), "players.json")
	stats := newPlayerStats(path)
	now := time.Now()
	stats.recordGame(2, now)
	stats.recordGame(2, now)
	if got := stats.view(); got.Total != 4 || got.Today != 4 {
		t.Fatalf("重复开局应重复计数: %+v", got)
	}
	loaded := newPlayerStats(path)
	if got := loaded.view(); got.Total != 4 || got.Today != 4 {
		t.Fatalf("统计应从 JSON 恢复: %+v", got)
	}
}

// TestCleanText 检查昵称清洗会去掉尖括号、控制字符并按长度截断
func TestCleanText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<script>x", "scriptx"},
		{"  阿甲  ", "阿甲"},
		{"a\x01b\x7fc", "abc"},
		{"", ""},
		{"一二三四五六七八九十甲乙", "一二三四五六七八九十"},
	}
	for _, c := range cases {
		if got := clean(c.in, 10); got != c.want {
			t.Fatalf("clean(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestSignAndRound 检查方向压缩、四舍五入与区间钳制的小工具
func TestSignAndRound(t *testing.T) {
	if signOf(2.5) != 1 || signOf(-0.1) != -1 || signOf(0) != 0 {
		t.Fatal("方向压缩不对")
	}
	if roundHalfUp(2.5) != 3 || roundHalfUp(-2.5) != -2 || roundHalfUp(2.4) != 2 {
		t.Fatal("四舍五入应该和 JS 的 Math.round 一致")
	}
	if clampInt(9, 0, 3) != 3 || clampInt(-1, 0, 3) != 0 || clampInt(2, 0, 3) != 2 {
		t.Fatal("区间钳制不对")
	}
}
