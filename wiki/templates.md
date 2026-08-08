# Templates and blueprints

Two ways to start from something instead of from nothing. A **template** copies
one page — with everything filed under it — into a new page. A **blueprint**
sets up a whole new workspace: its databases, their columns and views, and its
house rules. They are separate mechanisms with separate dialogs, and this page
covers both.

The one sentence that governs templates is the one the dialog itself shows:
*A template is a snapshot: using one copies it, and from then on the two have
nothing to do with each other.*

## What a template is

A template is an ordinary page carrying a flag. It has a body, an icon, a
cover, sub-pages, and — if it is a collection — a schema, views and rows.

The flag **adds a place rather than moving one**. The page is listed in a
**Templates** section of its own, and it also stays exactly where it was filed
in the tree. A template saved from a page under *Handbook* is still drawn under
*Handbook*. A template made from a collection is still drawn in **Collections**
and counted there like any other collection.

One number disagrees with its own tree, and it is worth knowing before you go
counting: the figure beside **Documents** leaves templates out, while the tree
underneath it still draws them. So a workspace with one page and one template
made from it shows *1* beside Documents and two rows below it.

**Save as template** does not flag the page you are looking at. It duplicates
the page and flags the *copy*. That asymmetry is the whole design:

- the page you were working on stays an ordinary page and keeps its place;
- later edits to it never change what the template offers;
- pages made from the template never change the template either.

Nothing is linked, so nothing can be broken from a distance. The cost is that a
template does not follow the page it came from — when the real page moves on,
you save a new template, or open the template and edit it like any other page.

## Saving a page as a template

1. In the sidebar, hover the page and press **⋯** — or right-click the row. A
   right-click opens the same menu, on pages, on collections and on database
   rows, so you never have to hunt for a button that only appears on hover.
2. Choose **Save as template**.

A short message says **Saved as template**, or **Could not save as template**
if it failed. You need write access to the page; a viewer cannot save one. See
[Permissions](permissions.md).

What travels into the snapshot:

| Travels | Stays behind |
| --- | --- |
| the page body, icon and cover | its tags |
| every sub-page, however deep | its description |
| database rows, with their property values | favourites |
| the collection's schema and views | later edits to the original |

Tags and descriptions are not copied. That matters more than it sounds, because
the gallery's category buttons are built from a template's tags and the second
line under each entry is its description — a freshly saved template has neither
until you open it and add them.

### Giving a template its tags and description

Both live on the template page itself, not in the gallery:

1. Open the template — click its row in the **Templates** section.
2. For the description, open the **⋯** menu at the top right of the page and
   choose **Add a description**. A text field appears under the title, with
   the placeholder *Add a description…*. The same menu offers **Remove
   description** once there is one.
3. For tags, use the field under the title. It reads **+ Add a tag** while the
   page has none and **+ Tag** afterwards.

Each tag you use becomes a category button in the gallery, so it is worth using
the same few words across a workspace rather than a new one each time.

### Where the template lands

The template lands **next to the page it was made from**, as its sibling, and
it stays visible there. If you saved a template from a page filed under
*Handbook*, the template sits under *Handbook* in the tree and is listed in the
Templates section at the same time. Pages you later make from it appear under
*Handbook* as well.

Only the parts of the subtree you are allowed to read are copied. A private
sub-page belonging to someone else is left out, and so is everything under it.

### Saving a database row as a template

A database row carries the same **⋯** menu as a page, so **Save as template**
is offered on one — and what you get is rarely what you wanted:

- The copy lands as a sibling of the row, which makes it another child of the
  collection. The collection lists every child that is not in the trash, so the
  template shows up in the table as an ordinary extra row.
- It does not reach the **Templates** section or the gallery at all. A row with
  no sub-pages of its own is left out of the page list the sidebar is built
  from, and the flag does not change that.
- An agent can still see it: `list` with `kind: "templates"` reads the flag
  directly and finds it.

If you want a reusable row, keep the shape in an ordinary page and template
that instead.

## The Templates section

The section appears in the sidebar once the current workspace has at least one
template, and starts collapsed the first time; after that it remembers whether
you left it open. The number beside it is how many there are. Templates belong
to a workspace: switch workspaces and you see that workspace's templates.

| Control | What it does |
| --- | --- |
| **＋** on the section (**New page from a template**) | opens the template gallery |
| **＋** on a row (**New page from this template**) | copies that template straight away and opens the copy |
| **⋯** → **Remove template flag** | turns it back into an ordinary page: it leaves this list and stays where it already was in the tree |
| **⋯** → **Move to trash** | throws the snapshot away — recoverable, see [Trash and recovery](trash-and-recovery.md) |
| clicking the row | opens the template so you can edit it |

