/* 子欣互传 service worker
 *
 * 策略要点（升级后必须能立刻看到新界面）：
 *   - 页面导航（HTML）：**网络优先**，断网才回落到缓存 —— 否则升级后浏览器会一直
 *     拿着上一版的 index.html，用户看到旧品牌、旧功能（这个坑踩过）。
 *   - 带 hash 的静态资源：缓存优先（文件名变了就是新文件，不会串版）。
 *   - /api/* 永不缓存。
 */
const CACHE = 'zx-shell-v3'
const SHELL = ['/', '/index.html', '/manifest.webmanifest']

self.addEventListener('install', (e) => {
  e.waitUntil(
    caches
      .open(CACHE)
      .then((c) => c.addAll(SHELL))
      .catch(() => undefined)
      .then(() => self.skipWaiting()),
  )
})

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', (e) => {
  const req = e.request
  const url = new URL(req.url)
  if (req.method !== 'GET') return
  if (url.pathname.startsWith('/api/')) return
  if (url.origin !== self.location.origin) return

  // 导航请求：网络优先，服务端不可用才用缓存
  if (req.mode === 'navigate') {
    e.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put('/index.html', copy)).catch(() => undefined)
          return res
        })
        .catch(() => caches.match('/index.html').then((hit) => hit || Response.error())),
    )
    return
  }

  // 静态资源：缓存优先
  e.respondWith(
    caches.match(req).then((hit) => {
      if (hit) return hit
      return fetch(req).then((res) => {
        if (res.ok && res.type === 'basic') {
          const copy = res.clone()
          caches.open(CACHE).then((c) => c.put(req, copy)).catch(() => undefined)
        }
        return res
      })
    }),
  )
})

// 页面可以主动催更：新版本就绪时立即接管
self.addEventListener('message', (e) => {
  if (e.data === 'skip-waiting') self.skipWaiting()
})
