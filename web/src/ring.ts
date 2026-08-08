/**
 * Speed of the running border on hover — without a jump.
 *
 * The obvious route is CSS: shorten `animation-duration` on hover. That jumps
 * visibly, and it cannot do otherwise. The progress of an animation is elapsed
 * time DIVIDED BY duration. If the arc has been running 4s on a 9s lap it sits
 * at 160°. Set the duration to 3.5s at that same moment and it becomes
 * (4 mod 3.5) / 3.5 — that is 51°. The light jumps backwards instead of
 * speeding up.
 *
 * `updatePlaybackRate` is made for exactly this: it holds the current time and
 * changes only the rate. And because a hard jump from 1 to 2.6 feels like a
 * lurch, the rate is pulled up over just under half a second.
 *
 * Delegated on the document, not per element: dialogs come into existence only
 * when opened, so a listener attached at mount would never reach them.
 */

const SELECTOR = '.ring, .dialog, .confirm-dialog';
const FAST = 2.6;
const RAMP_MS = 420;

/** Die Rand-Animation eines Elements (es kann mehrere Animationen tragen). */
function ringAnimations(el: Element): Animation[] {
  return el.getAnimations().filter((a) => (a as CSSAnimation).animationName === 'ring-rotate');
}

const ramps = new WeakMap<Element, number>();

function rampTo(el: Element, target: number) {
  const anims = ringAnimations(el);
  if (!anims.length) return;
  const from = anims[0].playbackRate;
  if (from === target) return;

  const previous = ramps.get(el);
  if (previous) cancelAnimationFrame(previous);

  const started = performance.now();
  const step = (now: number) => {
    const t = Math.min(1, (now - started) / RAMP_MS);
    // Kubisch ausklingend: schnell ansprechen, weich ankommen.
    const eased = 1 - Math.pow(1 - t, 3);
    for (const a of anims) a.updatePlaybackRate(from + (target - from) * eased);
    if (t < 1) ramps.set(el, requestAnimationFrame(step));
    else ramps.delete(el);
  };
  ramps.set(el, requestAnimationFrame(step));
}

export function installRingHover() {
  // Somebody sensitive to motion has switched the rotation off anyway
  // (see styles.css) — then there is nothing here to speed up.
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

  document.addEventListener('pointerover', (e) => {
    const el = (e.target as Element | null)?.closest?.(SELECTOR);
    if (!el) return;
    // Moving from one child to another is not a fresh entry.
    const from = (e as PointerEvent).relatedTarget as Element | null;
    if (from && el.contains(from)) return;
    rampTo(el, FAST);
  });

  document.addEventListener('pointerout', (e) => {
    const el = (e.target as Element | null)?.closest?.(SELECTOR);
    if (!el) return;
    const to = (e as PointerEvent).relatedTarget as Element | null;
    if (to && el.contains(to)) return;
    rampTo(el, 1);
  });
}
