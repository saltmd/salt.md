# Editor blocks

The body of a document is a stack of **blocks**. A paragraph is a block, so is a
heading, a list item, a table, an image, a callout, a whole two-column layout.
You reach every one of them the same three ways: the slash menu, a Markdown
shortcut, or a keyboard shortcut. This page is the complete catalogue — what
each block is, how to get it, what it does when you click it, and what happens
to it when the page leaves salt.md as Markdown.

Everything here applies to **documents** and to **collection rows** (a row is a
page with a body like any other). A collection page itself shows its table
instead of a body — see [Collections](collections.md).

## Before the blocks: how the editor behaves

**It saves itself.** There is no save button. Your keystrokes go to the other
people on the page immediately over the realtime connection, and a copy of the
document is written to the server after **1.5 seconds** of quiet — that copy is
what search, the exports, the backlink index and the API read. If a write fails
you get the notice *Page content not saved* and it retries on your next change
and again when you leave the page. See [Collaboration](collaboration.md).

**It is read-only when you may not write.** A viewer sees the document rendered
with no cursor, no slash menu, no drag handles, rather than an editable-looking
page whose writes the server would reject. Dropping a file onto such a page does
nothing and says nothing — the drop zone, the dashed outline and the bar at the
bottom are all switched off. See [Permissions](permissions.md).

**An agent writing into the page replaces what you see.** When something writes
through the API or MCP with `write_content` — in any of its three modes — the
live document is discarded and your editor reloads the stored content. Unsaved
keystrokes from the last moment before that are lost. `append` is the default
mode because it is the one that cannot destroy what is already on the page, not
because it spares open editors. See [Agents](agents.md).

**The editor's own menus are English**, always, including on an instance running
in another language. The four blocks salt.md adds itself are translated; the
menus that come with the editor — the slash menu's built-in entries, the block
menu, the formatting toolbar — are not.

## Getting a block

### The slash menu

Type `/` and a menu opens, filtered as you keep typing against each entry's
title and its hidden aliases. **Enter** inserts, **Escape** closes, the arrow
keys move. If nothing matches you get *No items found*.

Where the block lands depends on what you typed into: on an **empty block**, or
one holding nothing but the `/`, the new block **replaces** it. In a block with
other text, the new block is inserted **after** it.

| Entry | Group | Shortcut shown |
| --- | --- | --- |
| **Heading 1**, **Heading 2**, **Heading 3** | Headings | `Mod-Alt-1` … `3` |
| **Quote** | Basic blocks | — |
| **Toggle List** | Basic blocks | `Mod-Shift-6` |
| **Numbered List** | Basic blocks | `Mod-Shift-7` |
| **Bullet List** | Basic blocks | `Mod-Shift-8` |
| **Check List** | Basic blocks | `Mod-Shift-9` |
| **Paragraph** | Basic blocks | `Mod-Alt-0` |
| **Code Block** | Basic blocks | `Mod-Alt-c` |
| **Divider** | Basic blocks | — |
| **Columns** | Basic blocks | — |
| **Callout** | Basic blocks | — |
| **Embed a collection** | Basic blocks | — |
| **Table of contents** | Basic blocks | — |
| **Table** | Advanced | — |
| **Diagram** | Advanced | — |
| **Image**, **Video**, **Audio**, **File** | Media | — |
| **Bookmark / Embed** | Media | — |
| **Toggle Heading 1** … **3** | Subheadings | — |
| **Heading 4**, **Heading 5**, **Heading 6** | Subheadings | `Mod-Alt-4` … `6` |
| **Emoji** | Others | — |

`Mod` is ⌘ on a Mac and Ctrl elsewhere. Every shortcut in that column works
whether or not the menu is open — **except `Mod-Alt-c`**, which is printed
beside **Code Block** but is bound to no key at all. One shortcut goes the other
way and is advertised nowhere: `Mod-Alt-q` turns the current block into a quote.

The six salt.md blocks carry a one-line description in the menu:

- **Columns** — *Two blocks side by side*
- **Diagram** — *A flow chart written as text*
- **Callout** — *A highlighted note with an emoji*
- **Bookmark / Embed** — *A link card, or a YouTube/Vimeo player*
- **Embed a collection** — *Show an existing collection inside the document*
- **Table of contents** — *Auto-generated list of every heading*

**Columns** is a block that holds nothing itself. Inserting it gives you two
empty blocks side by side; the small **2 / 3** control at its right edge, which
appears on hover, switches between two and three. A column is one block, and a
block can be indented into it, so a column can hold more than one thing.

