# Trash and recovery

No page in salt.md is deleted by clicking one button. **Move to trash** takes a
page and everything under it out of the way; the trash sits at the bottom of the
sidebar and gives it back. Permanent deletion is a second, separate act — and
after a while the trash performs that act itself, which is the part most people
do not expect. This page covers what trashing changes, what restoring restores,
and what is genuinely gone.

(Comments are the exception in the product. Deleting your own comment happens the
moment you press the trash icon beside it: no confirmation, no trash, no way
back. See [Comments and notes](comments-and-notes.md).)

Rows in a [collection](collections.md) are pages, so everything here applies to
them too.

## Moving something to the trash

The action is called **Move to trash** everywhere it appears:

| Where | How to get there |
| --- | --- |
| Sidebar | the ⋯ beside a page, or a right-click anywhere on its row |
| A database row in the sidebar | the same — ⋯ or right-click |
| The open page | the ⋯ in the topbar (only if you may edit the page) |
| A board card | the ⋯ on the card, or a right-click on the card |
| A template | the ⋯ beside it in the Templates section, and the **Delete** button in the template gallery — both move the snapshot to the trash |

From the sidebar and from the page's own menu it happens immediately, with no
confirmation. The board asks first — *Move “…” to the trash?*, confirmed with
**Move to trash** — because a card carries no undo of its own.

Three things happen at once:

- **The whole subtree goes.** Sub-pages, their sub-pages, and — for a
  collection — every row in it. They are marked with one shared timestamp, and
  that batch is what restoring uses later.
- **Live editors are disconnected.** Anyone with that page (or one of its
  sub-pages) open in an editor is dropped from the shared document.
- **You are moved off it, and its tab closes.** If you were looking at the page,
  salt.md switches to another open tab, or to a page in the tree. The tab of a
  trashed page is removed from the tab bar in every open browser.

Other people's browsers follow along without a reload: the sidebar, boards and
lists all update from the change feed. One gap: when an **agent** trashes a row
over MCP, an open board is not told about that row, so the card stays on screen
until the view is reloaded. Trashing in the browser does tell it.

Trashing needs write access — the same permission as editing. A **viewer** in
the workspace cannot trash anything, and neither can someone holding emergency
access, which is read-only by design. The menu entry is still shown to a viewer;
pressing it fails at the server. See [Permissions](permissions.md).

## What a page in the trash does

It keeps everything: its blocks, its properties, its comments, its raw trail,
its version history, its uploads, its public link. What changes is that it stops
taking part.

| It is absent from | Note |
| --- | --- |
| The sidebar tree | it moves into the Trash section instead |
| [Search](search.md) | both the passage search and the page-title fallback skip it |
| The [library](library.md), the graph and the tag counts | — |
| **Linked from** under a page, and the **← Backlinks** column in the library | it disappears from the backlinks of every page it pointed at |
| Every [view](views.md) of its collection — table, board, list, gallery, calendar and timeline | a trashed row is not a row any more |
| Rollups and backrelations | a rollup counting related rows stops counting it |
| The [file list](files.md) | its uploads are hidden while it is away |
| Favourites | it drops out of the list; the star itself is remembered |
| The [calendar feed](automation.md) and workspace exports | — |
| Templates | a trashed template is not offered |

Two further consequences worth knowing:

- **Its public link stops working.** A shared page in the trash answers **Not
  found** to anyone opening the link. If the share carries a password, the
  visitor still sees the *Protected page* prompt first — the password is checked
  before the trash is, so a wrong password never reveals that the page was
  thrown away, and the right one produces Not found. The link is not revoked:
  restore the page and the same address works again. See
  [Sharing](sharing.md).
- **A collection's public [form](forms.md) stops accepting submissions.** The
  form link answers not found while its collection is in the trash.

**Editing is refused — but not on every route, and not with one message.** In the
browser the question does not arise, because a trashed page cannot be opened.
Over the API and over MCP it matters, and the exact answer differs:

| What you send | What comes back |
| --- | --- |
| `PATCH /api/pages/{id}` with a title, icon, cover, content or properties | refused, HTTP 409, *page is in the trash* |
| `set_properties` over MCP | refused: *page "…" is in the trash* |
| `update_page` or `write_content` over MCP | refused: *page "…" not found* |

