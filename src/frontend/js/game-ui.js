// 对局、房间和结算共用的玩家展示组件
import { h, mount, setText, appVersion, SEAT_COLORS, SEAT_NAMES } from './common.js'
import { ui, send, setPhase } from './network.js'
import { sfx } from './game-audio.js'

// 创建按座位编号着色的玩家头像
export function avatar(seat) {
  const index = Number.isInteger(seat) && seat >= 0 ? seat : 0
  return h('div', { class: 'avatar', style: { background: SEAT_COLORS[index % SEAT_COLORS.length] } }, SEAT_NAMES[index % SEAT_NAMES.length])
}

// 创建房间、结算和顶栏共用的玩家信息格子
export function playerSlot({ seat, name, detail, robot, self, dead, rank, tag, className, tail }) {
  const classes = ['seat-slot', className]
  if (robot) classes.push('bot')
  if (self) classes.push('self')
  if (dead) classes.push('dead')
  return h(
    'div',
    { class: classes.filter(Boolean).join(' ') },
    rank ? h('div', { class: 'rank', text: String(rank) }) : null,
    avatar(seat),
    h(
      'div',
      { class: 'seat-info' },
      h('div', { class: 'seat-name' }, name, tag ? h('span', { class: 'tag-ai', text: tag }) : null),
      detail ? typeof detail === 'string' ? h('div', { class: 'muted', text: detail }) : detail : null
    ),
    tail
  )
}

// 创建根据玩家数量自动选择列数的玩家格子列表
export function playerGrid(players, className) {
  return h('div', { class: ['seat-grid', 'seats-' + Math.min(2, Math.max(1, players.length)), className].filter(Boolean).join(' ') }, players.map(playerSlot))
}

// 创建顶栏玩家卡片及其局部更新引用
export function buildPlayerCards() {
  const refs = new Map()
  const nodes = [...ui.seats, ...ui.viewers].map((seat) => {
    const bombs = h('span', { title: '炸弹数', text: '💣1' })
    const fire = h('span', { title: '火力', text: '🔥1' })
    const speed = h('span', { title: '速度', text: '⚡0' })
    const lives = h('span', { title: '生命', text: '❤3' })
    const skull = h('div', { class: 'skull', text: '✖', style: { display: 'none' } })
    const card = playerSlot({
      seat: seat.seat,
      name: seat.name,
      detail: h('div', { class: 'stats' }, bombs, fire, speed, lives),
      self: seat.id === ui.me.id,
      tag: seat.robot ? 'AI' : seat.seat === undefined ? '观战' : '',
      className: 'pcard',
      tail: skull
    })
    refs.set(seat.id, { card, bombs, fire, speed, lives, skull })
    return card
  })
  return { refs, nodes }
}

// 页面未注入版本号时的本地开发展示值
const APP_VERSION = appVersion || 'dev'
// 创建首页和大厅共用的品牌标题
function logo() {
  return h('div', { class: 'logo' }, h('h1', { text: '炸弹人' }), h('p', { text: 'BOMBERMAN 在线对战' }))
}

// 创建可在进入对局前调整的静音按钮
function muteButton() {
  const button = h('button', { class: 'icon-btn', title: '', 'aria-label': '', onClick: toggle })

  // 刷新静音按钮的图标和辅助文本
  function refresh() {
    const muted = sfx.isMuted()
    setText(button, muted ? '🔇' : '🔊')
    button.title = muted ? '取消静音' : '静音'
    button.setAttribute('aria-label', button.title)
  }

  // 切换静音并保留用户选择
  function toggle() {
    sfx.toggleMute()
    refresh()
  }

  refresh()
  return button
}

// 创建说明文字中的按键样式标签
function key(text) {
  return h('span', { class: 'key', text })
}

// 将输入框回车事件转交给指定动作
function onEnter(fn) {
  return (e) => {
    if (e.key === 'Enter') fn()
  }
}

// 离开当前房间并回到首页
function returnHome() {
  send('leave', {})
  setPhase('login')
}

