import * as Y from 'yjs';
import {
  Awareness,
  applyAwarenessUpdate,
  encodeAwarenessUpdate,
  removeAwarenessStates,
} from 'y-protocols/awareness';
import { appendJournal, clearJournal, readJournal } from './offlineCache';

// Thin realtime provider for salt.md's relay protocol (see server/collab.go).
// The server replays the persisted doc, then relays updates. It never
// interprets CRDT data; compaction happens via {"snapshotRequest":seq}.

const FRAME_UPDATE = 0;
const FRAME_AWARENESS = 1;
const FRAME_SNAPSHOT = 2;

export class SaltProvider {
  doc: Y.Doc;
  awareness: Awareness;
  synced = false;
  isNew = false;
  // The generation of the document this doc descends from (server/docgen.go):
  // that of the body the page was loaded with, which a new live document is
  // seeded from. It goes with every connect and every autosave, so the server
  // can turn both away once a write from outside the editor has replaced the
  // document: a tab that slept through the 4001 must not push its old copy
  // back in, and one that loaded the body just before a rewrite must not seed
  // the live document from it.
  gen: number | null;
  // Set once the server has replaced the document under us. This doc is stale
  // from that moment on, and nothing derived from it may reach the server.
  replacedDoc = false;

  readonly pageId: string;

  private ws: WebSocket | null = null;
  private closed = false;
  private everSynced = false;
  private reconnectAttempt = 0;
  private onSynced: (isNew: boolean) => void;
  private onReset: () => void;
  // Dead-socket watchdog. Proxies kill idle WebSockets without telling the
  // browser (half-open TCP): the socket looks OPEN but nothing ever arrives
  // again — the exact "I only see his edits after a reload" bug. The server
  // sends {"ping":true} every 20s, so a healthy socket is never silent for
  // long; if it is, we force-close and let the reconnect logic take over.
  private lastMsg = 0;
  private connectStarted = 0;
  private watchdog: ReturnType<typeof setInterval>;

  constructor(
    pageId: string,
    gen: number | null,
    onSynced: (isNew: boolean) => void,
    onReset: () => void,
    opts?: { initialUpdate?: Uint8Array | null; onLocalUpdate?: (update: Uint8Array) => void },
  ) {
    this.pageId = pageId;
    this.gen = gen;
    this.onSynced = onSynced;
    this.onReset = onReset;
    this.doc = new Y.Doc();
    this.awareness = new Awareness(this.doc);
    // Offline snapshot taken while disconnected: apply before connecting so
    // the local edits survive a reload. The snapshot itself is never pushed
    // back: only the journal below travels, so a server-side rewrite
    // (restore, agent edit, import) is never reverted by a stale copy.
    if (opts?.initialUpdate && opts.initialUpdate.length > 0) {
      try {
        Y.applyUpdate(this.doc, opts.initialUpdate, 'offline');
      } catch {
        /* a corrupt snapshot must not block the fresh server state */
      }
    }

    this.doc.on('update', (update: Uint8Array, origin: unknown) => {
      if (origin === 'remote') return;
      this.send(FRAME_UPDATE, update);
      // While the channel is down the send above vanishes: journal the update
      // so the first sync can replay exactly what was typed, no more. (When
      // synced over a live socket, updates already reached the server.)
      // The socket check matters too: a half-dead socket still reads OPEN
      // until the watchdog kills it, and synced flips only afterwards.
      if (!this.synced || this.ws?.readyState !== WebSocket.OPEN) {
        const copy = new Uint8Array(update.length);
        copy.set(update);
        void appendJournal(this.pageId, copy);
      }
      // Persist every local change (origin 'offline' included): the tab may be
      // closed while disconnected, and the server replay is the only other copy.
      try {
        opts?.onLocalUpdate?.(update);
      } catch {
        /* persistence is best-effort */
      }
    });
    this.awareness.on(
      'update',
      ({ added, updated, removed }: { added: number[]; updated: number[]; removed: number[] }) => {
        // Announce only our OWN client. 'update' also fires for applied
        // REMOTE entries (the other peers' ~15s renewals, for one); without
        // this filter, foreign clientIDs would be echoed back to the server,
        // which would attribute them to this connection — and on disconnect
        // would clear away the presence of LIVE users.
        const me = this.doc.clientID;
        const ids = added.concat(updated, removed).filter((id) => id === me);
        if (ids.length === 0) return;
        this.send(FRAME_AWARENESS, encodeAwarenessUpdate(this.awareness, ids));
      },
    );
    this.watchdog = setInterval(() => {
      const ws = this.ws;
      if (!ws) return;
      const now = Date.now();
      if (ws.readyState === WebSocket.OPEN && now - this.lastMsg > 55_000) {
        this.dropConnection(ws); // silent for >2 missed server pings: presumed dead
      } else if (ws.readyState === WebSocket.CONNECTING && now - this.connectStarted > 15_000) {
        this.dropConnection(ws); // stuck handshake (broken proxy); aborts and retries
      }
    }, 10_000);
    this.connect();
  }

