import { useEffect, useRef, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { checkForNewVersion, isStandalone, requestRefresh } from '../pwa';

const THRESHOLD = 68; // how far you have to pull before it counts
const MAX_PULL = 108; // where the rubber band stops giving

/** The nearest ancestor that scrolls VERTICALLY, or null if nothing does.
 *
 *  salt.md does not scroll the window — `.page-body` scrolls, and inside it a
 *  table scrolls sideways on its own. Asking `window.scrollY` (the obvious
 *  check, and the one every example uses) would answer 0 halfway down a
 *  document and pull-to-refresh would fire in the middle of reading. */
function scrollableAncestor(el: Element | null): Element | null {
  for (let n = el; n && n !== document.body; n = n.parentElement) {
    const s = getComputedStyle(n);
    const scrolls = /auto|scroll|overlay/.test(s.overflowY);
    if (scrolls && n.scrollHeight > n.clientHeight + 1) return n;
  }
  return null;
}

/** Pull down to refresh — only in the installed app.
 *
 *  In a browser tab Safari and Chrome have their own pull gesture and their own
 *  reload button; taking the gesture over there would replace something that
 *  works with something that merely resembles it. */
export default function PullToRefresh() {
  const [pull, setPull] = useState(0);
  const [busy, setBusy] = useState(false);
  const startY = useRef(0);
  const startX = useRef(0);
  // Decided ONCE per touch and then held. Without the lock, a sideways swipe
  // across a wide table that drifts down by a few pixels turns into a refresh —
  // and salt.md's tables scroll sideways by design now.
  const mode = useRef<'idle' | 'pull' | 'reject'>('idle');

  useEffect(() => {
    if (!isStandalone()) return;

    const onStart = (e: TouchEvent) => {
      mode.current = 'idle';
      if (e.touches.length !== 1) {
        mode.current = 'reject';
        return;
      }
      startY.current = e.touches[0].clientY;
      startX.current = e.touches[0].clientX;
      const target = e.target as Element | null;
      // Not from inside a dialog, a menu or the editor's own surfaces, and not
      // unless whatever scrolls is already at the top.
      if (target?.closest('[role=dialog], .menu, .fs-popover, .modal, .ProseMirror')) {
        mode.current = 'reject';
        return;
      }
      const box = scrollableAncestor(target);
      if (box && box.scrollTop > 0) mode.current = 'reject';
    };

    const onMove = (e: TouchEvent) => {
      if (mode.current === 'reject') return;
      const dy = e.touches[0].clientY - startY.current;
      const dx = e.touches[0].clientX - startX.current;
      if (mode.current === 'idle') {
        if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return; // too early to tell
        if (dy > Math.abs(dx)) mode.current = 'pull';
        else {
          mode.current = 'reject';
          return;
        }
      }
      if (dy <= 0) {
        setPull(0);
        return;
      }
      e.preventDefault();
      setPull(Math.min(MAX_PULL, dy * 0.5)); // resistance, so it feels like rubber
    };

    const onEnd = () => {
      const wasPull = mode.current === 'pull';
      mode.current = 'idle';
      if (!wasPull) return;
      setPull((cur) => {
        if (cur < THRESHOLD) return 0;
        setBusy(true);
        requestRefresh();
        void checkForNewVersion();
        // Held for a moment on purpose. A spinner that vanishes in 80ms reads
        // as "nothing happened" — the point of the gesture is to SEE that the
        // app went and asked.
        window.setTimeout(() => {
          setBusy(false);
          setPull(0);
        }, 650);
        return THRESHOLD;
      });
    };

    window.addEventListener('touchstart', onStart, { passive: true });
    window.addEventListener('touchmove', onMove, { passive: false });
    window.addEventListener('touchend', onEnd, { passive: true });
    window.addEventListener('touchcancel', onEnd, { passive: true });
    return () => {
      window.removeEventListener('touchstart', onStart);
      window.removeEventListener('touchmove', onMove);
      window.removeEventListener('touchend', onEnd);
      window.removeEventListener('touchcancel', onEnd);
    };
  }, []);

  if (pull === 0 && !busy) return null;
  const progress = Math.min(1, pull / THRESHOLD);

  return (
    <div className="ptr" style={{ transform: `translateY(${pull * 0.55}px)` }} aria-hidden="true">
      <span className="ptr-dial" style={{ opacity: Math.max(0.35, progress) }}>
        <RefreshCw
          size={18}
          className={busy ? 'ptr-spin' : ''}
          style={busy ? undefined : { transform: `rotate(${progress * 270}deg)` }}
        />
      </span>
    </div>
  );
}
