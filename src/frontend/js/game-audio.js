// 用 WebAudio 合成音效，不依赖任何素材文件
let ac = null
let master = null
let musicGain = null
let musicTimer = null
let musicIndex = 0
let musicTheme = 0
let musicFast = false
let musicMode = 'none'
let muted = localStorage.getItem('bm_muted') === '1'
let focused = document.visibilityState === 'visible'
// 大厅只播放一次的原创八位机短曲，保留经典主机的轻快节奏但不复刻既有作品
const LOBBY_TRACK = [
  523.25, 659.25, 783.99, 1046.5, 783.99, 659.25, 523.25, 659.25,
  587.33, 739.99, 880, 1174.66, 880, 739.99, 587.33, 739.99,
  659.25, 783.99, 987.77, 1318.51, 987.77, 783.99, 659.25, 523.25
]
// 三套循环旋律对应三种棋盘主题
const MUSIC_TRACKS = [
  [261.63, 329.63, 392, 329.63, 293.66, 349.23, 440, 349.23],
  [220, 261.63, 329.63, 392, 329.63, 293.66, 261.63, 196],
  [293.66, 369.99, 440, 493.88, 440, 369.99, 329.63, 440]
]
// 所有声音共用的线性总音量，范围 0 到 1
const MASTER_VOLUME = 1
// 背景音乐线性音量，范围 0 到 1
const MUSIC_VOLUME = 0.6
// 放置炸弹的线性音量，范围 0 到 1
const PLACE_VOLUME = 0.8
// 爆炸白噪声的线性音量，范围 0 到 1
const BOOM_VOLUME = 0.5
// 爆炸低音的线性音量，范围 0 到 1
const BOOM_BASS_VOLUME = 0.34
// 拾取道具前半段的线性音量，范围 0 到 1
const ITEM_VOLUME = 0.16
// 拾取道具后半段的线性音量，范围 0 到 1
const ITEM_HIGH_VOLUME = 0.14
// 被击杀前半段的线性音量，范围 0 到 1
const DIE_VOLUME = 0.22
// 被击杀下沉音的线性音量，范围 0 到 1
const DIE_LOW_VOLUME = 0.26
// 落石的线性音量，范围 0 到 1
const STONE_VOLUME = 0.12
// 胜利旋律的线性音量，范围 0 到 1
const WIN_VOLUME = 0.18

// 按需创建浏览器音频上下文
function ensure() {
  if (ac) return ac
  const Ctx = window.AudioContext || window.webkitAudioContext
  if (!Ctx) return null
  ac = new Ctx()
  master = ac.createGain()
  master.gain.value = muted || !focused ? 0 : MASTER_VOLUME
  master.connect(ac.destination)
  return ac
}

// 生成一段白噪声缓冲
function noiseBuf(sec) {
  const len = Math.floor(ac.sampleRate * sec)
  const buf = ac.createBuffer(1, len, ac.sampleRate)
  const d = buf.getChannelData(0)
  for (let i = 0; i < len; i++) d[i] = (Math.random() * 2 - 1) * (1 - i / len)
  return buf
}

// 播放一段可选变调的短音
function tone(freq, dur, type, vol, slideTo, level = 1) {
  if (!(level > 0)) return
  if (!ensure()) return
  const o = ac.createOscillator()
  const g = ac.createGain()
  o.type = type || 'square'
  o.frequency.setValueAtTime(freq, ac.currentTime)
  if (slideTo) o.frequency.exponentialRampToValueAtTime(slideTo, ac.currentTime + dur)
  g.gain.setValueAtTime((vol || 0.2) * level, ac.currentTime)
  g.gain.exponentialRampToValueAtTime(0.0008, ac.currentTime + dur)
  o.connect(g)
  g.connect(master)
  o.start()
  o.stop(ac.currentTime + dur + 0.02)
}

// 创建当前背景音乐使用的增益节点
function startMusic() {
  if (!ensure() || musicTimer !== null) return
  musicGain = ac.createGain()
  musicGain.gain.value = MUSIC_VOLUME
  musicGain.connect(master)
  if (musicMode === 'lobby') playLobbyNote()
  else playGameNote()
}

// 停止循环旋律并释放背景音乐增益节点
function stopMusic() {
  if (musicTimer !== null) clearTimeout(musicTimer)
  musicTimer = null
  if (musicGain) musicGain.disconnect()
  musicGain = null
}

// 播放当前模式的一枚旋律音符
function playNote(freq, duration, type = 'triangle') {
  if (!(freq > 0) || !Number.isFinite(freq)) return
  const now = ac.currentTime
  if (focused) {
    const o = ac.createOscillator()
    const g = ac.createGain()
    o.type = type
    o.frequency.setValueAtTime(freq, now)
    g.gain.setValueAtTime(0.0001, now)
    g.gain.exponentialRampToValueAtTime(1, now + 0.025)
    g.gain.exponentialRampToValueAtTime(0.0001, now + duration)
    o.connect(g)
    g.connect(musicGain)
    o.start(now)
    o.stop(now + duration + 0.03)
  }
}

