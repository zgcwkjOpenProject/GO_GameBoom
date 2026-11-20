// Package game 实现炸弹人的核心规则。服务端权威运算，客户端只负责渲染与输入。
package game

import (
	"math"
	"math/rand"
	"sort"
)

// 小型棋盘尺寸
const (
	Cols = 19
	Rows = 17
)

// 格子类型
const (
	TileEmpty uint8 = 0
	TileHard  uint8 = 1
	TileSoft  uint8 = 2
)

// 道具类型
const (
	ItemBomb   = 1 // 炸弹容量道具
	ItemFire   = 2 // 火力道具
	ItemSpeed  = 3 // 速度道具
	ItemRevive = 4 // 复活道具
)

const (
	bombFuse     = 2400.0   // 引信毫秒
	flameMS      = 450.0    // 火焰存留毫秒
	chainDelay   = 40.0     // 连锁引爆的间隔感
	baseSpeed    = 3.6      // 每秒移动格数
	speedStep    = 0.55     // 每级加速
	maxSpeedTier = 5        // 速度上限档位
	maxFire      = 5        // 火力上限
	maxBombs     = 8        // 同时放弹上限
	defaultBombs = 1        // 初始炸弹容量
	defaultFire  = 1        // 初始火力
	defaultLives = 3        // 初始生命数量
	respawnDelay = 3000.0   // 受伤后复活等待毫秒数
	respawnRetry = 120.0    // 出生点暂时不可用时的重试间隔毫秒数
	playerBox    = 0.6      // 玩家碰撞盒边长
	softRate     = 0.78     // 软砖填充概率
	itemRate     = 0.34     // 软砖掉落道具概率
	roundMS      = 300000.0 // 正常回合时长
	suddenStep   = 260.0    // 急死阶段每块落石间隔
	endDelay     = 1100.0   // 分出胜负后的收尾延迟
)

// Cell 表示一个格子坐标
type Cell struct {
	Col int // 列坐标
	Row int // 行坐标
}

// Spawns 是小型棋盘的四个角落出生点
var Spawns = [4]Cell{
	{Col: 1, Row: 1},
	{Col: Cols - 2, Row: 1},
	{Col: 1, Row: Rows - 2},
	{Col: Cols - 2, Row: Rows - 2},
}

type dir struct {
	dx   int    // 列方向增量
	dy   int    // 行方向增量
	axis string // 火焰绘制方向
}

var dirs = [4]dir{
	{dx: 1, dy: 0, axis: "h"},
	{dx: -1, dy: 0, axis: "h"},
	{dx: 0, dy: 1, axis: "v"},
	{dx: 0, dy: -1, axis: "v"},
}

// boardLayout 按参战人数选择服务端权威的棋盘尺寸与出生点
func boardLayout(playerCount int) (int, int, []Cell) {
	if playerCount <= 4 {
		return Cols, Rows, append([]Cell(nil), Spawns[:]...)
	}
	if playerCount <= 8 {
		return 25, 21, []Cell{{Col: 1, Row: 1}, {Col: 11, Row: 1}, {Col: 23, Row: 1}, {Col: 1, Row: 9}, {Col: 23, Row: 9}, {Col: 1, Row: 19}, {Col: 11, Row: 19}, {Col: 23, Row: 19}}
	}
	return 31, 27, []Cell{{Col: 1, Row: 1}, {Col: 11, Row: 1}, {Col: 19, Row: 1}, {Col: 29, Row: 1}, {Col: 1, Row: 9}, {Col: 11, Row: 9}, {Col: 19, Row: 9}, {Col: 29, Row: 9}, {Col: 1, Row: 17}, {Col: 11, Row: 17}, {Col: 19, Row: 17}, {Col: 29, Row: 17}, {Col: 1, Row: 25}, {Col: 11, Row: 25}, {Col: 19, Row: 25}, {Col: 29, Row: 25}}
}

// spiralCells 生成指定尺寸棋盘的螺旋落石顺序，从外圈向内收缩
func spiralCells(cols, rows int) []Cell {
	cells := make([]Cell, 0, cols*rows)
	top, bottom, left, right := 1, rows-2, 1, cols-2
	for top <= bottom && left <= right {
		for c := left; c <= right; c++ {
			cells = append(cells, Cell{Col: c, Row: top})
		}
		for r := top + 1; r <= bottom; r++ {
			cells = append(cells, Cell{Col: right, Row: r})
		}
		if top < bottom {
			for c := right - 1; c >= left; c-- {
				cells = append(cells, Cell{Col: c, Row: bottom})
			}
		}
		if left < right {
			for r := bottom - 1; r > top; r-- {
				cells = append(cells, Cell{Col: left, Row: r})
			}
		}
		top++
		bottom--
		left++
		right--
	}
	return cells
}

