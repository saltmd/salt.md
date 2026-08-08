# Formulas

A **formula** is a property of a collection that holds arithmetic instead of a
value: *price times quantity*, *hours minus hours already billed*. The server
works it out every time a row is read, so there is nothing to recalculate and
nothing that can go stale — and nothing to type into, either. A formula cell is
read-only everywhere.

The language is deliberately small: four operators, parentheses, numbers, and
references to other properties of the same row. There are no functions, no text,
no dates and no conditions. This page says exactly what it does support, what
you see when an expression is wrong, and the places where a formula value is not
available at all.

A collection is called a **database** on the MCP surface that agents use — same
thing, two audiences. See [Collections](collections.md).

## Adding a formula column

1. Open the collection and click **Properties** in the view's toolbar. The
   dialog is titled **Collection properties**.
2. At the bottom, type a name into **New property name**, pick **Formula** from
   the type list, and click **Add**.
3. The new property gets an **Expression** field. Type the expression there. The
   placeholder shows the shape: `e.g. {price} * {qty} - {discount}`.
4. Under the field, the hint lists every other property of this collection as a
   token you can copy — *Reference other properties with `{id}`. Available: …
   Supports `+ - * / ( )`.* Hovering a token shows the property's name.
5. Optionally set **Display**: **Number** (the default), **Progress bar** or
   **Ring**. The last two ask for **Max (= 100%)**.
6. Click **Save**.

Nothing is written until you click **Save** — the dialog holds the whole schema
in front of the table until then, so adding the column and typing its expression
are one act. A formula column that *is* saved with an empty expression is an
error: every row reads **⚠ unexpected end** until the expression parses. Filling
the field in before saving avoids it entirely.

To remove a formula again, click the ✕ (**Delete property**) beside it in the
same dialog.

