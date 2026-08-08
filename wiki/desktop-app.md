# The desktop app

salt.md has a desktop application. It is a **window onto a server you run** —
not a second copy of the product, not a local instance, and not an offline mode.
You give it the address of your instance once, and it opens straight into your
workspace in a window with no address bar, a native menu, and its own place in
the dock or taskbar.

**It is not how you install salt.md, and it is no use on its own.** The server
is a separate program — one binary that runs on Linux, macOS or Windows and
answers in a browser; see [Getting started](getting-started.md). Install that
first. This app then points at it. The two are different downloads with
different version numbers and neither contains the other.

**The macOS build is signed and notarised by Apple**, so it opens with no
warning, and there is one for Apple silicon and one for Intel. A Linux build is
published too, as an AppImage and a `.deb`; it carries no signature, which is
normal there.

**There is no Windows build, on purpose.** An installer nobody has signed meets
SmartScreen — a full-screen panel saying Windows protected your PC, publisher
unknown, with the button to continue hidden behind "More info". That reads as
malware. Offering nothing is the better of two bad options until there is a
certificate for it.

Everything you see in it is the same salt.md the browser shows. Your pages,
files and databases stay on the server. This page covers connecting the app,
changing which instance it points at, the two ways of signing in, what the
window does that a browser tab does not, and how the app is built.

## What it is

One window, one instance, no pages of its own. Its own settings file holds two
things: the address you gave it, and the size of the window. Beyond that it
keeps what any browser keeps for a site you visit — the session cookie that
leaves you signed in after a restart, and the local state your workspace stores,
such as which document tabs were open. There is no local copy of your pages and
no offline access: with no connection to the server the app shows a page
explaining that, and the field to change the address.

It is deliberately built to work against **older instances than itself**. The
app carries its own window layout and its own extras rather than expecting the
server to supply them, so pointing a new app at an instance nobody has updated
in months is an ordinary case, not a broken one. The app's version (Help →
**About this app**) and your instance's version are separate numbers.

