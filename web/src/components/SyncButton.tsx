import { useEffect, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { checkForNewVersion, isDesktopApp, isDesktopPointer, isStandalone, requestRefresh } from '../pwa';
import { t } from '../i18n';

/** The answer to the missing reload button, for the two windows that have none.
 *
 *  On a phone the pull gesture covers this; on a desktop there is no gesture,
 *  and neither an installed app window nor the salt.md desktop app shows any
 *  browser chrome — a window that has been open since Tuesday has nothing at
 *  all to press. (The desktop app's View → Reload exists, but it starts the
 *  window over, and nobody goes looking in a menu for it.) So the button appears
 *  in exactly those two: in a browser tab the reload button is two centimetres
 *  away and a second one would be noise. */
export default function SyncButton() {
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const decide = () => setShow((isStandalone() || isDesktopApp()) && isDesktopPointer());
    decide();
    // Installing the app does not reload the window, so display-mode can change
    // under a running page.
    const mq = window.matchMedia('(display-mode: standalone)');
    mq.addEventListener('change', decide);
    return () => mq.removeEventListener('change', decide);
  }, []);

  if (!show) return null;

  return (
    <button
      className="icon-btn sync-btn"
      title={t('Fetch the latest changes')}
      aria-label={t('Fetch the latest changes')}
      disabled={busy}
      onClick={() => {
        setBusy(true);
        requestRefresh();
        void checkForNewVersion();
        // Same reason as the pull gesture: long enough to be seen.
        window.setTimeout(() => setBusy(false), 650);
      }}
    >
      <RefreshCw size={16} className={busy ? 'ptr-spin' : ''} />
    </button>
  );
}
