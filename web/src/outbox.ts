// Outbox: writes made while offline, replayed in order when back online.
//
// Every write site in the UI is optimistic already (state updates first, the
// request follows). On a network failure the request used to die in a catch
// with a toast; now the intent is journaled here and replayed by flush().
// flush() runs on the 'online' event, on boot, and from the sync button.
//
// Temp ids: a page created offline gets `temp-<uuid>` and renders immediately
// from the snapshot cache. Ops referencing it (its own content, comments) are
// remapped to the real id once the create replays. UI listens for
// REMAP_EVENT to swap tabs and tree entries.

import { api, ApiError } from './api';
import { t } from './i18n';
import { toast } from './toast';
import {
  deleteOp,
  dropPage,
  dropYDoc,
  getPage,
  getYDoc,
  listOps,
  putOp,
  putPage,
  putYDoc,
  type OutboxOp,
} from './offlineCache';
export type { OutboxOp };

export const OUTBOX_EVENT = 'salt:outbox';
export const REMAP_EVENT = 'salt:page-mapped';

export function newTempId(): string {
  const uuid =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${Math.floor(Math.random() * 1e9).toString(36)}`;
  return `temp-${uuid}`;
}

export function isTempId(id: string): boolean {
  return id.startsWith('temp-');
}

/** A fetch that never reached the server (offline, DNS, refused). Server
 *  answers — even errors — arrive as ApiError instead. */
export function isOfflineError(e: unknown): boolean {
  return e instanceof TypeError;
}

function notify(pending: number): void {
  window.dispatchEvent(new CustomEvent(OUTBOX_EVENT, { detail: pending }));
}

async function refreshCount(): Promise<number> {
  const n = (await listOps()).length;
  notify(n);
  return n;
}

export function onOutboxChange(fn: (pending: number) => void): () => void {
  const h = (e: Event) => fn((e as CustomEvent<number>).detail);
  window.addEventListener(OUTBOX_EVENT, h);
  return () => window.removeEventListener(OUTBOX_EVENT, h);
}

export async function pendingCount(): Promise<number> {
  return (await listOps()).length;
}

export async function pendingOps(): Promise<OutboxOp[]> {
  return listOps();
}

export async function enqueue(op: Omit<OutboxOp, 'key' | 'createdAt' | 'attempts'>): Promise<void> {
  const key =
    typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${Math.floor(Math.random() * 1e9).toString(36)}`;
  await putOp({ ...op, key, createdAt: Date.now(), attempts: 0 });
  await refreshCount();
}

export interface FlushResult {
  synced: number;
  dropped: number;
  kept: number;
}

let flushing = false;

/** Replay queued ops in creation order. Stops at the first op that still
 *  cannot reach the server; drops ops the server refuses (gone, forbidden)
 *  with a toast so nothing waits forever. Anything else stays queued no
 *  matter how long it takes: silently dropping writes after five tries loses
 *  user data, and the pending list (see SyncButton) says what still waits. */
export async function flush(): Promise<FlushResult> {
  if (flushing) return { synced: 0, dropped: 0, kept: await pendingCount() };
  flushing = true;
  const res: FlushResult = { synced: 0, dropped: 0, kept: 0 };
  try {
    const remap = new Map<string, string>();
    const resolve = (id: string) => remap.get(id) ?? id;
    for (;;) {
      const ops = await listOps();
      if (ops.length === 0) break;
      const op = ops[0];
      try {
        // eslint-disable-next-line no-await-in-loop
        await replay(op, resolve, remap);
        // eslint-disable-next-line no-await-in-loop
        await deleteOp(op.key);
        res.synced++;
      } catch (e) {
        if (isOfflineError(e)) break; // still offline: keep the rest
        if (e instanceof ApiError && (e.status === 404 || e.status === 403 || e.status === 400)) {
          // The target is gone or refuses us (deleted parent, revoked access,
          // replaced page): replaying forever helps nothing.
          // eslint-disable-next-line no-await-in-loop
          await deleteOp(op.key);
          res.dropped++;
          toast(t('Could not sync “{label}”: {reason}', { label: op.label, reason: e.message }));
          continue;
        }
        // A maybe: the request may or may not have landed (a timeout after
        // the server committed, a 500 mid-write). Creates carry the op key
        // as the idempotency key, so replaying is safe; content and meta
        // patches converge by construction. The attempts count is display
        // only (see SyncButton): the op itself is never dropped here.
        op.attempts++;
        // eslint-disable-next-line no-await-in-loop
        await putOp(op);
        break;
      }
    }
  } finally {
    flushing = false;
    res.kept = await refreshCount();
  }
  return res;
}