The app is not shipped with the server release. It is built from the `desktop/`
folder of the salt.md repository — see [Building it](#building-it) at the end of
this page.

## Connecting it to your instance

On first launch the window shows a single screen headed **Connect to your
salt.md**, with the line *This app is a window onto a server you run. Your data
stays there.*

1. Type the address of your instance in the field. It is empty on first launch;
   `salt.example.com` is the grey example inside it, not a value. Pressing
   **Connect** on an untouched field does nothing.
2. Press **Connect**, or the Enter key.

The app then asks that address whether a salt.md is actually there — it calls
`/api/health` and waits for the answer — before saving anything. Typing an
address that answers nothing and being dropped into a blank window is the
failure this check exists to prevent.

While it checks, the screen says *Looking for a salt.md there…*. Then one of:

| What it says | What happened |
| --- | --- |
| Found it. Opening… | The address is saved and the window loads your instance. |
| That does not look like an address. | What you typed cannot be read as a web address at all. |
| Something answered there, but it is not a salt.md. | The answer was readable and was not a healthy salt.md: another service with its own API, or a salt.md whose database is not responding. |
| Nothing answered there. Check the address, and that the server is running. | Nothing replied, or what replied was not an answer this check can read. Wrong host or port, server stopped, a name that does not resolve, a certificate the app refuses — and also a router page, a proxy or another application on that port, because a page of HTML lands here rather than in the row above. |

Under the field is a closing line for anybody who has no server yet: *No server
yet? salt.md is one binary you run yourself — see salt.md for how to start one.*
[Self-hosting](self-hosting.md) is that story.

### What you may type

You do not have to type a scheme. The app fills one in, and this is the only
place in it that guesses at what you meant:

| You type | The app uses |
| --- | --- |
| `salt.example.com` | `https://salt.example.com` |
| `https://salt.example.com` | `https://salt.example.com` — an explicit scheme always wins |
| `localhost:8420` | `http://localhost:8420` |
| `127.0.0.1:8420` | `http://127.0.0.1:8420` |
| `192.0.2.10:8420` | `https://192.0.2.10:8420` |
| `https://salt.example.com/p/9fd2?tab=x` | `https://salt.example.com` |

Two rules are worth knowing because they are not obvious:

- **A bare host becomes `https`, but this machine becomes `http`.**
  `localhost`, `127.0.0.1`, `0.0.0.0` and `[::1]` default to plain HTTP, because
  a salt.md you started on your own machine serves plain HTTP unless you gave it
  a certificate. Anything else — including a LAN address like `192.0.2.10` — gets
  `https`, since that may well be behind a proxy that terminates TLS.
- **A pasted page address is cut back to the instance.** People copy the address
  of the page they are looking at; the path, query and fragment are dropped.

Anything with another scheme (`ftp://…`, `file:///…`) is refused rather than
patched up, and so is text that is not an address.

## Changing the instance later

One instance at a time. Connecting to another one replaces the address and
nothing else — no data is touched on either server, and switching back is
retyping the old address.

There are two ways in:

- **Settings…** (⌘, / Ctrl+,) — in the application menu on macOS, in the **File**
  menu on Windows and Linux.
- **On the sign-in screen**, a quiet line at the bottom of the window: *Connected
  to* the host name, followed by **Change**. It appears only on the sign-in
  screen, because that is the moment you notice you are at the wrong instance —
  staring at a login you cannot use. The host is shown as it is addressed, port
  included: `localhost:8420` keeps its `:8420`.

The connect screen prefills whatever is configured, so changing the instance is
an edit rather than typing an address from memory. When a server is already
set, the screen also offers **Back to my workspace**, which leaves everything as
it was.

There is no way to disconnect entirely — no "forget this instance" anywhere in
the app. The address can be replaced with another one and that is all. If you
want the app to arrive at the connect screen again, point it at the instance you
do want.

**Upgrading the app keeps your address.** The application was named `salt.md`
before it was named `salt.md`, and the settings live in a folder derived from
that name. A new version copies the old file across once, and only when it has
nothing of its own yet, so an upgrade does not silently forget which server you
had configured.

## Signing in

Two routes, and which one you get depends on what you press.

### With a password, in the window

salt.md's own sign-in screen appears **inside the app window**, exactly as it
does in a browser: email, password, and a two-factor code if your account has one
([Account](account.md)). Nothing leaves the app — the form is submitted in place,
so the window never navigates and nothing diverts it. This is the ordinary way
in.

The *Connected to … Change* line described above sits at the bottom of that
screen.

### Through your real browser

Two things send you out to your browser instead:

- Pressing **Sign in with Google** or **Sign in with Microsoft** on the sign-in
  screen (they appear when an administrator has set them up — see
  [Single sign-on](sso.md)).
- Choosing **File → Sign in with your browser** by hand.

You then sign in in your browser exactly as you normally would, and the browser
hands control back to the app.

That is a deliberate trade of one extra step for two things you cannot get
inside an application window: you can see the address bar, and therefore check
that your password is going to your identity provider and not to a window some
program drew; and the browser session, password manager, passkeys and hardware
keys you already have all work. It is also why identity providers refuse
embedded sign-in windows in the first place.

What you see:

1. Your browser opens on your instance. If you are not signed in there, the
   normal sign-in screen appears first — password and two-factor code, or your
   company account. It returns to the right place afterwards.
2. A page headed **Sign in to the desktop app?** with the line *The salt.md app
   on this computer is asking for a session.* and a box showing which account it
   would use — your name, and your email address if you have one.
3. Press **Allow**. **Not now** cancels and takes you to your workspace in the
   browser.
4. A page says **Signed in.** — *You can close this tab and go back to the
   salt.md app.* — with an **Open salt.md** button. The browser usually jumps
   back to the app on its own; the button is there for browsers that will not
   follow an unfamiliar link without a click.
5. The app window opens your workspace.

**The Allow step is not ceremony.** Without it, any web page you happened to
open could send your browser through this flow and quietly mint a session for a
program waiting on the other end.

### What the app ends up with

An ordinary browser session, the same as signing in in a browser — not an agent
credential, and not a token with reduced powers. It lasts as long as your
instance's session length allows (90 days unless an administrator changed it,
see [Administration](administration.md)), and changing your password ends it
along with every other session ([Account](account.md)).

It is also written to the audit log, as a `desktop_signin` — and it stands out
there rather than blending in, because it is the **only** kind of sign-in this
product records. An ordinary password sign-in and a company-account sign-in
leave no entry at all ([History and audit](history-and-audit.md)).

The code that travels from the browser back to the app is single use, expires
after **five minutes**, and cannot be redeemed by anything except the app that
started the request. The app holds a secret that never goes through the browser
and never travels over the hand-back link; it goes straight to the server, once,
at the moment the code is redeemed. An intercepted code is therefore worthless.

If the app has been restarted in the meantime, that secret is gone, and the code
is ignored: nothing is exchanged, nothing is said, and the window stays exactly
where it was. Start again from **File → Sign in with your browser**.

