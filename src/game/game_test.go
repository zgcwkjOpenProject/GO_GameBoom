// 核心规则的单元测试，覆盖地图、移动、炸弹、火焰、道具与结算
package game

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

const testStep = 16.0 // 测试推进步长

// newTestGame 开一局真人对局，方便各测试自己布置场地
func newTestGame(t *testing.T, players int) *Game {
	t.Helper()
	seats := make([]Seat, 0, players)
	for i := 0; i < players; i++ {
		n := strconv.Itoa(i + 1)
		seats = append(seats, Seat{ID: "p" + n, Name: "玩家" + n})
	}
	g := New(seats, "normal")
	for i, p := range g.Players {
		p.Spawn = i % len(g.Spawns)
		p.X = float64(g.Spawns[p.Spawn].Col) + 0.5
		p.Y = float64(g.Spawns[p.Spawn].Row) + 0.5
	}
	return g
}

// clearInterior 把随机软砖清空，只留固定硬墙，测试就不受随机地图影响
func clearInterior(g *Game) {
	for row := 1; row < g.Rows-1; row++ {
		for col := 1; col < g.Cols-1; col++ {
			if col%2 == 0 && row%2 == 0 {
				continue
			}
			g.tiles[row*g.Cols+col] = TileEmpty
		}
	}
	g.items = g.items[:0]
}

// advance 按指定步长推进指定毫秒数，最后一帧不超过目标时间
func advance(g *Game, total, step float64) {
	for elapsed := 0.0; elapsed < total; {
		dt := min(step, total-elapsed)
		g.Update(dt)
		elapsed += dt
	}
}

// step 推进一步规则循环
func step(g *Game) {
	g.Update(testStep)
}

// TestNewMapInvariants 检查新开地图的硬墙布局、越界处理与出生点初始状态
func TestNewMapInvariants(t *testing.T) {
	g := New([]Seat{{ID: "p1", Name: "玩家1"}, {ID: "p2", Name: "玩家2"}, {ID: "p3", Name: "玩家3"}, {ID: "p4", Name: "玩家4"}}, "normal")
	for row := 0; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			tile := g.TileAt(col, row)
			edge := col == 0 || row == 0 || col == g.Cols-1 || row == g.Rows-1
			if edge || (col%2 == 0 && row%2 == 0) {
				if tile != TileHard {
					t.Fatalf("(%d,%d) 应该是硬墙，实际 %d", col, row, tile)
				}
				continue
			}
			if tile != TileEmpty && tile != TileSoft {
				t.Fatalf("(%d,%d) 出现了意外的格子类型 %d", col, row, tile)
			}
		}
	}
	if g.TileAt(-1, 5) != TileHard {
		t.Fatal("越界应该当成硬墙")
	}
	usedSpawns := make(map[int]bool, len(g.Players))
	for i, p := range g.Players {
		if p.Seat != i {
			t.Fatalf("%s 的座位编号不对，实际 %d", p.ID, p.Seat)
		}
		if p.Spawn < 0 || p.Spawn >= len(g.Spawns) || usedSpawns[p.Spawn] {
			t.Fatalf("%s 的随机出生角不合法: %d", p.ID, p.Spawn)
		}
		usedSpawns[p.Spawn] = true
		s := g.Spawns[p.Spawn]
		if p.X != float64(s.Col)+0.5 || p.Y != float64(s.Row)+0.5 {
			t.Fatalf("%s 没有出生在分配的角落", p.ID)
		}
		if !p.Alive || p.Bombs != defaultBombs || p.Fire != defaultFire || p.Lives != defaultLives || p.SpeedTier != 0 {
			t.Fatalf("%s 初始属性不对: %+v", p.ID, p)
		}
		// 出生点周围要留出活动空间
		if g.TileAt(s.Col+1, s.Row) == TileSoft && g.TileAt(s.Col, s.Row+1) == TileSoft {
			t.Fatalf("第 %d 个出生点被软砖堵住了", i)
		}
	}
}

