# Formulas

A **formula** is a property of a collection that holds an expression instead of
a value: *price times quantity*, *days until the due date*, *"Overdue" when the
date has passed and the box is not ticked*. The server works it out every time
a row is read, so there is nothing to recalculate and nothing that can go
stale, and nothing to type into, either. A formula cell is read-only
everywhere.

The language has numbers, text, checkboxes, dates and lists, the usual
operators, conditions, and a library of functions. It only ever sees the row it
sits on; anything from related rows comes in through a rollup. This page says
exactly what the language supports, what you see when an expression is wrong,
and the places where a formula value is not available at all.

A collection is called a **database** on the MCP surface that agents use: same
thing, two audiences. See [Collections](collections.md).

## Adding a formula column

1. Open the collection and click **Properties** in the view's toolbar. The
   dialog is titled **Collection properties**.
2. At the bottom, type a name into **New property name**, pick **Formula** from
   the type list, and click **Add**.
3. The new property gets an **Expression** box. Type the expression there. The
   placeholder shows the shape: `e.g. prop("Price") * prop("Qty")`.
4. Watch the result under the box (see [the editor](#the-editor)) until it
   shows what you meant.
5. Optionally set **Display**: **Number** (the default), **Progress bar** or
   **Ring**. The last two ask for **Max (= 100%)**.
6. Click **Save**.

Nothing is written until you click **Save**: the dialog holds the whole schema
in front of the table until then, so adding the column and typing its expression
are one act. The dialog does not refuse a broken expression. A formula column
that *is* saved with an empty expression shows **⚠ unexpected end** on every row
until it is filled in, and any other mistake shows its own message in the cells
the same way.

To remove a formula again, click the ✕ (**Delete property**) beside it in the
same dialog.

Changing an existing column's type to Formula does not delete what was in it.
The stored values stay on the rows; the computed value simply covers them, and
they reappear if you switch the type back. That leftover matters in one place:
see [where a formula value exists](#where-a-formula-value-exists-and-where-it-does-not).

## The editor

The **Expression** box is several lines tall and grows with what you type, up
to eight lines. Line breaks and indentation are allowed anywhere spaces are, so
a long condition can be laid out one branch per line.

Under the box sits a live result. About 300 ms after you stop typing, the server
checks the expression and works it out on the **first five rows** of the
collection, in the collection's own order:

- If the expression is fine, each of those rows is listed by title (**Untitled**
  for a row without one) with its result beside it, formatted the way the cell
  will show it. An empty result reads **—**; a row where the expression fails
  shows its **⚠** message in red, and the other rows still show theirs.
- If the expression cannot work at all (it does not parse, or names a property
  or a function that does not exist), there is one red line instead: the
  message, prefixed with ⚠.
- A collection without rows says **No rows to try it on yet.**

The check runs against the dialog's unsaved schema, so a property you added a
moment ago, in the same dialog, can already be referenced. Its cells are empty
until somebody fills them in, so the preview treats it as empty.

Below the result:

- **Click a property to insert it:** one button per other property of the
  collection, by name. A click inserts `prop("Name")` at the cursor, replacing
  any selected text. Hovering a button shows the property's id as `{id}`.
- **Operators and functions**, a collapsed reference. Opened, it lists the
  operators and the main functions by group (**Logic**, **Math**, **Text**,
  **Dates**, **Lists**). The full list is [below](#functions).

## Writing an expression

```
prop("Price") * prop("Qty")
if(prop("Done"), "Finished", "Open")
dateBetween(prop("Due"), today(), "days")
prop("Tags").length()
```

### Referring to other properties

`prop("Name")` reads another property of the same row. The text inside is looked
up in this order:

1. a property with exactly that **name**;
2. a property with that **id**;
3. a property with that name, ignoring upper and lower case;
4. `"Title"` or `"Name"` (any case) when no property is called that: the row's
   title.

Anything else is the error **⚠ unknown property "…"**. Two properties with the
same name resolve to the first one in the schema.

A name reference follows the name. **Renaming a property breaks every
`prop("Old name")` that pointed at it**: the cells show *unknown property* until
the expression is updated. A reference by id, `prop("due-date")`, survives
renaming, because a property's id never changes. Ids are shown when you hover the
insert buttons, and agents get them from `get_collection`.

What `prop()` gives you depends on the property's type:

| Property type | `prop()` reads as |
| --- | --- |
| number | a number |
| text, url | text |
| person | what is stored, as text (a member id, or a name an agent wrote as text), or a list of them when several people may be picked |
| checkbox | `true` or `false`; a box nobody ever touched is empty |
| date | a date, with a time of day if one is stored |
| select | the option's **name**, as text |
| multi-select | a list of option names |
| checklist | a list of the items' texts |
| relation, backrelation | a list of row ids (not titles) |
| rollup | its result: a number, or a list for *Show original* and *Show unique values* |
| another formula | its result |
| last activity | a date and time |
| a property with nothing in it | empty |

### The older `{id}` form

Before `prop()`, a formula referred to properties as `{price}`, by id. That form
still works and still means what it did: **the value read as a number**. A
number is itself, a ticked box is 1, text holding a number is that number, and
everything else, including a date, a list, a select whose option id is not a
number, an empty cell and an id that does not exist, is **0**, silently. A
`{…}` naming another formula reads that formula's result as a number, and passes
its error on if it has one; otherwise the evaluator never complains about a
`{…}` reference, and the editor's check does not either.

`{id}` is fine for plain arithmetic between number columns. For anything else
use `prop()`, which reads the real value and reports a reference it cannot find.

### Values and types

Every value has one of six types:

| Type | Written as | Notes |
| --- | --- | --- |
| number | `12`, `0.5`, `.5`, `1e3`, `1.5e-2` | |
| text | `"Done"` or `'Done'` | `\n` is a line break, `\t` a tab, and a backslash before any other character stands for that character: `\"`, `\\` |
| checkbox | `true`, `false` | |
| date | from a date property, `today()`, `now()` or `parseDate("2026-07-18")` | a day, or a day with a time |
| list | `[1, 2, 3]`, `["a", "b"]` | items may be of any type, lists included |
| empty | a property with nothing in it | there is no literal for it; `ifs` without a fallback gives it |

How the types meet:

- **Empty counts as 0 in arithmetic**, and as empty text when joined to text.
- **A checkbox counts as 1 or 0** in arithmetic.
- **Text, dates and lists are not numbers.** `prop("Label") * 2` is the error
  **⚠ expected a number, got text**, not 0. `toNumber()` turns text that holds
  a number into one.
- **True and false** for `if`, `and`, `or`, `not` and `? :`: empty, `false`,
  `0`, empty text and an empty list are false; everything else is true.

### Operators

| Operator | Means |
| --- | --- |
| `+` | adds numbers; **joins text** as soon as either side is text: `"Qty: " + prop("Qty")` |
| `-` `*` `/` | subtract, multiply, divide; dividing by 0 is an error |
| `%` | remainder (`7 % 3` is 1; the sign follows the left side); by 0 it is an error |
| `^` | power. Right to left: `2 ^ 3 ^ 2` is `2 ^ 9`, 512. `-2 ^ 2` is -4 |
| `==` (also `=`), `!=` | equal, not equal. Never an error: values of different types are simply not equal, so `5 == "5"` is false. Empty equals `""` and `[]`, but not `0` and not `false`. Lists are equal item by item, dates when they name the same moment |
| `<` `<=` `>` `>=` | numbers as numbers, text alphabetically (upper case before lower case), dates by time. Text or a date against anything else is an error: **⚠ cannot compare text with number** |
| `and` (also `&&`), `or` (also `\|\|`) | true or false; the right side is only worked out when it matters |
| `not` (also `!`) | true or false |
| `cond ? a : b` | `a` when `cond` is true, otherwise `b`; only the chosen side is worked out |
| `-x`, `+x` | negative, and a number unchanged |
| `( )` | grouping |
| `x.f(a)` | the same as `f(x, a)`: `prop("Name").upper()`, `prop("Tags").length()`. Without arguments the brackets may be dropped: `prop("Tags").length` |

From weakest to strongest binding: `? :`, `or`, `and`, `not`, the comparisons,
`+ -`, `* / %`, a leading minus, `^`, and the `.f()` call. So
`not prop("Done") and prop("Qty") > 3` means `(not prop("Done")) and
(prop("Qty") > 3)`, and `not 1 == 2` means `not (1 == 2)`.

`+` refuses dates: **⚠ use dateAdd() to add to a date, not + (got 2026-07-18)**.
When text is joined, a number is written without floating-point noise (`0.1 +
0.2` joins as `0.3`), a checkbox as `true` or `false`, a date as `2026-07-18` or
`2026-07-18 09:30`, a list as its items separated by `, `, and empty as nothing.

### Functions

A function is called by name with its arguments in brackets, separated by
commas. A call with the wrong number of arguments is an error that says how many
it takes: **⚠ if() takes 3 arguments**. A name that is not a function is **⚠
unknown function foo()**. An error raised *inside* a function carries its name in
front: **⚠ round(): expected a number, got text**.

Function names are case-sensitive and written as shown.

**Logic**

| Function | Returns |
| --- | --- |
| `if(cond, a, b)` | `a` when `cond` is true, otherwise `b` |
| `ifs(c1, a1, c2, a2, …, fallback)` | the value after the first true condition; the fallback (optional) when none is; empty without one |
| `empty(x)` | true when `x` is empty, empty text (spaces only counts), an empty list, `false` or `0` |
| `let(name, value, expression)` | the expression, with `name` standing for `value`: `let(t, prop("Qty") * prop("Price"), t > 100 ? t * 0.9 : t)` |
| `lets(a, 1, b, a + 1, …, expression)` | the same with several names; each may use the ones before it |

`if`, `ifs`, `and`, `or` and `? :` only work out the branch they take, so
`if(prop("Qty") == 0, 0, prop("Price") / prop("Qty"))` never divides by zero.

**Numbers**

| Function | Returns |
| --- | --- |
| `abs(n)` `sign(n)` | the absolute value; -1, 0 or 1 |
| `round(n)`, `round(n, places)` | rounded half away from zero; `round(3.14159, 2)` is 3.14, a negative `places` rounds to tens, hundreds… |
| `floor(n)` `ceil(n)` | rounded down, up |
| `sqrt(n)` `cbrt(n)` | square root, cube root |
| `pow(a, b)` `mod(a, b)` | the same as `a ^ b` and `a % b` |
| `exp(n)` `ln(n)` `log10(n)` `log2(n)` | e to the power n; logarithms |
| `pi()` `e()` | the constants; the brackets are required |
| `sum(…)` `mean(…)` `median(…)` `min(…)` `max(…)` | over any mix of numbers and lists of numbers: `sum(1, 2, [3, 4])` is 10. Empty values are skipped; text is an error. With nothing to work on `sum` is 0 and the others are empty. `median` of an even count is the mean of the middle two |
| `toNumber(x)` | text holding a number as that number (`" 7.5 "` is 7.5), empty for other text; a checkbox as 1 or 0; a date as milliseconds since 1 January 1970 |

**Text**

| Function | Returns |
| --- | --- |
| `format(x)` | any value written out as text, by the rules of `+` above |
| `length(x)` | the number of characters, or of items in a list |
| `lower(t)` `upper(t)` `trim(t)` | lower case, upper case, without surrounding spaces |
| `concat(a, b, …)` | the arguments joined as text; when every argument is a list, one list |
| `join(list, separator)` | the items as text with the separator between them; no separator joins them directly |
| `split(t, separator)` | a list of the pieces; no separator, or `""`, splits into characters |
| `contains(t, part)` | whether the text contains the part (case-sensitive); on a list, whether an item equals it |
| `startsWith(t, part)` `endsWith(t, part)` | true or false |
| `substring(t, start, end)` | the characters from `start` up to, not including, `end` (optional); counting starts at 0, and a negative number counts from the end |
| `slice(x, start, end)` | the same, for text or for a list |
| `repeat(t, n)` | the text `n` times |
| `test(t, pattern)` | whether the regular expression matches |
| `match(t, pattern)` | a list of every match (at most 1000) |
| `replace(t, pattern, with)` | the first match replaced; `$1` in `with` is the first bracketed group |
| `replaceAll(t, pattern, with)` | every match replaced |

Patterns are regular expressions in the RE2 syntax (the one Go uses): character
classes, groups, `|`, `*`, `+`, `?`, `{n,m}`, `^`, `$`, and `(?i)` in front for
case-insensitive matching. There are no lookaheads and no back references. A
pattern that does not parse is **⚠ test(): invalid pattern "("**.

**Dates**

| Function | Returns |
| --- | --- |
| `now()` | the current date and time, to the minute |
| `today()` | the current date, without a time |
| `parseDate(t)` | the text as a date. Read: `2026-07-18`, `2026-07-18T09:30`, `2026-07-18T09:30:15`, `2026-07-18 09:30` and full ISO 8601 with a time zone. Anything else is an error |
| `dateAdd(d, n, unit)` `dateSubtract(d, n, unit)` | the date moved by `n` units. An empty date gives empty |
| `dateBetween(a, b, unit)` | **a minus b** in whole units, cut toward zero: positive when `a` is later. `dateBetween(prop("Due"), today(), "days")` is the days left |
| `formatDate(d, layout)` | the date written with the layout below |
| `year(d)` `month(d)` `date(d)` | the year, the month (1 to 12), the day of the month |
| `day(d)` | the day of the week, **1 for Monday to 7 for Sunday** |
| `week(d)` | the ISO week number |
| `hour(d)` `minute(d)` | the time of day's parts; 0 for a date without a time |
| `timestamp(d)` | milliseconds since 1 January 1970 |
| `fromTimestamp(ms)` | that moment as a date with a time |

The parts functions (`year` to `minute`) give empty for an empty date. A date
argument may also be text that `parseDate` would read.

Units are `years`, `quarters`, `months`, `weeks`, `days`, `hours` and
`minutes`, singular or plural, in any case. Anything else is **⚠
dateAdd(): unknown unit "fortnights": use years, quarters, months, weeks, days,
hours or minutes**. Years to days move on the calendar and take whole numbers
only (`1.5` days is 1 day); hours and minutes may be fractional and give the
date a time of day. Months move like a calendar does: adding one month to
31 January gives 3 March, because February has no 31st.

`formatDate` understands these tokens; everything else in the layout is copied
as it stands, and text in `[square brackets]` is copied without being read as
tokens:

| Token | Gives | Token | Gives |
| --- | --- | --- | --- |
| `YYYY` | 2026 | `HH` / `H` | 09 / 9, 24-hour |
| `YY` | 26 | `hh` / `h` | 09 / 9, 12-hour |
| `MMMM` / `MMM` | July / Jul | `mm` / `m` | 05 / 5, minutes |
| `MM` / `M` | 07 / 7 | `ss` / `s` | seconds |
| `DD` / `D` | 08 / 8 | `A` / `a` | PM / pm |
| `dddd` / `ddd` | Wednesday / Wed | | |

Month and weekday names are English whatever the interface language:
`formatDate(prop("Due"), "D MMMM YYYY")` is *8 July 2026*. To show a date in the
reader's own format, return the date itself rather than text (see [how the value
is shown](#how-the-value-is-shown)).

`now()` and `today()` use the **server's** clock and time zone, not the
reader's. Near midnight, a reader in another time zone can see `today()` name a
different day from their own. The value is worked out on every read, so a
formula using them moves on by itself.

**Lists**

| Function | Returns |
| --- | --- |
| `at(list, i)` | the item at position `i`, counting from 0; a negative `i` counts from the end; empty beyond either end |
| `first(list)` `last(list)` | the first, the last item; empty for an empty list |
| `sort(list)` | sorted by the rules of `<`; mixing text and numbers is an error |
| `reverse(list)` `unique(list)` `flat(list)` | reversed; each value once, first occurrence kept; nested lists flattened |
| `includes(list, x)` | whether an item equals `x`; the same as `contains` |
| `map(list, expression)` | the expression worked out for each item |
| `filter(list, condition)` | the items for which the condition is true |
| `find(list, condition)` `findIndex(list, condition)` | the first item for which it is true, and its position; empty and -1 when there is none |
| `some(list, condition)` `every(list, condition)` | whether it is true for at least one item, for every item (`every` is true for an empty list) |

Inside the expression of `map`, `filter`, `find`, `findIndex`, `some` and
`every`, `current` is the item and `index` its position:
`prop("Checklist").filter(current.startsWith("Ship")).length()`. Any other bare
word is the error **⚠ unknown name "x"**.

A list is empty-safe: `first`, `sort`, `map` and the rest treat an empty
property as an empty list. Anything else that is not a list is **⚠ expected a
list, got text**.

**The row**

| Function | Returns |
| --- | --- |
| `id()` | the row's own id |

### Edge cases worth knowing in advance

- **Empty dates do not compare.** `prop("Due") < today()` on a row without a due
  date is **⚠ cannot compare empty with date**. Guard it:
  `if(empty(prop("Due")), false, prop("Due") < today())`. The guard works because
  `if` only works out the branch it takes.
- **A box nobody touched is false.** `prop("Done")` reads an unticked box as
  `false` whether or not it was ever touched, so `prop("Done") == false` and
  `not prop("Done")` both hold for it.
- **`empty()` counts 0 and false as empty.** `empty(prop("Qty"))` is true for a
  quantity of 0. To test for a missing number, compare the text:
  `format(prop("Qty")) == ""`.
- **Equality across types is false, not an error.** `prop("Qty") == "4"` is
  false even when Qty is 4. Compare like with like, or convert with `toNumber()`
  or `format()`.
- **A select compares by name.** `prop("Status") == "Done"` matches the option
  named *Done*, so renaming the option changes the answer. (A rollup condition
  is the opposite: it compares the stored option id.)

### A formula may reference another formula

`prop("Margin")` may itself be a formula; it is worked out on demand, in any
order, and its own references are followed too. Rollups and backrelations are
worked out before any formula, so a formula can use them freely. A cycle is
refused rather than followed: two formulas that point at each other, or one that
points at itself, both show **⚠ circular reference in formula** and nothing
hangs.

If the referenced formula fails, its error travels outward unchanged. A column
that divides by `prop("Margin")` shows **⚠ division by zero** because Margin
itself divided by zero, not a message of its own. So the cell that complains is
not always the cell with the mistake in it; check every formula it references.

### Limits

| Limit | Error |
| --- | --- |
| An expression may be at most 10,000 characters long | **⚠ formula is longer than 10000 characters** |
| Brackets, calls and operators may be nested at most 100 deep | **⚠ formula is nested too deeply** |
| A row's formulas together may take at most 200,000 evaluation steps | **⚠ formula does too much work** |
| A text result may be at most 100,000 bytes long | **⚠ formula does too much work** |

The step budget is shared by every formula of one row, so a very heavy formula
can push another formula on the same row over the limit. Ordinary formulas use
a few dozen steps; the limit is there for a `map` over a `repeat` that would
otherwise run for minutes.

## When an expression is wrong

The cell shows the message in place, in red, prefixed with ⚠. Errors are per row
and per formula: one bad cell does not disturb the rest of the row, and other
formula columns keep working. A mistake in the expression itself (one that the
editor's preview shows as a single red line) shows the same message on every
row; a mistake that depends on the data (a division by an empty quantity) shows
only on the rows it affects.

| What you see | What happened |
| --- | --- |
| `⚠ unexpected end` | the expression is empty, or breaks off after an operator (`1 + `) |
| `⚠ missing )` | an opening bracket was never closed; `missing ]` and `missing :` are the same for a list and for `? :` |
| `⚠ missing }` | a `{` was never closed |
| `⚠ missing closing "` | a text was never closed (`missing closing '` for one in single quotes) |
| `⚠ unexpected "…"` | something the language does not know, quoted back from where it gave up, to the end: `1 # 2` gives `unexpected "# 2"` |
| `⚠ "1.2.3" is not a number` | a number it cannot read |
| `⚠ unknown name "x"` | a bare word that is not `true`, `false`, a `let` name, or `current`/`index` inside a list function. A property needs `prop("x")` |
| `⚠ unknown property "Name"` | `prop()` found no property by that name or id; often a property that was renamed or deleted |
| `⚠ unknown function foo()` | no function has that name; names are case-sensitive |
| `⚠ if() takes 3 arguments` | the wrong number of arguments; the message gives the right count, or a range such as `2 to 3 arguments` |
| `⚠ expected a number, got text` | arithmetic on text, a date or a list (`got date`, `got list`) |
| `⚠ expected a list, got text` / `⚠ expected a date, got text` | a list or date function given something else, prefixed with the function: `dateBetween(): expected a date, got empty` |
| `⚠ cannot compare text with number` | `<`, `>`, `<=` or `>=` between text or a date and another type, or `sort()` on a mixed list |
| `⚠ use dateAdd() to add to a date, not + (got …)` | `+` with a date on the left |
| `⚠ division by zero` | the right-hand side of a `/` or `%` came out as 0 or empty |
| `⚠ result is not a number` | the result is not a finite number: `sqrt(-1)`, `ln(0)`, `pow(10, 400)` |
| `⚠ circular reference in formula` | formulas that reference each other, or one that references itself |
| `⚠ formula does too much work` | a limit from the table above |
| `⚠ formula is nested too deeply` | more than 100 levels of nesting |

The message travels as the cell's value, so it reads the same in every interface
language.

### Division by zero deserves its own paragraph

`/` by zero is an error, not a zero, and empty counts as zero. So
`prop("Done") / prop("Total") * 100` shows **⚠ division by zero** on every row
where Total is still empty, which is every row on the day it is created. Guard
it with a comparison that treats empty as 0:

```
prop("Total") > 0 ? prop("Done") / prop("Total") * 100 : 0
```

`prop("Total") == 0` is not enough on its own: it catches a Total of 0 but not
an empty one, because `==` does not turn empty into a number. `>` does.

For a share or a progress figure over related rows, a rollup with the
**Percent** function is simpler still. It answers `0` for a row with nothing
related to it, so a brand-new row shows an empty bar rather than an error message.

## How the value is shown

| Result | Shown as |
| --- | --- |
| number | formatted for the reader's region: `1234.5` reads `1,234.5` or `1.234,5` (see [Language and time](language-and-time.md)). Long decimals are shortened for display; the underlying value keeps its precision |
| text | as it is |
| true / false | ✓ / ✗ |
| date | a date in the reader's format, with the time when the result has one |
| list | the items, each formatted by these rules, separated by `, ` |
| empty | **—** in a table and on the row's page; nothing at all on a card |
| an error | the ⚠ message, in red |

- **Progress bar** and **Ring**: a numeric result against **Max (= 100%)**,
  clamped between 0 and 100 %, with the raw number beside it. That number is
  printed as it is, with no regional formatting, so a bar and a plain cell in the
  same table can spell the same value differently. A result that is not a number
  (text, a date, an error) is shown as text instead.
- **In a table**, the column footer shows a calculation over the column, `Σ` and
  the sum by default; click it to choose another one. A formula column offers
  every calculation, because its results may be numbers, dates, checkboxes or
  text. Cells holding an error are left out of a sum, and count as filled. See
  [the footer](views.md#the-calculation-row).
- **On a board card**, a formula is printed with its field name in front of it:
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

This is a property of the *view*, not of the column: the same formula can be
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
| The rows endpoint, `/api/collections/{id}/rows`, and `GET /api/pages/{id}` on a row | yes |
| A rollup in another collection that aggregates this formula | yes (see [Relations and rollups](relations-and-rollups.md#rollups)) |
| `get_page` over MCP on a **row** | no properties at all; that call returns the row's title and body, nothing else |
| `get_page` on the database (the Markdown table), and the Markdown export of a database | column heading yes; cells empty unless a value was left behind under that id (see below) |
| A database shared by public link | column heading yes; the cells are always blank |
| Form view, and a shared public form | not offered: a form only shows text, number, select, multiselect, date, checkbox and person |

The form case is not a gap: a formula has nothing for a person to fill in.

**"Left behind" is a real case, not a theoretical one.** The Markdown table and
the export print whatever is stored on the row under the property's id, and two
ordinary actions put something there: changing an existing column's type to
Formula keeps the old values, and a `set_properties` write to a formula id is
stored (see [below](#formulas-for-agents)). Either way those two print that old
value in the formula's column, where every live view shows the computed one. If
an exported table shows numbers that no longer match the app, that is what
happened.

## Filtering and sorting by a formula

Both work, on the computed results, in the browser, over the rows endpoint and
over `query_rows` alike.

A query that filters or sorts on a formula (or on any other computed property:
a rollup, a backrelation, a last-activity column) is finished by the server
after the values are worked out. Conditions on stored properties still narrow
the rows in the database first; the server then computes every row that is left,
applies the conditions on computed values, sorts, and only then cuts out the
page. `total` therefore counts the rows that really match, and paging is honest.
OR groups that mix stored and computed conditions are combined the same way.

The price is speed: such a query computes every row the stored conditions let
through, not just one page. On a collection of tens of thousands of rows, a
stored condition alongside the formula one keeps it quick.

In the **Filter** popover a formula offers *is*, *is not*, *greater than*,
*less than*, *between*, *contains*, *is empty* and *is not empty*. What they do
on a result:

| Operator | Matches |
| --- | --- |
| is / is not | `true` and `false` match a true/false result; anything else compares as text, exactly. A number result compares as a number, so `5` and `5.0` are equal. On a list result, *is* matches when any item does |
| greater than / less than / between | numerically when both the result and the value you type are numbers, otherwise as text, which orders ISO dates correctly: *greater than* `2026-07-01` works on a date result. *between* is inclusive and never matches an empty result |
| contains | the text of the result, ignoring case; on a list, any item |
| is empty | an empty result, empty text, an empty list, or **false**. A result of 0 is not empty |
| is not empty | everything else; an error message counts as not empty |

Sorting orders numbers as numbers and everything else as text, ignoring case;
`false` sorts before `true`, and empty results sort first in ascending order.
Rows with equal values keep their collection order.

A view's sort is stored **on the view** (`set_view` with `sort:
"propertyId:asc"`), so a view sorted by a formula is sorted the same way for
everyone, in the browser and for an agent reading it through `query_rows`.

## Formulas for agents

Create or change one with `update_schema`, which merges: send only the property
you mean and the rest of the schema is left alone.

```
update_schema(page_id: "<database>", properties: [
  { "name": "Total", "type": "formula",
    "formula": "prop(\"Price\") * prop(\"Qty\")",
    "numberDisplay": "bar", "numberMax": 1000 }
])
```

`numberDisplay` takes `plain`, `bar` or `ring` and `numberMax` sets the 100 %
mark, the same three settings the **Display** field offers in the dialog.

**The expression is checked before it is saved.** An expression that does not
parse, calls a function that does not exist or with the wrong number of
arguments, or names a property in `prop("…")` that is not in the schema (as it
will be after the call) is refused, and nothing is saved:

```
formula "Total": unknown property "Qty"
formula "Total": unknown function rnd()
formula "Total": unexpected end
```

The last one is what a formula property sent without a `formula` gets. Only the
properties in the call are checked, so an older broken formula elsewhere in the
schema does not block an unrelated change. What the check cannot see: `{id}`
references (they are never errors), a `prop()` whose name is computed rather than
written out, and everything that depends on the data, such as a division by an
empty value or a comparison with an empty date. Read a few rows with
`query_rows` after a change.

Send an existing `id` to change an expression in place. **Leaving `id` out does
not reliably add a column**: the name is slugged into an id first, and if that
id already exists the call merges into it. Sending `{name: "Total", formula: …}`
twice changes the first Total rather than creating a second one. Call
`get_collection` first: it returns every property's id, name, type and current
expression, which is what the references have to match.

Remove one with `remove_properties`, a list of property ids:

```
update_schema(page_id: "<database>", remove_properties: ["total"])
```

The row values of a removed property stay in the rows, so re-adding the property
brings them back.

`create_database` takes the whole schema in one call, formula properties
included, and checks every formula in it the same way, so a database can be
created with its computed columns already in place rather than added afterwards.

Read the results with `query_rows`. It is the only tool that returns computed
values: `get_page` on a database renders the stored table, and `get_page` on a
single row returns that row's title and body and no properties at all. A
formula's result arrives as JSON: a number, a string, `true`/`false`, a date as
`"2026-07-18"` or `"2026-07-18T09:30"`, a list as an array, empty as `null` or
missing, and an error as a string starting with `"⚠ "`. Filter and sort on
formulas with `query_rows` like on any property, and ask for a total over every
matching row with its `calculations` argument (see
[MCP tools](mcp-tools.md#query_rows)).

**Do not write to a formula property with `set_properties`.** It is not refused,
and it has no effect on what a table, a board, a row page, a filter or
`query_rows` sees: every read replaces it with the computed value. The value
*is* stored, though, and the Markdown table from `get_page` on the database and
the Markdown export both print what you wrote.

## Formula or rollup?

| The value comes from | Use |
| --- | --- |
| Other properties of the same row | a formula |
| A property across related rows (their sum, count, average, earliest date, the values themselves) | a rollup |
| The share of related rows meeting a condition | a rollup with **Percent**: no division, and 0 rather than an error when there are none |
| Something worked out from related rows | a rollup to fetch it, and a formula over the rollup: a rollup with **Show original** gives a formula a list to `join`, `filter` or `sum` |

A formula cannot reach into another row by itself: `prop("Project")` on a
relation is a list of row ids, not the rows. A rollup can, and since it is
worked out before formulas, the two combine freely. The other direction works
too: a rollup can aggregate a formula of the related rows, and test one in its
condition. See [Relations and rollups](relations-and-rollups.md).

## Importing a database that had formulas

A Notion export carries a formula column as its *results*: the values, already
worked out, as text in the CSV. The importer guesses each column's type from the
values it sees, and the types it can guess are text, number, select and date. So
a Notion formula arrives as an ordinary editable column of frozen values, which
stop moving the moment anything they were computed from changes. If you want it
live again, add a formula column and rewrite the expression by hand. Most Notion
formula functions exist here under the same names (`if`, `ifs`, `let`,
`dateBetween`, `formatDate`, `map`, `filter` and so on), so a Notion expression
often needs little more than its property references checked. See
[Import and export](import-export.md).

More on all the derived types in [Properties](properties.md#formula) and
[Relations and rollups](relations-and-rollups.md); more on views, filters and
sorting in [Views](views.md).
