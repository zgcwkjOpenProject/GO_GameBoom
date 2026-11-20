// 房主机器人：只计算输入意图并交给服务端，位置、爆炸、伤害和结算仍由服务端权威处理
import { ui, world, pushBots } from './network.js'

const thinkMS = 120
const fuseMS = 2400
const flameMS = 520
const safetyMS = 160
// 玩家距离机器人不超过此格数时进入追击优先级
const chaseDistance = 6
const plans = new Map()

// 将棋盘坐标转换成地图数组下标
export function cell(col, row) {
  return row * world.cols + col
}

// 返回四个正交方向及对应角色朝向
function dirs() {
  return [[1, 0, 1], [-1, 0, 3], [0, 1, 2], [0, -1, 0]]
}

// 返回指定格上的炸弹
function bombAt(col, row, bombs = world.bombs) {
  return bombs.find((bomb) => bomb.c === col && bomb.r === row)
}

// 判断路径能否进入目标格；所有炸弹均视为不可回穿障碍
export function open(col, row, bombs = world.bombs) {
  return col > 0 && row > 0 && col < world.cols - 1 && row < world.rows - 1 && world.map[cell(col, row)] === 0 && !bombAt(col, row, bombs)
}

// 计算炸弹的十字火线，保守模式把可能提前炸开的软砖当作空地
export function blast(bomb, conservative = false) {
  const cells = [[bomb.c, bomb.r]]
  for (const [dx, dy] of dirs()) {
    for (let distance = 1; distance <= bomb.p; distance++) {
      const col = bomb.c + dx * distance
      const row = bomb.r + dy * distance
      const tile = world.map[cell(col, row)]
      if (tile === 1) break
      cells.push([col, row])
      if (tile === 2 && !conservative) break
    }
  }
  return cells
}

// 判断一颗炸弹的火线是否覆盖指定格
function contains(bomb, col, row, conservative = false) {
  return blast(bomb, conservative).some(([x, y]) => x === col && y === row)
}

// 推算连锁爆炸后的最早起爆时间
function detonationAt(bombs) {
  const now = performance.now()
  const times = bombs.map((bomb) => {
    const elapsed = bomb.serverClock && Number.isFinite(bomb.at) ? Math.max(0, now - bomb.at) : 0
    return Math.max(0, bomb.t * fuseMS - elapsed)
  })
  for (let pass = 0; pass < bombs.length; pass++) {
    let changed = false
    for (let i = 0; i < bombs.length; i++) {
      for (let j = 0; j < bombs.length; j++) {
        if (i === j || !contains(bombs[i], bombs[j].c, bombs[j].r, true)) continue
        const next = times[i] + 40
        if (next < times[j]) {
          times[j] = next
          changed = true
        }
      }
    }
    if (!changed) break
  }
  return times
}

// 判断抵达该格的时刻是否会遇到现有或即将发生的火焰
function unsafeAt(col, row, at, bombs, times) {
  if (world.flames.some((flame) => flame.c === col && flame.r === row)) return true
  return bombs.some((bomb, i) => contains(bomb, col, row, true) && at + safetyMS >= times[i] && at <= times[i] + flameMS + safetyMS)
}

// 判断最终停留格不在任何炸弹的火线上
function safeRest(col, row, bombs) {
  return !world.flames.some((flame) => flame.c === col && flame.r === row) && !bombs.some((bomb) => contains(bomb, col, row, true))
}

// 判断已撤出自家火线的机器人能否原地等待，不忽略其他玩家后来放下的炸弹
function safeToWait(col, row, ownBomb) {
  return !world.flames.some((flame) => flame.c === col && flame.r === row) && !world.bombs.some((bomb) => bomb !== ownBomb && contains(bomb, col, row, true))
}

// 生成当前炸弹布局标识，新炸弹出现或旧炸弹消失时必须放弃已完成的逃生计划
function bombLayout(bombs) {
  return bombs.map((bomb) => `${bomb.c},${bomb.r},${bomb.p},${bomb.o}`).sort().join('|')
}

// 按当前速度保守估算走过一格的时间
function walkMS(player) {
  return 1000 / (3.6 + player.speed * 0.55) + 65
}

// 用全部炸弹做障碍，寻找一条每一步都能及时到达的最终安全路线
function escape(player, bombs, target = null, blocked = null) {
  const col = Math.floor(player.x)
  const row = Math.floor(player.y)
  const times = detonationAt(bombs)
  const stepMS = walkMS(player)
  const queue = [{ col, row, first: null, steps: 0 }]
  const seen = new Set([cell(col, row)])
  for (let index = 0; index < queue.length; index++) {
    const current = queue[index]
    const arrived = current.steps * stepMS
    const atTarget = target && current.col === target.col && current.row === target.row
    if (current.steps > 0 && !unsafeAt(current.col, current.row, arrived, bombs, times) && (atTarget || (!target && safeRest(current.col, current.row, bombs)))) {
      return { dir: current.first, col: current.col, row: current.row }
    }
    if (current.steps >= 8) continue
    for (const direction of dirs()) {
      const nextCol = current.col + direction[0]
      const nextRow = current.row + direction[1]
      const id = cell(nextCol, nextRow)
      if (seen.has(id) || (blocked && blocked.has(id)) || !open(nextCol, nextRow, bombs)) continue
      if (unsafeAt(nextCol, nextRow, (current.steps + 1) * stepMS, bombs, times)) continue
      seen.add(id)
      queue.push({ col: nextCol, row: nextRow, first: current.first || direction, steps: current.steps + 1 })
    }
  }
  return null
}