// TestDemoSnapshot 检查首页演示会带上与地图匹配的棋盘尺寸
func TestDemoSnapshot(t *testing.T) {
	snapshot := DemoSnapshot()
	if snapshot.Cols <= 0 || snapshot.Rows <= 0 || len(snapshot.Map) != snapshot.Cols*snapshot.Rows {
		t.Fatalf("首页演示棋盘数据不匹配: %dx%d，地图 %d", snapshot.Cols, snapshot.Rows, len(snapshot.Map))
	}
}

// TestBoardLayout 检查服务端会按参战人数选择棋盘和足够的随机出生点
func TestBoardLayout(t *testing.T) {
	cases := []int{
		1,
		len(Spawns),
		len(Spawns) + 1,
	}
	for _, players := range cases {
		g := newTestGame(t, players)
		if g.Cols <= 0 || g.Rows <= 0 || len(g.Spawns) < players {
			t.Fatalf("%d 人棋盘不正确: %dx%d，出生点 %d", players, g.Cols, g.Rows, len(g.Spawns))
		}
		used := make(map[int]bool, len(g.Players))
		for _, p := range g.Players {
			if used[p.Spawn] {
				t.Fatalf("%d 人棋盘出现重复出生点: %d", players, p.Spawn)
			}
			used[p.Spawn] = true
		}
	}
}

// TestSpiralCoversInterior 检查落石螺旋不重不漏地覆盖内场
func TestSpiralCoversInterior(t *testing.T) {
	cells := spiralCells(Cols, Rows)
	want := (Cols - 2) * (Rows - 2)
	if len(cells) != want {
		t.Fatalf("螺旋序列长度 %d，期望 %d", len(cells), want)
	}
	seen := make(map[Cell]bool, len(cells))
	for _, c := range cells {
		if c.Col < 1 || c.Row < 1 || c.Col > Cols-2 || c.Row > Rows-2 {
			t.Fatalf("螺旋走到了边界外 %+v", c)
		}
		if seen[c] {
			t.Fatalf("螺旋重复经过 %+v", c)
		}
		seen[c] = true
	}
	if cells[0] != (Cell{Col: 1, Row: 1}) {
		t.Fatalf("落石应该从左上角开始，实际 %+v", cells[0])
	}
}

// TestPlaceBombRules 检查放弹的同格限制、携带上限与已放置计数
func TestPlaceBombRules(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]

	g.RequestBomb(p.ID)
	step(g)
	if g.BombAt(1, 1) == nil {
		t.Fatal("放弹请求没有生效")
	}
	if p.Placed != 1 {
		t.Fatalf("已放置数应该是 1，实际 %d", p.Placed)
	}

	// 同一格不能叠第二颗，也不能超过携带上限
	g.RequestBomb(p.ID)
	step(g)
	if len(g.bombs) != 1 {
		t.Fatalf("同格重复放弹应该被拒绝，现在有 %d 颗", len(g.bombs))
	}

	p.Bombs = 2
	p.X, p.Y = 2.5, 1.5
	g.RequestBomb(p.ID)
	step(g)
	if g.BombAt(2, 1) == nil {
		t.Fatal("提升上限后应该能放第二颗")
	}
	if p.Placed != 2 {
		t.Fatalf("已放置数应该是 2，实际 %d", p.Placed)
	}
}

// TestBombOwnerCanWalkOff 检查主人能走出自己的炸弹格，离开后不能再穿回去
func TestBombOwnerCanWalkOff(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	g.RequestBomb(p.ID)
	g.Update(16)
	b := g.BombAt(1, 1)
	if b == nil || !b.Pass[p.ID] {
		t.Fatal("刚放下的炸弹应该对主人放行")
	}

	// 往右走出这一格，豁免随即失效，回头就撞得住
	g.SetInput(p.ID, 1, 0)
	advance(g, 400, testStep)
	if floor(p.X) == 1 {
		t.Fatalf("主人应该已经离开炸弹格，x=%.2f", p.X)
	}
	if b.Pass[p.ID] {
		t.Fatal("离开后不应该再保留通行豁免")
	}
	g.SetInput(p.ID, -1, 0)
	advance(g, 600, testStep)
	if floor(p.X) == 1 {
		t.Fatalf("炸弹应该挡住回头路，x=%.2f", p.X)
	}
}

