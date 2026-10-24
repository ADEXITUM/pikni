// Оболочка кассы без связи: страницы — сначала из сети, при ошибке из кеша; файлы сборки
// неизменны (в имени хеш), поэтому берутся из кеша. Запросы к API не кешируются никогда.
const CACHE = 'pikni-pos-v1'

self.addEventListener('install', (event) => {
  event.waitUntil(caches.open(CACHE).then((c) => c.add('/pos/')))
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)))),
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (event.request.method !== 'GET' || url.pathname.startsWith('/api/')) return

  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request)
        .then((res) => {
          const copy = res.clone()
          void caches.open(CACHE).then((c) => c.put('/pos/', copy))
          return res
        })
        .catch(() => caches.match('/pos/')),
    )
    return
  }
  event.respondWith(
    caches.match(event.request).then(
      (hit) =>
        hit ??
        fetch(event.request).then((res) => {
          if (res.ok) {
            const copy = res.clone()
            void caches.open(CACHE).then((c) => c.put(event.request, copy))
          }
          return res
        }),
    ),
  )
})
