# The blueprint library

The ready-made workspaces the library offers. Embedded into the binary with
`go:embed`, so a fresh install has them without a network, an account, or a
call home.

**They are plain JSON, not ZIP archives.** The import reads an `fs.FS`, and a
`zip.Reader` is one — so an uploaded archive and a shipped blueprint take the
same path. Keeping these unzipped costs nothing and buys the thing a ZIP can
never give: a change to a blueprint shows up in a diff and can be reviewed like
any other change.

Layout, one directory per blueprint, named after its id in `library.json`:

```
library.json          the shelf: id, title, one line, icon, colour, price
<id>/salt-workspace.json   manifest — name, icon and THE RULES
<id>/pages.json            the databases with their schemas and views
<id>/tags.json             tag colours (optional)
<id>/files/…               uploads (optional; keep it empty, this ships in the binary)
```

## Rules for what goes in here

**English.** These ship with the product and are read by strangers. Column
names, option names and the workspace rules are all English; the library's
titles and taglines go through `t()` in the browser like every other string.

**No rows and no documents.** A blueprint carrying somebody's tasks is not a
blueprint. `pages.json` holds `type: "collection"` entries only — the import
enforces it as well, this is about not writing them in the first place.

**Ids are 32 hex characters.** The import rewrites every id in the file by text
substitution, and that is what makes a relation point at the copied database
instead of the original. An id of any other shape silently keeps pointing at
nothing. The convention here is `5a17b<n>` + zeros + a counter.

**Counts are never written down.** The shelf reads how many databases, columns
and views a blueprint has out of the blueprint itself. A number typed beside it
would be wrong within two edits — the same lesson as the version column that
used to sit in CLAUDE.md.

**Nothing that needs a file.** Covers and images would go into the binary and
stay there for every user forever. The look comes from an icon and an accent
colour.