It replaced a paid add-on. Two things it does not do, which that one did:
dragging a block sideways to open a new column, and pulling the edges to change
their widths. Both are worth having; neither was worth a component whose licence
would have to be renegotiated the day any part of salt.md is closed.

**A drawing** arrives as a file. Upload a `.excalidraw` — by hand or with
`upload_file` from an agent — and salt.md draws it on the page instead of
offering a download, with the file itself still reachable underneath. It is a
reader, not an editor: the drawing is made in Excalidraw and looked at here.

Its picture is kept on the block, like a diagram's and for the same reason, so
it appears in a PDF. Before anybody has opened the page there is no picture yet,
and the export names the file rather than printing nothing. The library that
draws it is about 1.4 MB and is fetched only for a drawing that has no picture
yet — every later reader gets the picture and loads nothing.

**Diagram** is written, not drawn. The block holds
[Mermaid](https://mermaid.js.org) source — `A --> B` rather than coordinates for
every box — and draws it. Click the picture to get back to the text; leave the
box and it is redrawn. Escape discards.

Written rather than drawn is the point: an agent can produce one. Placing boxes
by pixel is exactly what an agent does badly, and a diagram as text also lives
in the page itself, so it is searchable, sits in the version history, and an
agent changes it with an ordinary write.

The drawing is kept on the block beside its source. That is what puts it in a
PDF: the print view is built by the server, which cannot draw. A diagram that an
agent wrote and nobody has opened yet has no drawing — the export prints its
source instead, which is worth more than a gap. Open the page once and the
picture is there from then on.

Their aliases are deliberately bilingual, so `datenbank`, `tabelle`, `hinweis`,
`inhalt`, `spalten`, `diagramm`, `flussdiagramm` and `warnung` find them as well as
the English words.

### Markdown shortcuts

Type these at the start of an empty block; the shortcut fires on the space that
follows — except `---`, which fires on the third hyphen with no space involved.

| You type | You get |
| --- | --- |
| `# ` … `###### ` | Heading 1 to 6 |
| `- `, `* `, `+ ` | Bullet list item |
| `1. ` | Numbered list item — any number, and the list starts there |
| `[] ` | Check list item, unchecked |
| `[x] ` | Check list item, checked |
| `> ` | Quote |
| ` ``` ` | Code block; text after the backticks sets the language |
| `---` | Divider |

Inline, while you write anywhere in a block:

| You type | You get | Also |
| --- | --- | --- |
| `**bold**` or `__bold__` | **bold** | `Mod+B` |
| `*italic*` or `_italic_` | *italic* | `Mod+I` |
| `~~strike~~` | struck through | `Mod+Shift+S` |
| `` `code` `` | inline code | `Mod+E` |
| — | underline | `Mod+U` |

Underscores are only read as emphasis when the opening one starts the line or
follows a space, so `my_var_name` stays literal.

Typing `:` followed by at least two letters opens the **emoji picker**; typing
`@` or `[[` opens the page-link menu (below).

### The block handles

Hover a block and two controls appear in the left margin: **Add block** (`+`),
which opens the slash menu — on the current block if it is empty, otherwise on a
fresh paragraph below it — and **Open block menu** (the grip), which is also the
drag handle. The menu holds **Delete** and **Colors** (text and background, nine
named colours plus *Auto*).

Drag the grip to move a block. Dropping it against the **left or right edge** of
another block puts the two side by side as columns; dropping it on a column's
edge adds a column beside that one. Drag the gap between two columns to change
their widths.

### Editing shortcuts

| Keys | What it does |
| --- | --- |
| `Tab` | Nest the current block under the one above it |
| `Shift+Tab` | Unnest it |
| `Shift+Mod+↑` / `↓` | Move the block up or down past its neighbour |
| `Mod+Z` | Undo |
| `Mod+Shift+Z`, `Mod+Y` | Redo |

**Undo is yours alone.** In a document two people are editing, undo takes back
*your* last change, not whatever happened most recently — a co-author's
paragraph cannot be undone out from under them.

`Tab` has two exceptions: inside a table it moves to the next cell, and inside a
code block it inserts two spaces. Pressing **Enter** in an empty list item turns
it back into a paragraph, which is how you leave a list.

### The formatting toolbar

Select text and a toolbar appears over it: the block type, **Bold**,
*Italic*, **Underline**, **Strike**, text alignment (left, centre, right),
**Colors**, **Nest block** / **Unnest block**, and **Create link** (`Mod+K`).

Clicking an existing link opens a second small bar with **Edit**, **Open in new
tab** and **Remove link**. **Edit** is not only the address: it opens a small
form with two fields, the link's text (*Edit title*) and its destination
(*Edit URL*).

## The blocks

### Paragraph

The default. An empty one shows *Enter text or type '/' for commands*.

### Headings

Six levels. Levels 1 to 3 sit in the slash menu under **Headings**; 4 to 6 under
**Subheadings**. Every heading is picked up by the table of contents block and
by the outline of the exported HTML.

**Toggle Heading 1–3** is a heading that can be collapsed, with the blocks under
it as its content. It is an ordinary heading carrying one extra setting, not a
block type of its own — which is why it exports as a plain heading (below).

### Lists

**Bullet**, **Numbered**, **Check** and **Toggle** list items. All four nest with
Tab. A check list item carries a real checkbox that anyone with write access can
tick. A numbered list started with `7. ` begins at seven.

A **Toggle List** item hides its children until you open it; an empty one shows
*Empty toggle. Click to add a block.*

### Quote

An indented, emphasised paragraph. `> ` or `Mod-Alt-q`.

### Code block

Monospaced, with syntax highlighting and a language chosen from the block's own
picker (or by the word after the opening backticks). Four keys behave
differently inside it:

- **Tab** inserts two spaces instead of nesting the block.
- **Shift+Enter** leaves the block and starts a paragraph underneath.
- **Enter** at the end inserts a line break — until two blank lines have piled
  up, at which point the next Enter removes them and puts the cursor in a
  paragraph below. That is the other way out.
- **Delete** in an empty code block removes the block.

Pasting inside a code block always pastes plain text. The Markdown detection
described under [Paste](#paste) is skipped there on purpose, so a pasted snippet
of Markdown stays a snippet of Markdown.

### Divider

A horizontal rule. Type `---`.

### Table

A table of text with editable cells, **three columns and two rows** to start. A
cell holds inline content — text, bold, italic, links — not blocks.

Moving around: **Tab** goes to the next cell, **Shift+Tab** to the previous one,
**Enter** to the cell below. **Backspace** at the start of a cell does nothing,
so a table cannot be deleted from the inside by a stray keystroke. Drag the
border between two columns to change their widths.

Hover a row and a handle appears at its left edge; hover a column and one
appears above it. Drag a handle to move that row or column. Click it for a menu,
and the menu follows the handle you clicked:

| Handle | Menu |
| --- | --- |
| Row | **Delete row**, **Add row above**, **Add row below** |
| Column | **Delete column**, **Add column left**, **Add column right** |

A `+` sits under the last row and beside the last column. Click it to add one;
press and drag it to add or remove several at once.

Three things this table does not do, so you do not go looking: cells cannot be
merged or split, a cell has no background colour of its own, and there is no
header row to mark. The Markdown export writes a separator line after the first
row anyway, so the top row reads as the heading wherever the file lands.

The slash menu does open inside a cell, but a cell has no room for blocks — what
you pick is inserted **after** the table.

This is a table of text, not a database. If the rows need types, filters, a
board or a calendar, use a [collection](collections.md) instead — and if you
want it inside this document, embed it (below).

### Columns

**Two Columns** and **Three Columns** insert a layout of empty columns. Blocks
are dragged in and out; column widths are dragged. Columns are a layout, not
content — a Markdown export flattens them back into one sequence.

### Image, Video, Audio, File

Four blocks around one uploaded byte. An empty one shows **Add image** /
**Add video** / **Add audio** / **Add file** and opens a panel with two tabs:

- **Upload** — pick a file from this machine.
- **Embed** — paste an address that is already on the web (*Enter URL*), then
  **Embed image** / **Embed video** / **Embed audio** / **Embed file**.

Once filled, the toolbar over the block offers **Edit caption**, **Replace
file**, **Rename file**, **Download file**, **Delete file** and **Toggle
preview** — and those four middle labels name the block's own type, so an image
block says **Replace image**, **Rename image** and so on. An image or video
shown as a preview is resized by dragging the handle at either edge.

**A block filled from the Embed tab is a link, not a file of yours.** It points
at somebody else's server, so it is not in the workspace's file list, its text
is never extracted for [search](search.md), and it is not previewed even when it
is a PDF. Only the **Upload** tab, a drag from the desktop or a paste puts a
byte on this instance.

**A PDF file block opens in a viewer instead of downloading.** Click the file
name and the document opens full screen with its name, a **Download** button and
**Close**; Escape closes it too. This only happens for PDFs that were uploaded
to this instance — a file block pointing at somebody else's server keeps opening
the ordinary way, because a foreign address is not something salt.md will frame.
Office formats are not previewed: no browser reads them without help, and the
help costs either the single-binary install or the promise that a self-hosted
instance keeps its documents to itself.

### Callout

A boxed note with an emoji at the left. Click the emoji to cycle it — the
tooltip says *Change symbol* — through 💡 ⚠️ ❗ ✅ 📌 🔥 ℹ️ and back to the
start. The text beside it is ordinary inline content.

### Table of contents

A generated list of every heading in the document, indented by level, headed
**Contents**. Clicking an entry scrolls to that heading. It recomputes as you
type, so it is never stale, and a heading with no text is listed as *Untitled*.
With no headings yet it says *No headings.*

It reaches nested headings too: a heading inside a toggle, or inside a column,
is listed like any other.

Because it is generated on the spot, it is the one block that exports as
nothing at all.

### Bookmark / Embed

Insert it and you get a field: *Paste a link (https://…) and press Enter*. An
address without a scheme gets `https://` put in front of it.

What you get back depends on the address:

| Address | Result |
| --- | --- |
| `youtube.com/watch?v=…`, `m.youtube.com`, `youtu.be/…` | an embedded player, loaded from the no-cookie domain |
| `vimeo.com/<number>` | an embedded player |
| anything else | a link card: 🔖, the full address, and the host underneath |

Anything that is not `http`, `https` or `mailto` is refused as a destination and
the card leads nowhere — a link planted through the API or through the realtime
connection cannot smuggle a script into the page that way.

### Embed a collection

Puts an existing [collection](collections.md) inside the document, with text
above and below it. Insert the block and a picker appears: *Search collections…*
lists up to eight matching collections you can see; *No collection found* when
none match. Pick one and the collection renders in place, with its title as a
button — clicking it, or **Open as page ↗**, goes to the collection's own page.

**The block stores only a reference.** The collection stays one object in one
place, so the same collection can appear in several documents and an edit shows
up in all of them at once. If it is later deleted, or lives in a workspace you
cannot read, the block says *This collection is no longer available.* rather
than failing.

The [views](views.md) shown are the collection's own views. This is the same
thing an agent gets with `embed_database`.

## Page links

Two triggers, one menu:

- **`@`** — mention a page.
- **`[[`** — the same menu, for wiki-link habits. The trigger is really the
  first `[`; a second one, and closing brackets, are ignored when matching.

The menu lists up to **twelve** pages whose titles contain what you typed, each
marked *Page* or *Database*, excluding the page you are on and anything in the
trash. Once you have typed something it also offers `Create "…"`, which makes a
page with that title and links it in one motion — **at the top level of your
default workspace**, not under the page you are writing in.

What you get is a **page link chip**: 🔗 and the page's title, carrying the
target's id. Clicking it navigates. This is not the same as typing a URL: the
backlink list, the [library](library.md) graph and `get_links` read page links
and nothing else, so a hand-typed address leaves the page an island.

Pages that link *to* the page you are reading are listed as **Linked from · N**
under the body, or in the structure panel when that is open. See
[Pages](pages.md#page-links-and-backlinks).

Bare database rows are not offered in the menu. A row with sub-pages of its own
is.

## Files: three ways in

### Drag from the desktop

Drag one or more files anywhere onto the page — the text, the wide margins, the
empty stretch under the last block, the title, the cover. The whole scrolling
area lights up with a dashed outline and a bar appears at the bottom of the
screen: **Drop to add to this page**.

Where the file lands depends on where you let go. Dropped **on the text**, it is
inserted at that point, above or below the block you were pointing at. Dropped
**anywhere else**, it is appended after the last block, because a drop out there
names no position.

Several files at once are uploaded **one after another**, not in parallel: the
server sizes its text extraction to the memory it has, and a folder dragged in
all at once is the shape that has taken an instance down. A file that fails does
not stop the rest — you get the reason that one failed, and *N files added* once
more than one has gone up.

Dropping a file anywhere else in the application does nothing at all, on
purpose. The browser's own default for a dropped file is to navigate to it,
which would throw the open workspace away.

### Paste

A file on the clipboard — a screenshot, an image copied from another
application — is uploaded and inserted at the cursor.

Pasting text is not so plain either. If what you paste **looks like Markdown**,
it is converted into real blocks: headings, lists, quotes, tables, code fences
and inline styles all arrive as themselves rather than as literal characters.
Pasting a URL **over selected text** turns that text into a link. Inside a code
block none of this happens — see [Code block](#code-block).

Copying goes the same way round. Blocks copied out of the editor land on the
clipboard as **Markdown** in the plain-text flavour, so a block pasted into a
chat window or a text editor arrives as Markdown rather than as a wall of
run-together words. A second, private flavour travels alongside it, which is
what makes a copy from one salt.md page into another lossless. Copying from
inside a code block puts the raw code on the clipboard instead.

### The block's own Upload tab

Insert an **Image**, **Video**, **Audio** or **File** block and use its
**Upload** tab.

### What happens to an upload

A thin progress bar runs across the top of the screen while it goes up.

The limit is **50 MB per file** and the editor refuses anything larger before
sending it: *File too large (…) — 50 MB max.* An administrator can raise the
server's own cap in the instance settings, but that does not lift this one —
uploads from a page stay capped at 50 MB either way. Lowering the server's cap
below 50 MB does change what you see: the file goes up and comes back refused,
as *The file is too large for this instance.*

Every file uploaded from a page is filed **against that page**: it appears in
the workspace's file list and in the structure panel, and if it is a PDF its
text is extracted and becomes findable in [search](search.md). See
[Files](files.md).

## What survives an export to Markdown

**⋯ → Markdown (.md)** downloads the page you are on. The file starts with
`# <icon> <title>` and then the body, block by block. Sub-pages are not
included; for a whole tree see [Import and export](import-export.md).

| Block | In the Markdown |
| --- | --- |
| Paragraph, headings 1–6 | as themselves |
| Bullet / numbered / check list | `-`, `1.`, `- [ ]` / `- [x]`, children indented four spaces |
| Toggle list | a plain list item; the children follow |
| Toggle heading | an ordinary heading of its level; the children follow, indented |
| Quote | `> ` |
| Code block | a fenced block carrying its language |
| Divider | `---` |
| Table | a Markdown table with a separator row after the first line |
| Image | `![name](url)` |
| Video, audio, file | `[name](url)` |
| Callout | `> 💡 text` — a quote led by its emoji |
| Bookmark | a link to the address |
| Embedded collection | a link to the collection page, labelled `Datenbank` |
| Table of contents | nothing |
| Columns | the contents, flattened into one sequence |
| Page link | `[label](/p/<id>)` |
| **bold**, *italic*, ~~strike~~, `code` | the Markdown for each |
| Underline | `<u>…</u>` — Markdown has none, HTML travels |

**⋯ → Web page (.html)** is the same document as standalone HTML, which keeps
more of the shape: a toggle list becomes a real `<details>`, columns stay
side by side, a callout stays a box. **⋯ → Print / as PDF** opens that same HTML
in a new tab, laid out for printing — which is also how you make a PDF on a
phone, where the browser's print command does nothing.

**On a collection page the same three entries mean something else.** There is no
body to export, so **Markdown (.md)** gives you a Markdown table of every row,
one column per property, titles first. **Web page (.html)** gives you that same
Markdown table — the HTML form is not offered for a collection, and the download
arrives as a `.md` file. **Print / as PDF** hands the job to the browser's own
print command rather than opening a print view, so it does nothing on a phone.

### And back again

**⋯ → Import (.md / .zip)** reads a Markdown file back in as a **new top-level
page**. The importer understands headings, bullet, numbered and check lists
(nested by indentation), quotes, fenced code with a language, images, tables,
paragraphs, and the inline styles bold, italic, strike, code and links.

A Markdown link pointing at a page of **this** instance — `/p/<id>`, or the full
address that sharing hands out — comes back as a real page link rather than a
plain one. That is what closes the round trip: export a page, import it
elsewhere, and its internal links are still links in the graph.

Four things do not survive a full round trip, and it is better to know than to
discover:

- **Headings 4, 5 and 6 come back as heading 3.** The importer clamps them.
- **A divider comes back as a paragraph containing `---`.**
- **Callouts, bookmarks and embedded collections come back as what they
  exported as** — a quote and two links.
- **The title heading stays in the body.** The first heading of an imported file
  becomes the page's title *and* remains the first block. Delete it if you do
  not want it twice.

A `.zip` is imported as a whole tree — folders become parent pages, `.md` files
become pages named after the file, and a Notion export's database CSVs become
real collections. That path is [Import and export](import-export.md).

Agents write into a page with the same Markdown converter, through
`write_content` — see [MCP tools](mcp-tools.md).

## Limits worth knowing

| | |
| --- | --- |
| Upload from the editor, per file | 50 MB, fixed |
| Upload accepted by the server | 50 MB by default, 1–2048 MB, set by the administrator |
| Page-link menu | 12 matches |
| Collection picker in the embed block | 8 matches |
| Heading levels | 1–6 in the editor, 1–3 after a Markdown import |
| Markdown file, when importing a zip | 2 MB per file |
| Content save | 1.5 seconds after you stop typing |
| Emoji picker | opens after 2 letters |

Comments on a block, and the append-only note trail under the body, are their
own thing: see [Comments and notes](comments-and-notes.md).
