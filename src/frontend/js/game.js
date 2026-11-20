// 战场屏：顶栏 + 画布 + 触屏方向键 + 结算面板。输入按下即发，服务端说了算。
import { h, mount, setText, setClass, formatGameTime, SEAT_COLORS, SEAT_NAMES } from './common.js'
import { ui, send, setPhase, pushInput, dropBomb, sendChat } from './network.js'
import { sfx } from './game-audio.js'
import { createStage } from './renderer.js'
import { playerGrid, buildPlayerCards } from './game-ui.js'
import { startBots } from './game-bot.js'

const DIRS = {
  up: { x: 0, y: -1 },
  down: { x: 0, y: 1 },
  left: { x: -1, y: 0 },
  right: { x: 1, y: 0 }
}

const KEYMAP = {
  w: 'up',
  W: 'up',
  ArrowUp: 'up',
  s: 'down',
  S: 'down',
  ArrowDown: 'down',
  a: 'left',
  A: 'left',
  ArrowLeft: 'left',
  d: 'right',
  D: 'right',
  ArrowRight: 'right'
}

const BOMB_KEYS = [' ', 'j', 'J', 'k', 'Enter']

// 创建完整对局屏幕及其输入处理
export function gameScreen() {
  const stack = [] // 方向按下顺序，后按的优先
  let cards = buildPlayerCards()
  const players = h('div', { class: 'players' }, cards.nodes)
  const clockT = h('div', { class: 't', text: '5:00' })
  const clockL = h('div', { class: 'lbl', text: '剩余时间' })
  const clock = h('div', { class: 'clock' }, clockT, clockL)
  const respawn = h('div', { class: 'respawn', style: { display: 'none' } })
  const stage = createStage({ overview: ui.spectator || !ui.maze, theme: ui.theme })
  const bots = startBots()
  const isTouch = 'ontouchstart' in window || navigator.maxTouchPoints > 0
  const pad = isTouch ? touchPad(setTouchDirection) : null
  const chatInput = h('input', { class: 'chat-input', maxlength: '80', placeholder: '输入消息，回车发送' })
  const chatLines = h('div', { class: 'chat-lines' })
  const chatBox = h('div', { class: 'chat-box' }, chatLines, chatInput)
  let chatTimer = 0
  let chatCloseTimer = 0
  let chatOpen = false

  // 把当前方向发给服务端
  function apply() {
    if (ui.spectator) return
    const dir = stack[stack.length - 1]
    const v = dir ? DIRS[dir] : { x: 0, y: 0 }
    pushInput(v.x, v.y)
  }

  // 记录一个按下方向并立即同步
  function press(dir) {
    if (!stack.includes(dir)) stack.push(dir)
    apply()
  }

  // 移除松开的方向并同步余下输入
  function release(dir) {
    const i = stack.indexOf(dir)
    if (i >= 0) stack.splice(i, 1)
    apply()
  }

  // 把触屏方向键当前方向同步到服务端输入
  function setTouchDirection(dir) {
    stack.length = 0
    if (dir) stack.push(dir)
    apply()
  }

  // 失焦时松开所有方向，避免角色一直跑
  function clearAll() {
    stack.length = 0
    pushInput(0, 0)
  }

  // 处理键盘移动与放弹
  function onKeyDown(e) {
    if (ui.result || ui.spectator) return
    if (e.key === 'Enter' && document.activeElement !== chatInput) {
      e.preventDefault()
      openChat()
      return
    }
    if (e.key === 'Escape' && chatOpen) {
      e.preventDefault()
      closeChat()
      return
    }
    if (document.activeElement === chatInput) return
    const dir = KEYMAP[e.key]
    if (dir) {
      e.preventDefault()
      sfx.unlock()
      press(dir)
      return
    }
    if (BOMB_KEYS.includes(e.key)) {
      e.preventDefault()
      sfx.unlock()
      if (!e.repeat) dropBomb()
    }
  }

  // 松开键盘移动方向
  function onKeyUp(e) {
    if (document.activeElement === chatInput) return
    const dir = KEYMAP[e.key]
    if (dir) release(dir)
  }

  // 刷新顶栏中的对局数据
  function hud() {
    const elapsed = ui.hud.timeAt ? (performance.now() - ui.hud.timeAt) / 1000 : 0
    const t = Math.max(0, Math.ceil(ui.hud.time - elapsed))
    sfx.setMusic('game', ui.theme, !ui.hud.sudden && t < 120)
    setText(clockT, ui.hud.sudden ? '场地收缩' : formatGameTime(t))
    setText(clockL, ui.hud.sudden ? '快躲开落石' : '剩余时间')
    setClass(clock, 'hot', ui.hud.sudden)
    const me = ui.hud.players.find((p) => p.id === ui.me.id)
    const overview = !ui.maze || ui.spectator || (me && !me.alive && me.lives <= 0)
    const disabled = ui.spectator || (me && !me.alive && me.lives <= 0)
    const left = me && !me.alive && me.lives > 0 && me.respawnAt ? Math.max(0, me.respawnAt - performance.now()) : 0
    respawn.style.display = left > 0 ? '' : 'none'
    if (left > 0) setText(respawn, '复活倒计时 ' + (left / 1000).toFixed(1) + ' 秒')
    stage.setOverview(overview)
    if (pad) setClass(pad, 'spectator', disabled)
    for (const p of ui.hud.players) {
      const c = cards.refs.get(p.id)
      if (!c) continue
      setText(c.bombs, '💣' + p.bombs)
      setText(c.fire, '🔥' + p.fire)
      setText(c.speed, '⚡' + p.speed)
      setText(c.lives, '❤' + (p.lives ?? 0))
      setClass(c.card, 'dead', !p.alive)
      c.skull.style.display = p.alive ? 'none' : ''
    }
  }

  // 观战者进出时重建顶部玩家列表
  function viewers() {
    cards = buildPlayerCards()
    mount(players, cards.nodes)
    hud()
  }

  // 渲染最新三条消息，十秒没有新消息时仅隐藏发送内容
  function chat() {
    mount(chatLines, ui.chat.map((item) => h('div', { class: 'chat-line' }, h('b', { text: item.name + '：' }), item.text)))
    chatLines.classList.toggle('hidden', ui.chat.length === 0)
    clearTimeout(chatTimer)
    if (ui.chat.length) {
      chatTimer = setTimeout(() => chatLines.classList.add('hidden'), 10000)
    }
  }

  // 把输入框中的文字交给服务端清洗与广播
  function submitChat() {
    const text = chatInput.value.trim()
    if (text) {
      sendChat(text)
      chatInput.value = ''
    }
    closeChat()
  }

  // 打开宽屏聊天输入框并把焦点交给输入框
  function openChat() {
    clearTimeout(chatCloseTimer)
    chatOpen = true
    chatBox.classList.add('chat-open')
    chatInput.focus()
  }

  // 收起宽屏聊天输入框并释放焦点
  function closeChat() {
    clearTimeout(chatCloseTimer)
    chatOpen = false
    chatBox.classList.remove('chat-open')
    if (document.activeElement === chatInput) chatInput.blur()
  }

  // 宽屏聊天失焦五秒后收起输入框，避免遮挡战场
  function closeChatAfterBlur() {
    if (!chatOpen || !window.matchMedia('(min-width: 701px)').matches) return
    clearTimeout(chatCloseTimer)
    chatCloseTimer = setTimeout(closeChat, 5000)
  }

  // 清除结算并返回房间
  function backToRoom() {
    ui.result = null
    setPhase('room')
  }

  // 离开当前房间回到大厅
  function quit() {
    ui.result = null
    send('leave', {})
  }

  // 结算面板出现时先把角色停下
  function over() {
    if (!ui.result) return
    stack.length = 0
    pushInput(0, 0)
    if (ui.result.winnerId === ui.me.id) sfx.win()
    el.append(resultOverlay(ui.result, backToRoom, quit))
  }

  const el = h(
    'div',
    { class: 'game' },
    h(
      'div',
      { class: 'topbar' },
      players,
      clock,
      respawn,
      ui.spectator ? h('div', { class: 'muted', text: '观战中' }) : null,
      h('button', { class: 'btn ghost sm', text: '退出', onClick: quit })
    ),
    stage.el,
    pad,
    chatBox
  )

  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  window.addEventListener('blur', clearAll)
  chatInput.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' || e.isComposing) return
    e.preventDefault()
    submitChat()
  })
  chatInput.addEventListener('focus', () => clearTimeout(chatCloseTimer))
  chatInput.addEventListener('blur', closeChatAfterBlur)
  const timer = setInterval(apply, 60)
  const respawnTimer = setInterval(hud, 100)
  chat()

  // 释放战场屏的事件、定时器与画布
  function destroy() {
    clearAll()
    window.removeEventListener('keydown', onKeyDown)
    window.removeEventListener('keyup', onKeyUp)
    window.removeEventListener('blur', clearAll)
    clearInterval(timer)
    clearInterval(respawnTimer)
    clearTimeout(chatTimer)
    clearTimeout(chatCloseTimer)
    bots.stop()
    stage.destroy()
  }

  return { el, hud, chat, over, viewers, destroy }
}