// 登录屏：起个昵称就能进大厅
export function loginScreen() {
  const nick = h('input', { maxlength: '10', placeholder: '输入昵称', onKeyUp: onEnter(enter) })
  nick.value = localStorage.getItem('bm_name') || '玩家' + Math.floor(Math.random() * 900 + 100)
  const btn = h('button', { class: 'btn', onClick: enter })
  const mute = muteButton()
  const count = h('div', { class: 'play-count' })
  const version = h('div', { class: 'app-version', text: '版本：' + APP_VERSION })

  // 保存昵称并向服务端完成登录
  function enter() {
    const name = nick.value.trim()
    if (!name) return
    localStorage.setItem('bm_name', name)
    ui.me.name = name
    sfx.unlock()
    send('hello', { name })
  }

  // 刷新登录页的连接状态
  function conn() {
    btn.disabled = !ui.connected
    setText(btn, ui.connected ? '进入大厅' : '连接中')
  }

  // 刷新首页展示的累计与当日游玩人数
  function stats() {
    setText(count, '总游玩 ' + ui.stats.total + ' · 今日游玩 ' + ui.stats.today)
  }

  conn()
  stats()
  const card = h(
    'div',
    { class: 'card home-card' },
    logo(),
    h('div', { class: 'home-content' },
      h('div', { class: 'field' }, nick, mute, btn),
      count,
      version,
      h(
        'div',
        { class: 'help' },
        h('div', { class: 'controls-help' }, '移动 ', key('W'), key('A'), key('S'), key('D'), ' 或方向键 · 放炸弹 ', key('空格'), ' / ', key('J')),
        h('div', { text: '炸开砖块吃道具，把对手送上天，最后站着的人获胜' }),
        h('div', { text: '四人在四个角落出生，五分钟后开始落石收缩场地' })
      )
    )
  )
  return { el: h('div', { class: 'screen' }, card), conn, stats }
}

// 大厅屏：房间列表 2.5 秒拉一次
export function lobbyScreen() {
  const who = h('div', { class: 'muted' })
  const list = h('div', { class: 'rows' })
  const code = h('input', { maxlength: '4', placeholder: '输入 4 位房间号加入', onKeyUp: onEnter(joinByCode) })

  // 按四位房号加入房间
  function joinByCode() {
    const id = code.value.trim()
    if (!id) return
    send('join', { roomId: id })
    code.value = ''
  }

  // 重绘大厅中的房间列表
  function rooms() {
    mount(list, ui.rooms.length ? ui.rooms.map(roomRow) : h('div', { class: 'empty', text: '暂无房间，创建一个等朋友来炸你' }))
  }

  // 刷新大厅的连接状态
  function conn() {
    setText(who, ui.me.name + ' · ' + (ui.connected ? '已连接' : '断线重连中'))
  }

  const card = h(
    'div',
    { class: 'card wide home-card' },
    logo(),
    h('div', { class: 'home-content lobby-content' },
      h(
        'div',
        { class: 'sec-title' },
        h('div', null, h('h2', { text: '游戏大厅' }), who),
        h('div', { class: 'actions' },
          h('button', { class: 'btn ghost sm', text: '首页', onClick: returnHome }),
          h('button', { class: 'btn', text: '创建房间', onClick: () => send('create', { name: ui.me.name + ' 的房间' }) })
        )
      ),
      h('div', { class: 'field', style: { marginTop: '16px' } }, code, h('button', { class: 'btn ghost', text: '加入', onClick: joinByCode })),
      list,
      h(
        'div',
        { class: 'help' },
        '支持多人同时游玩。服务器会按参战人数选择棋盘大小并随机分配出生点。',
        h('br'),
        '把房号发给朋友，或让他们在列表里直接点加入。'
      )
    )
  )

  conn()
  rooms()
  send('rooms', {})
  const timer = setInterval(() => send('rooms', {}), 2500)
  return { el: h('div', { class: 'screen' }, card), rooms, conn, destroy: () => clearInterval(timer) }
}

// 创建大厅中的单个房间行
function roomRow(r) {
  const full = !r.playing && r.count >= r.max
  return h(
    'div',
    { class: 'row' },
    h(
      'div',
      { class: 'grow' },
      h('div', { text: r.name }),
      h('div', { class: 'muted' }, '房号 ', h('span', { class: 'id', text: r.id }), ' · 房主 ' + r.host)
    ),
    h('div', { class: 'muted', text: r.count + '/' + r.max }),
    h('button', {
      class: full ? 'btn sm ghost' : 'btn sm',
      disabled: full,
      text: r.playing ? '观看' : r.count >= r.max ? '已满' : '加入',
      onClick: () => send('join', { roomId: r.id })
    })
  )
}

// 房间屏：房间视图一变就整块重画，够快也省心
export function roomScreen() {
  const el = h('div', { class: 'screen' })

  // 根据最新房间视图重绘房间页
  function room() {
    if (ui.room) mount(el, roomCard(ui.room, returnHome))
  }
  room()
  return { el, room }
}

