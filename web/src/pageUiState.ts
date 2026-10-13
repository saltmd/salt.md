// Per-page interface state that survives switching document tabs: which view
// of a collection was open, and where each page was scrolled.
//
// The document tabs in App.tsx remount the Editor on every switch
// (`key={currentId}`), so anything kept in component state alone — the view
// tab, the scroll position resets. This module is the memory instead.
//
// One JSON object per account, capped in size: the keys it replaces
// (`salt:view:`, `salt:scroll:`, `salt:hscroll:`) grew one entry per visited
// page and were shared between everyone using the browser. The old keys are
// swept on first use. View memory rebuilds itself.
//
// View ids persist across reloads (a choice, like the theme). Scroll positions
// also persist across reloads — localStorage is the store either way, and a
// reload lands back where you were rather than at the top.

const STORE_PREFIX = 'salt:ui:';
// Old unscoped keys, removed once the scoped store is in use.
const LEGACY_PREFIXES = ['salt:view:', 'salt:scroll:', 'salt:hscroll:'];
// Entries per map; oldest goes first. Enough for years of navigation, small
// enough that the store never matters.
const MAX_ENTRIES = 300;

interface UiStore {
  views: Record<string, string>;
  scrolls: Record<string, number>;
  hscrolls: Record<string, number>;
}

const emptyStore = (): UiStore => ({ views: {}, scrolls: {}, hscrolls: {} });

// The account whose memory this is. Null before sign-in: state is then kept
// in memory only, so a second person on the same browser never inherits the
// first person's scroll positions.
let account: string | null = null;
const memory = emptyStore();
let swept = false;

function storeKey(): string | null {
  return account ? STORE_PREFIX + account : null;
}

function load(): UiStore {
  const key = storeKey();
  if (!key) return memory;
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return emptyStore();
    const p = JSON.parse(raw) as Partial<UiStore>;
    return {
      views: p.views && typeof p.views === 'object' ? (p.views as Record<string, string>) : {},
      scrolls: p.scrolls && typeof p.scrolls === 'object' ? (p.scrolls as Record<string, number>) : {},
      hscrolls: p.hscrolls && typeof p.hscrolls === 'object' ? (p.hscrolls as Record<string, number>) : {},
    };
  } catch {
    return emptyStore();
  }
}

function save(store: UiStore): void {
  const key = storeKey();
  if (!key) return;
  try {
    localStorage.setItem(key, JSON.stringify(store));
  } catch {
    /* private mode, quota — state is a nice-to-have */
  }
}

function sweepLegacy(): void {
  if (swept) return;
  swept = true;
  try {
    const gone: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && LEGACY_PREFIXES.some((p) => k.startsWith(p))) gone.push(k);
    }
    gone.forEach((k) => localStorage.removeItem(k));
  } catch {
    /* best-effort */
  }
}

/** Whose memory to keep. Call on sign-in and sign-out; switching accounts
 *  switches the store with it. */
export function setUiAccount(id: string | null): void {
  account = id;
  if (id) sweepLegacy();
}

// LRU within the cap: touching a key moves it to the back, writing past the
// cap drops the front.
function put(map: Record<string, string>, key: string, value: string): void {
  delete map[key];
  map[key] = value;
  const keys = Object.keys(map);
  for (let i = 0; i < keys.length - MAX_ENTRIES; i++) delete map[keys[i]];
}

function putNum(map: Record<string, number>, key: string, value: number): void {
  delete map[key];
  map[key] = value;
  const keys = Object.keys(map);
  for (let i = 0; i < keys.length - MAX_ENTRIES; i++) delete map[keys[i]];
}

function drop(map: Record<string, unknown>, key: string): void {
  delete map[key];
}

// A horizontal scroller inside a collection view (the board columns, the wide
// table) is keyed by collection AND view: each view scrolls on its own.
const hkey = (collectionId: string, viewId: string) => `${collectionId}:${viewId}`;

/** The last open view of a collection, or null when never chosen. */
export function readViewId(collectionId: string): string | null {
  return load().views[collectionId] ?? null;
}

export function writeViewId(collectionId: string, viewId: string) {
  if (!viewId) return;
  const s = load();
  put(s.views, collectionId, viewId);
  save(s);
}

export function forgetViewId(collectionId: string) {
  const s = load();
  drop(s.views, collectionId);
  save(s);
}

export interface PageScroll {
  top: number;
}

/** The vertical scroll of a page's `.page-body`, or null. */
export function readPageScroll(pageId: string): PageScroll | null {
  const top = load().scrolls[pageId];
  return typeof top === 'number' && Number.isFinite(top) && top > 0 ? { top } : null;
}

export function writePageScroll(pageId: string, top: number) {
  if (!Number.isFinite(top) || top < 0) return;
  const s = load();
  putNum(s.scrolls, pageId, Math.round(top));
  save(s);
}

export function forgetPageScroll(pageId: string) {
  const s = load();
  drop(s.scrolls, pageId);
  save(s);
}

/** The horizontal scroll of one view of a collection, or null. */
export function readViewScroll(collectionId: string, viewId: string): number | null {
  const n = load().hscrolls[hkey(collectionId, viewId)];
  return typeof n === 'number' && Number.isFinite(n) && n > 0 ? n : null;
}

export function writeViewScroll(collectionId: string, viewId: string, left: number) {
  if (!viewId || !Number.isFinite(left) || left < 0) return;
  const s = load();
  putNum(s.hscrolls, hkey(collectionId, viewId), Math.round(left));
  save(s);
}

export function forgetViewScrolls(collectionId: string) {
  // Keys carry the view id, so drop them by prefix scan. Bounded anyway, but
  // forgetting with the collection keeps the store tight.
  const s = load();
  const prefix = `${collectionId}:`;
  for (const k of Object.keys(s.hscrolls)) {
    if (k.startsWith(prefix)) delete s.hscrolls[k];
  }
  save(s);
}