// 创建可持续拖动的虚拟摇杆，指针捕获后离开控件也可继续移动
function joystick(setDirection) {
  const knob = h('span', { class: 'joystick-knob' })
  const base = h('div', { class: 'joystick', 'aria-label': '移动摇杆' }, knob)
  let pointerID = null

  // 把摇杆归中并停止角色移动
  function reset() {
    pointerID = null
    base.classList.remove('active')
    knob.style.left = '50%'
    knob.style.top = '50%'
    setDirection(null)
  }

  // 根据手指偏移选择更符合四向移动的方向，中心仅保留很小的停止区
  function directionOf(dx, dy) {
    return Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? 'right' : 'left') : (dy > 0 ? 'down' : 'up')
  }

  // 根据手指相对摇杆中心的位置更新外观和四向移动
  function update(e) {
    const rect = base.getBoundingClientRect()
    const limit = rect.width * 0.27
    const dx = e.clientX - (rect.left + rect.width / 2)
    const dy = e.clientY - (rect.top + rect.height / 2)
    const distance = Math.hypot(dx, dy)
    const scale = distance > limit ? limit / distance : 1
    const x = dx * scale
    const y = dy * scale
    knob.style.left = `calc(50% + ${x}px)`
    knob.style.top = `calc(50% + ${y}px)`
    if (distance < rect.width * 0.08) {
      setDirection(null)
      return
    }
    setDirection(directionOf(dx, dy))
  }

  // 捕获开始移动的手指，后续拖动不受是否离开控件影响
  function down(e) {
    if (pointerID !== null) return
    e.preventDefault()
    pointerID = e.pointerId
    base.setPointerCapture(pointerID)
    base.classList.add('active')
    sfx.unlock()
    update(e)
  }

  // 持续读取当前手指方向
  function move(e) {
    if (e.pointerId !== pointerID) return
    e.preventDefault()
    update(e)
  }

  // 释放当前手指并确保角色停止
  function up(e) {
    if (e.pointerId !== pointerID) return
    if (base.hasPointerCapture(pointerID)) base.releasePointerCapture(pointerID)
    reset()
  }

  base.addEventListener('pointerdown', down)
  base.addEventListener('pointermove', move)
  base.addEventListener('pointerup', up)
  base.addEventListener('pointercancel', up)
  base.addEventListener('lostpointercapture', reset)
  return base
}

