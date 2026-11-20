// 画布渲染：精灵预渲染 + 客户端插值，画面只反映服务端快照。
import { world, ui, seatOf, nameOf, isRobot } from './network.js'
import { SEAT_COLORS, h } from './common.js'
import { sfx } from './game-audio.js'

const TILE = 42
const BOMB_FUSE_MS = 2400
const FLAME_MS = 520
// 正式对局的局部视野格数
const VIEW_COLS = 11
const VIEW_ROWS = 9
// 棋局主题依次为夜幕、森林与沙漠
const THEMES = [
  { floor: '#151a28', tileA: '#1e2536', tileB: '#232b3e', hardTop: '#6d78a3', hardMid: '#4b5578', hardBottom: '#2b3252', softTop: '#c07850', softMid: '#a05f3c', softBottom: '#7d452a' },
  { floor: '#15251d', tileA: '#213b2b', tileB: '#274532', hardTop: '#708f72', hardMid: '#486b4c', hardBottom: '#294631', softTop: '#b68b4e', softMid: '#806434', softBottom: '#513d21' },
  { floor: '#302419', tileA: '#4a3724', tileB: '#57412a', hardTop: '#b09a78', hardMid: '#806d51', hardBottom: '#50412e', softTop: '#d89d59', softMid: '#a76c35', softBottom: '#70431f' }
]

// 同一时刻只有一块画布，这些状态就放模块里
const spr = {}
const parts = []
const rings = []
const pops = []
let wrap = null
let cv = null
let ctx = null
let raf = 0
let last = 0
let shake = 0
let ro = null
// 首页演示和观战显示完整地图，存活玩家仅显示局部视野
let overview = false
let theme = 0

// 返回指定编号对应的棋局主题
function currentTheme() {
  return THEMES[theme] || THEMES[0]
}

// 在离屏画布预渲染一个精灵
function makeSprite(size, draw) {
  const c = document.createElement('canvas')
  c.width = size
  c.height = size
  const g = c.getContext('2d')
  draw(g, size)
  return c
}

// 在当前画布路径中加入圆角矩形
function roundRect(g, x, y, w, h, r) {
  g.beginPath()
  g.moveTo(x + r, y)
  g.arcTo(x + w, y, x + w, y + h, r)
  g.arcTo(x + w, y + h, x, y + h, r)
  g.arcTo(x, y + h, x, y, r)
  g.arcTo(x, y, x + w, y, r)
  g.closePath()
}

// 不可破坏的硬墙
function buildHard() {
  return makeSprite(84, (g, s) => {
    const colors = currentTheme()
    const k = s / 42
    const grad = g.createLinearGradient(0, 0, 0, s)
    grad.addColorStop(0, colors.hardTop)
    grad.addColorStop(0.42, colors.hardMid)
    grad.addColorStop(1, colors.hardBottom)
    roundRect(g, 1 * k, 1 * k, s - 2 * k, s - 2 * k, 6 * k)
    g.fillStyle = grad
    g.fill()
    g.strokeStyle = 'rgba(255,255,255,0.18)'
    g.lineWidth = 1.6 * k
    g.stroke()
    g.fillStyle = 'rgba(255,255,255,0.13)'
    roundRect(g, 5 * k, 4 * k, s - 10 * k, 12 * k, 4 * k)
    g.fill()
    g.fillStyle = 'rgba(0,0,0,0.26)'
    roundRect(g, 4 * k, s - 12 * k, s - 8 * k, 7 * k, 3 * k)
    g.fill()
  })
}

