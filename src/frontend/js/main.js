// 入口：按 phase 挂对应屏幕，其余事件交给当前屏幕自己刷新。
import { h, appVersion } from './common.js'
import { ui, connect, on } from './network.js'
import { loginScreen, lobbyScreen, roomScreen } from './game-ui.js'
import { gameScreen } from './game.js'
import { startAttractBackground } from './game-home.js'
import { sfx } from './game-audio.js'

const app = document.getElementById('app')
const screens = { login: loginScreen, lobby: lobbyScreen, room: roomScreen, game: gameScreen }
let current = null
const attract = startAttractBackground()
const pageVersion = appVersion

// 让浏览器外壳颜色跟随系统主题变化
function syncThemeColor() {
  const media = window.matchMedia('(prefers-color-scheme: dark)')
  // 同步当前系统主题对应的外壳颜色
  const apply = () => {
    document.querySelector('meta[name="theme-color"]').content = media.matches ? '#0b0e17' : '#f4f6fb'
  }
  apply()
  media.addEventListener?.('change', apply)
}

// 销毁旧屏幕并挂载当前阶段的界面
function mountScreen() {
  if (current && current.destroy) current.destroy()
  attract.setVisible(ui.phase !== 'game')
  sfx.setMusic(ui.phase === 'game' ? 'game' : ui.phase === 'lobby' ? 'lobby' : 'none', ui.theme)
  current = screens[ui.phase]()
  app.replaceChildren(current.el)
}

// 定期读取无缓存的发布版本，发现更新后重新加载整页
async function checkVersion() {
  try {
    const response = await fetch(new URL('version.json', document.baseURI), { cache: 'no-store' })
    const latest = await response.json()
    if (latest.version && pageVersion && latest.version !== pageVersion) location.reload()
  } catch {}
}

on('phase', mountScreen)

// 局部刷新：屏幕有同名钩子就调，没有就当这条消息与它无关
for (const type of ['conn', 'rooms', 'room', 'stats', 'hud', 'chat', 'over', 'viewers']) {
  on(type, () => {
    if (current && current[type]) current[type]()
  })
}

let toast = null
on('error', () => {
  if (toast) {
    toast.remove()
    toast = null
  }
  if (!ui.error) return
  toast = h('div', { class: 'toast', text: ui.error })
  document.body.append(toast)
})

mountScreen()
syncThemeColor()
connect()
setInterval(checkVersion, 30000)
