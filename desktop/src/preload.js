const { contextBridge, ipcRenderer } = require('electron');

// The entire bridge between the window and the app. Three functions, and only
// the connect page uses them — the workspace itself is an ordinary web page
// that never touches this.
//
// Deliberately not a generic `invoke(channel, ...args)`. That is the shape that
// looks tidy and hands a remote page the whole IPC surface: whatever the main
// process ever adds becomes callable by the server, and by anything that gets
// a script into it.
contextBridge.exposeInMainWorld('salt', {
  getServer: () => ipcRenderer.invoke('salt:getServer'),
  setServer: (url) => ipcRenderer.invoke('salt:setServer', url),
  cancel: () => ipcRenderer.invoke('salt:cancel'),
  forget: () => ipcRenderer.invoke('salt:forget'),
});

// ---- "which instance is this?" on the sign-in screen ----------------------
//
// Setting the address is easy to do and was hard to undo: he pointed the app at
// an instance and found no way back. It went into the app menu under Settings
// first, which is where a Mac user looks — and his answer was that the sign-in
// screen is the better place, which is right. That is the exact moment somebody
// realises they are at the wrong instance: staring at a login they cannot use.
//
// Only there. On a signed-in workspace this would be permanent clutter for
// something you need about twice.
//
// The line is injected rather than built into salt.md's own login page for the
// reason the window CSS taught: the app must not need a matching server. This
// works against every version, including ones released before the app existed.

const SWITCH_ID = 'salt-desktop-switch';
const SWITCH_CSS = `
#${SWITCH_ID} {
  position: fixed; left: 0; right: 0; bottom: 22px;
  display: flex; justify-content: center; gap: 7px; align-items: baseline;
  font: 12.5px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif;
  color: rgba(120,119,116,0.95); z-index: 2147483000;
  -webkit-app-region: no-drag; pointer-events: auto;
}
#${SWITCH_ID} b { font-weight: 600; color: inherit; }
#${SWITCH_ID} button {
  font: inherit; color: inherit; background: none; border: 0; padding: 0;
  text-decoration: underline; text-underline-offset: 2px; cursor: pointer;
}
#${SWITCH_ID} button:hover { color: #37352f; }
@media (prefers-color-scheme: dark) { #${SWITCH_ID} button:hover { color: #d4d4d4; } }
`;

async function syncSwitchLine() {
  const onLogin = !!document.querySelector('.login-card, .login-wrap');
  const existing = document.getElementById(SWITCH_ID);
  if (!onLogin) {
    existing?.remove();
    return;
  }
  if (existing) return;

  const server = await ipcRenderer.invoke('salt:getServer');
  if (!server || !document.querySelector('.login-card, .login-wrap')) return;
  if (document.getElementById(SWITCH_ID)) return;

  const style = document.createElement('style');
  style.textContent = SWITCH_CSS;
  const bar = document.createElement('div');
  bar.id = SWITCH_ID;
  const label = document.createElement('span');
  // Host only: the scheme and port are noise here, and the question this
  // answers is "which instance am I looking at".
  let host = server;
  try { host = new URL(server).host; } catch { /* keep the raw value */ }
  label.innerHTML = 'Connected to <b></b>';
  label.querySelector('b').textContent = host;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.textContent = 'Change';
  btn.addEventListener('click', () => void ipcRenderer.invoke('salt:openConnect'));
  bar.append(label, btn);
  (document.body || document.documentElement).append(style, bar);
}

// The sign-in screen is rendered by the app AFTER the page loads, and it comes
// and goes as somebody signs in and out — so this watches rather than checking
// once. Cheap: it only looks for one selector.
const watch = () => {
  void syncSwitchLine();
  new MutationObserver(() => void syncSwitchLine())
    .observe(document.documentElement, { childList: true, subtree: true });
};
if (document.body) watch();
else document.addEventListener('DOMContentLoaded', watch);

// The window has no title bar on macOS — the traffic lights are drawn INSIDE
// it, over whatever the page puts in its top-left corner. With the sidebar open
// that corner is empty and it looks right; collapsed, they land on the tab bar
// and on the page title.
//
// THE APP CARRIES THIS FIX, not the server.
//
// The first version put the rules in salt.md's own stylesheet, which was wrong
// in a way that only shows up later: the app would then look broken against
// every instance that has not been updated yet — and pointing this app at an
// older server is the normal case, not the exception. A window's own layout is
// the window's problem.
//
// Injected as a stylesheet rather than scripted: contextIsolation means this
// file shares no JS context with the page, and the DOM is the one thing both
// can see.