// TestPlayersOnBombCanWalkOff 检查同格的其他玩家也能从刚放下的炸弹中走出
func TestPlayersOnBombCanWalkOff(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p1, p2 := g.Players[0], g.Players[1]
	p1.X, p1.Y = 5.5, 5.5
	p2.X, p2.Y = 5.5, 5.5

	g.RequestBomb(p1.ID)
	step(g)
	b := g.BombAt(5, 5)
	if b == nil || !b.Pass[p1.ID] || !b.Pass[p2.ID] {
		t.Fatalf("放弹格中的玩家都应可以离开: %+v", b)
	}

	g.SetInput(p2.ID, 1, 0)
	advance(g, 400, testStep)
	if floor(p2.X) == 5 {
		t.Fatalf("同格玩家应该能离开炸弹，x=%.2f", p2.X)
	}
	if b.Pass[p2.ID] {
		t.Fatal("离开后不应该继续保留通行豁免")
	}
}

// TestExplosionShape 检查十字火焰的长度、末端标记与硬墙阻挡
func TestExplosionShape(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	p.Fire = 3
	g.RequestBomb(p.ID)
	step(g)
	advance(g, bombFuse+testStep, testStep)

	if g.BombAt(1, 1) != nil {
		t.Fatal("引信烧完后炸弹应该消失")
	}
	center := g.flameAt(1, 1)
	if center == nil || center.Kind != "c" {
		t.Fatalf("中心火焰不对: %+v", center)
	}
	// 左上两侧紧贴边界硬墙，火焰不能穿出去
	if g.flameAt(0, 1) != nil || g.flameAt(1, 0) != nil {
		t.Fatal("火焰穿过了边界硬墙")
	}
	for i := 1; i <= 3; i++ {
		right := g.flameAt(1+i, 1)
		if right == nil || right.Kind != "h" {
			t.Fatalf("(%d,1) 应该有横向火焰: %+v", 1+i, right)
		}
		if right.End != (i == 3) {
			t.Fatalf("(%d,1) 末端标记不对: %+v", 1+i, right)
		}
		down := g.flameAt(1, 1+i)
		if down == nil || down.Kind != "v" {
			t.Fatalf("(1,%d) 应该有纵向火焰: %+v", 1+i, down)
		}
	}
	if len(g.flames) != 7 {
		t.Fatalf("火力 3 在空旷角落应该产生 7 格火焰，实际 %d", len(g.flames))
	}
	if g.flameAt(1, 4+1) != nil {
		t.Fatal("火焰超出了火力范围")
	}
}

// TestFlameStopsAtSoftBrick 检查软砖被炸开后火焰停在该格并触发地图版本自增
func TestFlameStopsAtSoftBrick(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	g.tiles[1*g.Cols+3] = TileSoft
	p := g.Players[0]
	p.Fire = 4
	before := g.mapVersion
	g.RequestBomb(p.ID)
	step(g)
	advance(g, bombFuse+testStep, testStep)

	if g.TileAt(3, 1) != TileEmpty {
		t.Fatal("软砖应该被炸开")
	}
	if g.mapVersion == before {
		t.Fatal("地图变动后 mapVersion 应该自增")
	}
	if g.flameAt(3, 1) == nil {
		t.Fatal("被炸开的软砖格上应该留下火舌")
	}
	if g.flameAt(4, 1) != nil {
		t.Fatal("火焰应该被软砖挡住，不能继续向右")
	}
}

