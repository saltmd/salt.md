const { app, BrowserWindow, shell, Menu, dialog, ipcMain, session, nativeTheme, nativeImage } = require('electron');
const path = require('node:path');
const fs = require('node:fs');
const { normalizeURL } = require('./serverURL');
const crypto = require('node:crypto');

// salt.md in its own window.
//
// This is a SHELL around a server you run — not a copy of salt.md and not a
// local instance. You give it an address once and it opens straight into your
// workspace, in a window with no address bar and a native menu.
//
// The value over a browser tab is small and real: it opens where you left off,
// it does not sit in a row of thirty other tabs, and ⌘W closes the window
// rather than a page you were editing.
//
// Three rules shape the code below, and each one is a way the naive version
// goes wrong:
//
//  1. THE WINDOW STAYS ON YOUR INSTANCE. Anything else opens in the real
//     browser. Without this, one click on a bookmark block replaces your
//     workspace with somebody's website and there is no back button to see.
//  2. THE RENDERER GETS NO NODE. contextIsolation on, nodeIntegration off,
//     preload exposing exactly one function. A shell that loads a remote page
//     with Node in it hands that server the user's filesystem.
//  3. A SERVER THAT IS DOWN GETS A PAGE, NOT A CRASH. The failure everybody
//     hits first is a laptop opening the app on a train, and Chromium's own
//     error page is a dead end with no way back to the settings.

// The scheme the browser uses to hand control back. Registered at startup; if
// the system refuses, the app falls back to signing in inside its own window.
const desktopScheme = 'salt';

const store = path.join(app.getPath('userData'), 'settings.json');

// The app was called "salt.md" before it was called "salt.md", and Electron
// derives the settings directory from that name. Renaming it would therefore
// have silently forgotten which server somebody had configured — the one thing
// this app stores. Moved once, quietly, and only when there is nothing here yet.
(function carryOverOldSettings() {
  if (fs.existsSync(store)) return;
  const old = path.join(path.dirname(app.getPath('userData')), 'salt.md', 'settings.json');
  try {
    if (fs.existsSync(old)) {
      fs.mkdirSync(path.dirname(store), { recursive: true });
      fs.copyFileSync(old, store);
    }
  } catch {
    /* a missing old profile is the normal case, not a problem */
  }
})();

// The dock icon follows the system theme.
//
// macOS has no dark-mode app icon — what Finder and Launchpad show is fixed at
// build time, and that stays the light one. The DOCK icon can be set at
// runtime, though, and the dock is what you actually look at while the app is
// open. So it swaps, and it swaps live when the system does.
function applyDockIcon() {
  if (process.platform !== 'darwin' || !app.dock) return;
  const file = nativeTheme.shouldUseDarkColors ? 'icon-dark.png' : 'icon.png';
  const img = nativeImage.createFromPath(path.join(__dirname, '../build', file));
  if (!img.isEmpty()) app.dock.setIcon(img);
}

function readSettings() {
  try {
    return JSON.parse(fs.readFileSync(store, 'utf8'));
  } catch {
    return {};
  }
}

function writeSettings(next) {
  fs.mkdirSync(path.dirname(store), { recursive: true });
  fs.writeFileSync(store, JSON.stringify(next, null, 2));
}


// ---- the sign-in window --------------------------------------------------
//
// "Open" is a five-minute door, not a mode. If a person abandons a sign-in — a
// forgotten password, a provider that hangs — the window must not be left able
// to navigate anywhere for the rest of the session.

let authOpen = false;
let authTimer = null;

/** True for the instance's own routes that BEGIN a round trip to a provider:
 *  signing in, and an admin connecting a mailbox. */
function isAuthStart(server, url) {
  return url.startsWith(server + '/api/oauth/') ||
         url.startsWith(server + '/api/admin/mail-oauth/');
}

function openAuthWindow() {
  authOpen = true;
  clearTimeout(authTimer);
  authTimer = setTimeout(closeAuthWindow, 5 * 60 * 1000);
}

function closeAuthWindow() {
  authOpen = false;
  clearTimeout(authTimer);
  authTimer = null;
}


// ---- signing in through the real browser ----------------------------------
//
// The app used to show the provider's sign-in page in its own window. That
// works, and it is the wrong answer: in your own browser you can SEE the
// address bar and check that your password is going to the provider and not to
// a window an application drew. It also reuses the browser session you already
// have, and passkeys work there.
//
// The hand-back is the hard part. salt:// is not a private channel — any
// program may register for it. So the code that comes back is useless alone:
// the app keeps a secret (the verifier), sends only its digest to the server at
// the start, and must present the secret to redeem the code. Whoever intercepts
// the code cannot use it. See server/desktop_auth.go for the other half.

