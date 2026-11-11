import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)

/**
 * Service worker 只负责离线兜底；升级时必须让用户立刻看到新界面。
 * 这里做三件事：不缓存 sw 脚本本身、有 waiting 就催它接管、
 * 控制权变化后自动刷新一次（用 sessionStorage 防抖，避免刷新循环）。
 */
if ('serviceWorker' in navigator && location.protocol !== 'file:') {
  window.addEventListener('load', () => {
    navigator.serviceWorker
      .register('/sw.js', { updateViaCache: 'none' })
      .then((reg) => {
        // 首次安装 / 有新版本在等待
        reg.addEventListener('updatefound', () => {
          const sw = reg.installing
          sw?.addEventListener('statechange', () => {
            if (sw.state === 'installed' && navigator.serviceWorker.controller) {
              sw.postMessage('skip-waiting')
            }
          })
        })
        // 每次打开都主动查一次新版本
        void reg.update().catch(() => undefined)
        if (reg.waiting) reg.waiting.postMessage('skip-waiting')
      })
      .catch(() => {
        /* 离线缓存是增强项，失败不影响使用 */
      })

    let reloaded = false
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (reloaded) return
      if (sessionStorage.getItem('zx.sw-reloaded') === '1') return
      reloaded = true
      sessionStorage.setItem('zx.sw-reloaded', '1')
      location.reload()
    })
    sessionStorage.removeItem('zx.sw-reloaded')
  })
}