// isSpawnArea 判断格子是否属于出生点周边的保护区
func isSpawnArea(col, row int, spawns []Cell) bool {
	for _, s := range spawns {
		if abs(s.Col-col)+abs(s.Row-row) <= 2 {
			return true
		}
	}
	return false
}

// abs 返回整数的绝对值
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// sign 把浮点数压成 -1、0 或 1
func sign(v float64) int {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}

// floor 取整到格子索引
func floor(v float64) int {
	return int(math.Floor(v))
}

// round2 保留两位小数，和 JS 版的 Math.round(v*100)/100 一致
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// Bomb 是场上一颗炸弹
type Bomb struct {
	ID    int             // 炸弹唯一编号
	Col   int             // 所在列
	Row   int             // 所在行
	Owner string          // 放置者标识
	Power int             // 火焰长度
	Timer float64         // 剩余引信毫秒数
	Dead  bool            // 是否已经引爆或被清除
	Pass  map[string]bool // 放弹时仍在炸弹格内、可以走出的玩家
}

// Flame 是一格火焰
type Flame struct {
	Col   int     // 所在列
	Row   int     // 所在行
	Kind  string  // c 中心 h 横向 v 纵向
	End   bool    // 是否是火舌末端
	Owner string  // 火焰所属炸弹的放置者标识
	Timer float64 // 剩余存留毫秒数
}

// Item 是掉落的道具
type Item struct {
	Col  int // 所在列
	Row  int // 所在行
	Kind int // 道具类型
}

// Input 是玩家当前的方向输入
type Input struct {
	X int // 横向输入，只能为 -1、0 或 1
	Y int // 纵向输入，只能为 -1、0 或 1
}

// Player 是一名参战者，真人和电脑共用
type Player struct {
	ID           string  // 玩家或机器人标识
	Name         string  // 展示名称
	Seat         int     // 本局座位编号
	Spawn        int     // 本局分配的出生角编号
	Robot        bool    // 是否为房主浏览器控制意图的机器人
	X            float64 // 当前中心横坐标
	Y            float64 // 当前中心纵坐标
	Dir          int     // 0 上 1 右 2 下 3 左
	Alive        bool    // 是否仍在场上
	Bombs        int     // 可同时放置的炸弹数量
	Fire         int     // 火焰长度
	SpeedTier    int     // 速度道具等级
	Speed        float64 // 本帧移动速度
	Lives        int     // 剩余生命数量
	Placed       int     // 尚未引爆的本人炸弹数量
	Kills        int     // 击杀数量
	Input        Input   // 服务端已确认的移动方向
	WantBomb     bool    // 下一帧是否尝试放置炸弹
	DeadAt       float64 // 最近阵亡时的对局毫秒数
	RespawnTimer float64 // 复活倒计时毫秒
}

// Event 是一帧内发生的可播放反馈，客户端拿它出音效与粒子
// col/row 不加 omitempty：0 是合法坐标，省掉会让前端算出 NaN
type Event struct {
	K    string `json:"k"`
	Col  int    `json:"col"`
	Row  int    `json:"row"`
	ID   string `json:"id,omitempty"`
	Kind int    `json:"kind,omitempty"`
}

// Seat 是开局时的入座信息
type Seat struct {
	ID    string // 玩家或机器人标识
	Name  string // 展示名称
	Robot bool   // 是否由房主侧机器人逻辑控制
}

// BoardRow 是结算面板的一行
type BoardRow struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Seat  int    `json:"seat"`
	Kills int    `json:"kills"`
	Alive bool   `json:"alive"`
}

// Result 是一局的结算结果
type Result struct {
	WinnerID string     `json:"winnerId"`
	Draw     bool       `json:"draw"`
	Board    []BoardRow `json:"board"`
}

// Game 保存一局的全部状态，只在房间循环所在的 goroutine 里被访问
type Game struct {
	Cols   int      // 当前棋盘列数
	Rows   int      // 当前棋盘行数
	Spawns []Cell   // 当前棋盘可用出生点
	tiles  []uint8  // 按行存储的地图格子
	bombs  []*Bomb  // 尚未清理的炸弹
	flames []*Flame // 当前存留的火焰
	items  []*Item  // 当前地图上的道具

	Players []*Player // 真人和机器人的权威状态

	elapsed     float64    // 本局已推进的毫秒数
	bombSeq     int        // 下一颗炸弹的唯一编号
	sudden      bool       // 是否已进入场地收缩阶段
	suddenIndex int        // 下一块落石在螺旋序列中的位置
	suddenTimer float64    // 距离下一块落石的累计毫秒数
	spiral      []Cell     // 由外向内的落石顺序
	Over        bool       // 收尾时间结束后是否关闭对局
	overTimer   float64    // 结算动画剩余毫秒数
	Result      *Result    // 已判定的结算结果
	mapVersion  int        // 地图变更版本
	events      []Event    // 尚未发送给浏览器的离散事件
	tileChanges []wireTile // 尚未发送给浏览器的地图格变更
}