Changing an existing column's type to Formula does not delete what was in it.
The stored values stay on the rows; the computed value simply covers them, and
they reappear if you switch the type back. That leftover matters in one place —
see [where a formula value exists](#where-a-formula-value-exists-and-where-it-does-not).

## Writing the expression

### What the language has

| Element | Notes |
| --- | --- |
| `+` `-` `*` `/` | `*` and `/` bind tighter than `+` and `-` |
| `( )` | grouping, nested as deeply as you like |
| unary minus | `-{price}`; `--{price}` is `{price}` |
| numbers | plain decimals: `12`, `0.5`, `3.` |
| `{propId}` | the value of another property of the same row |

Spaces and tabs are ignored. Keep the whole expression on one line — a line
break is not treated as spacing and ends the expression with an error.

```
{price} * {qty}
({hours} - {billed}) * {rate}
{budget} - ({staff} * 8 * {rate})
```

### What it does not have

- **No functions.** `round({price})` is an error, and so is anything else with a
  name in front of a bracket.
- **No other operators.** `^` and `%` are errors; there is no exponent and no
  modulo.
- **No scientific notation.** `1e3` is an error; write `1000`.
- **No leading plus.** `+5` is an error; `5` is fine.
- **No comparisons, no conditionals, no text, no dates.** There is no way to
  express "if", to join two words, or to subtract one date from another.
- **No reach into other rows.** A formula only ever sees the row it sits on. For
  anything that comes from related rows, the answer is a rollup — see
  [Relations and rollups](relations-and-rollups.md).

### References are ids, not names

`{price}` names the property **id**, not the label a reader sees. Ids are slugs
of the name at the moment the property was created (`Due date` → `due-date`), and
renaming a property afterwards does not change its id — so renaming never breaks
a formula. The editor's hint lists the ids; agents get them from
`get_collection`.

Each property type is read as a number like this:

| Property type | Read as |
| --- | --- |
| number | its value |
| checkbox | `1` when ticked, `0` when not |
| text, url, person | the number if the whole stored string is one (`7.5`, spaces around it are fine), otherwise `0` — note that `12,5` with a comma is `0` |
| select | what is stored is the **option id**, read by the same rule. An option whose id happens to be a number — `2024`, `5` — reads as that number; every other id reads as `0` |
| date | `0` — a date is stored as `2026-07-18`, which is not a number |
| multiselect, relation, backrelation, checklist | `0` — these hold a list, and a list is never a number |
| rollup | its computed number — rollups are worked out before formulas |
| another formula | its result |
| an id that does not exist, or an empty cell | `0` |

### The zero rule, and why it is the thing that bites

**An unknown or unusable reference is not an error. It is zero.** Misspell an
id, delete a property something still references, or point at a date, and the
column keeps showing a perfectly plausible number that is quietly wrong. Nothing
turns red, because as far as the evaluator is concerned the row simply has a
zero there.

Two habits deal with it: copy the tokens out of the hint rather than typing
them, and check a formula against `get_collection` after removing any property.

### A formula may reference another formula

`{margin}` may itself be a formula; it is worked out on demand, in any order, and
its own references are followed too. A cycle is refused rather than followed —
two formulas that point at each other, or one that points at itself, both show
**⚠ circular reference in formula** and nothing hangs.

If the referenced formula fails, its error travels outward unchanged. A column
that divides by `{margin}` shows **⚠ division by zero** because `margin` itself
divided by zero — not a message of its own. So the cell that complains is not
always the cell with the mistake in it; check every formula it references.

## When an expression is wrong

The cell shows the message in place, in red, prefixed with ⚠. Errors are per row
and per formula: one bad cell does not disturb the rest of the row, and other
formula columns keep working.

| What you see | What happened |
| --- | --- |
| `⚠ unexpected end` | the expression is empty, or breaks off after an operator (`1 + `) |
| `⚠ division by zero` | the right-hand side of a `/` came out as 0 |
| `⚠ missing )` | an opening bracket was never closed |
| `⚠ missing }` | a `{` was never closed |
| `⚠ unexpected "…"` | something the language does not know, quoted back from where it gave up — `^ 3`, `%`, a stray `r` from `round(` |
| `⚠ strconv.ParseFloat: parsing "1.2.3": invalid syntax` | a number it cannot read, such as `1.2.3` |
| `⚠ circular reference in formula` | formulas that reference each other, or one that references itself |

The message travels as the cell's value, so it reads the same in every interface
language.

### Division by zero deserves its own paragraph

`/` by zero is an error, not a zero. Combine that with the zero rule and one very
common formula misbehaves: `{done} / {total} * 100` shows **⚠ division by zero**
on every row where `total` is still empty — which is every row on the day it is
created.

For a share or a progress figure over related rows, use a rollup with the
**Percent** function instead. It answers `0` for a row with nothing related to
it, so a brand-new row shows an empty bar rather than an error message.

## How the value is shown

- **Number** (the default): formatted for the reader's region, so `1234.5` reads
  as `1,234.5` or `1.234,5` depending on the account's setting — see
  [Language and time](language-and-time.md). Long decimals are shortened for
  display; the underlying value keeps its precision.
- **Progress bar** and **Ring**: the value against **Max (= 100%)**, clamped
  between 0 and 100 %, with the raw number beside it. That number is printed
  as it is, with no regional formatting — so a bar and a plain cell in the same
  table can spell the same value differently. A value that is not a finite
  number falls back to plain text, which is how error messages stay readable.
- **In a table**, the column footer shows `Σ` and the sum of the column across
  the loaded rows. Cells holding an error are left out of that sum.
- **On a board card**, a formula is printed with its field name in front of it —
  a bare number on a card means nothing on its own. A card prints at most eight
  such lines; the rest collapse into a **+3 more** button that opens them in
  place (**less** closes it again).
- **On the row's own page**, it appears in the property panel under the title,
  with no editing control.

### Hiding a formula column in one view

A formula column can be switched off per view like any other. Click **Columns**
in the view's toolbar: the panel has a **Shown** and a **Hidden** section, one
row per property, and a click moves a property between them. **Hide all** and
**Show all** do the whole list at once, and the button counts what is left
(`Columns (4/7)`) whenever anything is hidden.

This is a property of the *view*, not of the column — the same formula can be
visible in the table and hidden on the board. More in [Views](views.md).

## Where a formula value exists, and where it does not

Everything that reads a row *through the collection* computes the value.
Everything that renders the stored page directly does not, because the value is
not stored on the page.

| Where | Formula value |
| --- | --- |
| Table, board, list, gallery, calendar, timeline | yes |
| A row opened as its own page | yes |
| `query_rows` over MCP | yes |
| The rows endpoint, `/api/collections/{id}/rows` | yes |
| `get_page` over MCP on a **row** | no properties at all — that call returns the row's title and body, nothing else |
| `get_page` on the database — the Markdown table | column heading yes; cells empty unless a value was left behind under that id (see below) |
| Markdown export of a database, and a database shared by public link | the same: heading, and only whatever was left behind |
| Form view, and a shared public form | not offered — a form only shows text, number, select, multiselect, date, checkbox and person |

The form case is not a gap: a formula has nothing for a person to fill in.

**"Left behind" is a real case, not a theoretical one.** These three renderers
print whatever is stored on the row under the property's id, and two ordinary
actions put something there: changing an existing column's type to Formula keeps
the old values, and a `set_properties` write to a formula id is stored (see
[below](#formulas-for-agents)). Either way the Markdown table, the export and
the public link print that old value in the formula's column, where every live
view shows the computed one. If a shared table shows numbers that no longer
match the app, that is what happened.

## Filtering and sorting by a formula

This is the sharpest limit on the page, so it is worth stating plainly. Rows are
filtered and sorted **in the database, on stored values, before any formula
runs**. A formula is not a stored value.

| You do this | What happens |
| --- | --- |
| Sort a view by a formula in the browser | works — the browser loads all the rows and sorts them itself, on the computed numbers |
| Sort via `query_rows` by a formula | nothing; rows come back in their normal order |
| Filter by a formula with `is`, `>`, `<` or `is not empty` | **no rows match**, in the browser and over MCP alike |
| Filter by a formula with `is empty` | in the browser: **no rows are shown at all**; over `query_rows` and the rows endpoint: every row matches |
| Filter by a formula with `is not` | over `query_rows` and the rows endpoint: every row matches. In the browser: every row except those whose computed value is exactly the text you typed |

Two of those rows look contradictory and are not. The browser sends the filter
to the server *and* applies it a second time itself, on the values it got back —
which by then are computed. `is empty` therefore passes every row on the server
and then removes every one of them in the browser, because a formula cell always
holds something: a number, or a `⚠ …` message. You get **No rows match the
current filter.** `is not` survives that second pass and behaves like a real
filter on the computed number, which is the one combination that works by
accident rather than by design.

`contains` is not offered for a formula in the browser at all — the operator
list for a formula is *is*, *is not*, *>*, *<*, *is empty*, *is not empty*. An
agent can send `contains` over MCP, and it matches nothing.

A view's sort is stored **on the view** (`set_view` with `sort:
"propertyId:asc"`), so this is not a per-action choice: a view sorted by a
formula stays sorted correctly for everyone opening it in the browser, and comes
back unsorted every time an agent reads the same database through `query_rows`.

If you need to filter on a computed number, the number has to be written down —
by a person or by an agent — as a plain number property. The same applies to
rollups and backrelations, which are computed at the same stage.

## Formulas for agents

Create or change one with `update_schema`, which merges: send only the property
you mean and the rest of the schema is left alone.

```
update_schema(page_id: "<database>", properties: [
  { "name": "Total", "type": "formula", "formula": "{price} * {qty}",
    "numberDisplay": "bar", "numberMax": 1000 }
])
```

`numberDisplay` takes `plain`, `bar` or `ring` and `numberMax` sets the 100 %
mark — the same three settings the **Display** field offers in the dialog.

Send an existing `id` to change an expression in place. **Leaving `id` out does
not reliably add a column**: the name is slugged into an id first, and if that
id already exists the call merges into it. Sending `{name: "Total", formula: …}`
twice changes the first Total rather than creating a second one. Call
`get_collection` first — it returns every property's id, type and current
expression, which is what the `{…}` references have to match.

Remove one with `remove_properties`, a list of property ids:

```
update_schema(page_id: "<database>", remove_properties: ["total"])
```

The row values of a removed property stay in the rows, so re-adding the property
brings them back.

`create_database` takes the whole schema in one call, formula properties
included, so a database can be created with its computed columns already in
place rather than added afterwards.

Read the results with `query_rows`. It is the only tool that returns computed
values: `get_page` on a database renders the stored table, and `get_page` on a
single row returns that row's title and body and no properties at all.

**Do not write to a formula property with `set_properties`.** It is not refused,
and it has no effect on what a table, a board, a row page or `query_rows` shows
— the next read replaces it with the computed value. The value *is* stored,
though, and that has three consequences: the Markdown table from `get_page` on
the database, the Markdown export and a database shared by public link all print
what you wrote, and a server-side filter or sort on that property id can suddenly
see the row when no other row is visible to it.

## Formula or rollup?

| The number comes from | Use |
| --- | --- |
| Other properties of the same row | a formula |
| A property across related rows (their sum, count, average, smallest, largest) | a rollup |
| The share of related rows meeting a condition | a rollup with **Percent** — no division, and 0 rather than an error when there are none |
| Something a formula cannot express — text, dates, conditions | a plain property somebody fills in |

A rollup cannot aggregate a formula column, and a rollup's condition cannot test
one either: the pickers in **Collection properties** offer neither, because the
value does not exist on the other side of a relation. An agent can point
`rollupTarget` at a formula id over MCP anyway; the rollup then reads the stored
value on the related rows, finds nothing, and a Sum or an Average comes out as
`0`. A Count still counts, because it counts rows rather than values.

## Importing a database that had formulas

A Notion export carries a formula column as its *results* — the numbers, already
worked out, as text in the CSV. The importer guesses each column's type from the
values it sees, and the types it can guess are text, number, select and date. So
a Notion formula arrives as an ordinary editable column of frozen numbers, which
stop moving the moment anything they were computed from changes. If you want it
live again, add a formula column and rewrite the expression by hand. See
[Import and export](import-export.md).

More on all three derived types in [Properties](properties.md#formula) and
[Relations and rollups](relations-and-rollups.md); more on views, filters and
sorting in [Views](views.md).