// 计算站在某格放弹时可炸到的软砖数
function brickGain(col, row, power) {
  return blast({ c: col, r: row, p: power }).filter(([x, y]) => world.map[cell(x, y)] === 2).length
}

// 寻找最近可到达道具，明确把道具格当作可通行目标
function itemDirection(player) {
  if (!world.items.length) return null
  const startCol = Math.floor(player.x)
  const startRow = Math.floor(player.y)
  const queue = [{ col: startCol, row: startRow, first: null, steps: 0 }]
  const seen = new Set([cell(startCol, startRow)])
  for (let i = 0; i < queue.length; i++) {
    const current = queue[i]
    if (current.steps > 0 && world.items.some((item) => item.c === current.col && item.r === current.row)) return current.first
    if (current.steps >= 14) continue
    for (const direction of dirs()) {
      const col = current.col + direction[0]
      const row = current.row + direction[1]
      const id = cell(col, row)
      if (seen.has(id) || !open(col, row) || !safeRest(col, row, world.bombs)) continue
      seen.add(id)
      queue.push({ col, row, first: current.first || direction, steps: current.steps + 1 })
    }
  }
  return null
}

// 返回距离机器人最近的存活玩家
function nearestRival(player) {
  const col = Math.floor(player.x)
  const row = Math.floor(player.y)
  let nearest = null
  for (const rival of world.players.values()) {
    if (!rival.alive || rival.id === player.id) continue
    const distance = Math.abs(col - Math.floor(rival.x)) + Math.abs(row - Math.floor(rival.y))
    if (!nearest || distance < nearest.distance) nearest = { player: rival, distance }
  }
  return nearest
}

// 寻找最近能炸到软砖且可以安全撤离的落点
function brickDirection(player) {
  const startCol = Math.floor(player.x)
  const startRow = Math.floor(player.y)
  const queue = [{ col: startCol, row: startRow, first: null, steps: 0 }]
  const seen = new Set([cell(startCol, startRow)])
  for (let i = 0; i < queue.length; i++) {
    const current = queue[i]
    if (current.steps > 0 && brickGain(current.col, current.row, player.fire) > 0) {
      const virtual = [...world.bombs, { c: current.col, r: current.row, p: player.fire, t: 1, o: player.id }]
      if (escape(player, virtual)) return current.first
    }
    if (current.steps >= 18) continue
    for (const direction of dirs()) {
      const col = current.col + direction[0]
      const row = current.row + direction[1]
      const id = cell(col, row)
      if (seen.has(id) || !open(col, row) || !safeRest(col, row, world.bombs)) continue
      seen.add(id)
      queue.push({ col, row, first: current.first || direction, steps: current.steps + 1 })
    }
  }
  return null
}

// 寻找最近对手的可通行路线，避免机器人卡在墙角
function rivalDirection(player) {
  const rivals = [...world.players.values()].filter((other) => other.alive && other.id !== player.id)
  if (!rivals.length) return null
  const startCol = Math.floor(player.x)
  const startRow = Math.floor(player.y)
  const queue = [{ col: startCol, row: startRow, first: null, steps: 0 }]
  const seen = new Set([cell(startCol, startRow)])
  for (let i = 0; i < queue.length; i++) {
    const current = queue[i]
    if (current.steps > 0 && rivals.some((rival) => Math.floor(rival.x) === current.col && Math.floor(rival.y) === current.row)) return current.first
    if (current.steps >= 24) continue
    for (const direction of dirs()) {
      const col = current.col + direction[0]
      const row = current.row + direction[1]
      const id = cell(col, row)
      if (seen.has(id) || !open(col, row) || !safeRest(col, row, world.bombs)) continue
      seen.add(id)
      queue.push({ col, row, first: current.first || direction, steps: current.steps + 1 })
    }
  }
  return null
}