// 创建可点按和滑动切换的四方向按键
function directionPad(setDirection) {
  const directions = [['up', '上'], ['left', '左'], ['down', '下'], ['right', '右']]
  const buttons = directions.map(([dir, label]) => h('button', { class: 'direction-button ' + dir, type: 'button', 'data-dir': dir, 'aria-label': '向' + label }))
  const base = h('div', { class: 'direction-pad', 'aria-label': '移动方向键' }, buttons, h('span', { class: 'direction-center' }))
  let pointerID = null
  let current = null

  // 找到当前手指所在的方向按键
  function directionAt(event) {
    const target = document.elementFromPoint(event.clientX, event.clientY)
    const button = target && target.closest('.direction-button')
    return button ? button.dataset.dir : null
  }

  // 开始按住一个方向，并捕获后续滑动
  function down(event) {
    if (pointerID !== null) return
    const dir = directionAt(event)
    event.preventDefault()
    pointerID = event.pointerId
    base.setPointerCapture(pointerID)
    sfx.unlock()
    // 中心区域只负责承接手指，滑入任一方向后才开始移动
    if (!dir) return
    current = dir
    setDirection(dir)
  }

  // 手指滑到另一方向时立即切换，不必抬起重按
  function move(event) {
    if (event.pointerId !== pointerID) return
    event.preventDefault()
    const dir = directionAt(event)
    if (dir) {
      current = dir
      setDirection(dir)
    }
  }

  // 手指离开后停止移动，下一次点击可直接开始新方向
  function up(event) {
    if (event.pointerId !== pointerID) return
    if (base.hasPointerCapture(pointerID)) base.releasePointerCapture(pointerID)
    pointerID = null
    current = null
    setDirection(null)
  }

  base.addEventListener('pointerdown', down)
  base.addEventListener('pointermove', move)
  base.addEventListener('pointerup', up)
  base.addEventListener('pointercancel', up)
  base.addEventListener('lostpointercapture', () => { pointerID = null; current = null; setDirection(null) })
  return base
}