// TestChainDetonation 检查连锁引爆的间隔、火力延伸与炸完后的列表清理
func TestChainDetonation(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	p.X, p.Y = 11.5, 11.5 // 挪开，别被自己的连锁波及

	first := &Bomb{ID: 1, Col: 1, Row: 1, Owner: p.ID, Power: 1, Timer: 100, Pass: map[string]bool{}}
	second := &Bomb{ID: 2, Col: 1, Row: 2, Owner: p.ID, Power: 2, Timer: bombFuse, Pass: map[string]bool{}}
	g.bombs = append(g.bombs, first, second)

	advance(g, first.Timer+testStep, testStep)
	if !first.Dead {
		t.Fatal("第一颗炸弹应该已经引爆")
	}
	if second.Dead {
		t.Fatal("第二颗不该在同一帧就炸完，要留出连锁间隔")
	}
	if second.Timer > chainDelay {
		t.Fatalf("连锁应该把引信压到 %.0fms 以内，实际 %.1f", chainDelay, second.Timer)
	}

	advance(g, chainDelay+testStep, testStep)
	if !second.Dead {
		t.Fatal("连锁没有把第二颗点掉")
	}
	if g.flameAt(1, 4) == nil {
		t.Fatal("第二颗的火力应该延伸到 (1,4)")
	}
	if len(g.bombs) != 0 {
		t.Fatalf("炸完的弹应该被清出列表，实际还剩 %d", len(g.bombs))
	}
	if !p.Alive {
		t.Fatal("远处的玩家不该被牵连")
	}
}

// TestFlameKillCreditsOwner 检查击杀只记别人、同时阵亡判平局
func TestFlameKillCreditsOwner(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p1, p2 := g.Players[0], g.Players[1]
	p1.Lives, p2.Lives = 0, 0
	p2.X, p2.Y = 2.5, 1.5 // 站进火力范围
	p1.Fire = 2

	g.RequestBomb(p1.ID)
	step(g)
	advance(g, bombFuse+testStep, testStep)

	if p2.Alive {
		t.Fatal("被火焰覆盖的玩家应该阵亡")
	}
	if p1.Alive {
		t.Fatal("放弹者站在中心格，也应该被自己的火焰带走")
	}
	if p1.Kills != 1 {
		t.Fatalf("击杀数应该只算别人，实际 %d", p1.Kills)
	}
	if p2.Kills != 0 {
		t.Fatalf("被炸死的一方不该有击杀，实际 %d", p2.Kills)
	}
	if g.Result == nil || !g.Result.Draw || g.Result.WinnerID != "" {
		t.Fatalf("同时阵亡应该判平局: %+v", g.Result)
	}
}

// TestLastAliveWins 检查只剩一人时的胜负判定、排名与收尾延迟
func TestLastAliveWins(t *testing.T) {
	g := newTestGame(t, 3)
	clearInterior(g)
	p1, p2, p3 := g.Players[0], g.Players[1], g.Players[2]
	p1.Lives, p2.Lives, p3.Lives = 0, 0, 0

	g.Kill(p2, p1.ID)
	step(g)
	if g.Result != nil {
		t.Fatal("还剩两个人，不该出结果")
	}
	g.Kill(p3, p1.ID)
	step(g)

	if g.Result == nil {
		t.Fatal("只剩一个人时应该结算")
	}
	if g.Result.WinnerID != p1.ID || g.Result.Draw {
		t.Fatalf("胜者判定不对: %+v", g.Result)
	}
	if p1.Kills != 2 {
		t.Fatalf("击杀数应该是 2，实际 %d", p1.Kills)
	}
	if len(g.Result.Board) != 3 || g.Result.Board[0].ID != p1.ID {
		t.Fatalf("结算面板应该把存活者排在最前: %+v", g.Result.Board)
	}
	if g.Over {
		t.Fatal("收尾延迟还没走完，不该立刻结束")
	}
	advance(g, endDelay+testStep, testStep)
	if !g.Over {
		t.Fatal("收尾延迟结束后应该标记 Over")
	}
}