// 可以炸开的软砖
function buildSoft() {
  return makeSprite(84, (g, s) => {
    const colors = currentTheme()
    const k = s / 42
    const grad = g.createLinearGradient(0, 0, 0, s)
    grad.addColorStop(0, colors.softTop)
    grad.addColorStop(0.5, colors.softMid)
    grad.addColorStop(1, colors.softBottom)
    roundRect(g, 1.5 * k, 1.5 * k, s - 3 * k, s - 3 * k, 5 * k)
    g.fillStyle = grad
    g.fill()
    g.strokeStyle = 'rgba(0,0,0,0.32)'
    g.lineWidth = 1.4 * k
    g.stroke()
    g.strokeStyle = 'rgba(0,0,0,0.22)'
    g.lineWidth = 1.6 * k
    g.beginPath()
    g.moveTo(3 * k, 15 * k)
    g.lineTo(s - 3 * k, 15 * k)
    g.moveTo(3 * k, 28 * k)
    g.lineTo(s - 3 * k, 28 * k)
    g.moveTo(21 * k, 3 * k)
    g.lineTo(21 * k, 15 * k)
    g.moveTo(11 * k, 28 * k)
    g.lineTo(11 * k, 15 * k)
    g.moveTo(31 * k, 28 * k)
    g.lineTo(31 * k, 15 * k)
    g.moveTo(21 * k, 28 * k)
    g.lineTo(21 * k, s - 3 * k)
    g.stroke()
    g.fillStyle = 'rgba(255,255,255,0.16)'
    roundRect(g, 4 * k, 3 * k, s - 8 * k, 3 * k, 1.5 * k)
    g.fill()
  })
}

// 构建炸弹精灵
function buildBomb() {
  return makeSprite(84, (g, s) => {
    const k = s / 42
    const cx = s / 2
    const cy = s * 0.56
    const r = s * 0.31
    const grad = g.createRadialGradient(cx - r * 0.35, cy - r * 0.45, r * 0.1, cx, cy, r)
    grad.addColorStop(0, '#5b6280')
    grad.addColorStop(0.45, '#232838')
    grad.addColorStop(1, '#080a12')
    g.beginPath()
    g.arc(cx, cy, r, 0, Math.PI * 2)
    g.fillStyle = grad
    g.fill()
    g.fillStyle = 'rgba(255,255,255,0.75)'
    g.beginPath()
    g.ellipse(cx - r * 0.36, cy - r * 0.42, r * 0.2, r * 0.13, -0.6, 0, Math.PI * 2)
    g.fill()
    g.strokeStyle = '#8a6a3a'
    g.lineWidth = 3 * k
    g.lineCap = 'round'
    g.beginPath()
    g.moveTo(cx + r * 0.2, cy - r * 0.92)
    g.quadraticCurveTo(cx + r * 0.8, cy - r * 1.35, cx + r * 0.35, cy - r * 1.7)
    g.stroke()
  })
}

// 火焰精灵，c 中心 h 横向 v 纵向
function buildFlame(kind) {
  return makeSprite(84, (g, s) => {
    const cx = s / 2
    if (kind === 'c') {
      const grad = g.createRadialGradient(cx, cx, 1, cx, cx, s * 0.5)
      grad.addColorStop(0, 'rgba(255,255,240,1)')
      grad.addColorStop(0.28, 'rgba(255,226,130,0.98)')
      grad.addColorStop(0.6, 'rgba(255,132,32,0.8)')
      grad.addColorStop(1, 'rgba(220,40,20,0)')
      g.fillStyle = grad
      g.beginPath()
      g.arc(cx, cx, s * 0.5, 0, Math.PI * 2)
      g.fill()
      return
    }
    const horiz = kind === 'h'
    const grad = horiz ? g.createLinearGradient(0, 0, 0, s) : g.createLinearGradient(0, 0, s, 0)
    grad.addColorStop(0, 'rgba(226,60,20,0)')
    grad.addColorStop(0.22, 'rgba(255,128,30,0.72)')
    grad.addColorStop(0.5, 'rgba(255,248,210,1)')
    grad.addColorStop(0.78, 'rgba(255,128,30,0.72)')
    grad.addColorStop(1, 'rgba(226,60,20,0)')
    g.fillStyle = grad
    g.fillRect(0, 0, s, s)
  })
}

