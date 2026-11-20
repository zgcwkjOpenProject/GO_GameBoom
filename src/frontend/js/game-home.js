// 首页演示：复用正式战场画布、随机地图与机器人决策，推进仅在浏览器本地进行
import { cell, choose, blast, open, resetBotPlans } from './game-bot.js'
import { ui, world } from './network.js'
import { createStage } from './renderer.js'

const moveMS = 340
const fuseMS = 2400
const flameMS = 520
let active = false
let stage = null
let holder = null
let state = null
let last = 0
let generation = 0
let restartTimer = 0

// 清理砖块、创建火焰并淘汰被覆盖的演示机器人
function explode(bomb) {
  for (const [col, row] of blast(bomb)) {
    if (world.map[cell(col, row)] === 2) world.map[cell(col, row)] = 0
    const kind = col === bomb.c && row === bomb.r ? 'c' : col === bomb.c ? 'v' : 'h'
    world.flames.push({ c: col, r: row, k: kind, e: 0, t: 1, at: performance.now() })
    for (const bot of state.bots) {
      const player = bot.player
      if (player.alive && Math.floor(player.x) === col && Math.floor(player.y) === row) player.alive = false
    }
  }
}

// 在演示机器人阵亡后重新获取一张真实随机地图
function restart() {
  if (!state || state.restarting) return
  state.restarting = true
  const version = generation
  restartTimer = window.setTimeout(() => {
    if (active && version === generation) load(version).catch(() => {})
  }, 700)
}

// 推进本地演示的炸弹、火焰和两个机器人
function step(dt) {
  if (state.restarting) return
  for (const flame of world.flames) flame.t -= dt / flameMS
  world.flames = world.flames.filter((flame) => flame.t > 0)
  for (const bomb of world.bombs) bomb.t -= dt / fuseMS
  const exploding = world.bombs.filter((bomb) => bomb.t <= 0)
  world.bombs = world.bombs.filter((bomb) => bomb.t > 0)
  for (const bomb of exploding) explode(bomb)
  if (state.bots.some((bot) => !bot.player.alive)) {
    restart()
    return
  }
  for (const bot of state.bots) {
    const player = bot.player
    bot.wait -= dt
    if (!bot.target && bot.wait <= 0) {
      const action = choose(player)
      if (action.bomb) {
        world.bombs.push({ c: bot.col, r: bot.row, p: player.fire, t: 1, o: player.id, at: performance.now() })
      }
      if (action.dir) bot.target = { col: bot.col + action.dir[0], row: bot.row + action.dir[1], dir: action.dir[2] }
      bot.wait = moveMS
    }
    if (!bot.target) continue
    if (!open(bot.target.col, bot.target.row)) {
      bot.target = null
      player.moving = false
      continue
    }
    player.moving = true
    // 首页演示已在本循环更新坐标，阻止渲染器按朝向再次预测移动
    player.inputX = 0
    player.inputY = 0
    player.dir = bot.target.dir
    const x = bot.target.col + 0.5
    const y = bot.target.row + 0.5
    const rate = Math.min(1, dt / moveMS * 3)
    player.x += (x - player.x) * rate
    player.y += (y - player.y) * rate
    player.rx = player.x
    player.ry = player.y
    if (Math.abs(player.x - x) < 0.015 && Math.abs(player.y - y) < 0.015) {
      player.x = x
      player.y = y
      player.rx = x
      player.ry = y
      bot.col = bot.target.col
      bot.row = bot.target.row
      bot.target = null
      player.moving = false
    }
  }
}

// 在正式渲染器绘制之间推进首页演示数据
function animate(now) {
  requestAnimationFrame(animate)
  const dt = Math.min(60, now - last || 16)
  last = now
  if (active && state) step(dt)
}

// 从后端取得按真实开局规则生成的一张随机地图
async function load(version) {
  const response = await fetch(new URL('demo', document.baseURI), { cache: 'no-store' })
  if (!response.ok) throw new Error('加载首页演示地图失败')
  const snapshot = await response.json()
  if (!active || version !== generation || !Number.isInteger(snapshot.cols) || !Number.isInteger(snapshot.rows) || snapshot.map.length !== snapshot.cols * snapshot.rows) return false
  resetBotPlans()
  world.cols = snapshot.cols
  world.rows = snapshot.rows
  world.map = Uint8Array.from(snapshot.map, (tile) => tile.charCodeAt(0) - 48)
  world.bombs = []
  world.flames = []
  world.items = []
  world.events = []
  world.stateAt = performance.now()
  world.players.clear()
  // 首页演示不属于任何服务端回合，不能继承上一局的收缩提示
  ui.hud = { time: 0, sudden: false, players: [] }
  ui.seats = [{ id: 'demo-red', name: '红方电脑', seat: 0, robot: true }, { id: 'demo-blue', name: '蓝方电脑', seat: 1, robot: true }]
  state = { restarting: false, bots: snapshot.ps.map((source) => {
    const player = { id: source.id, x: source.x, y: source.y, rx: source.x, ry: source.y, dir: source.d, alive: source.a === 1, moving: false, inputX: 0, inputY: 0, bombs: 1, fire: 2, speed: 0, kills: 0 }
    world.players.set(player.id, player)
    return { player, col: Math.floor(player.x), row: Math.floor(player.y), target: null, wait: Math.random() * moveMS }
  }) }
  return true
}

// 销毁首页战场，为正式对局释放唯一渲染器
function stop() {
  generation++
  active = false
  clearTimeout(restartTimer)
  restartTimer = 0
  if (stage) stage.destroy()
  if (holder) holder.remove()
  stage = null
  holder = null
  state = null
  resetBotPlans()
}

// 初始化首页的正式战场画布
async function start() {
  if (stage) return
  const version = generation
  try {
    if (!await load(version)) return
  } catch {
    return
  }
  if (!active || version !== generation || stage) return
  holder = document.createElement('div')
  holder.className = 'attract-background'
  holder.setAttribute('aria-hidden', 'true')
  document.body.prepend(holder)
  stage = createStage({ overview: true })
  holder.append(stage.el)
}

// 管理首页演示和正式对局之间的画布切换
export function startAttractBackground() {
  active = true
  requestAnimationFrame(animate)
  start()
  return {
    // 正式对局时释放画布，回到首页时重新取随机地图
    setVisible(next) {
      if (next) {
        active = true
        start()
      } else {
        stop()
      }
    }
  }
}
