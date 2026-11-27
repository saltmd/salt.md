# Relations and rollups

Three property types turn separate collections into one connected thing: a
**relation** points at rows in another collection, a **backrelation** is that
same link read from the other end, and a **rollup** counts, totals or averages
something across the rows either of them reaches. This page covers all three in
full: how to set them up, exactly what each option does, what the numbers mean
when nothing is there, and the places where the answer is not what people
expect.

A note on words: what the interface calls a **collection**, the MCP tool surface
calls a **database**. Same thing, two audiences. This page uses the interface's
word in the interface sections and the MCP word in the MCP section.

## The example used throughout

Two collections, both invented:

- **Projects**: one row per project. Rows: *Apollo*, *Bluebird*.
- **Tasks**: one row per task, with a property **Project** (a relation) and a
  **Status** select with the options *To do*, *In progress*, *Done* and
  *Dropped*.

*Apollo* has eight tasks: five *Done*, two *In progress*, one *Dropped*.
*Bluebird* has none yet.

Everything below is built on that pair.

## Relations

### What a relation holds

A list of row ids. Even when a task belongs to exactly one project, the value is
a list of one: that is the shape the whole system reads. A relation is stored on
**one side only**: the Tasks collection carries **Project**, and Projects stores
nothing about tasks.

A relation may point at any collection you can read, including the collection it
lives in. Pointing at itself is how a table gets a sub-item tree, described
below.

### Adding one

1. Open the collection and click **Properties** in the view bar. The dialog is
   titled **Collection properties**.
2. Type a name into **New property name**, choose **Relation** from the type
   list, and click **Add**.
3. Under the new property, set **Links to**. The drop-down starts with
   **Select a collection…** and lists every collection you can read, in every
   workspace you are a member of, not only this one.
4. Click **Save**.

A relation with no target shows **No target** in every cell instead of a value.

### Filling one in

An empty relation cell reads **＋ Link**. Click it and a popover opens with a
**Search…** box and the rows of the target collection; click a row to link it,
click it again to unlink. Filled cells show one chip per linked row, carrying
that row's icon and title.

The same cell sits in the properties panel at the top of the row's own page, and
it is editable there too: linking a task to a project can be done from the task
without going back to the table. That panel shows every property of the
collection, including ones a view has hidden.

Two limits worth knowing:

- The picker loads **up to 500 rows** of the target collection. In a collection
  larger than that, a row beyond the first 500 cannot be picked from the list.
- A chip shows **Untitled** when the row behind it has been trashed, when you may
  not read it, **or when it sits beyond those first 500**: the chips take their
  titles from the same list the picker loads, so a perfectly live row can read
  *Untitled* purely because the collection is large. The id is stored either way,
  so restoring a trashed row brings its title back.

### Re-pointing an existing relation

Changing **Links to** on a relation that already holds values changes nothing in
the rows: the stored ids stay exactly as they were, and they now name rows in a
collection that does not contain them. Every chip turns into **Untitled** and
every rollup over the relation empties out. It is one click, it is silent, and
there is no warning. Point a relation somewhere else only while it is empty, or
expect to re-link every row.

### On a board

A board can group by a relation, and then the columns are the rows of the other
collection: one column per project instead of one per status. Dragging a card
into a column sets the relation; dragging it into the catch-all column at the
end, named **No** followed by the property name (*No Project*), clears it.

That catch-all also collects anything the board cannot place: a row pointing at a
target that has been removed, and a row pointing beyond the first 500 rows of the
target collection (the columns come from the same capped list the picker uses).

On a card, a relation shows as a chip. On the board that groups **by** that
relation it is left off the cards, so the column heading is never repeated
directly underneath itself.

### Sub-items: a relation pointing at its own collection

When a relation points at the collection it lives in, a table view can draw the
rows as a tree. The value of the relation names the row's **sub-items**: a
parent lists its children, not the other way round.

Switch it on per view: on a table view, a drop-down appears in the view bar
reading **No sub-items**, listing each self-relation the collection has. Pick
one and the rows nest, with a `▾` / `▸` toggle in front of any row that has
sub-items and a blank in front of any row that does not. It is a table-view
setting only: a board or a gallery has no tree to draw, and the drop-down does
not appear there. Rows that are nobody's sub-item sit at the top level. A
relation that accidentally forms a circle does not loop forever; the branch
simply stops.