let pendingVerifier = null;

/** Does this server know the browser hand-off at all?
 *
 *  It is a newer route than the app, and pointing the app at an instance that
 *  predates it is the ordinary case, not the exception. Without this check the
 *  browser opens on a path the old server does not have, falls through to the
 *  single-page app, and the person is simply standing in their workspace in a
 *  browser wondering what happened — which is exactly what it did.
 *
 *  An invalid challenge is used deliberately: a server that knows the route
 *  answers 400 and stores nothing, an older one falls through with 200. So the
 *  probe costs nothing and leaves no half-finished sign-in behind. */
async function serverDoesBrowserSignIn(server) {
  try {
    const res = await session.defaultSession.fetch(
      server + '/desktop/login?challenge=probe', { redirect: 'manual' });
    return res.status === 400;
  } catch {
    return false;
  }
}

function startBrowserSignIn() {
  const server = readSettings().server;
  if (!server) return;
  pendingVerifier = crypto.randomBytes(32).toString('base64url');
  const challenge = crypto.createHash('sha256').update(pendingVerifier).digest('base64url');
  shell.openExternal(server + '/desktop/login?challenge=' + encodeURIComponent(challenge));
}

/** Handles salt://auth?code=… — the browser handing control back. */
async function finishBrowserSignIn(rawURL) {
  let code = '';
  try {
    code = new URL(rawURL).searchParams.get('code') || '';
  } catch {
    return;
  }
  const server = readSettings().server;
  // A code without a verifier is not ours: either this arrived out of nowhere,
  // or the app restarted mid-flow and the secret is gone. Both mean start again
  // rather than send a bare code to the server.
  if (!code || !server || !pendingVerifier) return;
  const verifier = pendingVerifier;
  pendingVerifier = null;

  try {
    // Through the WINDOW's session, so the cookie it returns is the one the
    // window will use. A fetch from anywhere else would land the session in a
    // jar nobody reads.
    const res = await session.defaultSession.fetch(server + '/api/desktop/exchange', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code, verifier }),
    });
    if (!res.ok) throw new Error('exchange failed: ' + res.status);
    if (win && !win.isDestroyed()) {
      win.loadURL(server);
      win.show();
      win.focus();
    }
  } catch (e) {
    if (win && !win.isDestroyed()) {
      showUnreachable(server, 'Sign-in could not be completed: ' + (e.message ?? e));
    }
  }
}

let win = null;