// New 开一局，为每名玩家随机分配不同的角落出生点
func New(seats []Seat, _ string) *Game {
	cols, rows, spawns := boardLayout(len(seats))
	g := &Game{
		Cols:       cols,
		Rows:       rows,
		Spawns:     spawns,
		tiles:      make([]uint8, cols*rows),
		bombs:      make([]*Bomb, 0, 16),
		flames:     make([]*Flame, 0, 64),
		items:      make([]*Item, 0, 32),
		Players:    make([]*Player, 0, len(seats)),
		bombSeq:    1,
		spiral:     spiralCells(cols, rows),
		mapVersion: 1,
	}
	g.buildMap(len(seats))
	spawnOrder := make([]int, len(seats))
	order := rand.Perm(len(g.Spawns))
	for i := range spawnOrder {
		spawnOrder[i] = order[i]
	}
	for i, seat := range seats {
		g.Players = append(g.Players, g.createPlayer(seat, i, spawnOrder[i]))
	}
	return g
}

// buildMap 生成地图，边界与偶数交点为硬墙，其余随机填软砖
func (g *Game) buildMap(seatCount int) {
	_ = seatCount
	for row := 0; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			i := row*g.Cols + col
			switch {
			case col == 0 || row == 0 || col == g.Cols-1 || row == g.Rows-1:
				g.tiles[i] = TileHard
			case col%2 == 0 && row%2 == 0:
				g.tiles[i] = TileHard
			case isSpawnArea(col, row, g.Spawns):
				g.tiles[i] = TileEmpty
			case rand.Float64() < softRate:
				g.tiles[i] = TileSoft
			default:
				g.tiles[i] = TileEmpty
			}
		}
	}
}

// createPlayer 按随机出生角创建玩家，同时保留房间座位编号
func (g *Game) createPlayer(seat Seat, seatIndex, spawnIndex int) *Player {
	s := g.Spawns[spawnIndex]
	p := &Player{
		ID:    seat.ID,
		Name:  seat.Name,
		Seat:  seatIndex,
		Spawn: spawnIndex,
		Robot: seat.Robot,
		X:     float64(s.Col) + 0.5,
		Y:     float64(s.Row) + 0.5,
		Dir:   2,
		Alive: true,
		Bombs: defaultBombs,
		Fire:  defaultFire,
		Lives: defaultLives,
		Speed: baseSpeed,
	}
	return p
}

// TileAt 读取格子，越界视为硬墙
func (g *Game) TileAt(col, row int) uint8 {
	if col < 0 || row < 0 || col >= g.Cols || row >= g.Rows {
		return TileHard
	}
	return g.tiles[row*g.Cols+col]
}

// setTile 写入格子、递增地图版本并记录要同步的增量
func (g *Game) setTile(col, row int, v uint8) {
	g.tiles[row*g.Cols+col] = v
	g.mapVersion++
	g.tileChanges = append(g.tileChanges, wireTile{Col: col, Row: row, Tile: v})
}

// BombAt 返回该格上的炸弹
func (g *Game) BombAt(col, row int) *Bomb {
	for _, b := range g.bombs {
		if b.Col == col && b.Row == row {
			return b
		}
	}
	return nil
}

// flameAt 返回指定格上的火焰
func (g *Game) flameAt(col, row int) *Flame {
	for _, f := range g.flames {
		if f.Col == col && f.Row == row {
			return f
		}
	}
	return nil
}

// itemIndexAt 返回指定格上道具的切片下标
func (g *Game) itemIndexAt(col, row int) int {
	for i, it := range g.items {
		if it.Col == col && it.Row == row {
			return i
		}
	}
	return -1
}