// 三种道具图标
function buildItem(kind) {
  return makeSprite(84, (g, s) => {
    const k = s / 42
    const color = kind === 1 ? '#ff5c72' : kind === 2 ? '#ffb03a' : kind === 3 ? '#5ce08d' : '#d66bff'
    const dark = kind === 1 ? '#8d1f33' : kind === 2 ? '#8d5410' : kind === 3 ? '#0f7a44' : '#712b8d'
    const grad = g.createLinearGradient(0, 0, 0, s)
    grad.addColorStop(0, color)
    grad.addColorStop(1, dark)
    roundRect(g, 5 * k, 5 * k, s - 10 * k, s - 10 * k, 9 * k)
    g.fillStyle = grad
    g.fill()
    g.strokeStyle = 'rgba(255,255,255,0.55)'
    g.lineWidth = 2 * k
    g.stroke()
    g.fillStyle = 'rgba(255,255,255,0.22)'
    roundRect(g, 9 * k, 8 * k, s - 18 * k, 8 * k, 4 * k)
    g.fill()
    g.fillStyle = '#fff'
    const cx = s / 2
    const cy = s / 2 + 1 * k
    if (kind === 1) drawBombIcon(g, cx, cy, k)
    else if (kind === 2) drawFireIcon(g, cx, cy, k)
    else if (kind === 3) drawBoltIcon(g, cx, cy, k)
    else drawHeartIcon(g, cx, cy, k)
  })
}

// 绘制复活道具的心形符号
function drawHeartIcon(g, cx, cy, k) {
  g.beginPath()
  g.moveTo(cx, cy + 11 * k)
  g.bezierCurveTo(cx - 18 * k, cy - 1 * k, cx - 9 * k, cy - 12 * k, cx, cy - 4 * k)
  g.bezierCurveTo(cx + 9 * k, cy - 12 * k, cx + 18 * k, cy - 1 * k, cx, cy + 11 * k)
  g.fill()
}

// 绘制道具图标中的炸弹符号
function drawBombIcon(g, cx, cy, k) {
  g.beginPath()
  g.arc(cx, cy + 2 * k, 8 * k, 0, Math.PI * 2)
  g.fill()
  g.strokeStyle = '#fff'
  g.lineWidth = 2.6 * k
  g.lineCap = 'round'
  g.beginPath()
  g.moveTo(cx + 4 * k, cy - 6 * k)
  g.lineTo(cx + 8 * k, cy - 12 * k)
  g.stroke()
}

// 绘制道具图标中的火焰符号
function drawFireIcon(g, cx, cy, k) {
  g.beginPath()
  g.moveTo(cx, cy - 12 * k)
  g.quadraticCurveTo(cx + 10 * k, cy - 1 * k, cx + 4 * k, cy + 6 * k)
  g.quadraticCurveTo(cx + 8 * k, cy + 12 * k, cx, cy + 12 * k)
  g.quadraticCurveTo(cx - 8 * k, cy + 12 * k, cx - 4 * k, cy + 5 * k)
  g.quadraticCurveTo(cx - 10 * k, cy - 2 * k, cx, cy - 12 * k)
  g.fill()
}

// 绘制道具图标中的闪电符号
function drawBoltIcon(g, cx, cy, k) {
  g.beginPath()
  g.moveTo(cx + 3 * k, cy - 12 * k)
  g.lineTo(cx - 9 * k, cy + 2 * k)
  g.lineTo(cx - 1 * k, cy + 2 * k)
  g.lineTo(cx - 3 * k, cy + 12 * k)
  g.lineTo(cx + 9 * k, cy - 3 * k)
  g.lineTo(cx + 1 * k, cy - 3 * k)
  g.closePath()
  g.fill()
}