// TestSoloEndsOnlyWhenDead 检查单人练习只在自己阵亡后收场
func TestSoloEndsOnlyWhenDead(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	step(g)
	if g.Result != nil {
		t.Fatal("单人练习时活着就不该结算")
	}
	g.Players[0].Lives = 0
	g.Kill(g.Players[0], "")
	step(g)
	if g.Result == nil || !g.Result.Draw || g.Result.WinnerID != "" {
		t.Fatalf("单人阵亡应该算平局收场: %+v", g.Result)
	}
}

// TestSuddenDeathCancelsRespawn 检查收缩开始后不会再复活，且全部淘汰会结算
func TestSuddenDeathCancelsRespawn(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p1, p2 := g.Players[0], g.Players[1]
	g.Kill(p1, "")
	g.Kill(p2, "")
	if p1.Lives != defaultLives-1 || p1.RespawnTimer <= 0 {
		t.Fatal("普通阶段死亡后应保留生命并等待复活")
	}
	g.elapsed = roundMS
	step(g)
	if !g.sudden || p1.Lives != 0 || p1.RespawnTimer != 0 {
		t.Fatalf("场地收缩应取消待复活玩家: sudden=%v lives=%d timer=%.0f", g.sudden, p1.Lives, p1.RespawnTimer)
	}
	if p2.Lives != 0 || p2.RespawnTimer != 0 {
		t.Fatalf("场地收缩应取消全部待复活玩家: lives=%d timer=%.0f", p2.Lives, p2.RespawnTimer)
	}
	if g.Result == nil || !g.Result.Draw {
		t.Fatalf("场地收缩后全员淘汰应判平局: %+v", g.Result)
	}
}

// TestSuddenDeathDropsStones 检查急死阶段的落石节奏、环境击杀与快照标记
func TestSuddenDeathDropsStones(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p1, p2 := g.Players[0], g.Players[1]
	p1.Lives, p2.Lives = 0, 0
	g.elapsed = roundMS - testStep/2

	step(g)
	if !g.sudden {
		t.Fatal("超过回合时长后应该进入急死阶段")
	}
	if g.suddenIndex != 0 {
		t.Fatal("刚进急死阶段还不该落石")
	}

	advance(g, suddenStep, testStep)
	if g.TileAt(1, 1) != TileHard {
		t.Fatal("第一块石头应该落在螺旋起点 (1,1)")
	}
	if p1.Alive {
		t.Fatal("站在落点上的玩家应该被压死")
	}
	if p1.Kills != 0 || p2.Kills != 0 {
		t.Fatal("落石属于环境击杀，不该记在任何人头上")
	}
	if g.Result == nil || g.Result.WinnerID != p2.ID {
		t.Fatalf("剩下的玩家应该获胜: %+v", g.Result)
	}
	if s := g.Serialize(); !s.Sudden || s.Time != 0 {
		t.Fatalf("急死阶段的快照不对: sudden=%v time=%d", s.Sudden, s.Time)
	}
}

// TestDropStoneClearsCell 检查落石压掉道具与炸弹，并按螺旋顺序推进
func TestDropStoneClearsCell(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	p.X, p.Y = 11.5, 11.5
	g.items = append(g.items, &Item{Col: 1, Row: 1, Kind: ItemFire})
	p.Placed = 1
	b := &Bomb{ID: 9, Col: 1, Row: 1, Owner: p.ID, Power: 1, Timer: bombFuse, Pass: map[string]bool{}}
	g.bombs = append(g.bombs, b)

	g.dropStone()
	if g.TileAt(1, 1) != TileHard {
		t.Fatal("落点应该变成硬墙")
	}
	if g.itemIndexAt(1, 1) >= 0 {
		t.Fatal("落石应该压掉道具")
	}
	if !b.Dead {
		t.Fatal("落石应该让这颗炸弹作废")
	}
	if p.Placed != 0 {
		t.Fatalf("落石清除炸弹后应该归还放置容量，实际 %d", p.Placed)
	}
	p.X, p.Y = 3.5, 3.5
	g.RequestBomb(p.ID)
	step(g)
	if g.BombAt(3, 3) == nil {
		t.Fatal("落石清除炸弹后玩家应该能再次放弹")
	}

	g.dropStone()
	if g.TileAt(2, 1) != TileHard {
		t.Fatal("第二块石头应该顺着螺旋落到 (2,1)")
	}
}