Those two entries are the whole menu here — a template row does not offer the
page menu the tree rows have, and does not answer a right-click. To move a
template, duplicate it or export it, use its row in the tree instead.

If a copy cannot be made you get **The template could not be used**; a failed
flag change says **Could not be changed**.

**In a workspace set to the mixed tree** there is one section called **Pages**
instead of **Documents**, and no **Collections** section at all. Templates
behave the same way in both: listed in the Templates section, and still drawn
in the one tree where they were filed.

## The template gallery

The gallery is the screen for choosing a template you have not memorised: a list
on the left, the template's own content on the right, and one button that says
what happens. Open it with the **＋** on the Templates section. Clicking outside
the dialog closes it.

**Search templates…** filters by title and description. Below it sit category
buttons — **All** plus one per tag used on any template in this workspace, in
alphabetical order. Each entry shows its icon, its title, its description and its
tags as `#chips`. The list is sorted by title, and one template is already
selected when the dialog opens, so **Use template** works from the first moment.

The right-hand pane is the template's **Markdown export**: exactly the text you
get from **Export Markdown** and from an agent reading the page, rendered as
plain text rather than as formatted blocks. A collection appears as a table of
its rows with one column per property — which is precisely what you want to judge
before copying one. While it loads you see **Loading…**; if it cannot be
produced, **No preview available.** A collection also carries a small table icon
beside the title in the preview heading; its tooltip reads **Collection**.

The preview shows the template page itself. Sub-pages are copied when you use it
but are not part of the preview.

The buttons along the bottom, left to right:

| Control | Notes |
| --- | --- |
| the count (*3 templates*) | every template in the workspace — it ignores the search box and the chosen category, so filtering the list down to one still reads *3 templates* |
| **Remove template flag** | only while a template is selected |
| **Delete** | only while a template is selected |
| **Close** | |
| **Use template** | |

**Delete** trashes the template immediately, with no confirmation step. It is a
normal trashing, so the page can be restored. After **Delete** or **Remove
template flag** the dialog stays open and the selection moves to the first
template still in the list.

When the search or the chosen category matches nothing, the list says **No
templates yet.** and the right-hand side shows *Save any page as a template from
its ⋯ menu — the page itself stays untouched.* That is also the empty state for
a workspace with no templates at all — which you will not reach in practice,
because the gallery's only entrance is the **＋** on the Templates section, and
that section is only there once a template exists.

## Using a template

1. Open the gallery, or use the **＋** on a template row.
2. Pick the template. Check the preview.
3. Press **Use template**.

You get a new page and are taken to it. It **keeps the template's title** — no
"Copy of" prefix, because a snapshot is meant to be named what it is. It is not
itself a template, and it belongs to you: you are the owner of the copy even
when the template was set up by somebody else.

The copy lands as a sibling of the template, so it appears in the tree where the
template's own parent is — at the top level, if the template is at the top level.

To move it afterwards, **drag it in the sidebar**: drop it on the middle of
another row to file it inside that page, and on the top or bottom edge of a row
to place it before or after. The **⋯** menu has no general move; it offers
**Move to top level**, and only when the page has a parent to leave, and a
**Move to workspace** list, and only when you belong to more than one
workspace. That last one works on templates too — a template can be moved to
another workspace like any page, and it becomes that workspace's template.

## Templates that are collections

Saving a database as a template snapshots its rows along with its schema and its
views. That is usually what you want for a small starter set — five example
rows, a board already grouped, a filter already set — and rarely what you want
for a full table.

One limit worth knowing: a relation column in a copied database still points at
the **original** target database, and copied rows still point at the original
rows. Duplication does not rewrite references. If you want two related databases
set up cleanly against each other, that is what a blueprint does — see below.
Relations and rollups themselves are explained in
[Relations and rollups](relations-and-rollups.md).

## Templates for agents

Over MCP a template is reached with three of the tools listed in
[MCP tools](mcp-tools.md):

| Call | What it does |
| --- | --- |
| `list` with `kind: "templates"` | every template you may read: id, title, icon, kind (`doc` or `collection`), description and workspace |
| `create_page` with `template_id` | makes a page from that template; only `title` applies alongside it, and it renames the copy |
| `save_as_template` with `page_id` | snapshots the page — the answer says *the page itself is unchanged* |

`duplicate_page` is the plain copy, with no template flag involved.

