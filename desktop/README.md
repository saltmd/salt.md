# salt.md desktop

A window onto a salt.md server you run. Not a local instance, not a copy of the
product — a shell, so your workspace opens where you left it instead of living
in a browser tab among thirty others.

```sh
npm install
npm start          # run it
npm run check      # assert the address parser
npm run dist:mac   # build a .dmg (also :win, :linux)
```

## What it does beyond loading a page

- **Asks the server whether it is one** before saving an address. Typing
  something that answers nothing and landing in a blank window is the worst
  first five minutes this app could have.
- **Stays on your instance.** Any other link opens in your real browser. Without
  that, one click on a bookmark block replaces your workspace and there is no
  address bar to get back from.
- **Gives an unreachable server a page, not a crash.** A laptop opened on a
  train gets an explanation and the field to change the address — Chromium's own
  error page is a dead end.
- **No Node in the renderer.** `contextIsolation` on, `nodeIntegration` off,
  `sandbox` on, and a preload exposing exactly three functions. A shell that
  loads a remote page with Node in it hands that server the user's filesystem.
- Remembers the window size but **not its position**: a window restored onto a
  monitor that is no longer attached is invisible with no way to fetch it back.

## Signing

macOS builds are notarised when `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD` and
`APPLE_TEAM_ID` are in the environment. Without them the build still works and
macOS will tell whoever opens it that the app is damaged — which is what an
unsigned app looks like from the outside.

Windows and Linux builds are unsigned for now.

## Building it

```sh
npm install
npm run dist:mac     # → dist/salt.md-<version>-arm64.dmg  (also :win, :linux)
```

The icon is built from the product logo: `build/icon.svg` is the source,
`build/icon.png` (1024×1024) is what electron-builder turns into `.icns` and
`.ico`. The mark on its own is transparent and made of thin strokes — at 32px
in a dock it would vanish — so it gets a field in the house green and sits at
about 60% of the tile. macOS icons are not drawn edge to edge, and one that is
looks bigger than everything beside it.

## Unsigned builds and what they look like

Without signing, macOS attaches a quarantine flag on download and then refuses
to open the app with **"salt.md is damaged and can't be opened"**. That message
is about the missing signature, not about the file.

Until signing is set up, the way past it is:

```sh
xattr -dr com.apple.quarantine /Applications/salt.md.app
```

That is fine for you and unacceptable for anybody you hand the app to — nobody
should be told to run a shell command to open an application.

**Signing** happens automatically when `APPLE_ID`,
`APPLE_APP_SPECIFIC_PASSWORD` and `APPLE_TEAM_ID` are in the environment at
build time; the entitlements and the hardened runtime are already configured.
Windows and Linux builds stay unsigned for now.

## Why Finder writes "salt.md.app"

macOS hides the `.app` extension — except when hiding it would leave a name that
ends in another *known* extension. `salt.md` looks like a Markdown file, so
Finder shows the whole thing rather than risk an app that reads as a document.

Measured rather than guessed, because the obvious explanation ("a dot in the
name") is wrong:

| bundle | Finder shows |
| --- | --- |
| `salt.app` | salt |
| `saltmd.app` | saltmd |
| `salt md.app` | salt md |
| `salt.x.app` | salt.x — **a dot is fine** |
| `salt.md.app` | salt.md.app |

Nothing overrides it. `CFBundleDisplayName`, `LSHasLocalizedDisplayName` with a
localised `InfoPlist.strings`, and the per-file "hide extension" flag were all
tried and all ignored; the refusal is deliberate.

Where the name actually appears:

| surface | shows |
| --- | --- |
| menu bar, Dock, About | **salt.md** |
| Finder, Spotlight | salt.md.app |

The only way to change the last row is to name the bundle `salt.app`, and then
those two surfaces say "salt" — which drops the half of the name that says what
the product is. Left as it is on purpose.

## Changing the server afterwards

Two ways, and the second is the one that matters:

- **Settings…** in the app menu (⌘,)
- **On the sign-in screen**, a quiet line at the bottom: *Connected to
  example.com · Change*

The second exists because that is the moment somebody realises they are at the
wrong instance — staring at a login they cannot use. It appears only there;
on a signed-in workspace it would be permanent furniture for something you need
about twice.

It is injected by the preload rather than built into salt.md's own login page,
for the same reason the window CSS is: the app must not require a matching
server version. This works against instances released before the app existed.

The connect screen offers **Back to my workspace** whenever a server is already
configured — opening the settings and finding no way out is the complaint this
whole thing answers, and repeating it one level down would be worse.

## The `salt://` scheme

Claimed by `CFBundleURLTypes` in the packaged app, so macOS knows about it from
the moment it is installed — no run required.

**A development run must never claim it.** `npx electron .` used to register the
Electron binary under `node_modules` as the handler, which then answered
`salt://` with its own welcome screen and left the installed app unreachable.
`app.isPackaged` now gates the runtime registration.

If the handler ever ends up pointing somewhere wrong, LaunchServices is the
place to look and to fix it:

```sh
LS=/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister
$LS -u /path/to/the/wrong.app     # forget it
$LS -f -R /Applications/salt.md.app
```

Stale copies matter too: a deleted bundle stays in that database, and one
carrying the same bundle id can win the scheme from the real app.