// TestPickItemAppliesCaps 检查三种道具各自加成并发出拾取事件
func TestPickItemAppliesCaps(t *testing.T) {
	cases := []struct {
		name  string
		kind  int
		check func(*Player) (int, int)
	}{
		{"炸弹", ItemBomb, func(p *Player) (int, int) { return p.Bombs, 2 }},
		{"火力", ItemFire, func(p *Player) (int, int) { return p.Fire, 2 }},
		{"速度", ItemSpeed, func(p *Player) (int, int) { return p.SpeedTier, 1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newTestGame(t, 1)
			clearInterior(g)
			p := g.Players[0]
			g.items = append(g.items, &Item{Col: 1, Row: 1, Kind: c.kind})
			step(g)

			got, want := c.check(p)
			if got != want {
				t.Fatalf("吃到%s后属性应该是 %d，实际 %d", c.name, want, got)
			}
			if len(g.items) != 0 {
				t.Fatal("道具应该被拿走")
			}
			var picked bool
			for _, e := range g.events {
				if e.K == "item" && e.ID == p.ID && e.Kind == c.kind {
					picked = true
				}
			}
			if !picked {
				t.Fatalf("应该发出 item 事件: %+v", g.events)
			}
		})
	}
}

// TestReviveItemAndLives 检查默认三条命、复活道具加命与死亡后的重生
func TestReviveItemAndLives(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p := g.Players[0]
	initialLives := p.Lives
	if initialLives != defaultLives {
		t.Fatalf("默认生命不对，实际 %d", initialLives)
	}
	p.Lives = initialLives - 1
	g.items = append(g.items, &Item{Col: 1, Row: 1, Kind: ItemRevive})
	g.Update(16)
	if p.Lives != initialLives {
		t.Fatalf("复活道具应该增加一条命，实际 %d", p.Lives)
	}
	p.Lives = 100
	g.items = append(g.items, &Item{Col: 1, Row: 1, Kind: ItemRevive})
	g.Update(16)
	if p.Lives != 101 {
		t.Fatalf("复活道具不应有生命上限，实际 %d", p.Lives)
	}
	p.Lives = initialLives
	g.Kill(p, "")
	if p.Alive || p.Lives != initialLives-1 {
		t.Fatalf("死亡后生命应减一并等待复活: alive=%v lives=%d", p.Alive, p.Lives)
	}
	if p.RespawnTimer != respawnDelay {
		t.Fatalf("死亡后复活等待不对，实际 %.0fms", p.RespawnTimer)
	}
	advance(g, respawnDelay+testStep, testStep)
	if !p.Alive {
		t.Fatal("剩余生命应该自动复活")
	}
}

// TestRespawnAvoidsOccupiedSpawn 检查复活时不会挑选被存活玩家占用的出生点
func TestRespawnAvoidsOccupiedSpawn(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	respawning, blocker := g.Players[0], g.Players[1]
	respawning.Alive = false
	for i, spawn := range g.Spawns {
		if i == blocker.Spawn {
			continue
		}
		g.setTile(spawn.Col, spawn.Row, TileHard)
	}
	g.respawnPlayer(respawning)
	if respawning.Alive {
		t.Fatal("所有出生点被占用或封锁时不应复活")
	}
	if respawning.RespawnTimer != respawnRetry {
		t.Fatalf("出生点不可用时应短暂重试，实际 %.0f", respawning.RespawnTimer)
	}
}