Everything else still works on a page in the trash: adding a comment, adding a
trail note, an agent checking in with `working_on`, restoring an older revision
(which really does rewrite the trashed page's title and content), changing tags,
visibility or description, and moving the page to another parent. Nothing warns
you that the page you are writing to is in the trash.

A **relation** pointing at a trashed row keeps the id but loses the name: the
chip reads *Untitled* until the row comes back. Nothing was overwritten — the
picker only offers live rows, so it no longer knows the title.

## The trash in the sidebar

The **Trash** section sits below Templates, above your account row. It appears
only when there is something in it, and it belongs to the **workspace you are
currently in** — switching workspaces switches the trash.

The number beside the label counts **entries, not pages**. Throwing away a page
with twelve sub-pages adds one entry; the twelve travel inside it. A single row
thrown away from a board is an entry of its own and can be restored here — this
is the only place a board card has a way back.

Open the section and each entry shows its icon, its title with a line through
it, and two buttons:

- **Restore** (the arrow) — puts it back, no confirmation.
- **Delete forever** (the cross) — asks first, then it is gone.

The order is neither alphabetical nor by when things were thrown away: entries
arrive in the order the sidebar list itself is sorted, by the position number a
page carries among its siblings. A former sub-page can therefore sit above a
former top-level page. There is no "empty the trash" button, no search of the
trash, and no view of what is inside an entry — you restore it to see it.

A **viewer** sees the section and both buttons like everybody else. Neither one
works: the server refuses, and nothing visible happens.

## Restoring

1. Open the **Trash** section in the sidebar.
2. Find the entry and press **Restore**.

The page comes back with its content, properties, comments, trail, history,
tags, files and share link, exactly as it was. Its search entries are rebuilt,
so it is findable again straight away.

Two rules decide where it lands, and both exist to stop a restore recreating
something broken:

- **A missing parent means the top level.** If the page used to sit under
  another page and that parent has been permanently deleted, or is itself still
  in the trash, the restored page becomes a top-level page in its workspace
  instead — the parent link is cleared outright. It is not lost; it is one level
  up from where you expect it. This applies to database rows too: restore a row
  while its collection is still in the trash and you get an ordinary page at the
  top level, with its property values but no table to belong to. Restore the
  collection first.
- **Only its own batch comes back.** Everything trashed together is restored
  together. A sub-page that had been thrown away *earlier*, on its own, keeps
  its own place in the trash — restoring the parent does not quietly resurrect
  it. It reappears in the trash list as an entry of its own.

## What is permanent

**Delete forever** asks *Delete this page and all its sub-pages forever?* and
the confirming button is labelled **Delete**. There is no undo afterwards.

Gone with it: the page and its whole subtree, their content and properties,
comments, the raw trail, every revision in the version history, the public share
link, favourite entries, backlink records, and their search entries.

Two things survive, and both surprise people:

- **Uploaded files stay on disk.** The file itself is not deleted; its record
  loses the page it hung off and shows up in the workspace file list as a file
  with no page. Only deleting a whole workspace or an account clears uploads
  away.
- **The audit entry stays.** Who deleted what, and when, outlives the page —
  that is the point of it. See [History and audit](history-and-audit.md).

The only route back from a permanent deletion is a backup. Instance settings →
**Maintenance** → **Download backup (.tar.gz)** takes the whole database and
every upload, and a backup contains the trash as well as the live pages. The
Maintenance panel is open to instance admins only, and it names both halves of
the route: putting an archive back is `./salt restore backup.tar.gz` on the
server, and `./salt backup` writes the same archive from cron without a browser.
See [Self-hosting](self-hosting.md).

### The trash empties itself

By default, anything that has been in the trash for **30 days** is deleted
permanently, without asking. The sweep runs every half hour while the server is
up, and it writes nothing to the audit log — unlike **Delete forever**, which
records who pressed it.

It works **per page, not per batch**: each page goes when its own trashing date
passes the cutoff. A sub-page thrown away three weeks before its parent
therefore expires three weeks earlier, and restoring the parent afterwards gives
it back incomplete, with nothing on screen to say a piece is missing.

An instance admin changes the retention in Instance settings → **General** →
*Empty the trash automatically after (days, 0 = never)*. The range is 0 to 3650
days; **0 switches the sweep off entirely**, and then nothing ever leaves the
trash on its own. Instance settings → **Maintenance** shows the live figure as
*Pages (trashed)*.

Self-hosters can set the same number with the `SALT_TRASH_DAYS` environment
variable, which is read afresh at every sweep — change it and restart, and the
next sweep uses it. The admin setting wins over it, and there is a trap in that:
the settings dialog sends the retention value with every save, so the first time
anybody presses **Save** in Instance settings the number is written to the
database and the environment variable is ignored from then on. See
[Administration](administration.md).

### Deleting a workspace or an account

These do not use the trash at all, and neither is reversible.

Deleting a **workspace** removes every page in it, live and trashed, immediately.
It asks you to type the workspace name into the prompt (*Type the workspace name
to confirm:*) and the button is **Delete permanently**.

Deleting an **account** destroys the personal spaces it takes with it, just as
finally. It asks differently: a summary of what will disappear — which personal
spaces and how many pages, which shared workspaces stay, which are left with
nobody in charge — confirmed with a plain **Delete**. There is no name to type.
Both are covered in [Workspaces](workspaces.md) and
[Administration](administration.md).

## Over MCP

One tool covers both directions, because both are reversible:

```
set_trashed(page_id: "…", trashed: true)    # to the trash, with its subtree
set_trashed(page_id: "…", trashed: false)   # back out
```

What an agent should know before using it:

- **There is no permanent deletion over MCP.** No tool in the catalogue deletes
  a page for good. That act belongs to a person in the browser.
- **`set_trashed` with `trashed: false` restores only the page you name.** It
  does not bring the subtree with it, and it leaves the parent link pointing at
  the trashed parent. The page is not stranded — the sidebar draws any page
  whose parent is not visible at the top level — but it slides back underneath
  as soon as that parent is restored, which the **Restore** button in the
  sidebar never does, because that one clears the parent link. Restore from the
  top of the subtree downwards, or use the browser.
- **A trashed page can still be read** — `get_page` returns it. Not with
  `include_children` (or `recursive`): that form walks the subtree and answers
  *page "…" not found* for a page in the trash.
- **Not every write is refused.** `update_page`, `write_content` and
  `set_properties` are (see the table above). `note`, `comments` with
  `action: "add"`, `working_on` and `revisions` with `action: "restore"` are
  not — they write to a trashed page without complaint, and the last one
  replaces its title and content.
- **Read `in_trash`, not `can_write`.** `get_permissions` answers both, and
  `can_write` reports your role and your token's scope only: it stays `true` for
  a page in the trash, and `read_only_reason` stays empty. An agent that checks
  `can_write` alone learns nothing about the trash.
- **The trash cannot be listed.** `list` with `kind: "pages"` returns live pages
  only, `search` skips trashed pages, `query_rows` and `get_collection` no
  longer return a trashed row, and `list` with `kind: "templates"` hides a
  trashed template. An agent that trashed something and wants it back needs the
  page id it already had.
- **A read-only token cannot trash or restore at all.** `set_trashed` counts as
  a mutating tool and is refused.

Trashing over MCP is recorded in the audit log like any other agent write. See
[MCP tools](mcp-tools.md) and [Agents](agents.md).

## Over the API

`/api/pages/{id}` with `DELETE` moves a page to the trash; `?permanent=1` on the
same request deletes it and its subtree outright, whether or not it was in the
trash first. `/api/pages/{id}/restore` is the way back. A read-only API token is
refused on all three.

Two routes ignore the trash, and both are worth knowing because they have no
equivalent in the browser:

- **Duplicating a trashed page brings it back alive.**
  `/api/pages/{id}/duplicate` and the `duplicate_page` tool do not check the
  trash, and the copy is created without one — so you get a live *Copy of …*
  beside the original, which stays where it was. It is a second route out of the
  trash, and the only one that leaves the original behind.
- **A trashed page still accepts a move.** A `PATCH` that only changes
  `parentId` or `position` goes through, so a page can be reparented while it is
  in the trash. Moving it to another **workspace** is refused; a trashed
  *sub-page* does travel along when its live parent is moved, and lands in the
  target workspace's trash.

**Webhooks.** A [webhook](webhooks.md) subscriber receives one `page.trashed`
event **per page in the subtree**, so a receiver watching a single page hears
about it. Three things that event does not tell you: a permanent deletion sends
the same `page.trashed` burst, so the two cannot be distinguished; a restore
sends nothing at all; and `set_trashed` over MCP sends no webhook either — only
the API and browser route fires them.

Full details in the [API reference](api.md).
