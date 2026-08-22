// Running as an installed app, and how to ask everything on screen to refetch.
//
// An installed PWA has no browser chrome: no reload button on macOS, no
// pull-down in Safari on iOS. That is fine while the app is open — the SSE feed
// keeps it current — and not fine at all after the phone has slept for an hour,
// because the stream dies with the tab and nothing says so. So the app has to
// offer the gesture and the button itself.

/** Is this window an installed app rather than a browser tab? */
export function isStandalone(): boolean {
  return (
    window.matchMedia('(display-mode: standalone)').matches ||
    // iOS Safari has never implemented display-mode and reports it here instead.
    (navigator as { standalone?: boolean }).standalone === true
  );
}

/** A pointer that can hover is a mouse or a trackpad: a desktop app window,
 *  where a pull gesture does not exist and a button has to take its place. */
export function isDesktopPointer(): boolean {
  return window.matchMedia('(hover: hover) and (pointer: fine)').matches;
}

const EVENT = 'salt:refresh';

/** Ask every screen holding server data to fetch it again. Deliberately a
 *  broadcast rather than a reload: reloading throws away the open tabs, the
 *  scroll position and any unsaved editor state to answer a question that only
 *  needs new data. */
export function requestRefresh(): void {
  window.dispatchEvent(new Event(EVENT));
}

/** Subscribe. Returns the unsubscribe, so it drops straight into an effect. */
export function onRefresh(fn: () => void): () => void {
  window.addEventListener(EVENT, fn);
  return () => window.removeEventListener(EVENT, fn);
}

/** Ask the service worker whether a newer build is published, and adopt it.
 *  Without this the app shell can sit in the cache across a deploy for as long
 *  as the window stays open — which, for an app somebody never closes, is
 *  forever. */
export async function checkForNewVersion(): Promise<void> {
  if (!('serviceWorker' in navigator)) return;
  try {
    const reg = await navigator.serviceWorker.getRegistration();
    await reg?.update();
  } catch {
    /* an offline refresh is still a refresh */
  }
}