// TestItemCapsAreClamped 检查炸弹、火力、速度都不会超过各自上限
func TestItemCapsAreClamped(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	p.Bombs, p.Fire, p.SpeedTier = maxBombs, maxFire, maxSpeedTier
	for _, kind := range []int{ItemBomb, ItemFire, ItemSpeed} {
		g.items = append(g.items[:0], &Item{Col: 1, Row: 1, Kind: kind})
		step(g)
	}
	if p.Bombs != maxBombs || p.Fire != maxFire || p.SpeedTier != maxSpeedTier {
		t.Fatalf("属性不该超过上限: bombs=%d fire=%d speed=%d", p.Bombs, p.Fire, p.SpeedTier)
	}
	if math.Abs(p.Speed-(baseSpeed+maxSpeedTier*speedStep)) > 1e-9 {
		t.Fatalf("速度换算不对: %.2f", p.Speed)
	}
}

// TestSerializeShape 检查快照字段、计时归一化、事件出队与空切片序列化
func TestSerializeShape(t *testing.T) {
	g := newTestGame(t, 2)
	clearInterior(g)
	p := g.Players[0]
	p.SpeedTier = 2
	g.SetInput(p.ID, 1, 0)
	g.RequestBomb(p.ID)
	elapsed := min(roundMS/100, bombFuse-testStep)
	advance(g, elapsed, testStep)

	s := g.Serialize()
	if len(s.Map) != g.Cols*g.Rows {
		t.Fatalf("地图串长度应该是 %d，实际 %d", g.Cols*g.Rows, len(s.Map))
	}
	if strings.Trim(s.Map, "012") != "" {
		t.Fatal("地图串只能由 0/1/2 组成")
	}
	if want := int(math.Ceil((roundMS - elapsed) / 1000)); s.Time != want {
		t.Fatalf("剩余秒数应该是 %d，实际 %d", want, s.Time)
	}
	if s.Sudden {
		t.Fatal("开局不该是急死阶段")
	}
	if len(s.Players) != 2 || s.Players[0].ID != p.ID {
		t.Fatalf("玩家列表不对: %+v", s.Players)
	}
	self := s.Players[0]
	if self.Alive != 1 || self.Moving != 1 || self.Dir != 1 || self.Speed != 2 {
		t.Fatalf("玩家字段不对: %+v", self)
	}
	if self.X != round2(p.X) || self.X <= 1.5 {
		t.Fatalf("坐标应该是两位小数且已经前进: %v", self.X)
	}
	if len(s.Bombs) != 1 {
		t.Fatalf("场上应该有一颗炸弹，实际 %d", len(s.Bombs))
	}
	if b := s.Bombs[0]; b.Timer <= 0 || b.Timer > 1 {
		t.Fatalf("炸弹计时应该归一化到 0..1，实际 %v", b.Timer)
	}
	if len(g.events) != 0 {
		t.Fatal("事件队列应该在打包后清空")
	}

	// 空列表要序列化成 []，前端才能直接遍历
	raw, err := json.Marshal(g.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"ps":[`, `"bs":[`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("快照缺少 %s，可能序列化成了 null: %s", key, raw)
		}
	}
}

// TestSerializeRespawnTimer 检查快照会带上服务端权威的复活倒计时
func TestSerializeRespawnTimer(t *testing.T) {
	g := newTestGame(t, 1)
	p := g.Players[0]
	g.Kill(p, "")
	s := g.Serialize()
	if s.Players[0].Respawn != int(respawnDelay) {
		t.Fatalf("复活倒计时未同步: %d", s.Players[0].Respawn)
	}
}

// TestSerializeEventsOnlyIncludesAffectedPlayers 检查事件状态不重复发送未受影响玩家
func TestSerializeEventsOnlyIncludesAffectedPlayers(t *testing.T) {
	g := newTestGame(t, 2)
	p := g.Players[0]
	g.items = append(g.items, &Item{Col: floor(p.X), Row: floor(p.Y), Kind: ItemBomb})
	step(g)
	s := g.SerializeEvents()
	if !s.Delta || len(s.Players) != 1 || s.Players[0].ID != p.ID {
		t.Fatalf("事件状态应该只包含受影响玩家: %+v", s)
	}
}

// TestEventSnapshotIsSmallerWithManyPlayers 检查多人对局的事件包不会重复携带全部玩家
func TestEventSnapshotIsSmallerWithManyPlayers(t *testing.T) {
	g := newTestGame(t, len(Spawns)+1)
	p := g.Players[0]
	g.items = append(g.items, &Item{Col: floor(p.X), Row: floor(p.Y), Kind: ItemBomb})
	step(g)
	delta, err := json.Marshal(g.SerializeEvents())
	if err != nil {
		t.Fatal(err)
	}
	full, err := json.Marshal(g.Serialize())
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) >= len(full) {
		t.Fatalf("多人事件包应小于完整快照: delta=%d full=%d", len(delta), len(full))
	}
}

// TestSerializeBombPass 保证放弹瞬间仍站在炸弹格内的玩家可以收到离开豁免
func TestSerializeBombPass(t *testing.T) {
	g := newTestGame(t, 1)
	p := g.Players[0]
	g.RequestBomb(p.ID)
	g.Update(16)
	s := g.Serialize()
	if len(s.Bombs) != 1 || len(s.Bombs[0].Pass) != 1 || s.Bombs[0].Pass[0] != p.ID {
		t.Fatalf("炸弹离开豁免序列化不正确: %+v", s.Bombs)
	}
}

// TestInputRules 检查斜向输入只保留横轴，阵亡与未知玩家的输入被忽略
func TestInputRules(t *testing.T) {
	g := newTestGame(t, 1)
	p := g.Players[0]

	g.SetInput(p.ID, 3, -2)
	if p.Input.X != 1 || p.Input.Y != 0 {
		t.Fatalf("斜向输入应该只留横向: %+v", p.Input)
	}
	g.SetInput(p.ID, 0, -5)
	if p.Input.X != 0 || p.Input.Y != -1 {
		t.Fatalf("纵向输入应该规整成 -1: %+v", p.Input)
	}

	// 未知 id 与阵亡玩家都要被忽略，且不能 panic
	g.SetInput("nobody", 1, 0)
	g.RequestBomb("nobody")
	g.Kill(p, "")
	g.SetInput(p.ID, 1, 0)
	g.RequestBomb(p.ID)
	if p.Input.X != 0 || p.Input.Y != 0 || p.WantBomb {
		t.Fatalf("阵亡后不该再接受输入: %+v", p)
	}
	g.Update(16)
	if len(g.bombs) != 0 {
		t.Fatal("阵亡玩家不该放出炸弹")
	}
}

// TestWallBlocksMovement 检查撞墙时贴住墙面且朝向仍然更新
func TestWallBlocksMovement(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]

	g.SetInput(p.ID, -1, 0)
	advance(g, 500, 16)
	if p.X < 1.3 {
		t.Fatalf("玩家穿过了左侧硬墙，x=%.4f", p.X)
	}
	if p.X > 1.36 {
		t.Fatalf("玩家应该贴到墙面，x=%.4f", p.X)
	}
	if p.Dir != 3 {
		t.Fatalf("朝向应该是左，实际 %d", p.Dir)
	}
}

// TestSnapAlignsCorridor 检查横向移动时自动对齐到走廊中线
func TestSnapAlignsCorridor(t *testing.T) {
	g := newTestGame(t, 1)
	clearInterior(g)
	p := g.Players[0]
	p.Y = 1.32 // 贴着上墙，但碰撞盒还在走廊里

	g.SetInput(p.ID, 1, 0)
	advance(g, 200, 16)
	if math.Abs(p.Y-1.5) > 0.01 {
		t.Fatalf("横向移动时应该自动对齐到走廊中线，y=%.4f", p.Y)
	}
}
