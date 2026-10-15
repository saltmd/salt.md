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

Shortcuts covers the single letters (#23): each acts only while nothing is
being typed, stays a plain letter while something is, and appears in the sheet.

Embeds covers a collection inside a document (#22, #26): the embed keeps its
filters to itself and across a reload, a second embed and the collection's own
page still show the saved view. Column visibility inherits the shared view until
changed locally; show/hide all and individual toggles persist across reloads
without affecting other pages or a second block in the same document.
The embed's toolbar leaves out what belongs to the collection page.

People covers a person property with several people and a list of who can be
picked (#11): only those are offered, nobody else can be typed in, the list
stays open for several picks, the setting is in the Properties dialog, and a
row holding several keeps them until somebody picks one after a switch back.

Properties autosave covers debounced writes, immediate close, edits during a
pending close, reverting during a pending write, failed writes and retry,
deletion confirmation, and removed filters staying removed after recreation.

Inline checkbox covers slash insertion within a table cell, persistence, undo/redo,
collaborative updates, read-only access and Markdown/HTML export.

Slash-menu regression covers unique section headings, filtering with `/ch`,
clearing the query and a query with no matches.

Collection creation covers relation and status defaults from a block's local
filters, unchanged shared views, visibility after returning, opening the created
page, duplicate protection during slow requests, and retry after an error.
The check gate also tests typed defaults, alternatives, conflicting filters,
read-only properties and explicit board/calendar presets.

Empty-page coverage advances past a minute while the new page remains open,
then checks disposal only after navigation, immediate title/body edits before
debounce, existing blank pages and another active editor. Server tests cover
recoverable disposal, creation identity, changed properties and structured content.

Trash navigation returns a deleted row or child document to its parent and
removes its open tab.

Mermaid coverage checks flowchart and sequence rendering after dependency upgrades,
SVG images, saved diagram persistence, reload and HTML export.
