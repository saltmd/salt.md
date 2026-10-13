/* salt.md service worker: app-shell caching plus offline snapshots.
 *
 * Strategy:
 *  - Hashed /assets/* → cache-first (immutable by construction).
 *  - Navigations (index.html) → network-first, cache fallback, so the app
 *    shell opens offline but a deploy is picked up on the next online load.
 *  - Read-only API snapshots (GET /api/pages, /api/collections and their
 *    sub-paths) → network-first with cache fallback. These are the pages and
 *    collections the user has already opened; a stale copy with an on-screen
 *    "offline copy" badge beats no copy. Writes are never cached.
 *  - EVERYTHING else (/collab, /files, /mcp, /public) → network only.
 *    The CRDT and REST layers own their own consistency.
 */
const SHELL = 'salt-shell-v1';
const DATA = 'salt-data-v1';

// GETs safe to serve stale: single pages, the page tree, collection schemas
// and paged rows. Search, presence, audit, revisions and shares stay
// network-only — they are either live queries or security-sensitive.
// GETs safe to serve stale: the session, the workspace list, single pages,
// the page tree, collection schemas and paged rows. Search, presence, audit,
// revisions and shares stay network-only — they are either live queries or
// security-sensitive. The session entry is served stale only when the network
// fails outright (network-first); as soon as the server answers, the fresh
// account state wins, including a logged-out one.
function isSnapshotRequest(pathname) {
  if (pathname === '/api/me' || pathname === '/api/workspaces') return true;
  return (
    pathname === '/api/pages' ||
    pathname.startsWith('/api/pages/') ||
    pathname === '/api/collections' ||
    pathname.startsWith('/api/collections/') ||
    pathname === '/api/templates'
  );
}

self.addEventListener('install', (e) => {
  // Precache the shell so the first offline reload already works: without
  // this '/' only lands in the cache after a navigation the worker saw, and
  // an SPA session may never do one (login + clicks are pushState).
  e.waitUntil(
    caches
      .open(SHELL)
      .then((c) => c.add('/'))
      .catch(() => {
        /* first paint must never fail over a cache */
      })
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => k !== SHELL && k !== DATA).map((k) => caches.delete(k))),
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
    return;
  }

  // Offline snapshots of opened pages and collections: network-first so the
  // fresh server state wins whenever it answers, cache fallback so a reload
  // in a tunnel or on a plane still shows the last seen copy. Only GET, only
  // same-origin (checked above), never a write.
  if (isSnapshotRequest(url.pathname)) {
    e.respondWith(
      fetch(e.request)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(DATA).then((c) => c.put(e.request, copy));
          }
          return res;
        })
        .catch(() => caches.match(e.request)),
    );
  }
  // Everything else: default (network) — deliberately not cached.
});
