// Offline snapshots of pages and collections the user has already opened.
//
// Two layers work together: the service worker keeps raw GET responses for
// /api/pages and /api/collections in the Cache API, and this module keeps the
// parsed snapshots in IndexedDB so the UI can render them without a network
// round trip. Both layers are best-effort: private mode or missing IndexedDB
// degrades to online-only, never to a crash.

import type { CollectionConfig, Page } from './types';

export interface CachedCollection {
  key: string;
  config: CollectionConfig;
  rows: {
    id: string;
    title: string;
    icon: string;
    cover: string;
    props: Record<string, unknown>;
    position: number;
    tags?: string[];
  }[];
  total: number;
  savedAt: number;
}

interface CachedPage {
  id: string;
  page: Page;
  savedAt: number;
}

export interface OutboxOp {
  key: string;
  kind: 'create-page' | 'update-page' | 'comment' | 'note';
  /** Target page; may be a temp id (see newTempId in outbox.ts). */
  pageId: string;
  /** create-page only: parent, possibly a temp id too. */
  parentId?: string | null;
  payload: Record<string, unknown>;
  /** Human-readable snapshot for the pending list. */
  label: string;
  createdAt: number;
  attempts: number;
}

interface CachedYDoc {
  id: string;
  update: ArrayBuffer;
  savedAt: number;
}

interface JournalEntry {
  id: string; // page id
  updates: ArrayBuffer[];
  savedAt: number;
}

// Offline data belongs to the account, not the browser: the database name
// carries the account id, so a second person signing in on the same browser
// never sees the first person's pages, rows or queued writes. Null while
// signed out: nothing is stored then.
let accountId: string | null = null;

/** Whose offline data to keep. Call on sign-in, switch and sign-out. */
export function setOfflineAccount(id: string | null): void {
  if (id !== accountId) {
    accountId = id;
    if (dbPromise) {
      void dbPromise.then((d) => {
        try {
          d?.close();
        } catch {
          /* best-effort */
        }
      });
      dbPromise = null;
    }
  }
  sweepLegacy();
}

export async function clearOfflineData(): Promise<void> {
  const name = DB_NAME + (accountId ? `-${accountId}` : '');
  setOfflineAccount(null);
  try {
    indexedDB.deleteDatabase('salt-offline-v1');
    indexedDB.deleteDatabase(name);
  } catch {
    /* best-effort */
  }
  try {
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k.startsWith('salt-data')).map((k) => caches.delete(k)));
  } catch {
    /* best-effort */
  }
}

const DB_NAME = 'salt-offline-v1';
const DB_VERSION = 3;
const MAX_PAGES = 100;
const MAX_COLLECTIONS = 50;
const MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000;

function dbName(): string {
  // Legacy unscoped database: left in place for the one-time sweep below,
  // never written anymore.
  return accountId ? `${DB_NAME}-${accountId}` : DB_NAME;
}

