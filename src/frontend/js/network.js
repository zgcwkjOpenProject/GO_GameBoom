// 网络层：连接、协议、战场缓存。界面刷新靠一个极简事件总线，不用框架。
// 事件：conn 连接状态 / phase 换屏 / rooms 大厅列表 / room 房间视图 / hud 顶栏 / over 结算 / error 提示

export const ui = {
  connected: false,
  phase: 'login', // login | lobby | room | game
  me: { id: '', name: '' },
  rooms: [],
  room: null,
  seats: [],
  viewers: [],
  theme: 0,
  maze: false,
  spectator: false,
  result: null,
  error: '',
  stats: { total: 0, today: 0 },
  chat: [],
  hud: { time: 0, timeAt: 0, sudden: false, players: [] }
}

// 战场数据，渲染循环直接读取，不进事件总线
export const world = {
  cols: 19,
  rows: 17,
  map: new Uint8Array(19 * 17),
  players: new Map(),
  bombs: [],
  flames: [],
  items: [],
  events: [],
  stateAt: 0
}

const bus = new Map()

// 订阅界面状态事件，并返回取消订阅函数
export function on(type, fn) {
  const list = bus.get(type)
  if (list) list.push(fn)
  else bus.set(type, [fn])
  return () => {
    const l = bus.get(type)
    const i = l ? l.indexOf(fn) : -1
    if (i >= 0) l.splice(i, 1)
  }
}

// 通知某类状态事件的全部订阅者
function emit(type) {
  const list = bus.get(type)
  if (!list) return
  for (const fn of list.slice()) fn()
}

// 切换界面阶段，并返回是否真的切换
export function setPhase(phase) {
  if (ui.phase === phase) return false
  ui.phase = phase
  emit('phase')
  return true
}

let ws = null
let reconnectTimer = null
let errorTimer = 0
let hudAt = 0
const lastInput = { x: 0, y: 0 }
const lastBotInputs = new Map()