### When something goes wrong mid-flow

| What you see | What it means |
| --- | --- |
| That sign-in request is malformed. | The browser arrived on the sign-in page without a proper request from the app. Start it again from the app. |
| You are not signed in any more. | Your browser session ended between the approval page and pressing **Allow**. Sign in again in the browser and repeat. |
| Could not create the sign-in. | The server could not record the request. Try again; if it repeats, the server log is the place to look. |

If the hand-back fails after you pressed **Allow** — the code expired, or it was
already used — the app window returns to the connect screen. The line it shows
there is the generic one, *Could not reach https://salt.example.com. Is it
running?*, even when the server answered perfectly well; what failed is the
sign-in. Start it again from **File → Sign in with your browser**.

### When the browser is not used

Two cases keep a provider sign-in inside the app window, unchanged and working:

- **Your instance is older than this feature.** When a sign-in starts on its own
  — you pressed **Sign in with Google** or **Sign in with Microsoft** — the app
  first asks the server whether it knows the browser hand-off. An instance that
  does not gets the sign-in in the window, and the round trip to the provider
  runs there. Without that question you would end up standing in your workspace
  in a browser wondering what happened.
- **The machine would not let the app claim its `salt://` link.** Without that,
  the browser has no way to reach back, so the app keeps the in-window sign-in
  as a way in rather than leaving you with none. A run started from source
  (`npm start`) never claims the link on purpose, so a development run cannot
  take it away from an installed copy.

**File → Sign in with your browser** asks neither question. It opens your
browser every time, on a page an instance without the feature does not have — so
use it against an instance new enough to have it. Its purpose is the case where
a session has expired and the app is sitting on a sign-in screen.

While a sign-in is running **in the window**, the window is allowed off your
instance, because a round trip to Google or Microsoft is exactly that. Two
details follow. A provider's consent step opened as a popup is kept in the app
window instead of being pushed to your browser — finishing it in the browser
would put the session in the wrong program. And the permission closes five
minutes after the sign-in started: an abandoned sign-in does not leave the
window free to wander for the rest of the session.

### Signing out, and switching accounts

Sign out the ordinary way: your name at the bottom of the sidebar → **Sign
out**. The window returns to salt.md's own sign-in screen inside the app, with
the *Connected to … Change* line back at the bottom, and you can sign in as
somebody else from there.

### Connecting a mailbox does not work inside the app

Administrators only. The **Email** tab of the admin settings offers **Connect
with Google** and **Connect with Microsoft** to send invitations through a real
mailbox ([Mail](mail.md)). Pressing either one inside the app window does not
connect a mailbox: the window treats every round trip to a provider as a
sign-in, so what starts is the browser sign-in described above. You approve a
session for the app, the window returns to your workspace, and no mailbox has
been connected. Do that piece of setup in a browser.

## What the window adds over a browser tab

- **It opens where you left off**, and it is not one of thirty tabs. The
  document tabs you had open come back, because the app keeps the same local
  storage for your instance that a browser would.
- **The size is remembered, the position is not.** A window restored onto a
  monitor that is no longer attached is invisible with no way to fetch it back.
  The first launch opens at 1280 × 860, the window cannot be made smaller than
  700 × 500, and the size is not recorded while the window is minimised or in
  full screen — so neither state becomes the size you get next time.
- **On macOS the window has no title bar of its own.** salt.md's own top bar is
  the chrome; the traffic lights sit inside it, and the app supplies the spacing
  around them. Drag the window by the empty space in the sidebar header or the
  tab bar.
- **Right-click gives you salt.md's own menus** — on a page in the sidebar, on a
  row, on a card. The browser's own menu is suppressed so it cannot open on top
  of them. It is kept in exactly two places: where you are typing, and on text
  you selected first. There you get Copy and the spelling suggestions, and the
  system spell checker underlines misspellings as you write. Right-clicking a
  word in ordinary text with nothing selected gives no menu at all.
- **The View menu** has Reload (⌘R / Ctrl+R), Force Reload, Actual Size, Zoom In,
  Zoom Out, Toggle Full Screen and Toggle Developer Tools. **Edit** is the
  standard one: Undo, Redo, Cut, Copy, Paste, Delete, Select All — plus, on
  macOS, Paste and Match Style and the system Substitutions and Speech submenus.
- **The last entry in the File menu differs by platform.** On macOS it is Close
  Window (⌘W), which closes the window rather than a page you were editing and
  leaves the app running in the dock. On Windows it is Exit and on Linux Quit
  (Ctrl+Q) — there the last window closing ends the app.