// playerByID 按玩家标识查找参战者
func (g *Game) playerByID(id string) *Player {
	for _, p := range g.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// boxHitsCell 判断玩家碰撞盒与某格是否重叠
func (g *Game) boxHitsCell(x, y float64, col, row int) bool {
	h := playerBox / 2
	return x-h < float64(col)+1 && x+h > float64(col) &&
		y-h < float64(row)+1 && y+h > float64(row)
}

// blocked 判断该格对该玩家是否不可通行
func (g *Game) blocked(p *Player, col, row int) bool {
	if g.TileAt(col, row) != TileEmpty {
		return true
	}
	if b := g.BombAt(col, row); b != nil && !b.Pass[p.ID] {
		return true
	}
	return false
}

// canStand 判断玩家能否站在给定坐标
func (g *Game) canStand(p *Player, x, y float64) bool {
	h := playerBox / 2
	c0, c1 := floor(x-h), floor(x+h)
	r0, r1 := floor(y-h), floor(y+h)
	for c := c0; c <= c1; c++ {
		for r := r0; r <= r1; r++ {
			if g.blocked(p, c, r) {
				return false
			}
		}
	}
	return true
}

// moveAxis 沿单轴推进，撞墙时二分逼近贴到墙面
func (g *Game) moveAxis(p *Player, dx, dy float64) bool {
	if g.canStand(p, p.X+dx, p.Y+dy) {
		p.X += dx
		p.Y += dy
		return true
	}
	step := dx
	if step == 0 {
		step = dy
	}
	for i := 0; i < 4; i++ {
		step /= 2
		nx, ny := p.X, p.Y
		if dx != 0 {
			nx = p.X + step
		}
		if dy != 0 {
			ny = p.Y + step
		}
		if g.canStand(p, nx, ny) {
			p.X = nx
			p.Y = ny
			return true
		}
	}
	return false
}

// snapAxis 做走廊自动对齐，避免转向时卡在砖角
func (g *Game) snapAxis(p *Player, axis string, amount float64) {
	cur := p.X
	if axis == "y" {
		cur = p.Y
	}
	target := math.Floor(cur) + 0.5
	diff := target - cur
	if math.Abs(diff) < 0.002 {
		return
	}
	move := math.Max(-amount, math.Min(amount, diff))
	nx, ny := p.X, p.Y
	if axis == "x" {
		nx = p.X + move
	} else {
		ny = p.Y + move
	}
	if g.canStand(p, nx, ny) {
		p.X = nx
		p.Y = ny
	}
}

// SetInput 设置玩家方向，斜向输入只保留横向，并返回是否接受
func (g *Game) SetInput(id string, x, y int) bool {
	p := g.playerByID(id)
	if p == nil || !p.Alive {
		return false
	}
	p.Input.X = sign(float64(x))
	p.Input.Y = sign(float64(y))
	if p.Input.X != 0 && p.Input.Y != 0 {
		p.Input.Y = 0
	}
	return true
}

// InputOf 返回服务端当前确认的玩家方向
func (g *Game) InputOf(id string) (Input, bool) {
	p := g.playerByID(id)
	if p == nil {
		return Input{}, false
	}
	return p.Input, true
}

// RequestBomb 记下放弹意图，下一帧结算
func (g *Game) RequestBomb(id string) {
	p := g.playerByID(id)
	if p == nil || !p.Alive {
		return
	}
	p.WantBomb = true
}

// placeBomb 在玩家所在格放置炸弹
func (g *Game) placeBomb(p *Player) {
	if p.Placed >= p.Bombs {
		return
	}
	col, row := floor(p.X), floor(p.Y)
	if g.TileAt(col, row) != TileEmpty {
		return
	}
	if g.BombAt(col, row) != nil {
		return
	}
	pass := make(map[string]bool)
	for _, player := range g.Players {
		if player.Alive && g.boxHitsCell(player.X, player.Y, col, row) {
			pass[player.ID] = true
		}
	}
	p.Placed++
	g.bombs = append(g.bombs, &Bomb{
		ID:    g.bombSeq,
		Col:   col,
		Row:   row,
		Owner: p.ID,
		Power: p.Fire,
		Timer: bombFuse,
		Pass:  pass,
	})
	g.bombSeq++
	g.events = append(g.events, Event{K: "place", Col: col, Row: row})
}

// breakSoft 破坏软砖并按概率掉落道具
func (g *Game) breakSoft(col, row int) {
	g.setTile(col, row, TileEmpty)
	if rand.Float64() >= itemRate {
		return
	}
	roll := rand.Float64()
	kind := ItemSpeed
	if roll < 0.4 {
		kind = ItemBomb
	} else if roll < 0.78 {
		kind = ItemFire
	} else if roll < 0.9 {
		kind = ItemRevive
	}
	g.items = append(g.items, &Item{Col: col, Row: row, Kind: kind})
}

// addFlame 添加火焰或刷新已有火焰的存留时间
func (g *Game) addFlame(col, row int, kind string, end bool, owner string) {
	if exist := g.flameAt(col, row); exist != nil {
		exist.Timer = flameMS
		exist.Owner = owner
		if kind == "c" {
			exist.Kind = "c"
		}
		return
	}
	g.flames = append(g.flames, &Flame{
		Col: col, Row: row, Kind: kind, End: end, Owner: owner, Timer: flameMS,
	})
}

// explode 引爆炸弹，处理连锁与破坏
func (g *Game) explode(b *Bomb) {
	if b.Dead {
		return
	}
	g.removeBomb(b)
	g.addFlame(b.Col, b.Row, "c", false, b.Owner)
	g.events = append(g.events, Event{K: "boom", Col: b.Col, Row: b.Row})
	for _, d := range dirs {
		for i := 1; i <= b.Power; i++ {
			col := b.Col + d.dx*i
			row := b.Row + d.dy*i
			t := g.TileAt(col, row)
			if t == TileHard {
				break
			}
			if t == TileSoft {
				g.breakSoft(col, row)
				g.addFlame(col, row, d.axis, true, b.Owner)
				break
			}
			if idx := g.itemIndexAt(col, row); idx >= 0 {
				g.items = append(g.items[:idx], g.items[idx+1:]...)
			}
			g.addFlame(col, row, d.axis, i == b.Power, b.Owner)
			if next := g.BombAt(col, row); next != nil && !next.Dead {
				next.Timer = math.Min(next.Timer, chainDelay)
			}
		}
	}
}

// removeBomb 清除炸弹并归还所属玩家的放置容量
func (g *Game) removeBomb(b *Bomb) {
	if b == nil || b.Dead {
		return
	}
	b.Dead = true
	if owner := g.playerByID(b.Owner); owner != nil && owner.Placed > 0 {
		owner.Placed--
	}
}

// dropStone 急死阶段落石，把格子压成硬墙
func (g *Game) dropStone() {
	for g.suddenIndex < len(g.spiral) {
		cell := g.spiral[g.suddenIndex]
		g.suddenIndex++
		if g.TileAt(cell.Col, cell.Row) == TileHard {
			continue
		}
		g.setTile(cell.Col, cell.Row, TileHard)
		if idx := g.itemIndexAt(cell.Col, cell.Row); idx >= 0 {
			g.items = append(g.items[:idx], g.items[idx+1:]...)
		}
		if b := g.BombAt(cell.Col, cell.Row); b != nil {
			g.removeBomb(b)
		}
		for _, p := range g.Players {
			if p.Alive && floor(p.X) == cell.Col && floor(p.Y) == cell.Row {
				g.Kill(p, "")
			}
		}
		g.events = append(g.events, Event{K: "stone", Col: cell.Col, Row: cell.Row})
		return
	}
}

// Kill 让玩家阵亡，killerID 为空表示环境击杀
func (g *Game) Kill(p *Player, killerID string) {
	if !p.Alive {
		return
	}
	p.Alive = false
	if g.sudden {
		p.Lives = 0
		p.RespawnTimer = 0
	} else if p.Lives > 0 {
		p.Lives--
		if p.Lives > 0 {
			p.RespawnTimer = respawnDelay
		}
	}
	p.DeadAt = g.elapsed
	p.Input.X = 0
	p.Input.Y = 0
	if killerID != "" && killerID != p.ID {
		if killer := g.playerByID(killerID); killer != nil {
			killer.Kills++
		}
	}
	g.events = append(g.events, Event{K: "die", ID: p.ID})
}

// startSudden 开启场地收缩，并淘汰尚未完成复活的玩家
func (g *Game) startSudden() {
	g.sudden = true
	for _, p := range g.Players {
		if !p.Alive {
			p.Lives = 0
			p.RespawnTimer = 0
		}
	}
}

// respawnPlayer 随机选择没有被障碍、炸弹、火焰或其他玩家占用的出生点
func (g *Game) respawnPlayer(p *Player) {
	for _, index := range rand.Perm(len(g.Spawns)) {
		s := g.Spawns[index]
		if g.TileAt(s.Col, s.Row) != TileEmpty || g.BombAt(s.Col, s.Row) != nil || g.flameAt(s.Col, s.Row) != nil || g.playerAtSpawn(p, s) {
			continue
		}
		p.X = float64(s.Col) + 0.5
		p.Y = float64(s.Row) + 0.5
		p.Alive = true
		p.RespawnTimer = 0
		p.Input = Input{}
		g.events = append(g.events, Event{K: "respawn", ID: p.ID})
		return
	}
	p.RespawnTimer = respawnRetry
}

// playerAtSpawn 判断出生点是否被其他存活玩家的碰撞盒占用
func (g *Game) playerAtSpawn(respawning *Player, spawn Cell) bool {
	for _, other := range g.Players {
		if other != respawning && other.Alive && g.boxHitsCell(other.X, other.Y, spawn.Col, spawn.Row) {
			return true
		}
	}
	return false
}

// Update 推进主循环，dt 为毫秒
func (g *Game) Update(dt float64) {
	sec := dt / 1000
	g.elapsed += dt
	if !g.sudden && g.elapsed > roundMS {
		g.startSudden()
	}
	for _, p := range g.Players {
		if g.sudden || p.Alive || p.RespawnTimer <= 0 {
			continue
		}
		p.RespawnTimer -= dt
		if p.RespawnTimer <= 0 {
			g.respawnPlayer(p)
		}
	}

	if g.sudden && !g.Over {
		g.suddenTimer += dt
		for g.suddenTimer >= suddenStep {
			g.suddenTimer -= suddenStep
			g.dropStone()
		}
	}

	for _, p := range g.Players {
		if !p.Alive {
			continue
		}
		p.Speed = baseSpeed + float64(p.SpeedTier)*speedStep
		step := p.Speed * sec
		if p.Input.X != 0 {
			p.Dir = 3
			if p.Input.X > 0 {
				p.Dir = 1
			}
			g.moveAxis(p, float64(p.Input.X)*step, 0)
			g.snapAxis(p, "y", step)
		} else if p.Input.Y != 0 {
			p.Dir = 0
			if p.Input.Y > 0 {
				p.Dir = 2
			}
			g.moveAxis(p, 0, float64(p.Input.Y)*step)
			g.snapAxis(p, "x", step)
		}
		if p.WantBomb {
			p.WantBomb = false
			g.placeBomb(p)
		}
		g.pickItem(p)
	}

	g.stepBombs(dt)
	g.stepFlames(dt)
	g.checkEnd(dt)
}

// pickItem 玩家踩到道具就吃掉
func (g *Game) pickItem(p *Player) {
	idx := g.itemIndexAt(floor(p.X), floor(p.Y))
	if idx < 0 {
		return
	}
	it := g.items[idx]
	g.items = append(g.items[:idx], g.items[idx+1:]...)
	switch it.Kind {
	case ItemBomb:
		p.Bombs = min(maxBombs, p.Bombs+1)
	case ItemFire:
		p.Fire = min(maxFire, p.Fire+1)
	case ItemRevive:
		p.Lives++
	default:
		p.SpeedTier = min(maxSpeedTier, p.SpeedTier+1)
	}
	g.events = append(g.events, Event{K: "item", ID: p.ID, Kind: it.Kind})
}

// stepBombs 走引信、清理站位豁免，并让连锁在同一帧内炸完
func (g *Game) stepBombs(dt float64) {
	for _, b := range g.bombs {
		if b.Dead {
			continue
		}
		b.Timer -= dt
		for id := range b.Pass {
			p := g.playerByID(id)
			if p == nil || !g.boxHitsCell(p.X, p.Y, b.Col, b.Row) {
				delete(b.Pass, id)
			}
		}
	}
	for guard := 0; guard < 64; guard++ {
		again := false
		for _, b := range g.bombs {
			if !b.Dead && b.Timer <= 0 {
				g.explode(b)
				again = true
			}
		}
		if !again {
			break
		}
	}
	alive := g.bombs[:0]
	for _, b := range g.bombs {
		if !b.Dead {
			alive = append(alive, b)
		}
	}
	g.bombs = alive
}

// stepFlames 火焰倒计时并结算踩火的玩家
func (g *Game) stepFlames(dt float64) {
	for _, f := range g.flames {
		f.Timer -= dt
	}
	live := g.flames[:0]
	for _, f := range g.flames {
		if f.Timer > 0 {
			live = append(live, f)
		}
	}
	g.flames = live

	for _, p := range g.Players {
		if !p.Alive {
			continue
		}
		if f := g.flameAt(floor(p.X), floor(p.Y)); f != nil {
			g.Kill(p, f.Owner)
		}
	}
}

// checkEnd 判定胜负，留一点收尾时间播完爆炸
func (g *Game) checkEnd(dt float64) {
	if g.Result != nil {
		g.overTimer -= dt
		if g.overTimer <= 0 {
			g.Over = true
		}
		return
	}
	alive := make([]*Player, 0, len(g.Players))
	for _, p := range g.Players {
		if p.Alive {
			alive = append(alive, p)
		}
	}
	if len(g.Players) == 1 {
		if g.Players[0].Alive || g.Players[0].Lives > 0 {
			return
		}
	} else {
		contenders := 0
		for _, p := range g.Players {
			if p.Alive || p.Lives > 0 {
				contenders++
			}
		}
		if contenders > 1 || (contenders == 1 && len(alive) == 0) {
			return
		}
	}
	if len(alive) > 1 {
		return
	}
	winner := ""
	if len(alive) == 1 {
		winner = alive[0].ID
	}
	g.Result = &Result{WinnerID: winner, Draw: len(alive) != 1, Board: g.board()}
	g.overTimer = endDelay
}

// board 生成结算排名：先看是否存活，再看击杀数
func (g *Game) board() []BoardRow {
	rows := make([]BoardRow, 0, len(g.Players))
	for _, p := range g.Players {
		rows = append(rows, BoardRow{
			ID: p.ID, Name: p.Name, Seat: p.Seat, Kills: p.Kills, Alive: p.Alive,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ai, aj := 0, 0
		if rows[i].Alive {
			ai = 1
		}
		if rows[j].Alive {
			aj = 1
		}
		if ai != aj {
			return ai > aj
		}
		return rows[i].Kills > rows[j].Kills
	})
	return rows
}

// mapString 把地图压成一串数字字符，客户端逐字符还原
func (g *Game) mapString() string {
	buf := make([]byte, len(g.tiles))
	for i, t := range g.tiles {
		buf[i] = '0' + t
	}
	return string(buf)
}

// 下面这些是发给客户端的紧凑结构，字段名要和前端解析保持一致

type wirePlayer struct {
	ID      string  `json:"id"` // 玩家或机器人标识
	X       float64 `json:"x"`  // 中心横坐标
	Y       float64 `json:"y"`  // 中心纵坐标
	Dir     int     `json:"d"`  // 朝向
	Alive   int     `json:"a"`  // 是否存活
	Moving  int     `json:"mv"` // 是否正在移动
	Bombs   int     `json:"b"`  // 炸弹容量
	Fire    int     `json:"f"`  // 火力
	Speed   int     `json:"s"`  // 速度等级
	Lives   int     `json:"l"`  // 剩余生命
	Kills   int     `json:"k"`  // 击杀数量
	Respawn int     `json:"r"`  // 剩余复活等待毫秒
}

type wireBomb struct {
	ID    int      `json:"i"`            // 炸弹唯一编号
	Col   int      `json:"c"`            // 所在列
	Row   int      `json:"r"`            // 所在行
	Timer float64  `json:"t"`            // 剩余引信的归一化比例
	Power int      `json:"p"`            // 火焰长度
	Owner string   `json:"o"`            // 放置者标识
	Pass  []string `json:"ps,omitempty"` // 放弹时仍在炸弹格内、可以离开的玩家
}

type wireFlame struct {
	Col   int     `json:"c"` // 所在列
	Row   int     `json:"r"` // 所在行
	Kind  string  `json:"k"` // 绘制方向
	End   int     `json:"e"` // 是否是火舌末端
	Timer float64 `json:"t"` // 剩余存留的归一化比例
}

type wireItem struct {
	Col  int `json:"c"` // 所在列
	Row  int `json:"r"` // 所在行
	Kind int `json:"k"` // 道具类型
}

// wireTile 是一块已变更地图格子的紧凑同步格式
type wireTile struct {
	Col  int   `json:"c"`
	Row  int   `json:"r"`
	Tile uint8 `json:"v"`
}

// Snapshot 是打包给客户端的一帧状态
type Snapshot struct {
	Delta        bool              `json:"d,omitempty"`      // 是否为合并到现有状态的增量
	Map          string            `json:"map,omitempty"`    // 按行编码的完整地图
	MapVersion   int               `json:"mv,omitempty"`     // 当前地图版本
	Time         int               `json:"time,omitempty"`   // 剩余秒数
	Sudden       bool              `json:"sudden,omitempty"` // 是否进入场地收缩
	Players      []wirePlayer      `json:"ps,omitempty"`     // 完整或受影响的玩家
	Bombs        []wireBomb        `json:"bs"`               // 当前全部炸弹，空数组也要发送以清除浏览器旧状态
	Flames       []wireFlame       `json:"fs"`               // 当前全部火焰，空数组也要发送以清除浏览器旧状态
	Items        []wireItem        `json:"it"`               // 当前全部道具，空数组也要发送以清除浏览器旧状态
	BombChanges  []wireBombChange  `json:"bd,omitempty"`     // 炸弹增删改
	FlameChanges []wireFlameChange `json:"fd,omitempty"`     // 火焰增删改
	ItemChanges  []wireItemChange  `json:"id,omitempty"`     // 道具增删改
	Events       []Event           `json:"ev,omitempty"`     // 本次状态附带的离散事件
	Tiles        []wireTile        `json:"tl,omitempty"`
}

// wireBombChange 是一颗炸弹的增删改记录
type wireBombChange struct {
	ID     int       `json:"i"`           // 炸弹唯一编号
	Bomb   *wireBomb `json:"b,omitempty"` // 新增或更新后的炸弹
	Remove bool      `json:"x,omitempty"` // 是否删除
}

// wireFlameChange 是一格火焰的增删改记录
type wireFlameChange struct {
	Col    int        `json:"c"`           // 所在列
	Row    int        `json:"r"`           // 所在行
	Flame  *wireFlame `json:"f,omitempty"` // 新增或更新后的火焰
	Remove bool       `json:"x,omitempty"` // 是否删除
}

// wireItemChange 是一件道具的增删改记录
type wireItemChange struct {
	Col    int       `json:"c"`            // 所在列
	Row    int       `json:"r"`            // 所在行
	Item   *wireItem `json:"it,omitempty"` // 新增或更新后的道具
	Remove bool      `json:"x,omitempty"`  // 是否删除
}

// Serialize 打包一帧状态，事件队列读完即清空
func (g *Game) Serialize() Snapshot {
	return g.serialize(nil, false)
}

// SerializeEvents 打包离散事件的增量状态，只携带受影响玩家
func (g *Game) SerializeEvents() Snapshot {
	ids := make(map[string]bool)
	for _, event := range g.events {
		if event.ID != "" {
			ids[event.ID] = true
		}
	}
	return g.serialize(ids, true)
}

// serialize 打包完整或增量状态，事件队列读完即清空
func (g *Game) serialize(playerIDs map[string]bool, delta bool) Snapshot {
	events := g.events
	if events == nil {
		events = []Event{}
	}
	g.events = nil
	tiles := g.tileChanges
	g.tileChanges = nil

	s := Snapshot{
		Delta:      delta,
		Map:        g.mapString(),
		MapVersion: g.mapVersion,
		Time:       max(0, int(math.Ceil((roundMS-g.elapsed)/1000))),
		Sudden:     g.sudden,
		Players:    make([]wirePlayer, 0, len(g.Players)),
		Bombs:      make([]wireBomb, 0, len(g.bombs)),
		Flames:     make([]wireFlame, 0, len(g.flames)),
		Items:      make([]wireItem, 0, len(g.items)),
		Events:     events,
		Tiles:      tiles,
	}
	for _, p := range g.Players {
		if playerIDs != nil && !playerIDs[p.ID] {
			continue
		}
		s.Players = append(s.Players, wirePlayer{
			ID:      p.ID,
			X:       round2(p.X),
			Y:       round2(p.Y),
			Dir:     p.Dir,
			Alive:   boolInt(p.Alive),
			Moving:  boolInt(p.Input.X != 0 || p.Input.Y != 0),
			Bombs:   p.Bombs,
			Fire:    p.Fire,
			Speed:   p.SpeedTier,
			Lives:   p.Lives,
			Kills:   p.Kills,
			Respawn: max(0, int(math.Ceil(p.RespawnTimer))),
		})
	}
	for _, b := range g.bombs {
		pass := make([]string, 0, len(b.Pass))
		for _, p := range g.Players {
			if b.Pass[p.ID] {
				pass = append(pass, p.ID)
			}
		}
		s.Bombs = append(s.Bombs, wireBomb{
			ID: b.ID, Col: b.Col, Row: b.Row, Timer: math.Max(0, round2(b.Timer/bombFuse)), Power: b.Power, Owner: b.Owner, Pass: pass,
		})
	}
	for _, f := range g.flames {
		s.Flames = append(s.Flames, wireFlame{
			Col: f.Col, Row: f.Row, Kind: f.Kind, End: boolInt(f.End), Timer: round2(f.Timer / flameMS),
		})
	}
	for _, it := range g.items {
		s.Items = append(s.Items, wireItem{Col: it.Col, Row: it.Row, Kind: it.Kind})
	}
	return s
}

// HasEvents 返回本帧是否有必须立即同步给浏览器的离散事件
func (g *Game) HasEvents() bool {
	return len(g.events) > 0
}

// boolInt 将布尔值压缩为协议中的 0 或 1
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