// 播放大厅的一次性短曲，播完后不再循环
function playLobbyNote() {
  if (!musicGain || musicMode !== 'lobby') return
  const ending = musicIndex === LOBBY_TRACK.length - 1
  playNote(LOBBY_TRACK[musicIndex], ending ? 0.52 : 0.16, 'square')
  musicIndex++
  if (musicIndex < LOBBY_TRACK.length) {
    musicTimer = setTimeout(playLobbyNote, 190)
    return
  }
  musicTimer = setTimeout(() => {
    if (musicMode !== 'lobby' || !musicGain) return
    musicTimer = null
    musicGain.disconnect()
    musicGain = null
  }, 560)
}

// 按当前主题和节拍循环播放游戏旋律
function playGameNote() {
  if (!musicGain || musicMode !== 'game') return
  const interval = musicFast ? 330 : 480
  const duration = musicFast ? 0.3 : 0.42
  const notes = MUSIC_TRACKS[musicTheme]
  playNote(notes[musicIndex], duration)
  musicIndex = (musicIndex + 1) % notes.length
  musicTimer = setTimeout(playGameNote, interval)
}

// 根据页面焦点暂停或恢复全部声音
function setAudioFocus(value) {
  focused = value
  if (master) master.gain.value = muted || !focused ? 0 : MASTER_VOLUME
}

export const sfx = {
  // 浏览器要求首次交互后才能出声
  unlock() {
    const context = ensure()
    if (!context) return
    const ready = context.state === 'suspended' ? context.resume() : Promise.resolve()
    ready.catch(() => {})
  },
  // 根据页面状态、棋盘主题和剩余时间控制背景音乐
  setMusic(mode, theme = 0, fast = false) {
    const nextMode = mode === 'lobby' || mode === 'game' ? mode : 'none'
    const nextTheme = Number.isInteger(theme) && theme >= 0 ? theme % MUSIC_TRACKS.length : 0
    const changed = musicMode !== nextMode || musicTheme !== nextTheme || musicFast !== fast
    musicMode = nextMode
    musicTheme = nextTheme
    musicFast = fast
    if (musicMode === 'none') {
      stopMusic()
      return
    }
    if (changed) {
      stopMusic()
      musicIndex = 0
    }
    startMusic()
  },
  // 切换背景音乐和音效的静音状态
  toggleMute() {
    muted = !muted
    localStorage.setItem('bm_muted', muted ? '1' : '0')
    if (master) master.gain.value = muted || !focused ? 0 : MASTER_VOLUME
    return muted
  },
  // 返回当前是否静音
  isMuted() {
    return muted
  },
  place(level) {
    tone(420, 0.08, 'square', PLACE_VOLUME, 180, level)
  },
  boom(level) {
    if (!(level > 0)) return
    if (!ensure()) return
    const src = ac.createBufferSource()
    src.buffer = noiseBuf(0.45)
    const lp = ac.createBiquadFilter()
    lp.type = 'lowpass'
    lp.frequency.setValueAtTime(1600, ac.currentTime)
    lp.frequency.exponentialRampToValueAtTime(180, ac.currentTime + 0.4)
    const g = ac.createGain()
    g.gain.setValueAtTime(BOOM_VOLUME * level, ac.currentTime)
    g.gain.exponentialRampToValueAtTime(0.001, ac.currentTime + 0.45)
    src.connect(lp)
    lp.connect(g)
    g.connect(master)
    src.start()
    tone(72, 0.32, 'sine', BOOM_BASS_VOLUME, 36, level)
  },
  item() {
    tone(660, 0.09, 'triangle', ITEM_VOLUME)
    setTimeout(() => tone(990, 0.12, 'triangle', ITEM_HIGH_VOLUME), 80)
  },
  die(level) {
    tone(220, 0.16, 'square', DIE_VOLUME, 110, level)
    setTimeout(() => tone(90, 0.48, 'sawtooth', DIE_LOW_VOLUME, 42, level), 110)
  },
  stone(level) {
    tone(140, 0.14, 'square', STONE_VOLUME, 60, level)
  },
  win() {
    const seq = [523, 659, 784, 1046]
    seq.forEach((f, i) => setTimeout(() => tone(f, 0.18, 'triangle', WIN_VOLUME), i * 130))
  }
}

document.addEventListener('visibilitychange', () => setAudioFocus(document.visibilityState === 'visible'))
window.addEventListener('blur', () => setAudioFocus(false))
window.addEventListener('focus', () => setAudioFocus(document.visibilityState === 'visible'))