// macOS draws the traffic lights at roughly x 12-70, y 12-32 with this title
// bar style. Both numbers below come from that rectangle rather than from
// taste — the first two attempts guessed and were wrong in both directions.
const DESKTOP_CSS = `
/* Collapsed: the content starts at the left edge, so the corner is cleared
   sideways — the row is short and there is nothing to push down. */
html[data-desktop='mac'] .app.sidebar-collapsed .tab-bar,
html[data-desktop='mac'] .app.sidebar-collapsed .topbar {
  padding-left: 78px;
}

/* Open: the sidebar owns the corner and its first row is the workspace
   switcher, which sat straight under the buttons. Cleared downwards rather
   than sideways — an indented switcher in a full-width sidebar looks like a
   mistake, while a little air above it looks like a title bar, which is what
   the space is. */
html[data-desktop='mac'] .app:not(.sidebar-collapsed) .sidebar-header {
  padding-top: 38px;
}

/* Dragging the window.
   A window with no title bar has nothing to grab, so the strip at the height
   of the buttons has to do it — and that strip is a different element
   depending on the state, which is why the first version only worked
   sometimes: it named the tab bar, which is absent when a single tab is open.

   The container drags and every child opts out. What is left draggable is
   exactly the empty space — the padding beside the buttons and the gaps
   between controls — while every button, field and tab still takes its click.
   Listing the children the other way round would mean naming each one, and
   the next control added to that row would silently not be clickable. */
html[data-desktop='mac'] .sidebar-header,
html[data-desktop='mac'] .tab-bar,
html[data-desktop='mac'] .app.sidebar-collapsed .topbar {
  -webkit-app-region: drag;
}
html[data-desktop='mac'] .sidebar-header *,
html[data-desktop='mac'] .tab-bar *,
html[data-desktop='mac'] .app.sidebar-collapsed .topbar * {
  -webkit-app-region: no-drag;
}
`;

function apply() {
  const html = document.documentElement;
  if (!html) return;
  html.setAttribute('data-desktop', process.platform === 'darwin' ? 'mac' : 'other');
  if (process.platform !== 'darwin') return;
  if (document.getElementById('salt-desktop-css')) return;
  const style = document.createElement('style');
  style.id = 'salt-desktop-css';
  style.textContent = DESKTOP_CSS;
  (document.head || html).appendChild(style);
}

// Both, because neither alone covers every load: at document-start there may be
// no <head> yet, and on a page that is already parsed DOMContentLoaded has been
// and gone.
apply();
document.addEventListener('DOMContentLoaded', apply);


// ---- offline ---------------------------------------------------------------
//
// The window is a view onto a server. When the machine loses its network the
// page does not go anywhere — it just stops working, silently: clicks do
// nothing, edits are not saved, and the last thing on screen looks perfectly
// current. In a browser tab you at least know what a browser does when it is
// offline. Here there is no address bar and no reload button in sight, so the
// app has to say it itself.
//
// This is the v1 answer deliberately: there is NO local copy of anything, so
// there is nothing useful to do while offline except say so and get out of the
// way the moment the connection is back. Anything more is a cache, and a cache
// that silently disagrees with the server is a worse problem than this one.

const OFFLINE_ID = 'salt-desktop-offline';
const OFFLINE_CSS = `
#${OFFLINE_ID} {
  position: fixed;
  inset: 0;
  z-index: 2147483647;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 32px;
  text-align: center;
  font: 14px/1.6 -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  color: #37352f;
  background: rgba(255, 255, 255, 0.94);
  backdrop-filter: blur(6px);
  -webkit-app-region: drag;   /* the window must still be draggable */
}
@media (prefers-color-scheme: dark) {
  #${OFFLINE_ID} { color: #d4d4d4; background: rgba(25, 25, 25, 0.94); }
}
#${OFFLINE_ID} strong { font-size: 17px; font-weight: 600; }
#${OFFLINE_ID} p { margin: 0; max-width: 34em; opacity: 0.72; }
#${OFFLINE_ID} span { margin-top: 4px; font-size: 12px; opacity: 0.5; }
`;

function showOffline() {
  if (document.getElementById(OFFLINE_ID)) return;
  const style = document.createElement('style');
  style.id = OFFLINE_ID + '-css';
  style.textContent = OFFLINE_CSS;
  const box = document.createElement('div');
  box.id = OFFLINE_ID;
  const title = document.createElement('strong');
  title.textContent = 'You are offline';
  const body = document.createElement('p');
  body.textContent =
    'salt.md is a window onto your server, so it needs a connection to show or save anything. Nothing you did is lost — it is on the server, exactly as you left it.';
  const hint = document.createElement('span');
  hint.textContent = 'This closes by itself when the connection is back.';
  box.append(title, body, hint);
  (document.body || document.documentElement).append(style, box);
}

function hideOffline() {
  document.getElementById(OFFLINE_ID)?.remove();
  document.getElementById(OFFLINE_ID + '-css')?.remove();
}

// navigator.onLine is the browser's word for "this machine has a network", not
// "the server answers" — a captive portal or a server that is down both look
// online. That narrower case is the product's own job: it shows a badge when
// its event stream has gone quiet. This screen is only for the blunt one, which
// is the one that makes the whole window useless.
window.addEventListener('offline', showOffline);
window.addEventListener('online', hideOffline);
if (navigator.onLine === false) document.addEventListener('DOMContentLoaded', showOffline);
