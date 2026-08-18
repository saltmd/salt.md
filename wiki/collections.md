# Collections

A **collection** is a page whose child pages are its rows. It carries a schema —
the typed properties every row can hold — and a set of views that decide how
those rows are drawn: a table, a board, a calendar, a form. The interface calls
it a Collection. Agents connected over MCP call the same object a **database**,
and both words are permanent; the last section of this page says why.

This page covers what a collection is made of, how to create one, what the
toolbar above it does, how rows behave as pages, how to change the schema, and
how collections nest inside documents and inside each other. The property types
themselves are in [Properties](properties.md); what each view type shows is in
[Views](views.md).

## What a collection is made of

Three things, and nothing else:

- **A schema.** An ordered list of property definitions, each with an id, a
  name and a type. The id is what values are stored under; the name is what you
  read on screen.
- **Rows.** Ordinary pages whose parent is the collection. Their property values
  live on the row itself.
- **Views.** Saved configurations — type, filters, sort, hidden columns, what a
  board groups by. A collection always has at least one; the last one cannot be
  deleted.

A collection page has no body text of its own. Where a document has an editor,
a collection has its table. That is also why the collection page carries no
comment button and no raw note trail — a row has both, the collection itself
does not. It does keep an icon, a cover, a description and tags, like any page.

## Creating one

In the interface there are three ways to create one:

1. **The `+` on the Collections section** in the sidebar (tooltip
   *New collection*). Creates one at the top level of the current workspace.
   This section only exists while the sidebar keeps documents and collections
   apart — see [Nesting](#nesting).
2. **The `+` beside any page** in the sidebar, then **Collection** in the little
   menu that opens (the other entry is **Page**). Creates it inside that page.
3. **Right-click a page** (or its `⋯`) and choose **New collection inside**.

There is a fourth entry in the editor's slash menu, **Embed a collection**, and
it is not a way in: it inserts a block that searches for a collection you
already have. See
[Collections inside documents](#collections-inside-documents).

Whichever route you take, a new collection starts with:

- one property, **Status**, a select with the options **To do**, **In progress**
  and **Done**;
- two views, **Board** (grouped by Status) and **Table**, in that order — so the
  board is what opens first.

Over MCP, `create_database` takes an optional schema in place of that default
Status property:

```
create_database(title: "Sites", parent_id: "<optional>", schema: [
  { name: "Status", type: "select", options: ["Active", "Planned"] },
  { name: "Opened", type: "date" }
])
```

Options may be plain strings as above, or objects with a colour
(`{"name": "Done", "color": "#2f9e44"}`). Ids are derived from the names
("Opened" → `opened`) unless you give them.

Two things about that call are easy to get wrong:

- **The two views are always the same two, whatever schema you pass**, and the
  board still groups by the property id `status`. Give your schema a select
  property that slugs to `status`, or repoint the board with `set_view`
  afterwards — otherwise the new database opens on a board saying
  *This board needs a Select property to group by.* with no rows on it.
- **Without a `parent_id`, pass `workspace_id`.** With neither, the database is
  created in your first workspace and nothing on screen says which one that was.
  Call `list` with `kind: "workspaces"` first.

## Rows are pages

**New** in the toolbar creates a row called *Untitled* and opens it immediately.
On a board, the **＋ New** at the foot of a column does the same and presets the
grouping property to that column's value, so a card added under *In progress*
arrives in progress.

A row is a page in every other respect: title, icon, cover, tags, a body of
blocks, comments, attached files, revision history, its own raw note trail, and
sub-pages of its own. This is why a task can hold its own meeting notes and a
deal can hold a folder of documents without anybody inventing an attachment
system. Everything on [Pages](pages.md) applies.

To give a row a sub-page, expand the collection in the sidebar: every row
listed there carries the same `＋` and the same `⋯` as any other tree item, so a
dossier under a deal is made the same way a page under a page is.

Two consequences that surprise people:

**Bare rows are not in the sidebar tree.** A collection can hold tens of
thousands of rows, and putting them all in the page list would flood every
listing. A row *does* appear once it has a live sub-page — otherwise that
sub-page would hang in the tree with no visible parent. Expanding a collection
in the sidebar loads up to 50 rows on demand and shows them there.

**A private row is invisible to everyone else.** The lock in the page header
works on rows too. The row list applies it: a row set to private is filtered out
of the table, the board and the count for every other member. Workspace admins
still see everything. See [Permissions](permissions.md).

### The property panel on a row

Open a row and its properties sit between the title and the body, one per line:
the property name on the left, the value on the right. The panel shows **every**
property in the schema, including ones hidden in the view you came from, and
including the empty ones — this is the place to fill a field in rather than hunt
for its column.

The cells are the same editors the table uses, so they behave the same way:
click a select to open its list, type in the box to create an option, use the
`＋ Link` cell to point a relation at another row. Creating or recolouring an
option here changes the **schema**, which means it changes for everybody looking
at that collection, not just for this row. The same is true of the chips on a
board card, which is where most people will do it without noticing.

## The toolbar

Above every collection sits one bar. On the left, one tab per view; on the
right, the settings for the view you are on.

| Control | What it does |
| --- | --- |
| View tabs | Switch views. **Double-click** a tab to rename it. |
| `+` (Add view) | Menu: **Table**, **Board**, **Gallery**, **Calendar**, **Timeline**, **List**, **Form**. The new view is named after its type and opens straight away. |
| **Start:** / **End:** | Timeline only — which date property draws the bars. Leaving End empty gives one-day bars. |
| Sub-item picker | Table only, and only when a relation points back at this same collection. Choosing it draws the table as a tree; **No sub-items** turns it off. |
| **Filter** | Conditions on properties, ANDed. The button shows the count. |
| **Sort** | One property, **Ascending** or **Descending**, or **No sort**. |
| **Group** | Board only — which property makes the columns. |
| **Columns** | Show and hide properties. Two lists, **Shown** and **Hidden**, with **Hide all** and **Show all**. The button reads **Columns**, and `Columns (4/7)` once something is hidden. |
| **Properties** | Opens the schema dialog — see below. |
| `⋯` (View options) | **Rename view**, **Move left**, **Move right**, **Remove view**. The arrows grey out at the ends; with only one view left the menu offers nothing but **Rename view**. |
| **New** | Creates a row and opens it. |

On a **form** view the bar keeps only **Columns**, **Properties** and the `⋯`.
There is nothing to filter, sort or add by hand there, and hiding a property
under Columns removes its field from the form.

**Columns hides a property everywhere in that view, not only in a table.** The
same filtered list of properties is handed to the board cards, the gallery, the
list, the calendar, the timeline and the form's fields. A column hidden here
disappears from the cards too, and stops being asked on a shared form.

Everything in that bar is stored on the view, which is stored on the collection.
A filter is **not** a personal setting: change it and you change what everyone
sees in that view. If you want your own cut, add a view.

### Filters

A filter is a property, an operator and (usually) a value. Which operators are
offered depends on the type:

| Property type | Operators |
| --- | --- |
| number, rollup, formula | is · is not · > · < · is empty · is not empty |
| date | is · is not · after · before · is empty · is not empty |
| text, person | is · is not · contains · is empty · is not empty |
| everything else | is · is not · is empty · is not empty |

The value box adapts too. A select or multi-select offers its options, a
checkbox offers **Checked** / **Unchecked**, and a relation offers the titles of
the rows it points at — you never have to type an id. Anything else is a free
text box.

Filtering and sorting happen in the database first: a collection with fifty
thousand rows is narrowed before it is sent, and the browser then applies the
same conditions again to what arrives. The view loads what is left in batches of
200 until it has all of it, so column counts on a board are true counts and not
"the first page of results".

The row title is **not** a property and cannot be filtered on. To find rows by
title, use [Search](search.md).

### What the table adds

The first column is always **Name** and links to the row. After it comes one
column per visible property, editable in place — except **rollup**, **formula**
and **backrelation** columns. Those are computed on the server and always read
as text; a backrelation is edited from the side that owns the relation.

The foot of the table is a calculation row: it counts the rows on the left, and
under each column shows either a sum (`Σ`, for number, rollup and formula
columns) or how many cells are filled.

With a sub-item relation chosen, rows nest: a row that points at other rows in
the same collection gets a `▾` to fold them away. A relation cycle cannot loop
the display — each branch remembers what it has already drawn.

### What the board adds

One column per option of the grouping property, plus a catch-all column named
**No _<property>_** that collects rows with no value — and rows whose value
refers to an option that has since been deleted, so no card is ever lost. Each
column heading carries the option's colour and the number of cards in it.

Cards move by dragging, which works with a mouse and with a finger. Each card
also has a `⋯` (and answers a right-click) with **Open**, a **Move to** list of
every other column, and **Move to trash** — which asks first and is reversible
from [Trash](trash-and-recovery.md).

A card shows a fixed set of zones rather than every property in schema order:

1. **Chips** — select, multi-select and relation values, coloured.
2. **Facts** — numbers, dates, checkboxes, checklists, rollups and formulas,
   each with its field name in front of it, because a bare "55" or a second
   date means nothing.
3. **One text note**, clamped to a few lines.
4. **Contact icons** — an email, a phone number, a postal line or a URL
   property becomes an icon with the value in its tooltip. An IP address is
   treated as a fact instead, so the digits stay on screen.
5. **People**, collected into a single stack of faces in the card's top-right
   corner and deduplicated across every person property, so the same colleague
   never appears twice.

A **backrelation never appears on a card at all** — on a system row it would be
every task pointing at it, which is useful in a table and far too much here.
Everything else that does not fit is counted, not dropped: a card prints one
text note and up to eight facts, and the rest becomes **+3 more**, which opens
in place. A value that only looks filled counts as empty and never takes a
line: `-`, `–`, `/`, `n/a`, `none` and a couple of German equivalents that
imported boards are full of.

The property the board groups by is left off its own cards, and empty select
fields stay invisible until you hover the card so a status can still be set from
outside. Open comments show as a small speech bubble with their count, and a
coloured dot marks a row an agent is working on right now.

Grouping by a **relation** turns the rows of the other collection into the
columns — one column per customer, per system, per whatever the rows point at.

If the grouping property is missing, the board says
*This board needs a Select property to group by. Open ⚙ Properties to add one.*
The calendar and the timeline say the same about a date property.

### What the form adds

A form view is configured inside the view itself, not in a dialog. The heading
and the description under it are text boxes you type into, and they are what a
visitor reads. Above them sits a strip: **Share publicly** mints a public link,
after which it reads **Public** and offers **Copy link** and **Revoke**, with
the link itself in a box you can select. Anybody holding that link can add a row
without an account — see [Forms](forms.md) and [Sharing](sharing.md).

The fields are the visible properties, with the row title first and always
required. Only seven types can be filled in: text, number, select,
multi-select, date, checkbox and person. A relation, backrelation, rollup,
formula, checklist or URL property never appears on a form, in the app or on
the public link, even when the view shows it everywhere else.

## Editing the schema

**Properties** in the toolbar opens the **Collection properties** dialog. It
lists every property with its name (editable in place), a type dropdown, and a
`✕` to remove it. At the bottom, **New property name** plus a type, then
**Add**. Nothing is written until you press **Save**; **Cancel** throws the
whole session away.

The types on offer are **Text**, **Number**, **Select**, **Multi-select**,
**Date**, **Checkbox**, **Checklist**, **URL**, **Person**, **Relation**,
**Backrelation**, **Rollup** and **Formula**. [Properties](properties.md)
describes what each one stores.

Some types ask for more, right under the property:

| Type | What it asks |
| --- | --- |
| Number, Rollup, Formula | **Display**: Number, Progress bar or Ring — plus **Max (= 100%)** for the last two |
| Relation | **Links to** — which collection |
| Backrelation | **Rows from** — the collection that points here — and **That point here via** — which of its relation properties |
| Rollup | **Via relation**, **Of property**, **Calculate** (Sum, Count, Average, Min, Max, Percent) and **Only rows where** — an optional condition with is / is not / contains / is empty / is not empty |
| Formula | **Expression**, with the available `{id}` tokens listed underneath |

A rollup's condition can name **several** options at once. With *is* or *is not*
on a select or multi-select, the value box turns into a row of option chips and
you tick as many as apply — which is how "open" gets expressed at all, as
*status is not Done and not Discarded*. Tick exactly one and it is stored as a
single value, the same as before.

Select and multi-select properties show their options as coloured chips.
Clicking a chip opens the colour picker — that is where a board column's colour
comes from. The `+ Option` box adds one when you press Enter.

Three things about editing a schema that are worth knowing before you do it:

- **Changing a type does not destroy values.** The stored value stays as it is
  and is read through the new type. Text that is not a number simply shows blank
  in a number cell; switch back and it is there again.
- **Deleting a property does not delete the values either.** They stay on the
  rows. Re-adding a property with the same id brings the column back with its
  contents.
- **Saving repairs most of the views.** Filters, sorts and hidden columns that
  point at a property you just deleted are dropped, a board left without its
  grouping property is repointed at the first select, multi-select or relation,
  and a calendar at the first date. Options with no colour get one written in,
  so the dialog and the board agree. **A timeline is not repaired**: delete the
  date property its bars were drawn from and it keeps pointing at nothing and
  shows its empty message until somebody picks a new **Start:**.

Agents change the schema with `update_schema`, which **merges**: properties you
do not mention stay untouched, an id you pass changes that property, and one
without an id is added. Passing property ids in that call's `remove_properties`
list takes them away. This is deliberately different from the interface's Save,
which writes the whole schema at once — an agent adding one column should not
delete the rest by omission.

One trap in the merge: a property sent without an id gets one derived from its
name, and that derived id is then looked up in the existing schema. Sending
`{name: "Status", type: "text"}` against a collection that already has a
`status` property changes that property's type instead of adding a second
column. Call `get_collection` first and pass ids when you mean to change
something.

## Nesting

A collection can sit at the top level, under a document, or inside another
collection; and a row can have sub-pages, which can themselves be collections.
The `⋯` menu on any page offers **New collection inside**, and **Move to top
level** for anything that has a parent — which is the way back out for a
collection you dragged somewhere.

How the sidebar draws all this is a workspace setting, under **Layout** → *How
the sidebar is arranged*:

- **Documents and collections apart** (the default). Two sections, *Documents*
  and *Collections*. A collection filed under a document is listed in
  Collections rather than under its document. Right when the databases are the
  point.
- **One tree, filed where you put it.** One section, called *Pages*, holding
  both. A collection stays under the document it belongs to. Right for
  documentation. There is no Collections section in this mode, and therefore no
  `+` on one — create collections from a page's `+` or its `⋯` menu instead.

A collection nested **inside another collection** is not one of its rows: it
keeps its own chevron and its full menu, so it can always be moved back out.
Pages that live under a row — a dossier under a deal — belong to that
collection's subtree and are found by expanding the row.

## Collections inside documents

A collection can be **embedded** in a document's body: type `/` and choose
**Embed a collection**, then search for it by name. What you get is fully usable
in place — filter it, add rows, drag cards — with the document's own text above
and below it. The heading of the block opens it as its own page
(**Open as page ↗**).

Only a reference is stored. The collection stays one object in one place, the
same collection can be embedded in several documents, and an edit made in one of
them shows up in all of them. If the collection is deleted, or lives in a
workspace the reader may not see, the block says
*This collection is no longer available.* rather than breaking the page.

The MCP equivalent appends the block to the end of a document:

```
embed_database(page_id: "<the document>", database_id: "<the collection>")
```

See [Editor blocks](editor-blocks.md) for the other block types.

## Filling a collection

| Source | How |
| --- | --- |
| By hand | **New**, or **＋ New** in a board column |
| A public form | a `form` view, shared as a link — see [Forms](forms.md) |
| A Notion export (`.zip`) | each CSV in it becomes a **new** collection — see [Import and export](import-export.md) |
| An agent | `create_rows`, up to 200 rows per call |
| A JSON API | `import_url`, which fetches and writes on the server |

The import row is narrower than it looks, and worth spelling out: nothing in
the interface fills the collection you are looking at from a file. The `⋯` menu
on a page offers **Import (.md / .zip)**, which takes Markdown or an archive —
a Markdown file imported there becomes a page at the **top level**, not a page
inside whatever you had open. **Import workspace…** in the workspace settings
takes a `.zip` only. A CSV is read solely as part of a Notion export archive,
and it creates a collection rather than adding rows to one.

`import_url` matters for agents in particular: none of the source data passes
through the agent's context. It returns a job id to poll with
`get_import_status`.

## Collection or database — the two words

The interface says **Collection** everywhere: the sidebar section, the create
menus, the properties dialog. It covers table, board, calendar and gallery
alike, and it promises no SQL to somebody who does not want any.

The MCP surface says **database**: `create_database`, `embed_database`, and the
`database_id` argument. Renaming those would break every agent configuration
already in the wild, and an agent reads a schema, not marketing.

They are the same object. `get_collection` — the tool that hands an agent a
database's schema and its view ids — is named for both halves of that history,
and is the call to make before writing anything, because property values are
stored under **ids**, not names.

| Task | Tool |
| --- | --- |
| Create one | `create_database` |
| Read its schema and views | `get_collection` |
| Add or change properties | `update_schema` |
| Create or change a view | `set_view` · `delete_view` |
| Read rows | `query_rows` |
| Create rows | `create_rows`, or `create_page` with the collection as `parent_id` |
| Write property values | `set_properties` |
| Put it in a document | `embed_database` |

Two of them behave in a way worth knowing before the first call:

- **`query_rows` filters, sorts and paginates on the server** — the same
  conditions the toolbar offers (`is`, `is_not`, `contains`, `gt`, `lt`,
  `between`, `is_empty`, `is_not_empty`), ANDed, with `sort` spelled
  `propertyId:asc`. `is` / `is_not` take a set through `values`, `between` takes
  `value` and `value2`. A filter value may be given as an option **name**
  instead of its id. It returns
  50 rows by default and at most 500 per call, so walk a large database with
  `offset`.
- **`set_properties` merges field by field.** Only the keys you send are
  touched, and `null` clears one. Two writers changing different properties of
  the same row therefore do not overwrite each other.

[MCP tools](mcp-tools.md) documents each of them in full;
[Agents](agents.md) covers how an agent connects in the first place.

One leftover: the `@`-mention list inside the editor still labels a collection
*Database* under its title. It is the same thing you would pick from the
Collections section.

## Where to go next

- [Properties](properties.md) — all thirteen types, and what each one stores
- [Views](views.md) — table, board, gallery, calendar, timeline, list, form
- [Relations and rollups](relations-and-rollups.md) — pointing at other
  collections, and counting what points back
- [Formulas](formulas.md) — arithmetic across a row's own properties
- [Forms](forms.md) — a collection view anybody can fill in without an account
- [Templates](templates.md) and [Library](library.md) — starting from something
  instead of from nothing