// 创建实体摇杆与无文字炸弹按键
function touchPad(setDirection) {
  return h(
    'div',
    { class: 'pad' },
    directionPad(setDirection),
    h(
      'button',
      {
        class: 'bomb-button',
        'aria-label': '放置炸弹',
        onPointerDown: (e) => {
          e.preventDefault()
          sfx.unlock()
          dropBomb()
        }
      },
      h('span', { class: 'bomb-icon' }, h('span', { class: 'bomb-spark' }))
    )
  )
}

// 创建本局结算覆盖层
function resultOverlay(r, backToRoom, quit) {
  const win = r.winnerId ? ui.seats.find((s) => s.id === r.winnerId) : null
  const players = (r.board || []).map((b, i) => {
    const seat = ui.seats.find((item) => item.id === b.id)
    return {
      seat: b.seat,
      name: b.name,
      detail: '第 ' + (i + 1) + ' 名 · 击杀 ' + b.kills + ' · ' + (b.alive ? '存活' : '阵亡'),
      robot: seat?.robot,
      self: b.id === ui.me.id,
      dead: !b.alive,
      rank: i + 1,
      tag: seat?.robot ? 'AI' : ''
    }
  })
  const card = h(
    'div',
    { class: 'card result' },
    win ? h('div', { class: 'crown', style: { background: SEAT_COLORS[win.seat] } }, SEAT_NAMES[win.seat]) : null,
    h('h2', { text: win ? win.name + ' 获得胜利' : '本局平局' }),
    h('div', {
      class: 'muted sub',
      text: win && win.id === ui.me.id ? '干得漂亮，最后站着的就是你' : '再来一局把场子扳回来'
    }),
    playerGrid(players, 'result-grid'),
    h(
      'div',
      { class: 'actions' },
      h('button', { class: 'btn grow', text: '返回房间', onClick: backToRoom }),
      h('button', { class: 'btn ghost', text: '回到大厅', onClick: quit })
    )
  )
  return h('div', { class: 'overlay' }, card)
}