### Permission is checked per row

A relation cannot be used to see something you would not otherwise see. Every
target row is checked individually when its value is read, so a rollup over a
relation silently skips rows in pages you may not read, and so does a
backrelation. An API token narrowed to one workspace is held to that boundary
too, even when the person behind the token is a member of both. See
[Permissions](permissions.md).

### A relation is not a mention

Linking two rows with a relation does **not** create a backlink and does not draw
an edge in the [library's graph](library.md). Those are built from mentions
written in a page's body. A relation is structured data; a mention is prose.

## Backrelations

### What it is

The reverse of a relation somebody else declared. Put a **Backrelation** on
Projects and each project row answers the question *"which tasks point at me?"*
with a list of task chips, without Tasks or anybody else writing a second
value.

It needs two coordinates, because one collection can point at another through
several different relations and picking the wrong one lists the wrong rows:

1. In **Collection properties** on Projects, add a property of type
   **Backrelation**.
2. **Rows from**: the collection that points here. Choose *Tasks*.
3. **That point here via**: which of that collection's relation properties does
   the pointing. Until a collection is chosen the list reads
   **(pick collection first)**.
4. **Save**.

The third step is the one to be careful with. The list offers **every** relation
that collection has, including relations aimed at some third collection: nothing
checks that the relation points back here. Choose one of those and the column is
empty for ever, which looks exactly like "nothing points here".

### What you see

Chips, exactly like a relation's, and **always read-only**: the editing happens
on the side that owns the relation. To add a task to *Apollo* you set **Project**
on the task.

A backrelation is **never shown on a board card**. On a project row that would be
every task it has, which is right in a table and far too much on a card.

### Why it is computed rather than stored

The reverse side is worked out at read time, every time: one query for the
candidate rows plus a scan of their relation values. It is not kept as a second
list anywhere, and that has two consequences you can rely on:

- It **cannot drift**. A stored copy would have to be updated on every write from
  both directions, and the first missed update leaves two lists disagreeing with
  no way to tell which is right.
- There is **nothing to repair**. A backrelation is never stale, never needs
  reindexing, and shows the truth immediately after the other side changes.

### A half-configured backrelation

The two coordinates fail differently, and only one of them is visible:

- **That point here via** left unset: the column is **empty**. Not broken, not
  an error, just a list of nothing, indistinguishable from "no tasks point here".
- **Rows from** left unset: every cell reads **No target** instead, so at least
  something on screen says the property is unfinished.

Over MCP neither is possible: a backrelation written without both coordinates is
refused, precisely because a silent zero would be trusted.

## Rollups

A rollup aggregates one property across the rows a relation **or a backrelation**
reaches. That second half is what makes progress figures possible: the
backrelation on Projects lists Apollo's tasks, and a rollup then counts how many
of them are done.

### Adding one

1. In **Collection properties**, add a property of type **Rollup**.
2. **Via relation**: which relation or backrelation to follow. Both kinds are
   offered.
3. **Of property**: which property of the rows over there to aggregate. Until a
   relation is chosen this reads **(pick relation first)**.
4. **Calculate**: the function (below).
5. **Only rows where**: an optional condition. Defaults to **All rows**.
6. **Display**: **Number**, **Progress bar** or **Ring**, plus
   **Max (= 100%)** for the last two.

### The functions

**Calculate** lists **Count** and **Percent** first, then **Show original** and
**Show unique values**, then the calculations a table footer offers. Count and
Percent work on related *rows*; everything after them works on the *values* of
**Of property** in the related rows that meet the condition.

| Calculate | What it returns | With no related rows |
| --- | --- | --- |
| **Count** | how many related rows meet the condition | `0` |
| **Percent** | the share of related rows meeting the condition, to one decimal | `0` |
| **Show original** | the values themselves, as a list: every filled value, a multi-select's options one by one | an empty list |
| **Show unique values** | the same list with each value once, in order of first appearance | an empty list |
| **Count values** | how many values there are; a multi-select with three options counts three | `0` |
| **Count unique values** | how many different values | `0` |
| **Count empty** / **Count not empty** | how many related rows have the property empty / filled | `0` |
| **Percent empty** / **Percent not empty** | the same as a share of the rows meeting the condition, to one decimal | `0` |
| **Sum** | the total of the values it can read as numbers | `0` |
| **Average** | the mean of the values it can read as numbers | `0` |
| **Median** | the middle value; with an even count, the mean of the middle two | `0` |
| **Min** / **Max** | the smallest / largest of them | `0` |
| **Range** | the largest minus the smallest | `0` |
| **Earliest date** / **Latest date** | the earliest / latest of the values that are dates | empty |
| **Date range** | the whole days between the earliest and the latest date | `0` |
| **Checked** / **Unchecked** | how many related rows have the box ticked / not ticked | `0` |
| **Percent checked** / **Percent unchecked** | the same as a share, to one decimal | `0` |

Six details that decide what the number means:

- **A figure is never blank.** Every function that gives a number gives `0`
  when there is nothing to work on, not an em dash: a project with no tasks reads
  `0`, not "unknown". That includes an **Average** of nothing. **Earliest date**
  and **Latest date** are the exception: with no dates to choose from they are
  empty, because no date is the right answer and `0` is not a date. **Show original** and **Show unique values** give an
  empty list, which a cell shows as **—**.
- **Count counts rows, not filled values.** A related row is counted whether or
  not the chosen property has a value in it. For a Count rollup the **Of
  property** choice therefore does not change the answer. **Count not empty** is
  the one that looks at the values.
- **Empty means** missing, empty or whitespace-only text, an empty list, or an
  unticked box. **Unchecked** counts every row whose box is not ticked, including
  boxes nobody ever touched.
- **Sum, Average, Median, Min, Max and Range read every value they can turn into
  a number.** Text that looks like a number counts (a text property holding `42`
  is added like a number field), and a ticked checkbox counts as 1, an unticked
  one as 0. Text that is not a number is skipped, and so is a formula's `⚠`
  error message; Average divides by the number of values it could read, not by
  the number of related rows.
- **Stored values are read as stored.** **Show original** on a select lists the
  option **ids** (`done, in-progress`), not the names, and on a backrelation the
  row ids. To collect names, aggregate a formula on the other side that reads
  `prop("Status")`, which gives the option's name.
- **A rollup that is not finished counts.** Leave **Via relation** unset and the
  column shows `0` on every row rather than an error. A rollup with no
  **Calculate** at all, which only an agent can create, counts the matching rows.
  Check the configuration before believing a column of zeroes.

### Rolling up computed values

**Of property** offers every property of the related rows except their
relations, and that includes their **formulas**, **rollups** and
**backrelations**. **Only rows where** offers all of them. A computed property
over there is worked out for the related rows first, in their own collection,
exactly as it would show in that collection's table, and then aggregated here.

So a project can sum a formula *Hours × rate* of its tasks, or take the
**Earliest date** of a task formula that adds a lead time to the due date, and a
condition can test a task formula that reads *"Late"*.

This is followed **two collections deep**. A rollup on Projects over a rollup on
Tasks that in turn aggregates a formula on Subtasks works: the Tasks rollup and
the Subtasks formula are both worked out. One collection further, a computed
value is no longer computed; what is read there is whatever is stored under that
id, which is usually nothing, so a Sum comes out `0` and a Count still counts.

Permission still applies at every step: related rows you may not read are left
out before anything is computed for them.

### Where a rollup shows up

In a table cell, on a board card (labelled with its field name, alongside
numbers, dates and checklists), and in the properties panel at the top of a
row's own page: the same value in all three places. A list result is shown as its
values separated by commas, a date result as a date. In a table, the footer
under a rollup column shows a calculation over the column (by default **Σ**, the
sum) across every row the view's filters let through; click it to choose
another. See [Views](views.md#the-calculation-row).

## Conditions: "Only rows where"

Without a condition, a rollup sees every related row. A condition narrows it, and
that is the difference between *how many tasks a project has* and *how many are
done*.

The condition is one comparison: a property of the **related** row, an operator,
and a value.

| Operator | Matches a related row when |
| --- | --- |
| **is** | the value equals the comparison value (or any of them, if several are ticked) |
| **is not** | the value equals none of them, including rows where the property is missing entirely |
| **contains** | the value contains the text, ignoring case |
| **is empty** | the value is missing, empty, or only whitespace |
| **is not empty** | anything else |

The value field changes with the property you picked: a select or multi-select
with **is** / **is not** offers its options as chips you tick, other operators
give a drop-down starting **Select a value…**, a checkbox offers **Checked** and
**Unchecked**, and everything else is a text box labelled **Value**.

### Several values at once

With **is** or **is not** on a select or multi-select property you may tick more
than one option.
This exists because *open* is neither *Done* nor *Dropped*, and one comparison
cannot say that: **is not** *Done* alone counts every dropped task as open,
quietly, and by exactly the amount nobody notices. Ticking *Done* and *Dropped*
together with **is not** gives the right number.

### Six things that decide the answer

- **No condition means every related row counts.** A rollup written before
  conditions existed keeps its meaning exactly.
- **An unrecognised operator is treated as "is".** A typo does not turn into
  "match everything", because the convenient reading of a typo would be a
  progress bar quietly stuck at 100 %.
- **Comparison is against the stored value, not the label.** A select option
  displayed as *In progress* is stored as `in-progress`. The interface hides this
  by offering the options themselves; over MCP you must write the id, or the
  condition matches nothing and reports a confident `0`.
- **There is no greater-than or less-than.** The five operators above are all of
  them. A numeric threshold goes through a formula on the related rows:
  `prop("Hours") > 8` on Tasks, and the condition *that formula* **is** `true`.
- **A missing value is empty and is "not" anything.** A task with no status at
  all counts as *is not Done*, and as *is empty*.
- **A computed value is compared as text.** A formula, rollup or backrelation
  of the related row is worked out first (see
  [above](#rolling-up-computed-values)) and then compared the way it would be
  written out: a number as `3` or `2.5`, a true/false result as `true` or
  `false`, text as it is. *Only rows where* a task formula **is** `Late` works;
  a formula giving a select's name compares by that name, not by an id.

Two traps follow from that last one:

- **Checkboxes.** A box that was never ticked has no stored value, so **is
  Unchecked** does not match it: only a box that was ticked and then unticked
  is stored as *false*. Use **is not** *Checked* to mean "everything that is not
  ticked".
- **Multi-value properties.** A condition on a multi-select or a relation
  compares against the whole list written out as text, so **is** never matches a
  single entry. Use **contains** there.

## Percent, and the progress bar

**Percent** is the share of the related rows that meet the condition, rounded to
one decimal. It exists so that a progress bar needs no arithmetic: a formula
would have to divide, and zero of zero related rows is a division by zero, which
renders as an error in the column of every newly created row unless the formula
guards against it. Here it is simply `0`.

For *Apollo*, a rollup **Via relation** *Tasks*, **Calculate** *Percent*, **Only
rows where** *Status* **is** *Done*, gives `62.5`: five done out of eight
related tasks. Set **Display** to **Progress bar** and the cell shows a bar
filled to 62.5 % of **Max (= 100%)**.

Two things to know before trusting that figure:

- **The denominator is every related row**, dropped ones included. The condition
  selects the numerator only, and no setting changes what percent divides by. To
  exclude the dropped tasks you need a different relation or a different set of
  rows, not a second condition.
- **The denominator is every related row *you can see*.** Related rows in pages
  you may not read are skipped before the division, and so are rows outside the
  workspace a narrowed API token is allowed into. The same project row can
  therefore show one percentage to you and another to a colleague, and a third
  through a scoped token. Nothing on screen says a row was left out.

## The example, finished

With one relation on Tasks and three properties on Projects (a backrelation
plus two rollups), a project row reads:

| Column | Type | Configuration | *Apollo* | *Bluebird* |
| --- | --- | --- | --- | --- |
| Tasks | Backrelation | Rows from *Tasks*, via *Project* | 8 chips | empty |
| Open | Rollup | Via *Tasks*, Count, only rows where *Status* **is not** *Done*, *Dropped* | `2` | `0` |
| Progress | Rollup | Via *Tasks*, Percent, only rows where *Status* **is** *Done*, shown as a Progress bar | `62.5` | `0` |

Nothing on the Tasks side changed except the one **Project** relation, and no
number anywhere is maintained by hand.

## Filtering, sorting and hiding these columns

Three view settings meet these three property types, and they do not all behave
the same way. See [Views](views.md#filters) for the settings themselves.

**Filtering by a relation or a backrelation works, and needs no id.** Add the
filter with **+ Add filter…** and the value box becomes a drop-down of the target
collection's row titles: *Project is Apollo*, not a 32-character page id. The
one caveat is the same 500 everywhere else: the drop-down offers the first 500
rows of the target collection.

**Filtering by a rollup, a formula or a backrelation works too**, on the
computed values. The server works them out for every row the stored conditions
let through, then applies the conditions on computed values, then pages, so the
answer and its `total` are right in the browser, over the rows endpoint and over
`query_rows` alike. *Progress greater than 50*, *Open is 0* and *Tasks is
empty* (the projects nothing points at) all do what they say. A rollup is never
empty, except a **Show original** or **Show unique values** rollup with nothing
to show; a figure with nothing to work on is `0`, and 0 is not empty. The
operators each type offers are listed in [Views](views.md#filters).

**Sorting by any of them works the same way**: on the computed values, done by
the server before paging. Numbers sort as numbers. A backrelation sorts by its
row ids strung together, which orders nothing useful.

**Hiding is per view.** The **Columns** button lists every property under
**Shown** and **Hidden**; click one to move it. That is the answer to "this
rollup belongs on the table, not on the board": nothing is deleted and no other
view changes.

## Renaming, retyping and removing

All three live in **Collection properties**:

- **Rename** by typing in the property's name box. The property keeps its id, so
  rollups and backrelations that name it keep working, and so do the values in
  every row.
- **Change the type** with the drop-down beside the name. Nothing is deleted;
  existing values stay and are read differently by the new type.
- **Delete** with the `✕` button (tooltip **Delete property**). The values stay in
  the rows. Creating a property with the same id again brings them back.

Deleting the relation that a backrelation or a rollup depends on does not delete
those; they simply stop finding anything and read as empty or `0`.

## Over MCP

Agents build all of this with `update_schema`, which **merges**: properties you
do not mention stay as they are, so adding one column cannot delete the others.
Call `get_collection` first: property ids, select option ids and view ids all
come from there.

The relation on Tasks:

```json
[{ "name": "Project", "type": "relation", "relationCollection": "<projects-db-id>" }]
```

The backrelation and the two rollups on Projects:

```json
[
  { "name": "Tasks", "type": "backrelation",
    "backrelationCollection": "<tasks-db-id>", "backrelationProp": "project" },
  { "name": "Open", "type": "rollup", "rollupRelation": "tasks",
    "rollupTarget": "status", "rollupAgg": "count",
    "rollupWhereProp": "status", "rollupWhereOp": "is_not",
    "rollupWhereValues": ["done", "dropped"] },
  { "name": "Progress", "type": "rollup", "rollupRelation": "tasks",
    "rollupTarget": "status", "rollupAgg": "percent",
    "rollupWhereProp": "status", "rollupWhereOp": "is",
    "rollupWhereValue": "done", "numberDisplay": "bar" }
]
```

Field by field:

| Field | Meaning |
| --- | --- |
| `relationCollection` | the database this relation points at |
| `backrelationCollection` | the database whose rows point here |
| `backrelationProp` | which relation property over there does the pointing |
| `rollupRelation` | the id of a relation **or backrelation** on this database |
| `rollupTarget` | the property of the related row to aggregate; may be a formula, rollup or backrelation over there |
| `rollupAgg` | `count`, `percent`, `count_all`, `count_values`, `count_unique`, `count_empty`, `count_not_empty`, `percent_empty`, `percent_not_empty`, `sum`, `avg`, `median`, `min`, `max`, `range`, `earliest`, `latest`, `date_range`, `checked`, `unchecked`, `percent_checked`, `percent_unchecked`, `show_original` or `show_unique`. Left out, the rollup counts the matching rows |
| `rollupWhereProp` / `rollupWhereOp` / `rollupWhereValue` | the condition; ops are `is`, `is_not`, `is_empty`, `is_not_empty`, `contains` |
| `rollupWhereValues` | a list, instead of the single value, for `is` and `is_not`. If both are set the list wins; `contains` uses the single value only |
| `numberDisplay` / `numberMax` | `plain`, `bar` or `ring`, and what counts as 100 % |

Property ids are derived from the name when you do not supply one (*Open*
becomes `open`, *Due date* becomes `due-date`), which is why the rollups above
can name the backrelation as `tasks`.

Removing a column is the `remove_properties` argument of the same call, a list of
property ids. The reply says so in words: *removed tasks (row values kept,
re-adding the property brings them back)*.

Six rules for agents:

- **A relation value is always a list.** `set_properties(page_id: "<task>",
  properties: { "project": ["<project-row-id>"] })`, never a bare string. A
  single value is repaired on write now, and rows written before that are read
  leniently, but write the list.
- **A rollup condition needs the id, not the label**: `"done"`, not `"Done"`.
  Row values you write with `set_properties` and filter values you pass to
  `query_rows` are repaired for you: an option name is matched to its id,
  ignoring case. The rollup condition is the exception. It is compared exactly as
  written, and a label there matches nothing and reports `0`.
- **A backrelation without both coordinates is refused**, rather than accepted as
  an empty column.
- **A name that turns into an existing id edits that property.** Ids come from
  the name, and `update_schema` merges by id, so sending `{"name": "Tasks",
  "type": "text"}` to a database that already has a `tasks` column does not add a
  second one, it changes the one that is there. The reply distinguishes them
  (*added* versus *changed*); read it. Merging cannot delete a property, but it
  can overwrite one.
- **An unknown `rollupAgg` is refused**, with the list of valid ones:
  `rollup "Open": unknown rollupAgg "cnt", use one of count, percent, …`.
  Nothing is saved. `update_schema` checks only the properties in the call, and
  `create_database` checks every one.
- **`query_rows` returns computed values.** Rollups, formulas and backrelations
  come back in every row's properties, correctly: there is nothing to compute on
  your side. It pages: `limit` defaults to 50 and accepts up to 500, `offset`
  walks the rest, and the reply carries `total` so you know when to stop. A
  `limit` above 500 is not clamped: it falls back to 50. Filters and `sort` work
  on computed values as well, and `calculations` adds a total over every
  matching row (see [MCP tools](mcp-tools.md#query_rows)).

**`get_page` does not compute anything.** On a database it renders a Markdown
table straight from the stored values, so every rollup, formula and backrelation
column in it is blank. That is not an empty database; it is the wrong call. Use
`query_rows` for rows, and `get_page` for a document's text.

See [MCP tools](mcp-tools.md) for the full catalogue and [Agents](agents.md) for
connecting one.

## Limits and traps

| Thing | What actually happens |
| --- | --- |
| Filtering a view by a rollup, formula or backrelation | Works, on the computed values, in the browser and over the API and MCP alike. The server computes every row the stored conditions let through before it filters, so a large collection answers more slowly. |
| Filtering a view by a relation | Works: it is a stored value like any other, and the value box offers row titles. |
| Sorting by a computed column | Works, on the computed values, over MCP too. |
| The relation picker | Loads at most 500 rows of the target collection, which also caps a board grouped by that relation, and the chip titles. |
| Deleting a relation or rollup property | The values stay in the rows. Re-creating the property with the same id brings them back. |
| Changing a property's type | Non-destructive; existing values stay and are simply read differently. |
| Re-pointing **Links to** | Also non-destructive, and worse for it: the ids stay, aimed at a collection that does not hold them. |
| Trashing a linked row | It drops out of backrelations and rollups immediately, and its chip reads **Untitled**. Restoring it undoes both. |
| Public forms | Cannot fill a relation, backrelation, rollup or formula: a form only accepts text, number, select, multi-select, date, checkbox and person. See [Forms](forms.md). |
| Computed values on the other side | A rollup over a formula, rollup or backrelation of the related rows computes it first. This is followed two collections deep; a computed value three collections away is not computed and reads as whatever is stored, usually nothing. |

## See also

- [Properties](properties.md): all 13 property types, including these three
- [Collections](collections.md): schema, rows, sub-items, the properties dialog
- [Views](views.md): grouping a board by a relation, filters and sorting
- [Formulas](formulas.md): expressions over a single row's properties
- [Permissions](permissions.md): what "checked per row" means in practice