function createWindow() {
  const saved = readSettings();
  win = new BrowserWindow({
    width: saved.width ?? 1280,
    height: saved.height ?? 860,
    minWidth: 700,
    minHeight: 500,
    // The traffic lights sit inside the window on macOS: salt.md's own topbar
    // is the chrome, and a second title bar above it is a wasted stripe.
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    backgroundColor: '#191919',
    show: false,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      spellcheck: true,
    },
  });

  win.once('ready-to-show', () => win.show());

  // Remember the size, not the position: a window restored onto a monitor that
  // is no longer attached is invisible with no way to fetch it back.
  const remember = () => {
    if (!win || win.isDestroyed() || win.isMinimized() || win.isFullScreen()) return;
    const [width, height] = win.getSize();
    writeSettings({ ...readSettings(), width, height });
  };
  win.on('resize', remember);
  win.on('close', remember);

  // RULE 1, part one: a link that wants a new window is an outside link.
  win.webContents.setWindowOpenHandler(({ url }) => {
    // Some providers open the consent step in a popup. Mid-flow that popup is
    // part of signing in, so it belongs in this window rather than in the
    // browser — where it would finish the flow in the wrong program.
    if (authOpen && /^https?:/i.test(url)) {
      win.loadURL(url);
      return { action: 'deny' };
    }
    if (/^https?:/i.test(url)) shell.openExternal(url);
    return { action: 'deny' };
  });

  // RULE 1, part two: navigation inside the window may not leave the instance —
  // EXCEPT while a sign-in is running.
  //
  // Signing in with Microsoft or Google is a round trip: your instance sends the
  // browser to the provider, the provider sends it back. The naive version of
  // this rule breaks it in the middle — it sees login.microsoftonline.com,
  // decides that is not your instance, and hands the rest of the flow to the
  // real browser. The person then signs in successfully in the WRONG program:
  // the session cookie lands in the browser and the app sits on the login page
  // forever, which is exactly what it looked like.
  //
  // The gate is the FLOW, not a list of provider hostnames. A list would have to
  // name every identity provider, every asset domain they redirect through, and
  // every one they add later — and it would still refuse anybody running their
  // own. Instead: passing through one of the instance's own OAuth routes opens
  // the door, and coming back to the instance closes it again.
  win.webContents.on('will-navigate', (event, url) => {
    const server = readSettings().server;
    if (!server) return;
    if (url.startsWith('file://')) return; // our own connect / error pages

    // A sign-in starting: OUT to the real browser rather than in this window.
    // The embedded flow is kept as a fallback only for the case where the
    // protocol could not be registered — otherwise somebody whose machine
    // refuses salt:// would have no way in at all.
    if (isAuthStart(server, url)) {
      if (!protocolRegistered) {
        openAuthWindow();
        return;
      }
      // Held while we ask the server whether it can do this. If it cannot, the
      // same navigation is resumed in this window — nobody is left in a browser
      // looking at a page that means nothing to them.
      event.preventDefault();
      void serverDoesBrowserSignIn(server).then((yes) => {
        if (yes) {
          startBrowserSignIn();
          return;
        }
        openAuthWindow();
        if (win && !win.isDestroyed()) win.loadURL(url);
      });
      return;
    }
    if (url.startsWith(server)) {
      // Back home. Anything after this is ordinary navigation again.
      if (!url.startsWith(server + '/api/oauth/') &&
          !url.startsWith(server + '/api/admin/mail-oauth/')) closeAuthWindow();
      return;
    }
    if (authOpen) return; // mid-flow at the provider: let it through

    event.preventDefault();
    if (/^https?:/i.test(url)) shell.openExternal(url);
  });

  // salt.md draws its own context menus now (right-click a page, a row, a
  // card). Chromium's default menu would open on top of them — "Back",
  // "Reload", "Inspect" over the menu somebody actually wanted. Suppressed
  // except where there is text to act on, because "Copy" and the spelling
  // suggestions are the one case the browser does better than we would.
  win.webContents.on('context-menu', (event, params) => {
    const editable = params.isEditable || params.selectionText.trim() !== '';
    if (!editable) event.preventDefault();
  });

  // RULE 3: a server that cannot be reached gets an explanation and a way back.
  win.webContents.on('did-fail-load', (_e, code, description, failedURL, isMainFrame) => {
    if (!isMainFrame || code === -3 /* aborted, e.g. a redirect */) return;
    showUnreachable(failedURL, description);
  });

  route();
}

function route() {
  const server = readSettings().server;
  if (server) win.loadURL(server);
  else win.loadFile(path.join(__dirname, 'connect.html'));
}

/** The connect screen, opened deliberately rather than because nothing is set
 *  yet. It is told so, so it can offer a way back instead of looking like a
 *  fresh start with no escape. */
function openConnect() {
  if (!win || win.isDestroyed()) return;
  win.loadFile(path.join(__dirname, 'connect.html'), { query: { change: '1' } });
}

function showUnreachable(url, description) {
  win.loadFile(path.join(__dirname, 'connect.html'), {
    query: { error: description || 'unreachable', url: url || '' },
  });
}

// ---- what the connect page may ask for -------------------------------------

ipcMain.handle('salt:getServer', () => readSettings().server ?? '');

ipcMain.handle('salt:setServer', async (_e, input) => {
  const origin = normalizeURL(input);
  if (!origin) return { ok: false, error: 'not-a-url' };
  // Ask the instance whether it is one before saving. Typing an address that
  // answers nothing and being dropped into a blank window is the worst first
  // five minutes this app could have.
  try {
    const res = await fetch(origin + '/api/health', { redirect: 'follow' });
    const body = await res.json();
    if (!body || body.status !== 'ok') return { ok: false, error: 'not-salt' };
    writeSettings({ ...readSettings(), server: origin });
    route();
    return { ok: true, version: body.version ?? '' };
  } catch (e) {
    return { ok: false, error: 'unreachable', detail: String(e.message ?? e) };
  }
});

// Back to the workspace without changing anything. Only reachable when a
// server is already configured — otherwise there is nothing to go back to.
ipcMain.handle('salt:openConnect', () => {
  openConnect();
  return true;
});

ipcMain.handle('salt:cancel', () => {
  const server = readSettings().server;
  if (server && win && !win.isDestroyed()) win.loadURL(server);
  return !!server;
});

ipcMain.handle('salt:forget', () => {
  const { server, ...rest } = readSettings();
  writeSettings(rest);
  route();
  return true;
});

