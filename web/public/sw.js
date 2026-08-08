/* salt.md service worker: app-shell caching only.
 *
 * Strategy:
 *  - Hashed /assets/* → cache-first (immutable by construction).
 *  - Navigations (index.html) → network-first, cache fallback, so the app
 *    shell opens offline but a deploy is picked up on the next online load.
 *  - EVERYTHING else (/api, /collab, /files, /mcp, /public) → network only.
 *    Caching API responses would serve stale user data; the CRDT and REST
 *    layers own their own consistency.
 */
const SHELL = 'salt-shell-v1';

self.addEventListener('install', (e) => {
  self.skipWaiting();
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== SHELL).map((k) => caches.delete(k))),
    ).then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin) return;

  // Immutable hashed assets: cache-first.
  if (url.pathname.startsWith('/assets/')) {
    e.respondWith(
      caches.open(SHELL).then(async (cache) => {
        const hit = await cache.match(e.request);
        if (hit) return hit;
        const res = await fetch(e.request);
        if (res.ok) cache.put(e.request, res.clone());
        return res;
      }),
    );
    return;
  }

  // App-shell navigations: network-first with cache fallback. Server-rendered
  // documents (share pages, ICS, API, uploaded files) are NOT the app shell —
  // caching their HTML under the '/' key would poison the offline shell, so we
  // only ever store a genuine React-app navigation there.
  if (e.request.mode === 'navigate') {
    const isServerDoc = /^\/(public|ics|api|files|collab|mcp)(\/|$)/.test(url.pathname);
    e.respondWith(
      fetch(e.request)
        .then((res) => {
          if (res.ok && !isServerDoc) {
            const copy = res.clone();
            caches.open(SHELL).then((c) => c.put('/', copy));
          }
          return res;
        })
        .catch(() => caches.match('/')),
    );
  }
  // Everything else: default (network) — deliberately not cached.
});