// 按需构建全部可复用的精灵
function buildSprites() {
  if (spr.hard) return
  spr.hard = buildHard()
  spr.soft = buildSoft()
  spr.bomb = buildBomb()
  spr.fc = buildFlame('c')
  spr.fh = buildFlame('h')
  spr.fv = buildFlame('v')
  spr.i1 = buildItem(1)
  spr.i2 = buildItem(2)
  spr.i3 = buildItem(3)
  spr.i4 = buildItem(4)
}

// 切换主题后仅重建受主题影响的墙体精灵
function setTheme(next) {
  const value = Number.isInteger(next) && next >= 0 && next < THEMES.length ? next : 0
  if (theme === value) return
  theme = value
  delete spr.hard
  delete spr.soft
  buildSprites()
}

// 返回当前画布应显示的地图尺寸
function viewSize() {
  if (overview) return { cols: world.cols, rows: world.rows }
  if (!window.matchMedia('(max-width: 700px)').matches || !wrap || !wrap.clientWidth || !wrap.clientHeight) {
    return { cols: Math.min(VIEW_COLS, world.cols), rows: Math.min(VIEW_ROWS, world.rows) }
  }
  const aspect = wrap.clientWidth / wrap.clientHeight
  const cols = Math.max(7, Math.round(Math.sqrt(VIEW_COLS * VIEW_ROWS * aspect)))
  const rows = Math.max(7, Math.round(cols / aspect))
  return {
    cols: Math.min(cols, world.cols),
    rows: Math.min(rows, world.rows)
  }
}

// 根据玩家位置计算局部视野的左上角地图坐标
function cameraPosition(cols, rows) {
  if (overview) return { x: 0, y: 0 }
  const me = world.players.get(ui.me.id)
  const target = me && (me.alive || me.lives > 0) ? me : [...world.players.values()].find((player) => player.alive) || me
  const x = target && Number.isFinite(target.rx) ? target.rx - cols / 2 : (world.cols - cols) / 2
  const y = target && Number.isFinite(target.ry) ? target.ry - rows / 2 : (world.rows - rows) / 2
  return {
    x: Math.max(0, Math.min(world.cols - cols, x)),
    y: Math.max(0, Math.min(world.rows - rows, y))
  }
}

// 按容器大小等比缩放画布显示尺寸
function fit() {
  if (!wrap || !cv) return
  const w = wrap.clientWidth
  const h = wrap.clientHeight
  if (!w || !h) return
  resizeCanvas()
  if (!overview && window.matchMedia('(max-width: 700px)').matches) {
    cv.style.width = w + 'px'
    cv.style.height = h + 'px'
    return
  }
  const size = viewSize()
  const scale = Math.min(w / (size.cols * TILE), h / (size.rows * TILE))
  if (!(scale > 0)) return
  cv.style.width = size.cols * TILE * scale + 'px'
  cv.style.height = size.rows * TILE * scale + 'px'
}

// 按当前视野与设备像素比更新画布缓冲区
function resizeCanvas() {
  if (!cv) return
  const size = viewSize()
  const dpr = Math.min(2, window.devicePixelRatio || 1)
  const width = size.cols * TILE * dpr
  const height = size.rows * TILE * dpr
  if (cv.width !== width) cv.width = width
  if (cv.height !== height) cv.height = height
}

// 添加一组爆炸粒子，并限制总量
function addParts(x, y, n, colors, power) {
  for (let i = 0; i < n; i++) {
    const a = Math.random() * Math.PI * 2
    const sp = (0.4 + Math.random()) * power
    parts.push({
      x,
      y,
      vx: Math.cos(a) * sp,
      vy: Math.sin(a) * sp - 0.3,
      life: 1,
      decay: 0.8 + Math.random() * 0.9,
      size: 2 + Math.random() * 3.4,
      color: colors[(Math.random() * colors.length) | 0]
    })
  }
  if (parts.length > 400) parts.splice(0, parts.length - 400)
}

// 返回事件相对本机角色的线性音量，十格外的战场音效不播放
function soundLevel(col, row) {
  const listener = world.players.get(ui.me.id) || [...world.players.values()].find((player) => player.alive)
  if (!listener) return 0
  const distance = Math.hypot(listener.rx - col, listener.ry - row)
  return Math.max(0, 1 - distance / 10)
}

