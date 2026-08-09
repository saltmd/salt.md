# Properties

A **property** is a typed field on a [collection](collections.md). Every row of
that collection has a slot for it, and every [view](views.md) of that collection
can show it or hide it, and — with the exceptions noted below — filter and sort
by it. There are thirteen types. Ten store a value; three are computed every
time a row is read and never written down.

The interface calls the container a **collection**; agents connecting over MCP
call the same thing a **database**. The property types are named identically on
both sides.

## Adding, changing and removing a property

1. Open the collection and click **Properties** in the bar above the rows. The
   dialog is titled **Collection properties**.
2. Type a name into **New property name**, choose a type from the list beside
   it, and click **Add**. The property appears at the bottom of the list.
3. Fill in whatever the type needs — a select needs options, a relation needs
   **Links to**, a rollup needs four answers. Those fields appear under the
   property as soon as you pick the type.
4. Click **Save**. Nothing is written until you do; **Cancel** discards the lot.

Rename a property by typing over its name. Change its type with the dropdown
beside it. **Changing the type does not delete anything**: the values stay in
the rows exactly as they were, and a value the new type cannot read shows as
empty rather than being thrown away. Switch back and it reappears.

**Options are edited in this dialog too**, not only from a cell. Under a select
or multi-select, the empty box reading **+ Option** adds an option when you
press Enter, and clicking an option's chip opens the nine-colour palette
(**Change colour**). That is where a board's column colours are decided.

Remove a property with the ✕ at the end of its row (**Delete property**). **The
values stay in the rows too.** Add a property with the same name again and the
old values are visible again, because the id is derived from the name. This is
deliberate — an accidental deletion is curable.

Removing a property also cleans up after itself in the views: a filter, a sort
or a hidden-column entry that pointed at it is dropped, a board that grouped by
it falls back to the first select, multi-select or relation property, and a
calendar falls back to the first date property.

**A timeline is the exception.** Its start and end dates are not repaired, so
removing the date property a timeline drew its bars from leaves that view asking
for a start date instead of falling back. Pick a new one from the **Start:**
dropdown in the bar above the timeline.

## Names and ids

Every property has a **name** and an **id**. People see the name. Everything
written down — a stored value, a filter, a formula reference, an MCP call — uses
the id.

The id is made from the name when the property is created and then never
changes. Rename "Status" to "Stage" and the id stays `status`. Two properties
whose names produce the same id get a suffix so neither is lost.

**The two sides derive it slightly differently**, which matters exactly where
ids matter. The dialog turns every run of characters that is not a letter or a
digit into a hyphen. An agent's `update_schema` keeps letters and digits, turns
spaces, hyphens and underscores into hyphens, **drops** everything else, and
spells German umlauts out. So a property named "Größe" gets the id `gr-e` when a
person adds it in the dialog and `groesse` when an agent adds it. Colliding ids
get an underscore in the dialog and a number over MCP. Never assume an id —
read it from `get_collection`.

Select options work the same way: each has an id, a name and a colour, and the
id is what a row stores. An option shown as "In progress" is very likely stored
as `in-progress`.

This is the single most common source of confusion when writing values from
outside the interface. Call `get_collection` first; it returns every property
id, every type and every option id.

## The thirteen types