Two differences from the browser are worth knowing. `list` with
`kind: "templates"` returns templates from **every** workspace the connection
can reach and ignores `workspace_id`; each entry names its own workspace instead.
And an agent needs only *read* access to a template to build a page from it,
while **Use template** in the browser goes through
`/api/pages/{id}/duplicate` and asks for write access — so a template in a
workspace where you are a viewer can be used by an agent connected as you but
not by you in the interface.

## Blueprints: a whole workspace at once

A template copies a page. A blueprint sets up a workspace — which is a different
question, because everything that makes a workspace usable is invisible: the
rules, the option ids behind a select, the backrelations, the rollups, the view
filters. Rebuilt by hand it comes out almost-but-not-quite the same.

### Opening the shelf

Open the workspace switcher at the top of the sidebar and choose **New
workspace**.

The entry is only there if you may create one. Instance admins always may.
Everybody else may while the instance allows it — and when it is switched off
the entry is simply **absent**, with no explanation anywhere in the browser. An
agent or a direct API call gets the sentence instead: *creating workspaces is
disabled on this instance — ask an admin*. See
[Administration](administration.md).

The dialog is headed **Start with a ready-made workspace**, with the line *Each
one brings its databases, views and house rules — and no data. You fill it.*

The shelf holds:

- **Empty workspace** — *Start from nothing and build it yourself.*
- the three built-in blueprints below;
- under **Or like one you already have**, each of your non-personal workspaces,
  offering *Its databases and rules, without the content.* Personal workspaces
  are not offered.

**Cancel** at the bottom closes the dialog, and so does a click outside it.

Each ready-made card carries a facts line — so many databases, so many columns,
so many views, plus *house rules* when it has any. Those numbers are read out of
the blueprint itself rather than typed beside it, so the shelf cannot advertise
something the blueprint does not contain.

### What the built-in blueprints contain

They ship inside the salt.md binary, so a fresh install has them with no network
and no account. None of them contains a single row. The view names below are the
chips you see on the card's detail view; what each one is grouped or dated by is
in brackets.

**Software team** 🛠️ — *What we run, and what still has to be done to it.*

| Collection | Columns | Views |
| --- | --- | --- |
| **Systems** 🧩 | Status, Kind, If it goes down, Owner, Version, Runs on, Repository, Next check, Tasks (backrelation), Open, Progress | Table, Board (grouped by Status), Checks (calendar on Next check) |
| **Tasks** ✅ | Status, System (relation), Kind, Priority, Effort, Owner, Milestone, Due | Board (grouped by Status), Table, By system (board grouped by System), Dates (calendar on Due) |

*Open* counts the tasks whose status is not *Done*; *Progress* is the percentage
that are. Both are rollups over the backrelation, which is what makes "how much
of this system's work is finished" a column rather than a count you do by eye.

**Sales pipeline** 🤝 — *Companies on one side, deals on the other, and a board
you can drag.*

| Collection | Columns | Views |
| --- | --- | --- |
| **Companies** 🏢 | Status, Industry, Size, Owner, Website, Next touch, Deals (backrelation), Open deals, Pipeline | Table, Board (grouped by Status), Next touches (calendar on Next touch) |
| **Deals** 💶 | Stage, Company (relation), Value, Expected close, Owner, Came from | Board (grouped by Stage), Table, Closing (calendar on Expected close) |

*Open deals* counts the deals whose stage is not *Won*. *Pipeline* carries no
condition at all: it sums the Value of **every** deal on the company, won and
lost included. If you want only the open money there, add a condition to the
rollup — see [Relations and rollups](relations-and-rollups.md).

**Content calendar** 🗓️ — *Every channel, every piece, and the date it goes out.*

| Collection | Columns | Views |
| --- | --- | --- |
| **Channels** 📡 | Kind, Cadence, Owner, Link, Pieces (backrelation), Not out yet, Published | Table, Board (grouped by Kind) |
| **Pieces** ✍️ | Status, Channel (relation), Goes out, Owner, Topic, Published at | Calendar (on Goes out), Board (grouped by Status), Table |

*Not out yet* counts the pieces whose status is not *Published*; *Published* is
the percentage that are.

Every one of them carries **house rules** — a written page about how the
workspace is meant to be used, which agents connected to the workspace are told
to follow. See [Workspaces](workspaces.md).

### Creating one