// 消费服务端事件，转成画面与音效反馈
function drainEvents() {
  for (const e of world.events) {
    if (e.k === 'boom') {
      shake = Math.min(9, shake + 4)
      addParts((e.col + 0.5) * TILE, (e.row + 0.5) * TILE, 10, ['#ffd166', '#ff7b32', '#fff3c4'], 2.6)
      rings.push({ x: (e.col + 0.5) * TILE, y: (e.row + 0.5) * TILE, life: 1 })
      sfx.boom(soundLevel(e.col + 0.5, e.row + 0.5))
    } else if (e.k === 'place') {
      sfx.place(soundLevel(e.col + 0.5, e.row + 0.5))
    } else if (e.k === 'item') {
      const p = world.players.get(e.id)
      if (p) pops.push({ x: p.rx * TILE, y: p.ry * TILE, life: 1, kind: e.kind })
      if (e.id === ui.me.id) sfx.item()
    } else if (e.k === 'die') {
      const p = world.players.get(e.id)
      if (p) {
        const c = SEAT_COLORS[seatOf(e.id)]
        addParts(p.rx * TILE, p.ry * TILE, 26, [c, '#ffffff', '#ffd166'], 3.2)
      }
      if (p) sfx.die(soundLevel(p.rx, p.ry))
    } else if (e.k === 'stone') {
      addParts((e.col + 0.5) * TILE, (e.row + 0.5) * TILE, 8, ['#9aa3c0', '#6b7396'], 2)
      sfx.stone(soundLevel(e.col + 0.5, e.row + 0.5))
    }
  }
  world.events.length = 0
}

// 平滑插值服务端每帧同步的位置，浏览器不自行预测碰撞和移动
function updatePositions(dt) {
  const k = 1 - Math.exp(-dt / 42)
  for (const p of world.players.values()) {
    let tx = p.x
    let ty = p.y
    if (Math.abs(tx - p.rx) > 1.6 || Math.abs(ty - p.ry) > 1.6) {
      p.rx = tx
      p.ry = ty
    } else {
      p.rx += (tx - p.rx) * k
      p.ry += (ty - p.ry) * k
    }
  }
}

// 绘制棋盘的地面底图
function drawFloor(g) {
  const colors = currentTheme()
  const W = world.cols * TILE
  const H = world.rows * TILE
  g.fillStyle = colors.floor
  g.fillRect(0, 0, W, H)
  for (let r = 0; r < world.rows; r++) {
    for (let c = 0; c < world.cols; c++) {
      if (world.map[r * world.cols + c] !== 0) continue
      g.fillStyle = (c + r) % 2 === 0 ? colors.tileA : colors.tileB
      g.fillRect(c * TILE, r * TILE, TILE, TILE)
      g.fillStyle = 'rgba(255,255,255,0.03)'
      g.fillRect(c * TILE, r * TILE, TILE, 1)
    }
  }
}

// 绘制已阵亡玩家的幽灵
function drawGhost(g, p, color, now) {
  const cx = p.rx * TILE
  const cy = p.ry * TILE + Math.sin(now / 420) * 2.5
  g.save()
  g.globalAlpha = 0.42
  g.fillStyle = '#dfe6f5'
  g.beginPath()
  g.arc(cx, cy - 2, TILE * 0.24, Math.PI, 0)
  g.lineTo(cx + TILE * 0.24, cy + TILE * 0.16)
  for (let i = 0; i < 3; i++) {
    const x0 = cx + TILE * 0.24 - i * TILE * 0.16
    g.quadraticCurveTo(x0 - TILE * 0.08, cy + TILE * 0.28, x0 - TILE * 0.16, cy + TILE * 0.16)
  }
  g.closePath()
  g.fill()
  g.fillStyle = color
  g.beginPath()
  g.arc(cx - 4, cy - 4, 2.2, 0, Math.PI * 2)
  g.arc(cx + 4, cy - 4, 2.2, 0, Math.PI * 2)
  g.fill()
  g.restore()
}

