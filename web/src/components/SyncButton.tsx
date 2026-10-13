import { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { checkForNewVersion, isDesktopApp, isDesktopPointer, isStandalone, requestRefresh } from '../pwa';
import { flush, onOutboxChange, pendingCount, pendingOps, type OutboxOp } from '../outbox';
import { plural, t } from '../i18n';

/** The answer to the missing reload button, for the two windows that have none,
 *  and the face of the offline outbox everywhere else.
 *
 *  On a phone the pull gesture covers this; on a desktop there is no gesture,
 *  and neither an installed app window nor the salt.md desktop app shows any
 *  browser chrome — a window that has been open since Tuesday has nothing at
 *  all to press. (The desktop app's View → Reload exists, but it starts the
 *  window over, and nobody goes looking in a menu for it.) So the button appears
 *  in exactly those two: in a browser tab the reload button is two centimetres
 *  away and a second one would be noise.
 *
 *  Unless there are queued offline writes. Then the button appears everywhere:
 *  a pending count nobody can see is a promise nobody can check, and the panel
 *  lists what is waiting so "will sync" names names. */
export default function SyncButton() {
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState(0);
  const [ops, setOps] = useState<OutboxOp[]>([]);
  const [panelOpen, setPanelOpen] = useState(false);

  useEffect(() => {
    const decide = () => setShow((isStandalone() || isDesktopApp()) && isDesktopPointer());
    decide();
    // Installing the app does not reload the window, so display-mode can change
    // under a running page.
    const mq = window.matchMedia('(display-mode: standalone)');
    mq.addEventListener('change', decide);
    return () => mq.removeEventListener('change', decide);
  }, []);

  useEffect(() => {
    let alive = true;
    void pendingCount().then((n) => alive && setPending(n));
    const stop = onOutboxChange((n) => {
      setPending(n);
      if (n === 0) setPanelOpen(false);
      if (n > 0) void pendingOps().then((list) => alive && setOps(list));
    });
    return () => {
      alive = false;
      stop();
    };
  }, []);

  if (!show && pending === 0) return null;

  const syncNow = () => {
    setBusy(true);
    void flush().then(() => {
      requestRefresh();
      void checkForNewVersion();
    });
    // Same reason as the pull gesture: long enough to be seen.
    window.setTimeout(() => setBusy(false), 650);
  };

  return (
    <div className="sync-wrap">
      <button
        className="icon-btn sync-btn"
        title={pending > 0 ? t('Sync now') : t('Fetch the latest changes')}
        aria-label={pending > 0 ? t('Sync now') : t('Fetch the latest changes')}
        disabled={busy}
        onClick={() => {
          if (pending > 0) {
            const next = !panelOpen;
            setPanelOpen(next);
            if (next) void pendingOps().then(setOps);
          }
          syncNow();
        }}
      >
        <RefreshCw size={16} className={busy ? 'ptr-spin' : ''} />
        {pending > 0 ? <span className="sync-badge">{pending}</span> : null}
      </button>
      {panelOpen && pending > 0 ? (
        <div className="sync-panel" role="status">
          <div className="sync-panel-title">
            {plural(pending, '{n} change waiting to sync', '{n} changes waiting to sync')}
          </div>
          <ul>
            {ops.slice(0, 8).map((op) => (
              <li key={op.key}>{op.label}</li>
            ))}
          </ul>
          {ops.length > 8 ? <div className="sync-panel-more">{t('…and {n} more', { n: ops.length - 8 })}</div> : null}
        </div>
      ) : null}
    </div>
  );
}