// 创建当前真人和电脑组成的紧凑座位网格
function seatRows(r) {
  const slots = r.seats.map((s) => ({
    seat: s.seat,
    name: s.name + (s.id === ui.me.id ? '（你）' : ''),
    detail: s.id === r.hostId ? '房主' : s.ready ? '已准备' : '未准备',
    self: s.id === ui.me.id
  }))
  const bots = r.bots || 0
  for (let i = 0; i < bots; i++) {
    slots.push({
      seat: r.seats.length + i,
      name: (r.botNames || [])[i] || '',
      detail: '电脑玩家 · 自动参战',
      robot: true,
      tag: 'AI'
    })
  }
  return playerGrid(slots)
}

// 创建房主可操作的电脑数量设置区
function setupBox(r, isHost) {
  const limit = Math.max(0, r.max - r.seats.length)
  const count = h('output', { class: 'bot-count', text: String(r.bots || 0) })

  // 拖动时只更新当前数值，避免每个刻度都发送房间消息
  function previewBots(e) {
    setText(count, e.currentTarget.value)
  }

  // 松开滑块或键盘确认后再把电脑数量交给服务端校验
  function saveBots(e) {
    send('bots', { n: Number(e.currentTarget.value) })
  }

  const botRange = h('input', { class: 'bot-range', type: 'range', min: 0, max: limit, value: r.bots || 0, disabled: !isHost, onInput: previewBots, onChange: saveBots })
  return h(
    'div',
    { class: 'setup' },
    h('label', { class: 'line' }, h('span', { class: 'muted', text: '电脑玩家' }), botRange, count),
    h(
      'label',
      { class: 'line muted' },
      h('input', { type: 'checkbox', checked: r.maze, disabled: !isHost, onChange: (e) => send('maze', { v: e.currentTarget.checked }) }),
      ' 迷踪模式：参战者仅显示局部视野'
    ),
    h('div', { class: 'muted tip', text: '电脑会自己躲炸弹、炸砖吃道具，也会主动来炸你。仅房主可调整数量。' })
  )
}

// 创建对局规则与道具说明
function helpBox() {
  // 创建带颜色的道具名称
  const b = (color, text) => h('b', { style: { color }, text })
  return h(
    'div',
    { class: 'help' },
    '砖块随机生成。道具：',
    b('#ff6b3d', '炸弹+'),
    ' 同时放更多炸弹、',
    b('#ffd166', '火力+'),
    ' 爆炸范围更大、',
    b('#7ce38b', '速度+'),
    ' 跑得更快、',
    b('#d66bff', '生命+'),
    ' 增加一次复活。场地收缩后死亡将直接淘汰。'
  )
}

// 创建准备或开局按钮
function actionBar(r, isHost, isSpectator) {
  if (isSpectator) return h('div', { class: 'muted', text: '本局观战中，等待对局结束后可离开房间' })
  const me = r.seats.find((s) => s.id === ui.me.id)
  if (!isHost) {
    return h('button', {
      class: 'btn blue grow',
      text: me && me.ready ? '取消准备' : '准备就绪',
      onClick: () => send('ready', { v: !(me && me.ready) })
    })
  }
  const allReady = r.seats.every((s) => s.id === r.hostId || s.ready)
  return h('button', {
    class: 'btn grow',
    disabled: !allReady,
    text: !allReady ? '等待其他人准备' : r.seats.length === 1 ? '开始对战（对电脑）' : '开始对战',
    onClick: () => send('start', {})
  })
}

// 创建完整的房间信息卡片
function roomCard(r, home) {
  const isHost = r.hostId === ui.me.id
  const isSpectator = ui.spectator
  const tail = h('span', { class: 'muted' })
  const roomId = h('span', {
    class: 'id',
    style: { cursor: 'pointer' },
    text: r.id,
    onClick: () => {
      if (navigator.clipboard) navigator.clipboard.writeText(r.id)
      setText(tail, ' 已复制')
      setTimeout(() => setText(tail, ''), 1500)
    }
  })
  return h(
    'div',
    { class: 'card' },
    h('div', { class: 'room-content' },
      h(
        'div',
        { class: 'sec-title' },
        h('div', null, h('h2', { text: r.name }), h('div', { class: 'muted' }, '房号 ', roomId, tail)),
        h('div', { class: 'actions' },
          h('button', { class: 'btn ghost sm', text: '首页', onClick: home }),
          h('button', { class: 'btn ghost sm', text: '离开', onClick: () => send('leave', {}) })
        )
      ),
      seatRows(r),
      isSpectator ? null : setupBox(r, isHost),
      h('div', { class: 'actions' }, actionBar(r, isHost, isSpectator)),
      helpBox()
    )
  )
}