function idb(): Promise<IDBDatabase | null> {
  return new Promise((resolve) => {
    try {
      if (typeof indexedDB === 'undefined') {
        resolve(null);
        return;
      }
      const req = indexedDB.open(dbName(), DB_VERSION);
      req.onupgradeneeded = () => {
        const db = req.result;
        if (!db.objectStoreNames.contains('pages')) db.createObjectStore('pages', { keyPath: 'id' });
        if (!db.objectStoreNames.contains('collections')) {
          db.createObjectStore('collections', { keyPath: 'key' });
        }
        if (!db.objectStoreNames.contains('ydocs')) db.createObjectStore('ydocs', { keyPath: 'id' });
        if (!db.objectStoreNames.contains('outbox')) db.createObjectStore('outbox', { keyPath: 'key' });
        if (!db.objectStoreNames.contains('yjournal')) db.createObjectStore('yjournal', { keyPath: 'id' });
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
      req.onblocked = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

let dbPromise: Promise<IDBDatabase | null> | null = null;
function db(): Promise<IDBDatabase | null> {
  if (!dbPromise) dbPromise = idb();
  return dbPromise;
}

// One-time sweep of the legacy unscoped database: entries written before
// account scoping belong to whoever used the browser first and must not sit
// around for the next account to trip over.
let sweptLegacy = false;
function sweepLegacy(): void {
  if (sweptLegacy) return;
  sweptLegacy = true;
  try {
    indexedDB.deleteDatabase(DB_NAME);
  } catch {
    /* best-effort */
  }
}

function tx<T>(store: string, mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return db().then(
    (d) =>
      new Promise<T>((resolve, reject) => {
        if (!d) {
          reject(new Error('no idb'));
          return;
        }
        try {
          const t = d.transaction(store, mode);
          const req = fn(t.objectStore(store));
          req.onsuccess = () => resolve(req.result);
          req.onerror = () => reject(req.error ?? new Error('idb failed'));
        } catch (e) {
          reject(e instanceof Error ? e : new Error('idb failed'));
        }
      }),
  );
}

function prune(store: string, limit: number): void {
  // Evict oldest entries past the limit. Fire-and-forget: the cache must never
  // block rendering or break the app when quota is tight.
  void db().then((d) => {
    if (!d) return;
    try {
      const t = d.transaction(store, 'readwrite');
      const s = t.objectStore(store);
      const all = s.getAll();
      all.onsuccess = () => {
        const rows = (all.result ?? []) as { savedAt?: number; id?: string; key?: string }[];
        if (rows.length <= limit) return;
        rows
          .sort((a, b) => (a.savedAt ?? 0) - (b.savedAt ?? 0))
          .slice(0, rows.length - limit)
          .forEach((r) => {
            const k = (r.id ?? r.key) as string;
            if (k) s.delete(k);
          });
      };
    } catch {
      /* cache eviction is best-effort */
    }
  });
}

function fresh(savedAt: number): boolean {
  return Date.now() - savedAt < MAX_AGE_MS;
}

export async function putPage(page: Page): Promise<void> {
  try {
    const entry: CachedPage = { id: page.id, page, savedAt: Date.now() };
    await tx('pages', 'readwrite', (s) => s.put(entry));
    prune('pages', MAX_PAGES);
  } catch {
    /* offline cache is a nice-to-have; never break the app over it */
  }
}

export async function getPage(id: string): Promise<Page | null> {
  try {
    const entry = await tx<CachedPage | undefined>('pages', 'readonly', (s) => s.get(id));
    if (!entry || !fresh(entry.savedAt)) return null;
    return entry.page;
  } catch {
    return null;
  }
}

export function collectionKey(collectionId: string, filterKey: string): string {
  return `${collectionId}|${filterKey}`;
}

export async function putCollectionSnapshot(snap: Omit<CachedCollection, 'savedAt'>): Promise<void> {
  try {
    await tx('collections', 'readwrite', (s) => s.put({ ...snap, savedAt: Date.now() }));
    prune('collections', MAX_COLLECTIONS);
  } catch {
    /* best-effort */
  }
}

export async function getCollectionSnapshot(key: string): Promise<CachedCollection | null> {
  try {
    const entry = await tx<CachedCollection | undefined>('collections', 'readonly', (s) => s.get(key));
    if (!entry || !fresh(entry.savedAt)) return null;
    return entry;
  } catch {
    return null;
  }
}

export async function putYDoc(pageId: string, update: Uint8Array): Promise<void> {
  try {
    // Copy: the Yjs buffer is reused internally and must not be stored by view.
    const copy = new Uint8Array(update.length);
    copy.set(update);
    const entry: CachedYDoc = { id: pageId, update: copy.buffer, savedAt: Date.now() };
    await tx('ydocs', 'readwrite', (s) => s.put(entry));
  } catch {
    /* best-effort */
  }
}

export async function getYDoc(pageId: string): Promise<Uint8Array | null> {
  try {
    const entry = await tx<CachedYDoc | undefined>('ydocs', 'readonly', (s) => s.get(pageId));
    if (!entry || !fresh(entry.savedAt)) return null;
    return new Uint8Array(entry.update);
  } catch {
    return null;
  }
}

export async function dropYDoc(pageId: string): Promise<void> {
  try {
    await tx('ydocs', 'readwrite', (s) => s.delete(pageId));
  } catch {
    /* best-effort */
  }
}

// The offline journal: incremental Yjs updates typed while the channel was
// down, in order. The first sync replays exactly these, never a full state
// push, which would resurrect whatever the server rewrote meanwhile. Capped:
// an unbounded journal is a leak with a trench coat.
const MAX_JOURNAL = 1000;

export async function appendJournal(pageId: string, update: Uint8Array): Promise<void> {
  try {
    const copy = new Uint8Array(update.length);
    copy.set(update);
    const entry = await tx<JournalEntry | undefined>('yjournal', 'readonly', (s) => s.get(pageId));
    const updates = entry?.updates ?? [];
    updates.push(copy.buffer);
    while (updates.length > MAX_JOURNAL) updates.shift();
    await tx('yjournal', 'readwrite', (s) => s.put({ id: pageId, updates, savedAt: Date.now() } satisfies JournalEntry));
  } catch {
    /* best-effort */
  }
}

export async function readJournal(pageId: string): Promise<Uint8Array[]> {
  try {
    const entry = await tx<JournalEntry | undefined>('yjournal', 'readonly', (s) => s.get(pageId));
    return (entry?.updates ?? []).map((b) => new Uint8Array(b));
  } catch {
    return [];
  }
}

export async function clearJournal(pageId: string): Promise<void> {
  try {
    await tx('yjournal', 'readwrite', (s) => s.delete(pageId));
  } catch {
    /* best-effort */
  }
}

export async function dropPage(id: string): Promise<void> {
  try {
    await tx('pages', 'readwrite', (s) => s.delete(id));
  } catch {
    /* best-effort */
  }
}

export async function putOp(op: OutboxOp): Promise<void> {
  try {
    await tx('outbox', 'readwrite', (s) => s.put(op));
  } catch {
    /* the write already happened optimistically on screen; losing the queue
       entry only means it will not replay — surface nothing here, the caller
       toasts */
  }
}

export async function deleteOp(key: string): Promise<void> {
  try {
    await tx('outbox', 'readwrite', (s) => s.delete(key));
  } catch {
    /* best-effort */
  }
}

export async function listOps(): Promise<OutboxOp[]> {
  try {
    const ops = await tx<OutboxOp[]>('outbox', 'readonly', (s) => s.getAll());
    return (ops ?? []).sort((a, b) => a.createdAt - b.createdAt);
  } catch {
    return [];
  }
}