| Type | In the dialog | Stores |
| --- | --- | --- |
| [text](#text) | Text | a string |
| [number](#number) | Number | a number |
| [select](#select) | Select | one option id |
| [multiselect](#multiselect) | Multi-select | a list of option ids |
| [date](#date) | Date | a calendar date |
| [checkbox](#checkbox) | Checkbox | true or false |
| [checklist](#checklist) | Checklist | a list of sub-tasks |
| [url](#url) | URL | a link |
| [person](#person) | Person | one person |
| [relation](#relation) | Relation | a list of row ids |
| [backrelation](#backrelation) | Backrelation | nothing — computed |
| [rollup](#rollup) | Rollup | nothing — computed |
| [formula](#formula) | Formula | nothing — computed |

## Stored types

### text

A string of any length. The cell shows the text; click it to edit, press Enter
or click away to save.

**Filters** with is, is not, contains, is empty, is not empty. **Sorts** the way
the active language sorts words, ignoring case and accents, and a number inside
the text compares as a number — so "Room 2" comes before "Room 10". Because
accents are ignored rather than ordered, "Ander" and "Änder" compare as equal
and land next to each other in no fixed order.

**On a board card** a text property is the one type whose *value* decides where
it goes. An email address, a phone number or a postcode-and-town line becomes a
small icon in the card's bottom row: on a card those say "this exists", and the
value itself is in the tooltip. An IPv4 address such as `192.0.2.40` stays a
labelled line, because the digits are the whole point of it. Anything else
becomes a note — one paragraph clamped to two lines, and only the first such
property gets that treatment.

### number

A number. The cell is a number field.

Under **Display** a number can be drawn three ways: **Number** (the default),
**Progress bar** or **Ring**. Both of the latter need **Max (= 100%)**, which
defaults to 100. A bar or a ring shows the figure beside it and turns back into
a plain input when you click it. Its tooltip reads the value and the maximum,
which is the only place the configured Max can be seen while reading.

**Filters** with is, is not, >, <, is empty, is not empty — but use > and <
rather than an exact is, which matches nothing (see
[Filtering](#filtering-sorting-and-hiding)). **Sorts** numerically. In a table
the column footer shows the sum of the column, marked with a Σ.

**On a board card** it is a labelled fact: the property name, then the value. A
bare "55" on a card means nothing, so the name comes along.

### select

One option out of a fixed set. Each option has a name and a colour, and the row
stores the option's id.

Click the cell to open the picker: type to narrow the list, type a name that
does not exist yet and an entry appears offering to create it on the spot, or
click the ⋯ beside an option to recolour it (nine named colours, Gray through
Red) or delete it. Choosing the option that is already set clears the cell.

Deleting an option here removes it from the collection's schema and clears it
from the row you are on — **but not from other rows**. Those cells keep the id
and show it in grey, which is how a stray `in-progress` sometimes appears on
screen. On a board they collect in the catch-all column.

**Filters** with is, is not, is empty, is not empty, and the value is picked
from the option list rather than typed. **Sorts** by the stored option id, not
by the option's position in the list.

**On a board card** it is a coloured chip and stays editable there. An *empty*
select is still reachable: it is invisible until you hover the card, so a status
can be set without opening the row.

A board groups by a select by default: one column per option, plus a catch-all
column named "No " and the property's name — "No Status" — which also collects
every row whose value points at an option that no longer exists, so no card can
get lost.

### multiselect

Zero or more options from the same kind of set. Stored as a list of ids.

The same picker as a select, except that choosing does not close it and choosing
an already-picked option removes it.

**Filters** with is, is not, is empty, is not empty; "is" matches when the list
contains that option. **Sorts** by the ids joined together, which is rarely
what you want.

**On a board card** each option is a chip. An empty multi-select behaves like an
empty select — invisible until you hover the card, editable there. A board can
group by a multi-select, and then a row with two options appears as a card in
both columns.

### date

A calendar date, `2026-07-18`, optionally with a time, `2026-07-18T14:30`. The
cell is a date picker, so the interface writes dates without a time; a time can
arrive from an import or from an agent, and is shown when it is there.

**A date is never converted between time zones.** A deadline on the 18th is the
18th for a reader in Auckland and for one in Los Angeles. This is deliberate and
pinned by tests — converting it moves it, and a contract then expires a day
early. It is also why the date is shown in the reader's regional format but not
in their zone. See [Language and time](language-and-time.md).

**Dates carry urgency where they are read rather than edited** — on a board
card, on a gallery card, and anywhere the row is read-only: a date in the past
is drawn in bold orange, today and tomorrow in bold amber, everything else
quietly. A cell you can type into is a plain date picker and shows no colour at
all, so the same deadline is orange on the board and black in the table.

**Filters** with is, is not, after, before, is empty, is not empty. **Sorts**
chronologically, because the stored form sorts correctly as text.

A date property is what a [calendar or timeline view](views.md) needs, and it is
also what feeds a calendar subscription: every date value of every row becomes
one event, named after the row and the property, all-day unless the value
carries a time.

**On a board card** it is a labelled fact. If a card has only one fact and the
property is literally called "Date" (or the German `Datum`), the label is
dropped as noise. The same goes for a lone number called "Number" or `Zahl`.

### checkbox

True or false. The cell is a checkbox; click it.

**Filters** with is, is not, is empty, is not empty, and the value is offered as
**Checked** or **Unchecked** — though "is" matches nothing either way (see
[Filtering](#filtering-sorting-and-hiding)), so narrow with "is not" instead:
"is not Checked" gives you the unfinished rows.

**In a view, "is not empty" returns only the ticked boxes.** A box that was
explicitly unticked counts as empty there. Over `query_rows` the same condition
gives you the unticked box back as well, which is the one place the two surfaces
disagree. **Sorts** with unticked before ticked.

**On a board card** it is a labelled fact showing a locked checkbox. A box that
was never touched has no stored value at all and stays off the card; a box
explicitly unticked shows as an empty box, because "we checked and it is not
done" is worth seeing.

### checklist

A list of sub-tasks, each with its own text and its own tick. One property, many
boxes — for the case where a row has a handful of steps that do not deserve a
collection of their own.

A filled cell shows a progress bar with a percentage; the tooltip says how many
of how many. An empty one reads **+ Sub-task**, so there is always something to
click. Inside: tick items, type over their text, press Enter or click
**Sub-task** to add the next one, use the bin to remove one, and click **Done**
to close. Empty items are kept while the list is open — you have to be able to
type into a fresh one — and dropped when you close it. Items with no text never
count towards the percentage.

The progress is derived from the ticks. There is no stored percentage to keep in
step, which is the whole reason this is a type and not a convention.

**Filters** with is, is not, is empty, is not empty, against a typed value —
which is of limited use, since the stored value is a list of objects. **Sorts**
by the same raw value.

**On a board card** it is a labelled fact: the bar and its percentage. A
checklist cannot be filled in on a [form](forms.md).

### url

A link, **stored exactly as typed**. An address with no scheme is opened as
`https://` anyway — the scheme is added to the link and to the shortened label,
never to the value in the row.

The cell shows a link chip with only the host, `www.` removed, and the full
address in the tooltip. Clicking opens it in a new tab without opening the row
behind it.

**A URL cell cannot be typed into.** It renders as a link everywhere — in a
table, on a card, on the row's own page — and it is not offered on a
[form](forms.md) either, so the only ways to fill one in are an agent or the
API, or switching the property to Text, typing the address, and switching it
back (values survive a type change).

**Filters** with is, is not, is empty, is not empty. **Sorts** by the stored
text.

**On a board card** it is a link icon in the bottom row, next to the other
contact icons — on a card a full address is noise.

### person

One person: an account, or a name typed for somebody without one.

Click the cell — an empty one reads **＋ Person** so there is always something to
hit — and pick a colleague from the list, or type a name and take it as it
stands. Enter takes the single remaining match, or, if there is none, the text
as typed, so somebody without an account can be entered without touching the
mouse. **Remove** at the foot of the list clears the cell.

Picking stores the **account id**, so the cell follows a later rename; typed
text is stored as typed. The cell shows a face and the name either way, and an
unrecognised value stays readable text rather than disappearing.

The list holds the members of every workspace this browser can see. It is
fetched once and shared by every cell on the page.

**Filters** with is, is not, contains, is empty, is not empty. **Sorts** by what
is stored — an id for somebody with an account, the typed text otherwise — so
sorting a person column will not put people in alphabetical order.

**On a board card** people do not get a line of their own. Every person property
on the row is collapsed into one stack of faces in the card's top right corner,
deduped by person: the same colleague named by two different fields is one face,
and the names are in the tooltip. Three faces are shown, the rest become "+2".

### relation

A pointer at rows in another collection — or in the same one.

Click the cell (**＋ Link** when empty) and pick rows by title; the picker
searches the target collection and shows each row's icon. Picking again removes
it. The value is **always a list**, even for a single target. The picker loads
up to 500 rows of the target collection and searches within those, so a row
beyond that cannot be linked from here.

Two things it can do beyond linking. A board can **group by** a relation, giving
one column per row of the target collection — one column per customer, per
system, per project. And a table can use a relation that points at its *own*
collection as a **sub-item** relation, which draws the rows as a collapsible
tree.

**A relation stores ids and hands them back as stored.** Permission is checked
per target row where the value is *used*: a [rollup](#rollup) over the relation
and a [backrelation](#backrelation) both skip rows you are not allowed to read,
so neither can reveal that such a row exists. The chips themselves are a
different matter — an id the picker cannot resolve is drawn as a chip reading
"Untitled" rather than being hidden, and that is what you see for a row you may
not read, for a row that has been deleted, and for a target beyond the picker's
500.

**Filters** with is, is not, is empty, is not empty, and the value is picked
from a list of row titles rather than typed — filtering by a 32-character id was
possible in theory and unusable in practice. **Sorts** by the stored ids.

**On a board card** each linked row is a chip carrying its icon and title. On a
board that groups by that relation the chip is dropped, so a card never repeats
its own column heading.

More in [Relations and rollups](relations-and-rollups.md).

## Computed types

These three hold no data. They are worked out by the server every time rows are
read, in this order: **backrelation, then rollup, then formula.** The order is
load-bearing — each one can build on the one before.

Because the rows are chosen in the database *before* the values exist, computed
properties behave differently from stored ones in two places.

**Filtering by one is half-broken, and which half depends on the operator.** A
condition the database cannot answer keeps every row, and a view then applies
the same condition a second time in the browser, with the values in hand. So:

| Operator | In a view | Over `query_rows` |
| --- | --- | --- |
| is, >, < | nothing at all | nothing at all |
| is not empty | nothing at all | nothing at all |
| is not | works | every row |
| is empty | works | every row |

The two that work are worth knowing: "Backrelation is empty" finds the rows
nothing points at, and "Rollup is not 0" finds the rows whose number is not
zero. Everything else has to be aimed at the stored properties the computation
reads.

**Sorting by one works in a view but not over `query_rows`**, for the same
reason — the interface re-sorts once the values are there, the tool does not.

### backrelation

The reverse of a relation somebody else declared. It answers "which rows over
there point at me?" and it stores nothing at all.

It needs two answers, both in the property's own configuration: **Rows from**
(the collection that points here) and **That point here via** (which of that
collection's relation properties does the pointing). The second dropdown only
lists relations, and only after the first is chosen — a collection can hold
several relations back to yours, and picking the wrong one silently lists the
wrong rows. A backrelation missing either half is an empty column, not an error,
which is why an agent that omits one is refused outright.

Nothing is stored because a stored reverse side means keeping two lists in step
on every write from both directions, and the first missed update leaves them
disagreeing with no way to tell which is right.

It reads exactly like a relation — the same chips, the same titles — but it is
never editable: you change it by changing the relation that produces it.
Permission is checked per row, so a row you may not read is left out.

**Filtering** by it obeys the table above: "is empty" and "is not" work in a
view, "is" and "is not empty" match nothing. A filter on a backrelation offers a
dropdown of row titles, exactly as a relation does. **Sorting** by it compares
the linked ids strung together, which says nothing useful.

**On a board card it never appears.** On a customer row that would be every
order ever placed. Fine in a table, far too much for a card.

### lastActivity

When anything last happened on this row, and who did it. Nothing to fill in and
nothing to keep up to date: it is computed when the row is read, so it moves by
itself the moment somebody edits a property, writes in the row, or an agent
changes it over MCP.

The time comes from the row's own last-changed stamp, which every write path
sets. The
name comes from the newest entry in the activity log for that row, and is simply
left out when nothing was logged — a guess would be worse than a gap.

Read-only, like a rollup or a formula. Setting it does nothing.

### rollup

Aggregates a property across the rows a relation — or a backrelation — points
at. Four answers configure it:

| Field | Means |
| --- | --- |
| **Via relation** | which relation (or backrelation) to follow |
| **Of property** | which property of the related rows to take |
| **Calculate** | Sum, Count, Average, Min, Max or Percent |
| **Only rows where** | an optional condition; the default is **All rows** |

What each calculation does:

| Calculate | Result |
| --- | --- |
| Sum | the total of the values that are numbers |
| Count | how many related rows, whether or not the property is filled |
| Average | the mean of the values that are numbers |
| Min / Max | the smallest / largest number |
| Percent | the share of the related rows that meet the condition |

Sum, Average, Min and Max ignore anything that is not a number. With no related
rows at all they are 0 rather than blank.

**The condition is what turns a count into progress.** Pick a property of the
related rows, then is, is not, contains, is empty or is not empty, then a value.
For a select the value is offered as its options, because the comparison is
against the stored option id and typing the label yields a silent zero. For
**is** and **is not** you may tick *several* options: "open" is neither done nor
discarded, and one comparison cannot say that — an "is not done" that forgets
the discarded rows overstates the work left by exactly the amount nobody
notices.

Point the condition at a property that holds one value — a select, a checkbox, a
date, a number, a text field. On a **multi-select** the whole list is compared
as one piece of text, so "is" and "is not" never match an entry, and "is empty"
never matches even a multi-select that has been emptied, because an emptied list
is still something rather than nothing. Only **contains** is usable there — it
finds an option inside the list.

**Percent** exists so that a progress bar needs no arithmetic: set Calculate to
Percent, set a condition, set **Display** to Progress bar. The alternative — two
rollups and a formula that divides — puts a division-by-zero message in the
column of every newly created row, because zero of zero related rows is a
perfectly ordinary state.

A rollup can be shown as a plain number, a progress bar or a ring, the same
three options a number property has.

**On a board card** it is a labelled fact.

Worked examples in [Relations and rollups](relations-and-rollups.md).

### formula

Arithmetic over the row's own properties, written in the **Expression** field.
The dialog lists the available references under it, ready to copy.

What it can do: `+`, `-`, `*`, `/`, parentheses, plain numbers, a leading minus,
and a reference to another property written `{propertyId}`.

```
{done} / {total} * 100
({hours} - {billed}) * {rate}
```

What it cannot do: no functions, no text handling, no dates, no conditions, no
reaching into other rows. When you need any of those, the answer is a rollup or
a number that a person or an agent writes.

Three behaviours worth knowing:

- **A formula may reference a rollup**, because rollups are computed first. That
  is how "percentage done" is expressed without a second data source.
- **A formula may reference another formula.** A circle between them is detected
  and reported rather than hanging.
- **A reference to a property that does not exist counts as zero.** A typo in an
  id does not produce an error; it produces a wrong number quietly. Copy the
  references out of the dialog instead of typing them.

A bad expression — a division by zero, an unbalanced bracket, a circle — shows
in the cell as a short warning beginning with a warning sign, in place of the
number. Like a number, a formula can be displayed as a bar or a ring.

**On a board card** it is a labelled fact.

More in [Formulas](formulas.md).

## Filtering, sorting and hiding

The bar above the rows carries these controls, and each one belongs to the
**current view only** — filtering the board does not touch the table. A view of
type `form` has no Filter, Sort or Group control at all; it can only hide
columns.

- **Filter** adds conditions, combined with AND. Choose a property from the
  **+ Add filter…** dropdown at the foot of the panel and a row appears; the ✕
  at its end removes it again. Each row names a property, an operator and a
  value; the operators offered depend on the type, and so does the value field:
  a dropdown of options for a select, a dropdown of row titles for a relation or
  a backrelation, Checked/Unchecked for a checkbox, a text box for everything
  else.

  **One operator has a blind spot.** Rows are selected in the database, where an
  "is" compares the stored value against text. That is exactly right for a
  select, a date, a relation or a text property, and it matches nothing at all
  for a number or a checkbox, whose values are stored as a number and a boolean.
  Use ">" and "<" on a number, and "is not" on a checkbox; both work.
- **Sort** takes one property and a direction, **Ascending** or **Descending**.
  **No sort** at the top of the property list clears it again. Numbers compare
  as numbers; everything else compares the way the active language sorts,
  ignoring case and accents and treating digits inside text as numbers.
- **Group** appears on a board and chooses the column property. Only select,
  multi-select and relation properties can be grouped by.
- **Columns** shows every property under **Shown** or **Hidden** with its type
  icon; clicking one moves it between the two, and **Hide all** / **Show all**
  do the obvious. A hidden property is dropped from every renderer of that view
  — table columns, cards, list lines, the form — but keeps its values, and the
  row's own page still shows it.

Two view-specific dropdowns sit in the same bar. Above a **table** that has a
relation pointing at its own collection, a dropdown offers **No sub-items** or
that relation, which turns the rows into a collapsible tree. Above a
**timeline**, **Start:** and **End: (none)** choose which date properties the
bars run between; leaving the end unset draws one-day bars.

## The table's footer row

A table with rows in it ends in a calculation row. The first cell counts the
rows. Under a number, a rollup or a formula column stands **Σ** and the sum of
that column. Under every other column stands how many cells are filled — an
empty list, an empty string and an unticked box all count as unfilled.

## What a board card shows

A board card does not print the schema. Each property is sorted into a zone by
its type, and a text property by its value, so that cards look the same
everywhere no matter how the schema grew.

| Zone | Types | Appearance |
| --- | --- | --- |
| Chips | select, multiselect, relation | coloured chips, editable in place |
| People | person | one stack of faces, top right |
| Facts | number, date, checkbox, checklist, rollup, formula | one entry each with the property name, wrapping across the card |
| Note | text that reads like prose | one paragraph, clamped to two lines |
| Contacts | url, text that reads like a mail address, phone number or postal line | icons in the bottom row |
| Nothing | backrelation | never on a card |

Four rules on top of the zones:

- **The property the board groups by is dropped from its own cards.** A card
  never repeats its column heading.
- **Empty values are dropped**, and so are values that only look filled: a lone
  `-`, `–`, `—`, `/`, `n/a`, `na` or `none`, plus the German `k.a.` and
  `keine`, because that is what imports from other tools leave behind.
- **A card prints at most eight facts, counting the note as one.** It is a cap
  on facts, not on lines: they flow across the card and wrap, so eight of them
  are a handful of lines rather than eight. Everything beyond the eighth
  collapses into a **+3 more** button in the
  card's bottom line, which names the missing fields in its tooltip and opens
  them below the card; the button then reads **less**. Nothing is hidden
  silently.
- **Empty selects survive.** They are invisible until you hover the card, so a
  status can be set from the board without opening the row. The same applies to
  an empty multi-select.

Beyond reading, a card can be acted on. Drag it to another column to set the
grouping property. The ⋯ in its corner — or a right-click anywhere on the card —
opens a menu with **Open**, a **Move to** list naming every column the card is
not already in, and **Move to trash**. And the **＋ New** button at the foot of a
column creates a row that already carries that column's value, which is how a
board writes a select, a multi-select or a relation without opening a row at
all.

## Properties over MCP

Agents read and write the same properties, by id.

| Tool | Does |
| --- | --- |
| `get_collection` | the full schema and views — call this first, always |
| `update_schema` | add or change properties; merges, so unmentioned ones are untouched |
| `set_properties` | set values on one row; merges per property, `null` clears one |
| `create_rows` | up to 200 rows in one call, each with its properties |
| `query_rows` | filter, sort and page through rows, computed values included |
| `set_view` | a view's filters, sort, grouping, date properties and hidden columns |

Two conveniences on the write path, both of them there because the obvious
spelling used to fail quietly:

- **A select value written as its name is resolved to the option id**, case
  insensitively. `{"status": "Planned"}` lands as `planned`. A value that
  matches no option is stored as written. Filter values are resolved the same
  way, so an agent that writes with a name can search with it too.
- **A single value written to a list-shaped property is wrapped in a list.**
  `{"system": "abc"}` is stored as `["abc"]`. Writing it unwrapped used to look
  correct — the row still grouped and still filtered — while every backrelation
  and rollup passed straight over it.

Options may be written the short way, `["To do", "Done"]`, or with colours,
`[{"name": "Done", "color": "#2f9e44"}]`. Both are accepted and normalised.

A type an agent invents is refused with the list of the thirteen, and a
backrelation missing either of its two coordinates is refused as well. See
[Agents](agents.md) and [MCP tools](mcp-tools.md).