// ---- menu ------------------------------------------------------------------

function buildMenu() {
  const isMac = process.platform === 'darwin';
  const template = [
    // The app menu is spelled out rather than taken from the `appMenu` role,
    // for one entry: Settings. Changing the server used to sit under File as
    // "Change server…", where nobody found it — he had set an address and
    // concluded there was no way back. ⌘, in the app menu is where a Mac user
    // looks, and it costs nothing to be there.
    ...(isMac
      ? [{
          label: app.name,
          submenu: [
            { role: 'about' },
            { type: 'separator' },
            { label: 'Settings…', accelerator: 'CmdOrCtrl+,', click: () => openConnect() },
            { type: 'separator' },
            { role: 'services' },
            { type: 'separator' },
            { role: 'hide' }, { role: 'hideOthers' }, { role: 'unhide' },
            { type: 'separator' },
            { role: 'quit' },
          ],
        }]
      : []),
    {
      label: 'File',
      submenu: [
        {
          label: 'Sign in with your browser',
          click: () => startBrowserSignIn(),
        },
        ...(isMac
          ? []
          : [{ label: 'Settings…', accelerator: 'CmdOrCtrl+,', click: () => openConnect() }]),
        { type: 'separator' },
        isMac ? { role: 'close' } : { role: 'quit' },
      ],
    },
    { role: 'editMenu' },
    {
      label: 'View',
      submenu: [
        { role: 'reload' },
        { role: 'forceReload' },
        { type: 'separator' },
        { role: 'resetZoom' },
        { role: 'zoomIn' },
        { role: 'zoomOut' },
        { type: 'separator' },
        { role: 'togglefullscreen' },
        { role: 'toggleDevTools' },
      ],
    },
    { role: 'windowMenu' },
    {
      role: 'help',
      submenu: [
        {
          label: 'salt.md documentation',
          click: () => shell.openExternal('https://salt.md/wiki'),
        },
        {
          label: 'About this app',
          click: () =>
            dialog.showMessageBox(win, {
              type: 'info',
              message: 'salt.md',
              detail:
                `Version ${app.getVersion()}\n\n` +
                'This app is a window onto a salt.md server you run. ' +
                'Your data lives on that server, not here.\n\n' +
                `Connected to: ${readSettings().server || 'nothing yet'}`,
            }),
        },
      ],
    },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

// ---- start -----------------------------------------------------------------

// macOS delivers the URL as an event; Windows and Linux relaunch the binary
// with it in argv and the first instance has to pick it out.
let protocolRegistered = false;

app.on('open-url', (event, url) => {
  event.preventDefault();
  void finishBrowserSignIn(url);
});

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', (_e, argv) => {
    const hit = argv.find((a) => a.startsWith(desktopScheme + '://'));
    if (hit) void finishBrowserSignIn(hit);
    if (win && !win.isDestroyed()) {
      if (win.isMinimized()) win.restore();
      win.focus();
    }
  });
}

app.whenReady().then(() => {
  // The scheme is claimed by CFBundleURLTypes in the packaged app, so macOS
  // knows about it from the moment it is installed — no run required.
  //
  // A DEVELOPMENT run must never claim it. `npx electron .` would register the
  // Electron binary in node_modules as the handler, which then answers salt://
  // with its own welcome screen and leaves the installed app unreachable. That
  // is not hypothetical: it happened, and it looked exactly like the rename had
  // broken the sign-in.
  protocolRegistered = app.isPackaged
    ? app.setAsDefaultProtocolClient(desktopScheme)
    : false;

  // Identity providers refuse to show a sign-in page to anything whose user
  // agent says "Electron" — Google answers `disallowed_useragent` outright.
  // The objection is to hidden webviews that can read what somebody types; this
  // is a visible window with no script of ours in it, so the honest description
  // is a browser. Dropping the two Electron tokens says exactly that.
  const ua = session.defaultSession
    .getUserAgent()
    .replace(/ Electron\/[\d.]+/, '')
    .replace(/ salt-desktop\/[\d.]+/i, '')
    .replace(/ Salt\.md\/[\d.]+/i, '');
  session.defaultSession.setUserAgent(ua);

  // The renderer loads a REMOTE page, so it is treated as one: no permission is
  // granted by default. Notifications and clipboard reads are the ones a
  // workspace might plausibly ask for; both can be added deliberately later.
  session.defaultSession.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));

  buildMenu();
  applyDockIcon();
  nativeTheme.on('updated', applyDockIcon);
  createWindow();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});