// 寻找能在安全撤离后炸到对手的最近落点
function attackAction(player) {
  const rivals = [...world.players.values()].filter((other) => other.alive && other.id !== player.id)
  if (!rivals.length) return null
  const startCol = Math.floor(player.x)
  const startRow = Math.floor(player.y)
  const queue = [{ col: startCol, row: startRow, first: null, steps: 0 }]
  const seen = new Set([cell(startCol, startRow)])
  for (let index = 0; index < queue.length; index++) {
    const current = queue[index]
    const virtual = [...world.bombs, { c: current.col, r: current.row, p: player.fire, t: 1, o: player.id }]
    const hitsRival = rivals.some((rival) => contains(virtual.at(-1), Math.floor(rival.x), Math.floor(rival.y)))
    const exit = hitsRival && escape(player, virtual)
    if (exit) {
      return current.steps === 0 ? { bomb: true, dir: exit.dir } : { dir: current.first }
    }
    if (current.steps >= 12) continue
    for (const direction of dirs()) {
      const col = current.col + direction[0]
      const row = current.row + direction[1]
      const id = cell(col, row)
      if (seen.has(id) || !open(col, row) || !safeRest(col, row, world.bombs)) continue
      seen.add(id)
      queue.push({ col, row, first: current.first || direction, steps: current.steps + 1 })
    }
  }
  return null
}

// 找到机器人本轮移动方向或安全的放弹机会
export function choose(player) {
  const col = Math.floor(player.x)
  const row = Math.floor(player.y)
  const existing = world.bombs.find((bomb) => bomb.o === player.id)
  if (existing) {
    let plan = plans.get(player.id)
    if (!plan) plan = { c: null, r: null, escaped: false, layout: '' }
    const ownFire = new Set(blast(existing, true).map(([x, y]) => cell(x, y)))
    const layout = bombLayout(world.bombs)
    if (!ownFire.has(cell(col, row))) plan.escaped = true
    if (plan.escaped && plan.c === col && plan.r === row && plan.layout === layout && safeToWait(col, row, existing)) return { dir: null }
    const blocked = plan.escaped ? ownFire : null
    const route = escape(player, world.bombs, plan.c === null ? null : plan, blocked) || escape(player, world.bombs, null, blocked)
    if (route) {
      plan.c = route.col
      plan.r = route.row
    }
    plan.layout = layout
    plans.set(player.id, plan)
    return { dir: route ? route.dir : null }
  }
  plans.delete(player.id)
  const danger = escape(player, world.bombs)
  if (world.flames.some((flame) => flame.c === col && flame.r === row) || !safeRest(col, row, world.bombs)) return { dir: danger ? danger.dir : null }
  const nearbyRival = nearestRival(player)
  if (nearbyRival && nearbyRival.distance <= chaseDistance) {
    const attack = attackAction(player)
    if (attack) return attack
    const rivalDir = rivalDirection(player)
    if (rivalDir) return { dir: rivalDir }
  }
  const itemDir = itemDirection(player)
  if (itemDir) return { dir: itemDir }
  if (!bombAt(col, row) && brickGain(col, row, player.fire) > 0) {
    const virtual = [...world.bombs, { c: col, r: row, p: player.fire, t: 1, o: player.id }]
    const exit = escape(player, virtual)
    if (exit) {
      plans.set(player.id, { c: exit.col, r: exit.row, escaped: false, layout: '' })
      return { bomb: true, dir: exit.dir }
    }
  }
  const brickDir = brickDirection(player)
  if (brickDir) return { dir: brickDir }
  const rivalDir = rivalDirection(player)
  if (rivalDir) return { dir: rivalDir }
  const rival = nearbyRival && nearbyRival.player
  let best = null
  let score = Infinity
  for (const direction of dirs()) {
    const nextCol = col + direction[0]
    const nextRow = row + direction[1]
    if (!open(nextCol, nextRow) || !safeRest(nextCol, nextRow, world.bombs)) continue
    const nearBrick = brickGain(nextCol, nextRow, player.fire) > 0 ? 2 : 0
    const itemDistance = world.items.reduce((best, it) => Math.min(best, Math.abs(nextCol - it.c) + Math.abs(nextRow - it.r)), 99)
    const itemBonus = itemDistance < 8 ? (8 - itemDistance) * 0.8 : 0
    const distance = rival ? Math.abs(nextCol - Math.floor(rival.x)) + Math.abs(nextRow - Math.floor(rival.y)) : 0
    if (distance - nearBrick - itemBonus < score) {
      score = distance - nearBrick - itemBonus
      best = direction
    }
  }
  return { dir: best }
}

// 由房主为所有存活机器人提交一次安全意图
function decide() {
  if (!ui.room || ui.room.hostId !== ui.me.id || ui.phase !== 'game') return
  const inputs = []
  for (const seat of ui.seats) {
    if (!seat.robot) continue
    const player = world.players.get(seat.id)
    if (!player || !player.alive) continue
    const action = choose(player)
    const direction = action.dir || [0, 0]
    if (action.bomb) {
      inputs.push({ id: player.id, x: direction[0], y: direction[1], bomb: true })
      continue
    }
    inputs.push({ id: player.id, x: direction[0], y: direction[1], bomb: false })
  }
  pushBots(inputs)
}

// 启动房主侧的机器人决策循环，并提供页面卸载清理方法
export function startBots() {
  const timer = setInterval(decide, thinkMS)
  return {
    // 停止当前战场屏的房主机器人决策循环
    stop() {
      clearInterval(timer)
      plans.clear()
    }
  }
}

// 清空一局机器人保留的逃生目标
export function resetBotPlans() {
  plans.clear()
}