// 绘制一名玩家及其状态
function drawPlayer(g, p, now) {
  const color = SEAT_COLORS[seatOf(p.id)] || '#fff'
  const cx = p.rx * TILE
  const cy = p.ry * TILE
  g.fillStyle = 'rgba(0,0,0,0.34)'
  g.beginPath()
  g.ellipse(cx, cy + TILE * 0.3, TILE * 0.24, TILE * 0.1, 0, 0, Math.PI * 2)
  g.fill()
  if (!p.alive) {
    drawGhost(g, p, color, now)
    return
  }

  const bob = p.moving ? Math.sin(now / 85) * 1.7 : 0
  const step = p.moving ? Math.sin(now / 85) * 2.6 : 0
  // 脚
  g.fillStyle = '#2a3047'
  roundRect(g, cx - 8, cy + TILE * 0.16 + step * 0.2, 6.5, 6, 2.4)
  g.fill()
  roundRect(g, cx + 1.5, cy + TILE * 0.16 - step * 0.2, 6.5, 6, 2.4)
  g.fill()
  // 身体
  const bg = g.createLinearGradient(0, cy - 4 + bob, 0, cy + TILE * 0.2 + bob)
  bg.addColorStop(0, color)
  bg.addColorStop(1, 'rgba(0,0,0,0.45)')
  roundRect(g, cx - 10, cy - 3 + bob, 20, TILE * 0.22, 6)
  g.fillStyle = bg
  g.fill()
  g.strokeStyle = 'rgba(0,0,0,0.35)'
  g.lineWidth = 1
  g.stroke()
  drawHead(g, p, cx, cy - TILE * 0.19 + bob, color)
  drawLabel(g, p, cx, cy, color)
}

// 绘制玩家头部和朝向
function drawHead(g, p, cx, hy, color) {
  g.fillStyle = '#f4f7ff'
  g.beginPath()
  g.arc(cx, hy, TILE * 0.215, 0, Math.PI * 2)
  g.fill()
  g.strokeStyle = 'rgba(0,0,0,0.22)'
  g.lineWidth = 1
  g.stroke()
  // 头盔
  g.save()
  g.beginPath()
  g.arc(cx, hy, TILE * 0.225, Math.PI * 0.98, Math.PI * 2.02)
  g.closePath()
  g.fillStyle = color
  g.fill()
  g.restore()
  g.fillStyle = 'rgba(255,255,255,0.4)'
  g.beginPath()
  g.ellipse(cx - 3.4, hy - 5.4, 3, 1.7, -0.5, 0, Math.PI * 2)
  g.fill()
  // 眼睛按朝向偏移，朝上时看不到脸
  if (p.dir !== 0) {
    const ox = p.dir === 1 ? 2.6 : p.dir === 3 ? -2.6 : 0
    g.fillStyle = '#1b2030'
    g.beginPath()
    g.arc(cx - 3.6 + ox, hy + 2.4, 2, 0, Math.PI * 2)
    g.arc(cx + 3.6 + ox, hy + 2.4, 2, 0, Math.PI * 2)
    g.fill()
  } else {
    g.strokeStyle = 'rgba(0,0,0,0.18)'
    g.lineWidth = 1.4
    g.beginPath()
    g.moveTo(cx - 5, hy + 3)
    g.lineTo(cx + 5, hy + 3)
    g.stroke()
  }
}