// 根据页面基址生成支持子路由的 WebSocket 地址
function url() {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}${new URL('ws', document.baseURI).pathname}`
}

// 建立或复用 WebSocket 连接，并在断线后重连
export function connect() {
  if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) return
  ws = new WebSocket(url())
  ws.onopen = () => {
    ui.connected = true
    if (ui.me.name) send('hello', { name: ui.me.name })
    send('rooms', {})
    emit('conn')
  }
  ws.onclose = () => {
    ui.connected = false
    emit('conn')
    if (!reconnectTimer) {
      reconnectTimer = setTimeout(() => {
        reconnectTimer = null
        connect()
      }, 1500)
    }
  }
  ws.onmessage = (e) => {
    let msg
    try {
      msg = JSON.parse(e.data)
    } catch {
      return
    }
    const fn = routes[msg.t]
    if (fn) fn(msg)
  }
}

// 向服务端发送一条协议消息
export function send(type, data) {
  if (!ws || ws.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ t: type, ...data }))
}

// 重置战场缓存
function resetWorld() {
  world.players.clear()
  world.bombs = []
  world.flames = []
  world.items = []
  world.events = []
  world.map = new Uint8Array(world.cols * world.rows)
}

const routes = {
  welcome(m) {
    // 重连后服务端给的是新 id，原来的座位已经没了，直接退回大厅
    const reborn = ui.me.id !== '' && ui.me.id !== m.id
    ui.me.id = m.id
    ui.me.name = m.name
    if (reborn) {
      ui.room = null
      ui.result = null
      ui.seats = []
      ui.viewers = []
      ui.spectator = false
      setPhase('lobby')
    } else if (ui.phase === 'login') {
      setPhase('lobby')
    }
  },
  rooms(m) {
    ui.rooms = m.list || []
    emit('rooms')
  },
  stats(m) {
    ui.stats = m.stats || { total: 0, today: 0 }
    emit('stats')
  },
  joined(m) {
    ui.room = m.room
    ui.result = null
    ui.spectator = false
    if (!setPhase('room')) emit('room')
  },
  room(m) {
    ui.room = m.room
    emit('room')
  },
  left() {
    ui.room = null
    ui.spectator = false
    if (ui.phase !== 'login') setPhase('lobby')
  },
  state(m) {
    applyState(m.s)
  },
  acts(m) {
    for (const action of m.a || []) {
      if (!Array.isArray(action) || action.length !== 5) continue
      const seat = Number(action[0])
      const player = ui.seats.find((item) => item.seat === seat)
      if (!player) continue
      applyAction({ id: player.id, x: action[1], y: action[2], px: action[3] / 100, py: action[4] / 100 })
    }
  },
  viewers(m) {
    ui.viewers = m.viewers || []
    emit('viewers')
  },
  chat(m) {
    if (!m.chat) return
    ui.chat = [...ui.chat, m.chat].slice(-3)
    emit('chat')
  },
  over(m) {
    ui.result = m.result
    emit('over')
  },
  error(m) {
    toast(m.msg)
  },
  pong() {},
  start(m) {
    ui.seats = m.seats || []
    ui.viewers = m.viewers || []
    ui.theme = Number.isInteger(m.theme) ? m.theme : 0
    ui.maze = m.maze === true
    ui.spectator = m.spectator === true
    ui.result = null
    ui.chat = []
    world.cols = m.cols
    world.rows = m.rows
    resetWorld()
    lastInput.x = 0
    lastInput.y = 0
    lastBotInputs.clear()
    ui.hud = { time: 0, timeAt: 0, sudden: false, players: [] }
    hudAt = 0
    // 上一局的结算面板可能还开着，这时也要把战场屏重挂一次
    if (!setPhase('game')) emit('phase')
  }
}

// 弹出提示，2.6 秒后自动收起
export function toast(msg) {
  ui.error = msg || ''
  emit('error')
  clearTimeout(errorTimer)
  errorTimer = setTimeout(() => {
    ui.error = ''
    emit('error')
  }, 2600)
}

// 应用服务端的周期校准或离散事件状态
function applyState(s) {
  if (!s) return
  const delta = s.d === true
  const now = performance.now()
  world.stateAt = now
  if (s.map) {
    for (let i = 0; i < s.map.length; i++) world.map[i] = s.map.charCodeAt(i) - 48
  }
  for (const tile of s.tl || []) world.map[tile.r * world.cols + tile.c] = tile.v

  const seen = new Set()
  for (const p of s.ps || []) {
    seen.add(p.id)
    let e = world.players.get(p.id)
    if (!e) {
      e = { id: p.id, x: p.x, y: p.y, rx: p.x, ry: p.y }
      world.players.set(p.id, e)
    } else {
      reconcilePosition(e, p.x, p.y, e.alive !== (p.a === 1))
    }
    e.dir = p.d
    e.alive = p.a === 1
    e.moving = p.mv === 1
    if (e.moving && e.inputX === undefined) {
      e.inputX = p.d === 1 ? 1 : p.d === 3 ? -1 : 0
      e.inputY = p.d === 2 ? 1 : p.d === 0 ? -1 : 0
    }
    e.bombs = p.b
    e.fire = p.f
    e.speed = p.s
    e.lives = p.l || 0
    e.kills = p.k
    e.respawnAt = p.r > 0 ? now + p.r : 0
  }
  if (!delta) {
    for (const id of [...world.players.keys()]) if (!seen.has(id)) world.players.delete(id)
  }

  if (!delta || Array.isArray(s.bs)) {
    world.bombs = (s.bs || []).map((bomb) => ({ ...bomb, at: now, serverClock: true }))
    world.flames = (s.fs || []).map((flame) => ({ ...flame, at: now }))
    world.items = s.it || []
  } else {
    applyBombChanges(s.bd, now)
    applyFlameChanges(s.fd, now)
    applyItemChanges(s.id)
  }
  if (s.ev && s.ev.length) world.events.push(...s.ev)

  // 服务端时间到达时立刻校准，本地时钟会在两次同步之间连续递减
  if (Object.hasOwn(s, 'time')) {
    ui.hud.time = s.time
    ui.hud.timeAt = now
  }
  if (Object.hasOwn(s, 'sudden')) ui.hud.sudden = s.sudden

  // 顶栏不需要每帧刷，200 毫秒一次够了
  if (now - hudAt > 200) {
    hudAt = now
    ui.hud.players = [...world.players.values()].map((p) => ({
      id: p.id,
      alive: p.alive,
      bombs: p.bombs,
      fire: p.fire,
      speed: p.speed,
      lives: p.lives,
      kills: p.kills,
      respawnAt: p.respawnAt
    }))
    emit('hud')
  }
}

// 应用服务端已校验的移动动作，使各浏览器在周期校准间隔内同步演进
function applyAction(action) {
  const p = world.players.get(action.id)
  if (!p) return
  reconcilePosition(p, action.px, action.py, false)
  p.inputX = action.x
  p.inputY = action.y
  p.moving = action.x !== 0 || action.y !== 0
  if (action.x) p.dir = action.x > 0 ? 1 : 3
  else if (action.y) p.dir = action.y > 0 ? 2 : 0
}

// 应用服务端位置，画布继续使用 rx 和 ry 平滑追随，避免显示旧的预测坐标
function reconcilePosition(player, x, y, force) {
  if (!Number.isFinite(x) || !Number.isFinite(y)) return
  const jump = !Number.isFinite(player.x) || !Number.isFinite(player.y) || Math.hypot(player.x - x, player.y - y) > 1.6
  player.x = x
  player.y = y
  if (force || jump) {
    player.rx = x
    player.ry = y
  }
}

// 方向有变化才发包，减少无用流量
export function pushInput(x, y) {
  if (x === lastInput.x && y === lastInput.y) return
  lastInput.x = x
  lastInput.y = y
  const player = world.players.get(ui.me.id)
  if (player && player.alive) {
    player.inputX = x
    player.inputY = y
    player.moving = x !== 0 || y !== 0
    if (x) player.dir = x > 0 ? 1 : 3
    else if (y) player.dir = y > 0 ? 2 : 0
  }
  send('input', { x, y })
}

// 提交当前真人玩家的放弹意图
export function dropBomb() {
  send('bomb', {})
}

// 提交一条房间短消息，内容仍由服务端清洗和广播
export function sendChat(text) {
  send('chat', { text })
}

// 由房主提交机器人的输入或放弹意图，服务端仍校验身份并执行规则
export function pushBots(inputs) {
  const changed = []
  for (const input of inputs) {
    if (input.bomb) {
      lastBotInputs.delete(input.id)
      changed.push({ id: input.id, x: input.x || 0, y: input.y || 0, bomb: true })
      continue
    }
    const previous = lastBotInputs.get(input.id)
    if (previous && previous.x === input.x && previous.y === input.y) continue
    lastBotInputs.set(input.id, { x: input.x, y: input.y })
    changed.push({ id: input.id, x: input.x, y: input.y, bomb: false })
  }
  if (changed.length) send('bot', { bs: changed })
}

// 应用炸弹的新增、更新和删除
function applyBombChanges(changes, now) {
  for (const change of changes || []) {
    const index = world.bombs.findIndex((bomb) => bomb.i === change.i)
    if (change.x) {
      if (index >= 0) world.bombs.splice(index, 1)
      continue
    }
    if (!change.b) continue
    const bomb = { ...change.b, at: now, serverClock: true }
    if (index >= 0) world.bombs[index] = bomb
    else world.bombs.push(bomb)
  }
}

// 应用火焰的新增、更新和删除
function applyFlameChanges(changes, now) {
  for (const change of changes || []) {
    const index = world.flames.findIndex((flame) => flame.c === change.c && flame.r === change.r)
    if (change.x) {
      if (index >= 0) world.flames.splice(index, 1)
      continue
    }
    if (!change.f) continue
    const flame = { ...change.f, at: now }
    if (index >= 0) world.flames[index] = flame
    else world.flames.push(flame)
  }
}

// 应用道具的新增、更新和删除
function applyItemChanges(changes) {
  for (const change of changes || []) {
    const index = world.items.findIndex((item) => item.c === change.c && item.r === change.r)
    if (change.x) {
      if (index >= 0) world.items.splice(index, 1)
      continue
    }
    if (!change.it) continue
    if (index >= 0) world.items[index] = change.it
    else world.items.push(change.it)
  }
}

// 按玩家标识查找座位信息
function seatByID(id) {
  return ui.seats.find((v) => v.id === id)
}

// 返回玩家的座位编号
export function seatOf(id) {
  const s = seatByID(id)
  return s ? s.seat : 0
}

// 返回玩家展示名称
export function nameOf(id) {
  const s = seatByID(id)
  return s ? s.name : ''
}

// 该座位是不是电脑玩家
export function isRobot(id) {
  const s = seatByID(id)
  return !!(s && s.robot)
}