  // Gives up on a socket judged dead, immediately. ws.close() alone is not
  // enough: against a half-dead peer the close handshake runs 20–60s depending
  // on the browser before onclose fires — and the user would stay offline that
  // whole time. So: detach the handlers, close, reconnect straight away.
  private dropConnection(ws: WebSocket) {
    ws.onclose = null;
    ws.onmessage = null;
    ws.onopen = null;
    try {
      ws.close();
    } catch {
      /* nothing — the socket is lost anyway */
    }
    this.handleClosed(null);
  }

  private connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const gen = this.gen === null ? '' : `?gen=${this.gen}`;
    const ws = new WebSocket(`${proto}://${location.host}/collab/${this.pageId}${gen}`);
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    this.connectStarted = Date.now();
    this.lastMsg = Date.now();

    ws.onopen = () => {
      this.lastMsg = Date.now();
    };

    ws.onmessage = (e: MessageEvent) => {
      this.lastMsg = Date.now();
      if (typeof e.data === 'string') {
        const msg = JSON.parse(e.data) as Record<string, unknown>;
        if ('ping' in msg) {
          return; // keepalive traffic; receiving it is all the watchdog needs
        }
        if ('isNew' in msg) {
          // isNew only counts for the very first sync; after a reconnect the
          // local doc already has state and must not be re-seeded.
          if (!this.everSynced) this.isNew = msg.isNew === true;
        } else if ('synced' in msg) {
          this.synced = true;
          this.reconnectAttempt = 0; // a clean sync resets the backoff
          // Replay the offline journal, if any: exactly the edits typed while
          // disconnected, merged into the server's current state. Never the
          // whole local doc: pushing that would resurrect whatever the
          // server rewrote meanwhile (a restore, an agent edit, an import).
          void readJournal(this.pageId).then((journal) => {
            for (const u of journal) this.send(FRAME_UPDATE, u);
            if (journal.length > 0) void clearJournal(this.pageId);
          });
          const first = !this.everSynced;
          this.everSynced = true;
          if (first) this.onSynced(this.isNew);
          // Re-announce presence after (re)connects. When the old connection
          // died the server broadcast our removal with clock+1 — a plain
          // re-announce (same clock) would be discarded as stale by every peer
          // and we would stay invisible for up to 30s. Calling setLocalState
          // twice bumps the clock safely past the removal; each set fires the
          // update listener, which sends the announce.
          const st = this.awareness.getLocalState();
          if (st !== null) {
            this.awareness.setLocalState(st);
            this.awareness.setLocalState(st);
          } else {
            this.send(
              FRAME_AWARENESS,
              encodeAwarenessUpdate(this.awareness, [this.doc.clientID]),
            );
          }
        } else if ('snapshotRequest' in msg) {
          const seq = BigInt(msg.snapshotRequest as number);
          const snap = Y.encodeStateAsUpdate(this.doc);
          const buf = new Uint8Array(9 + snap.length);
          buf[0] = FRAME_SNAPSHOT;
          new DataView(buf.buffer).setBigUint64(1, seq);
          buf.set(snap, 9);
          if (ws.readyState === WebSocket.OPEN) ws.send(buf);
        }
        return;
      }
      const data = new Uint8Array(e.data as ArrayBuffer);
      if (data.length < 1) return;
      if (data[0] === FRAME_UPDATE) {
        Y.applyUpdate(this.doc, data.subarray(1), 'remote');
      } else if (data[0] === FRAME_AWARENESS) {
        applyAwarenessUpdate(this.awareness, data.subarray(1), 'remote');
      }
    };

    ws.onclose = (e: CloseEvent) => this.handleClosed(e);
  }

  // Shared exit for onclose and for the watchdog kill (which has no event).
  private handleClosed(e: CloseEvent | null) {
    this.synced = false;
    // Peers from the dead connection are unknowable now; drop them locally
    // instead of showing stale avatars. The server replays live presence
    // on reconnect, so real peers reappear immediately.
    removeAwarenessStates(
      this.awareness,
      [...this.awareness.getStates().keys()].filter((id) => id !== this.doc.clientID),
      'disconnect',
    );
    if (this.closed) return;
    if (e?.code === 4001) {
      // Server-side doc was reset (API write): local state is stale.
      this.replaced();
      return;
    }
    // Exponential backoff with full jitter, capped at 30s, so a flapping or
    // downed server isn't hammered by every open tab every 1.5s.
    const base = Math.min(30000, 1000 * 2 ** this.reconnectAttempt);
    this.reconnectAttempt++;
    const delay = Math.round(base / 2 + Math.random() * (base / 2));
    setTimeout(() => {
      if (!this.closed) this.connect();
    }, delay);
  }

  private send(type: number, payload: Uint8Array) {
    if (this.replacedDoc) return;
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    const buf = new Uint8Array(1 + payload.length);
    buf[0] = type;
    buf.set(payload, 1);
    this.ws.send(buf);
  }

  /** The document was replaced from outside the editor: told by a 4001, or by
   *  an autosave the server refused as stale. Fence this doc first, then let
   *  the page remount onto the new one. */
  replaced() {
    if (this.replacedDoc || this.closed) return;
    this.replacedDoc = true;
    this.onReset();
  }

  destroy() {
    if (this.closed) return; // StrictMode double-mount / repeated cleanup
    this.closed = true;
    clearInterval(this.watchdog);
    removeAwarenessStates(this.awareness, [this.doc.clientID], 'destroy');
    this.ws?.close();
    this.doc.destroy();
  }
}