// 绘制玩家名称与本人的位置标记
function drawLabel(g, p, cx, cy, color) {
  const label = nameOf(p.id)
  if (label) {
    g.font = '600 10px system-ui, sans-serif'
    g.textAlign = 'center'
    g.lineWidth = 3
    g.strokeStyle = 'rgba(0,0,0,0.72)'
    g.strokeText(label, cx, cy - TILE * 0.46)
    g.fillStyle = p.id === ui.me.id ? '#ffffff' : 'rgba(255,255,255,0.72)'
    g.fillText(label, cx, cy - TILE * 0.46)
  }
  if (p.id === ui.me.id) {
    g.fillStyle = color
    g.beginPath()
    g.moveTo(cx - 4, cy - TILE * 0.42)
    g.lineTo(cx + 4, cy - TILE * 0.42)
    g.lineTo(cx, cy - TILE * 0.36)
    g.closePath()
    g.fill()
  }
}

// 绘制并闪烁场上的炸弹
function drawBombs(g, now) {
  for (const b of world.bombs) {
    const left = Math.max(0, b.t - (now - b.at) / BOMB_FUSE_MS)
    const pulse = 1 + 0.14 * Math.abs(Math.sin((1 - left) * (6 + (1 - left) * 26)))
    const size = TILE * 0.96 * pulse
    const x = b.c * TILE + (TILE - size) / 2
    const y = b.r * TILE + (TILE - size) / 2
    g.drawImage(spr.bomb, x, y, size, size)
    if (Math.sin(now / 60) > 0) {
      g.fillStyle = '#ffe08a'
      g.beginPath()
      g.arc(b.c * TILE + TILE * 0.68, b.r * TILE + TILE * 0.12, 2.2 + Math.random(), 0, Math.PI * 2)
      g.fill()
    }
  }
}

// 火焰与冲击波用叠加混合更炫目
function drawFlames(g, dt) {
  g.save()
  g.globalCompositeOperation = 'lighter'
  for (const f of world.flames) {
    const left = Math.max(0, f.t - (performance.now() - f.at) / FLAME_MS)
    const grow = left > 0.85 ? (1 - left) / 0.15 : 1
    g.globalAlpha = Math.min(1, left * 1.8) * (0.55 + 0.45 * grow)
    const s = f.k === 'c' ? spr.fc : f.k === 'h' ? spr.fh : spr.fv
    const ex = f.k === 'h' ? 1 : 0.94 * grow
    const ey = f.k === 'v' ? 1 : 0.94 * grow
    const w = TILE * (f.k === 'c' ? 1.2 * grow : ex + 0.06)
    const h = TILE * (f.k === 'c' ? 1.2 * grow : ey + 0.06)
    g.drawImage(s, f.c * TILE + (TILE - w) / 2, f.r * TILE + (TILE - h) / 2, w, h)
  }
  for (let i = rings.length - 1; i >= 0; i--) {
    const rg = rings[i]
    rg.life -= dt / 380
    if (rg.life <= 0) {
      rings.splice(i, 1)
      continue
    }
    g.globalAlpha = rg.life * 0.6
    g.strokeStyle = '#ffd9a0'
    g.lineWidth = 3 * rg.life
    g.beginPath()
    g.arc(rg.x, rg.y, TILE * (1.4 - rg.life) * 1.1, 0, Math.PI * 2)
    g.stroke()
  }
  g.globalAlpha = 1
  g.restore()
}

// 推进并绘制爆炸粒子
function drawParts(g, dt) {
  for (let i = parts.length - 1; i >= 0; i--) {
    const p = parts[i]
    p.life -= (dt / 1000) * p.decay
    if (p.life <= 0) {
      parts.splice(i, 1)
      continue
    }
    p.x += p.vx * (dt / 16)
    p.y += p.vy * (dt / 16)
    p.vy += dt / 320
    g.globalAlpha = Math.max(0, p.life)
    g.fillStyle = p.color
    g.beginPath()
    g.arc(p.x, p.y, p.size * p.life, 0, Math.PI * 2)
    g.fill()
  }
  g.globalAlpha = 1
}

