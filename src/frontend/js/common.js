// 前端各模块共用的 DOM 工具、展示常量和纯工具函数

// 按座位编号展示的主题颜色
export const SEAT_COLORS = ['#ff4d6d', '#4cc9ff', '#ffd166', '#7ce38b', '#c77dff', '#ff9f1c', '#00c2a8', '#f15bb5', '#90be6d', '#577590', '#f94144', '#43aa8b', '#f3722c', '#277da1', '#f9c74f', '#9b5de5']

// 按座位编号展示的颜色名称
export const SEAT_NAMES = ['红', '蓝', '黄', '绿', '紫', '橙', '青', '粉', '草', '灰', '赤', '碧', '焰', '海', '金', '星']

// 读取构建脚本注入入口页面的版本号
export const appVersion = document.querySelector('meta[name="app-version"]')?.content || ''

// 把剩余秒数格式化为分秒
export function formatGameTime(seconds) {
  return Math.floor(seconds / 60) + ':' + String(seconds % 60).padStart(2, '0')
}

// 使用安全 DOM API 创建元素及其子节点
export function h(tag, props, ...kids) {
  const el = document.createElement(tag)
  if (props) {
    for (const key of Object.keys(props)) {
      const value = props[key]
      if (value === null || value === undefined || value === false) continue
      if (key === 'class') el.className = value
      else if (key === 'text') el.textContent = value
      else if (key === 'style' && typeof value === 'object') Object.assign(el.style, value)
      else if (key === 'disabled') el.disabled = !!value
      else if (key.startsWith('on')) el.addEventListener(key.slice(2).toLowerCase(), value)
      else el.setAttribute(key, value)
    }
  }
  append(el, kids)
  return el
}

// 递归追加文本、节点或节点数组
function append(el, kids) {
  for (const kid of kids) {
    if (kid === null || kid === undefined || kid === false || kid === true) continue
    if (Array.isArray(kid)) append(el, kid)
    else el.append(kid instanceof Node ? kid : document.createTextNode(String(kid)))
  }
}

// 清空容器后重新填充
export function mount(el, ...kids) {
  el.replaceChildren()
  append(el, kids)
  return el
}

// 只在内容真的变了才写 DOM，HUD 每帧刷新时用得上
export function setText(el, text) {
  const value = String(text)
  if (el.textContent !== value) el.textContent = value
}

// 按布尔值切换一个 CSS 类
export function setClass(el, name, on) {
  el.classList.toggle(name, !!on)
}