async function replay(
  op: OutboxOp,
  resolve: (id: string) => string,
  remap: Map<string, string>,
): Promise<void> {
  const pageId = resolve(op.pageId);
  switch (op.kind) {
    case 'create-page': {
      const p = op.payload as {
        title: string;
        type: 'doc' | 'collection';
        props?: Record<string, unknown>;
        workspaceId?: string;
      };
      const parentId = op.parentId == null ? null : resolve(op.parentId);
      // The op key doubles as the idempotency key: two tabs flushing the
      // same queue (or one tab retrying) create the page exactly once.
      const created = await api.createPage(parentId, p.title, p.type, p.props, p.workspaceId, op.key);
      remap.set(op.pageId, created.id);
      // Migrate the offline state onto the real id: snapshot, CRDT edits,
      // then tell the UI to swap tabs and tree entries.
      const snap = await getYDoc(op.pageId);
      if (snap && snap.length > 0) await putYDoc(created.id, snap);
      await dropYDoc(op.pageId);
      const cached = await getPage(op.pageId);
      if (cached) {
        await putPage({ ...cached, id: created.id });
        await dropPage(op.pageId);
      }
      window.dispatchEvent(new CustomEvent(REMAP_EVENT, { detail: { tempId: op.pageId, realId: created.id } }));
      break;
    }
    case 'update-page': {
      const p = op.payload as { patch: Record<string, unknown>; materialize?: boolean };
      await api.updatePage(pageId, p.patch, p.materialize ? { materialize: true } : undefined);
      break;
    }
    case 'comment': {
      const p = op.payload as { body: string; blockId?: string };
      await api.createComment(pageId, p.body, p.blockId ?? '');
      break;
    }
    case 'note': {
      const p = op.payload as { body: string };
      await api.addNote(pageId, p.body);
      break;
    }
  }
}

/** updatePage with an offline fallback: temp ids never reach the network
 *  (there is nothing with that id server-side yet), anything else tries the
 *  server first and journals on a network failure. Returns true when the
 *  write landed or was queued, false when it failed hard (caller toasts). */
export async function savePageUpdate(
  pageId: string,
  patch: Record<string, unknown>,
  opts: { materialize?: boolean; label?: string } = {},
): Promise<boolean> {
  const label = opts.label ?? t('Change');
  if (isTempId(pageId)) {
    await enqueue({ kind: 'update-page', pageId, payload: { patch, materialize: opts.materialize }, label });
    toast(t('Will sync when the connection is back'));
    return true;
  }
  try {
    await api.updatePage(pageId, patch, opts.materialize ? { materialize: true } : undefined);
    return true;
  } catch (e) {
    if (isOfflineError(e)) {
      await enqueue({ kind: 'update-page', pageId, payload: { patch, materialize: opts.materialize }, label });
      toast(t('Will sync when the connection is back'));
      return true;
    }
    return false;
  }
}

/** Registered once from App: flush on reconnect and shortly after boot, so
 *  ops queued in a previous session replay without a click. */
export function initOutbox(): void {
  window.addEventListener('online', () => {
    void flush();
  });
  window.setTimeout(() => {
    void flush();
  }, 3000);
}