- **Links to anywhere else open in your real browser.** A bookmark block, a URL
  property, a link in a document — they leave the app rather than replacing your
  workspace with somebody's website in a window with no back button.
- **No browser permissions are granted.** Notifications, clipboard reads, camera,
  microphone and location are all refused, because the window loads a remote
  page and is treated as one.
- **Launching it twice focuses the window you already have** instead of opening
  a second one. On macOS, clicking the dock icon brings a closed window back.
  The dock icon follows your system's light or dark appearance, and changes with
  it while the app is open.
- **The app does not announce itself as Electron.** It removes that token from
  what it tells websites it is. Identity providers refuse a sign-in page to
  anything that says Electron, and this window is a visible one with no script
  of the app's own in the page.
- **Help → salt.md documentation** opens this wiki. **Help → About this app**
  shows the app's version and which instance it is connected to.

One consequence worth knowing: anything the interface opens **in a new tab**
opens in your browser, even when it points at your own instance. That is the
**Print / as PDF** view of a document ([Pages](pages.md)) and a file that cannot
be previewed in the panel ([Files](files.md)). Those pages need a signed-in
browser — which, if you signed in through the browser, is the browser you used.

## When the server cannot be reached

The app does not show the browser's error page. A failed load lands on the
connect screen with the address prefilled and a line naming it, scheme and all:
*Could not reach https://salt.example.com. Is it running?*

The usual causes, in the order they happen: the laptop is not on the network;
the instance is only reachable through a VPN or a tunnel that is not up (see
[Reaching it from outside](domain.md)); the server is stopped. Press **Back to
my workspace** to try the same address again once the cause is gone, or type a
different one. [Troubleshooting](troubleshooting.md) covers the server side.

## Building it

The app lives in `desktop/` in the repository and is built with Electron:

```sh
cd desktop
npm install
npm start            # run it
npm run check        # assert the address parser
npm run pack         # an unpacked build, for testing
npm run dist         # an installer for the platform you are on
npm run dist:mac     # or name one: dist:mac, dist:win, dist:linux
```

Builds land in `desktop/dist/`.

What the build is **configured** to produce is not the same as what anybody has
produced, so the table says both. A target nobody has built does not work until
proven otherwise.

| Platform | Configured to build | Ever built | Signed |
| --- | --- | --- | --- |
| macOS | `.dmg`, for Apple silicon and Intel | yes | yes, and notarised |
| Linux | AppImage and `.deb` | yes | no, and none is expected there |
| Windows | an NSIS installer, 64-bit | no | no — see above |

`.github/workflows/desktop.yml` builds macOS and Linux on a tag of the form
`desktop-v*`. Windows is left out of it deliberately.

Cross-building is why this needs a build server rather than one machine: an
NSIS installer wants Wine on a Mac, and the Linux targets want a Linux machine
or a container.

**Signing.** macOS builds are signed and then notarised automatically when a
Developer ID certificate is in the keychain **and** `APPLE_ID`,
`APPLE_APP_SPECIFIC_PASSWORD` and `APPLE_TEAM_ID` are in the environment at
build time; the hardened runtime and entitlements are already configured. With
no certificate the build skips signing altogether — notarisation is part of the
signing step and never runs, so the three variables change nothing on their own.

An unsigned build still works, and macOS then tells whoever opens it that the
app is damaged — that message is about the missing signature, not about the
file. Removing the quarantine flag by hand gets past it:

```sh
xattr -dr com.apple.quarantine /Applications/salt.md.app
```

That is acceptable for your own machine and not something to ask a colleague to
do — and it is the reason the released builds are signed and notarised, so that
nobody ever has to. Linux builds carry no signature and are not expected to.

**Why Finder writes "salt.md.app".** macOS hides the `.app` extension — except
when hiding it would leave a name that ends in another known extension, and
`salt.md` reads as a Markdown file. The menu bar, the dock and the About box say
**salt.md**; Finder and Spotlight say salt.md.app. Nothing overrides it, and a
dot in the name is not the cause: `salt.x.app` shows as *salt.x*.

## Related pages

- [Getting started](getting-started.md) — installing the server itself
- [Single sign-on](sso.md) — signing in with a company account
- [Account](account.md) — sessions, two-factor, ending a session everywhere
- [Agent access](agent-access.md) — why the desktop app is not an agent
- [Self-hosting](self-hosting.md) — running the instance the app points at
