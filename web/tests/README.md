# Browser regressions

These tests use synthetic data only. They build a temporary Salt binary, start it
on a randomly selected loopback port with a fresh temporary data directory, and
remove it afterwards. They never connect to an existing Salt instance.

From the repository root, with Go and Node.js installed:

```sh
npm ci --prefix web
npm run build --prefix web
npm ci --prefix web/tests
npx --prefix web/tests playwright install chromium
npm test --prefix web/tests
```

`GO_BINARY` may select a Go executable. To use an already installed Chrome instead
of downloading Chromium, skip the browser installation and set
`PLAYWRIGHT_CHANNEL=chrome` when running the tests.

The browser dependency is isolated in this test package; the application has no
additional runtime dependencies. Tests do not upload fixtures or screenshots.

Inline comments cover selection creation, saved anchors after reload,
resolve/reopen, narrow light/dark navigation, localized stale-anchor errors in
German, Spanish, Simplified Chinese and Traditional Chinese, draft retention, detachment and deletion.

Callout coverage: all seven preset icons, immediate background changes when
cycling icons, existing saved blocks, light/dark appearance, dark-mode text
contrast, and persistence after reload without changing the callout text.

Property ordering covers dragging in both directions and across multiple rows,
Save/Cancel, persistence after reload, unchanged row values and view settings,
ignoring unrelated drags, and narrow layouts.

Menus and arrows covers four reported bugs: a right-click on a view's tab acts
on that view (#24), a template opens its menu on a right-click and closes it on
a click elsewhere (#20), a row menu at the foot of the sidebar's tree stays
whole and on top (#21), ↑ from the second block stops in an empty first block
instead of jumping to the title (#25), and the library's shelves scroll inside
a narrow pane (#19).

Language covers which catalog a browser gets (Traditional and Simplified tags
reach their own scripts, a regional tag still reaches its language), both Chinese
choices surviving a reload, the editor's separate Traditional dictionary and
slash-menu groups, lists that used to be constants, and Enter leaving a word
alone while an input method is composing it (#124).

Shortcuts covers the single letters (#23): each acts only while nothing is
being typed, stays a plain letter while something is, and appears in the sheet.

Embeds covers a collection inside a document (#22, #26): the embed keeps its
filters to itself and across a reload, a second embed and the collection's own
page still show the saved view, and the embed's toolbar leaves out what belongs
to the collection page.

People covers a person property with several people and a list of who can be
picked (#11): only those are offered, nobody else can be typed in, the list
stays open for several picks, the setting is in the Properties dialog, and a
row holding several keeps them until somebody picks one after a switch back.

Filtered creation covers persisted filter defaults, keeping the collection open,
creation from an embedded collection, and leaving ambiguous alternatives empty
with an explanation when the new row falls outside the view.

Signed-in connections covers ending an agent's sign-in from the interface
(#115): the consent screen names the place, the row under API tokens →
Signed-in connections shows the client, its scope, its workspace and the
address it was last used from, and ending it there (after a confirmation)
leaves its access token and its refresh token useless.

Reply and inbox coverage uses two browser sessions for live delivery, reply drafts,
failed sends, exact links (including reopening the same notification), read state,
thread resolution and phone layouts. Real server conflicts are rendered in German,
Spanish, Simplified Chinese and Traditional Chinese, retaining the reply draft. Run the focused check
with `GO_BINARY=/path/to/go PLAYWRIGHT_CHANNEL=chrome npm run test:comments --prefix web/tests`.
Go coverage includes migration/restart persistence, atomic failure, concurrent retries,
MCP/REST attribution, coded errors, recipient deduplication, credential scope,
read retention, permission-aware keyset batches, typed-inbox migration and targeted
deletion invalidations. Anchored roots with replies cover thread/highlight
resolution, stale-anchor rejection and deletion without losing replies.