// 吃到道具时冒出的文字
function drawPops(g, dt) {
  for (let i = pops.length - 1; i >= 0; i--) {
    const p = pops[i]
    p.life -= dt / 900
    if (p.life <= 0) {
      pops.splice(i, 1)
      continue
    }
    g.globalAlpha = Math.min(1, p.life * 1.6)
    g.font = '700 12px system-ui, sans-serif'
    g.textAlign = 'center'
    g.fillStyle = p.kind === 1 ? '#ff8b9c' : p.kind === 2 ? '#ffc46a' : p.kind === 4 ? '#e08bff' : '#8bf0b4'
    const txt = p.kind === 1 ? '炸弹 +1' : p.kind === 2 ? '火力 +1' : p.kind === 4 ? '生命 +1' : '速度 +1'
    g.fillText(txt, p.x, p.y - 26 - (1 - p.life) * 18)
  }
  g.globalAlpha = 1
}

// 推进视觉插值并绘制一帧画面
function render(now) {
  if (!ctx || !cv) return
  raf = requestAnimationFrame(render)
  const g = ctx
  const dt = Math.min(120, now - last || 16)
  last = now

  drainEvents()
  updatePositions(dt)

  const size = viewSize()
  const viewW = size.cols * TILE
  const viewH = size.rows * TILE
  const camera = cameraPosition(size.cols, size.rows)
  g.setTransform(1, 0, 0, 1, 0, 0)
  g.clearRect(0, 0, cv.width, cv.height)
  g.scale(cv.width / viewW, cv.height / viewH)
  g.translate(-camera.x * TILE, -camera.y * TILE)
  if (shake > 0.1) {
    g.translate((Math.random() - 0.5) * shake, (Math.random() - 0.5) * shake)
    shake *= Math.pow(0.86, dt / 16)
  }

  drawFloor(g)

  // 道具画在砖块之下，砖炸开才露出来
  for (const it of world.items) {
    const fy = Math.sin(now / 320 + it.c) * 2
    g.drawImage(spr['i' + it.k], it.c * TILE + 1, it.r * TILE + 1 + fy, TILE - 2, TILE - 2)
  }

  for (let r = 0; r < world.rows; r++) {
    for (let c = 0; c < world.cols; c++) {
      const t = world.map[r * world.cols + c]
      if (t === 1) g.drawImage(spr.hard, c * TILE, r * TILE, TILE, TILE)
      else if (t === 2) g.drawImage(spr.soft, c * TILE, r * TILE, TILE, TILE)
    }
  }

  drawBombs(g, now)
  for (const p of world.players.values()) drawPlayer(g, p, now)
  drawFlames(g, dt)
  drawParts(g, dt)
  drawPops(g, dt)

  // 落石阶段给场地描一圈红边
  if (ui.phase === 'game' && ui.hud.sudden) {
    g.strokeStyle = 'rgba(255,80,80,' + (0.25 + 0.2 * Math.sin(now / 260)) + ')'
    g.lineWidth = 4
    g.strokeRect(camera.x * TILE + 2, camera.y * TILE + 2, viewW - 4, viewH - 4)
  }
}

// 建出画布并开始渲染，destroy 之后可以再建一次
export function createStage(options = {}) {
  setTheme(options.theme)
  buildSprites()
  overview = options.overview === true
  cv = h('canvas')
  wrap = h('div', { class: 'stage' }, cv)
  resizeCanvas()
  ctx = cv.getContext('2d')
  parts.length = 0
  rings.length = 0
  pops.length = 0
  shake = 0
  last = 0
  ro = new ResizeObserver(fit)
  ro.observe(wrap)
  raf = requestAnimationFrame(render)
  // 切换完整地图或局部视野，并按新视野重新适配画布
  function setOverview(enabled) {
    if (overview === enabled) return
    overview = enabled
    fit()
  }

  return { el: wrap, destroy, setOverview, setTheme }
}

// 停止渲染并释放画布引用
function destroy() {
  cancelAnimationFrame(raf)
  raf = 0
  if (ro) {
    ro.disconnect()
    ro = null
  }
  ctx = null
  cv = null
  wrap = null
}