1. Click a card. The detail view opens with the blueprint's tagline as its lead
   line, then each database: its icon, its name, its view chips, its own
   one-line description, and its columns with the real colours of their select
   options. Under those, **House rules** shows the opening **paragraph** of the
   rules document as plain text — the heading is skipped, and bold, italic,
   code and link markup are stripped rather than rendered. Choosing your own
   workspace instead shows *Copies the databases with their columns, options and
   views, plus the workspace rules. Rows and documents stay where they are.*;
   the empty card shows *No databases, no rules — a blank workspace.*
2. Fill in **Name** (placeholder *e.g. Team*). A ready-made blueprint pre-fills
   its own title — change it to what the workspace is actually for. Two ways
   back to the shelf: the **Back** button beside the create button, and the
   arrow at the left of the heading.
3. Press **Create workspace**, or press Enter in the name field. You are
   switched into the new workspace, as its admin.

Creating from one of the three ready-made blueprints is written to the
[audit log](history-and-audit.md) — that path is its own route,
`/api/library/{id}`. An **Empty workspace** and a copy of one of your own are
not: both go through `/api/workspaces`, which records nothing.

**If a workspace of that name already exists on the instance**, the new one is
created as *Name (Import)* — including when the existing one belongs to somebody
else and you cannot see it. That rename applies to the ready-made blueprints and
to imported archives, not to copying a workspace of your own.

Nothing on the shelf costs anything today. The mechanism for paid entries exists
and refuses rather than handing one out (*this blueprint has to be bought
first*), so a card that will not create is what that looks like.

### Copying a workspace you already have

**Or like one you already have** points at a workspace instead of a file. There
is no saved workspace-template object on purpose: the workspace that already
works *is* the blueprint, so it cannot drift out of step with how you actually
work.

What comes across: the workspace rules, every database that is not in the trash,
their full property schemas including option ids, and their views. What does
not: rows, documents, files, members and tag colours. The copied databases land
at the top level of the new workspace, in the order they had.

Two repairs happen on the way, and both are visible in the result:

- **Relations are re-pointed at the copied databases.** A relation whose target
  was *not* part of the copy loses its target instead of keeping it — the column
  shows as unconfigured. Left alone it would quietly read rows out of the old
  workspace, which looks like it works until somebody notices the numbers belong
  to another project.
- **View filters on a relation column are dropped.** No rows are copied, so such
  a filter could only match nothing; without it the view simply shows everything.

A workspace with no databases cannot be used this way: *workspace "…" has no
databases to copy — nothing to use as a blueprint*.

If you want the content too, that is an export and import rather than a
blueprint — see [Import and export](import-export.md).

### Blueprints for agents

An agent creates a workspace from an existing one with the `workspace` tool and
`from_workspace`: the same structure copy, the same repairs, no rows and no
documents. The built-in shelf has no MCP tool; it is a browser dialog that reads
`/api/library` and creates through `/api/library/{id}`. A connection limited to
particular workspaces cannot create new ones at all — it would not be able to
open them. See [Agents](agents.md).

## Which one do I want?

| | Template | Blueprint |
| --- | --- | --- |
| Copies | one page and everything under it | a whole workspace's structure |
| Includes content | yes — sub-pages, and a collection's rows | no |
| Includes workspace rules | no | yes |
| Where it comes from | a page you saved | the built-in shelf, or a workspace of yours |
| Lands | beside the template | in a brand-new workspace |
| Started from | the **Templates** section | the workspace switcher → **New workspace** |

## Limits worth knowing

- **No Templates section until there is a template.** The gallery has no other
  entry point, so the first one is always saved from a page's **⋯** menu.
- **A template does not update itself.** It is a snapshot of the moment you
  saved it. Keeping one current means editing the template or saving a new one
  and removing the old flag.
- **Tags and descriptions do not survive the copy**, so the gallery's categories
  stay empty until you tag the template page itself.
- **Templates are still pages.** They are found by [search](search.md), counted
  on the shelves in the [library](library.md), and they sit in the tree where
  they were filed. The one place the flag takes them out of is the **graph** —
  a template is not drawn there and neither are its connections.
- **A row's template is a trap.** Saved from a database row, it becomes another
  row of that collection and never appears in the Templates section.
- **Duplication does not rewrite references.** A copied database keeps its
  relation columns pointed at the original target.
- **Creating workspaces can be switched off** instance-wide, which removes the
  **New workspace** entry and with it the blueprint shelf — for everybody
  except instance admins, and with no message to say why.

Related: [Pages](pages.md) for the ⋯ menu the template is saved from,
[Collections](collections.md) and [Views](views.md) for what a database template
carries, [Properties](properties.md) for the column types the blueprints use.
