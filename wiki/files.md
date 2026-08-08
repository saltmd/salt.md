# Files

Anything you upload — a PDF, an image, a spreadsheet, a workspace logo — is
stored as a file on disk beside the database, referenced from the page it was
added to, and listed in an index you can browse and search. This page covers
how a file gets in, where it ends up, who can read it, what happens to the text
inside a PDF, and what removing something does and does not do.

## Where a file lives

Uploads go into the `files` folder of the server's data directory, next to the
SQLite database. Each one is stored under a random id plus its extension —
`4f2c…a1.pdf`, never `Offer Acme.pdf`.

The extension is cleaned first: lower case, letters and digits only. An upload
through the browser also drops the extension entirely when it is longer than
twelve characters counting the dot, so a twelve-letter extension is thirteen
characters and does not survive. An upload from an agent keeps whatever it is
given, cleaned of everything but letters, digits and the dot.

The name a person recognises is kept separately — in the block on the page and
in the file index — which is why a file can be downloaded under its real name
while the byte on disk stays anonymous.

Files are served from `/files/<stored name>` and that path **requires a signed-in
account or an API token**. Three things follow from how it is served:

- There is no directory listing. Asking for `/files/` returns "not found",
  so the random names cannot be enumerated.
- Every file is delivered with a sandbox policy and `X-Content-Type-Options:
  nosniff`. An uploaded HTML or SVG file can never run a script on the
  application's origin.
- An anonymous visitor to a [publicly shared page](sharing.md) is not signed
  in, so images uploaded into that page do not load for them. Shared pages are
  text and structure; the uploads behind them stay behind the sign-in.

## Adding a file to a page

### In the editor

Drag a file from your desktop onto an open document. A drop that lands on the
text is placed exactly where the pointer is. A drop anywhere else on the page —
the wide margins, the empty space below the last block, the title — is caught by
Salt, shows a **Drop to add to this page** overlay, and appends the file at the
end of the document.

Dropping several files at once works; they upload one after another, and one
failure does not stop the rest — you get the ones that worked plus a message
saying why the other one failed. The message names the reason, not the file, so
two oversized files in one drop produce two identical notices. More than one
file added at once is confirmed with "2 files added".

To put a file at a specific place without dragging, type `/` where you want it.
The **Media** group of the slash menu carries four entries — **Image**,
**Video**, **Audio** and **File** — and each inserts an empty block with its own
panel. The panel has two tabs:

- **Upload** opens a file picker. What you choose is stored by Salt exactly as
  a dropped file is: same size cap, same index, same PDF text extraction.
- **Embed** takes an address instead (**Enter URL**, then **Embed image** /
  **Embed file**). Nothing is uploaded. The block points at somebody else's
  server, the file appears in no file list, no backup contains it, no PDF text
  is indexed from it, and it never opens in the built-in viewer. If the far side
  goes away, so does the content.

What a dropped file becomes depends on its type: an image, video or audio block
for those, and a file block for everything else, which renders as a download row
showing the name. While an upload runs, a thin progress bar sits at the top of
the window.

Uploading into a page requires write access to that page.

### From an agent

Over MCP, `upload_file(file_name, data_base64, page_id)` uploads a file from
base64 data. With a `page_id` the file is attached to that page as a block —
an image block for `.png`, `.jpg`, `.jpeg`, `.gif` and `.webp`, a file block for
everything else — and a PDF's text is extracted for search. Without a `page_id`
the file is stored and given back as a URL, but it belongs to no page and
appears in no file list.

See [Agents](agents.md) for connecting one.

### From a script

`POST /api/upload` takes an ordinary multipart form with one field named
`file`. Add `?page=<page id>` to attach it — without that query parameter the
upload is stored but belongs to no page, exactly as over MCP. The answer is
JSON: `{"url":"/files/<stored name>"}`, which is the address to put in a block
or a property. The request needs a session cookie or an API token like every
other call; see [API](api.md).

### Size limits

| | |
| --- | --- |
| Default cap per file | 50 MB |
| Range an admin can set | 1–2048 MB, under **Max. file size per upload (MB)** in [Administration](administration.md) |
| What the browser refuses on its own | anything over 50 MB, before sending — *"File too large (…) — 50 MB max."* |
| What an agent may send in one MCP request | the file cap plus a third (base64 costs a third) plus 1 MB |

The cap is enforced on the whole request body with one megabyte of slack for
the multipart wrapping around the file, so a file a few hundred kilobytes over
the stated number is accepted rather than refused. The slack is for the
envelope, not a second limit to plan around.

The browser's own 50 MB check does not follow the admin setting. Raising the
limit above 50 MB takes effect for agents and for direct calls to
`/api/upload`, but the editor will still refuse a larger file before it sends
one. Lowering the limit works everywhere: the server answers `413` and the
editor shows the server's own message, *"file too large — max 10 MB"*, naming
the limit this instance is set to.

If you run the instance yourself and put a reverse proxy in front of it, its own
body limit has to be at least as large — the Administration dialog prints a
matching `client_max_body_size` line for exactly that reason.

## Finding files again

### The structure panel

Open the structure panel beside a document ([Pages](pages.md)). Its **Files**
section lists every file on this page **and every page under it**, with a
coloured extension badge, the size, and — when the file hangs off a sub-page
rather than the page you are looking at — the name of the page it came from.
Empty, it says "No files".

Clicking a PDF opens it in a viewer. Anything else opens in a new tab.

### The file list

Two ways in, both opening the same dialog:

1. **Workspace settings → Data → Files** ("Every uploaded file in this
   workspace, with the page carrying it"). Workspace settings are open to
   workspace admins.
2. The **⋯** menu on any page in the sidebar → **Files in this subtree**. The
   dialog is then headed **Files below “…”** and covers that page and its
   descendants only. Right-clicking the page row opens the same menu, which
   saves hunting for a three-dot button that appears on hover.

The dialog shows, newest first: the file name as a link, its size, the date it
was added, and a button carrying the title of the page it belongs to — pressing
it opens that page. A file whose page is gone shows "no page" instead. With
nothing to show it says "No files here yet."

Above the list sit a filter box (**Filter by file or page…**, which matches both
the file name and the page title) and a type dropdown (**All types**, then every
extension actually present — `.pdf`, `.png`, and so on, with the leading dot,
because that is how they are stored). The dropdown is built from everything in
scope, not from what the filter box currently leaves visible, so the choices do
not shift under you as you type. Under them is a running count and the total
size of what is currently shown, so filtering to `.pdf` answers "how much of
this is PDFs" without any arithmetic.

The dialog always covers the workspace you are currently in and offers no
workspace picker — including when you open it from Workspace settings. To see
another workspace's files, switch to it first.

### Over MCP and the API

`list(kind: "files")` returns the files of a workspace — name, extension, size,
the page carrying each one and its `/files/` URL. Pass `under` with a page id
for one subtree. Over HTTP the same answer comes from `/api/files`, with
`?workspace=` or `?under=`.

Both go through **two permission checks**: first the workspaces you can reach,
then the individual page carrying each file. The second is what keeps a file
attached to somebody's private page out of your list even though you are in the
same workspace. Files on pages in the trash drop out of the list until the page
is restored.

## Previewing and downloading

Clicking the name of a file block in a document normally sends it to your
downloads folder. PDFs open in a viewer instead — a bar with the file's name, a
**Download** button and **Close**, and the document itself filling the rest.
Escape closes it, as does clicking outside.

The viewer is the browser's own PDF renderer, which means it costs no
conversion on the server and nothing leaves the machine. It applies only to
PDFs, and only to files stored in this instance: a file block made with the
**Embed** tab points at a foreign address and keeps opening the normal way.

Office formats — `.docx`, `.xlsx`, `.pptx` — deliberately have no preview. No
browser reads them natively, and the two ways to change that are shipping an
office suite inside the server or handing your documents to somebody else's
online viewer. Neither is worth it.

**Download** keeps the readable file name rather than the random id it is stored
under.

## PDF text and search

When a PDF is uploaded **with a page**, Salt extracts its text and indexes it
under that page. Searching for a phrase that appears only inside the PDF finds
the page carrying it. See [Search](search.md). Agents reach the same text: the
`search` tool covers titles, page content and indexed PDF attachments in one
query.

Two limits keep one large document from taking the server down, and both are
sized to the memory the server believes it has:

| Memory available | Largest PDF whose text is extracted | Extractions at the same time |
| --- | --- | --- |
| not detectable | 10 MB | 1 |
| 512 MB | 5 MB | 1 |
| 2 GB | 20 MB | 1 |
| 8 GB | 50 MB | 2 |
| 16 GB and above | 50 MB | 3 |

The extraction limit is one hundredth of available memory, never below 5 MB and
never above 50 MB. Parsing a PDF costs several times the file's own size in
memory, and the server has to keep answering everything else meanwhile.
Extractions beyond the slot count wait their turn instead of running together.

**Getting this wrong never costs you an upload.** A PDF over the limit is still
stored, still listed, still downloadable, and still shows in the file list —
only its text stays out of the search index. The server log says so plainly:
`pdf extract …: skipped for indexing, N bytes is over the M byte limit (the file
itself is stored and listed as usual)`.

At most 500,000 bytes of text are kept per PDF; a whole book is not worth the
database weight.

The startup log prints what the instance decided:
`memory: 16000 MB available, soft limit 12800 MB, PDF indexing up to 50 MB,
3 extraction(s) at a time`. If that figure is wrong — which happens in nested
setups, a container inside a virtual machine — set `SALT_MEMORY_MB` to the real
number of megabytes. See [Self-hosting](self-hosting.md).

Extraction runs for PDFs only. No other format's contents are read.

## Covers, icons, logos and pictures

These are uploads too, and they behave slightly differently:

| What | Where it is set | Counted in the file list |
| --- | --- | --- |
| Page cover (**Cover** → **Upload image**) | on the page | no |
| Page icon (icon picker → **Upload** → **Upload picture**) | on the page | yes |
| Workspace logo (**Workspace picture** → **Choose a picture…**) | on the workspace | no |
| Account picture (**Profile** → **Upload picture**) | on the account | no |

Only the icon upload names a page, so only it appears in that workspace's file
list. The others belong to a workspace or an account rather than to a page, so
the index treats them as unattached and no workspace list shows them. They are
still stored, still backed up, and still protected from clean-up: the routine
that removes unreferenced uploads checks page content, page properties, covers,
old page versions, comments, workspace logos and account pictures before
deleting anything.

Each of them can be taken off again, and each has its own control:
**Remove icon** in the page's icon picker, **Remove picture** in the **Profile**
dialog next to **Upload picture**, and **Remove** in the workspace picture
dialog. Removing clears the reference; the stored file itself stays, like every
other upload.

A workspace has one identity at a time: setting an emoji clears the logo and
uploading a logo clears the emoji. **Remove** clears both.

## Removing files

No button deletes a single stored file. Only deleting a whole workspace or a
whole account reaches the bytes on disk. What the per-file actions actually do:

- **Deleting the file block from a page** removes the reference, not the byte.
  The file stays on disk, stays downloadable to anyone signed in who has the
  link, and stays in the file list until the index is next rebuilt. This is
  deliberate: a block removed by accident, or by a collaborative edit landing
  badly, would otherwise destroy the only copy.
- **Moving the page to the trash** takes its files out of every list. Restoring
  the page brings them back. See [Trash and recovery](trash-and-recovery.md).
- **Deleting the page permanently** removes the extracted PDF text from the
  search index. The file itself stays, and shows in the workspace's file list
  with "no page" — until the index is next rebuilt, at which point it is
  recorded with no page and no workspace and drops out of every workspace list.
  It is still on disk and still in every backup.
- **Deleting a workspace or an account** is where uploads are removed from disk
  — and only those that nothing anywhere references any more.

**Workspace settings → Data → Delete workspace** ("Takes every page in it
along") is the common one. It is open to workspace admins, asks you to type the
workspace name as confirmation, and is refused if it would leave you with no
workspace at all. It deletes the pages and then removes from disk every upload
nothing else points at.

**Deleting an account** does the same for the personal spaces that go with it.
Shared workspaces are handed over or left behind instead; a workspace left with
no members can be deleted from the instance owner's clean-up view, again after
typing its name, and that path clears its files too. See
[Account](account.md) and [Administration](administration.md).

"Nothing else points at it" is the full check listed above — content,
properties, covers, old versions, comments, logos and account pictures. The same
upload can live on in a copy or a moved page, and a missing image in somebody
else's workspace would be worse than a leftover on disk.

Unattached files are not lost track of. When the index is rebuilt, everything in
the files folder that no page mentions is recorded without a page, and the
startup log counts them:
`file index: built (version 2, 128 files on 40 pages, 3 unreferenced)`.

## What the index is, and why it can be thrown away

The file list is a **derived** index. The truth is the block on the page plus
the byte on disk; the index only makes "every document for this customer" a
question the server can answer in one query instead of a walk through every
page's content.

That is why it can be rebuilt whenever the rules for reading it change — the
rebuild deletes the whole index and reads it back out of the pages and the files
folder. Files are found by looking for **a URL pointing into `/files/`**, not by
block type: the editor writes image, video, audio and file blocks, an agent's
upload writes its own, and the list of types keeps growing, while a `/files/`
URL is the one signal that stays stable.

If a count ever looks wrong, that is the repair. It costs one thing: the upload
dates are recorded nowhere else, so a rebuild stamps every file with the moment
of the rebuild, and the list's "newest first" order collapses into a fallback
sort by stored name until new uploads arrive. Nothing else in the index is
authoritative.

## Backups and exports

- The instance backup (**Maintenance → Download backup (.tar.gz)** in
  [Administration](administration.md), or `/api/admin/backup`) contains the
  database and **every file**, including the unattached ones. The same section
  shows the total size under **Uploads**. Only the instance owner may take it —
  an admin asking for it is refused, because the archive holds every workspace,
  every upload, password hashes and session tokens.
- The native workspace archive (**Workspace settings → Data → Export
  workspace**, or `/api/workspaces/{id}/export`) carries every upload the
  workspace refers to — from page content, from properties, from covers and the
  workspace logo — inside the ZIP under `files/`. Importing it on another
  instance restores them.
- The Markdown export (`/api/export`) writes text. It links to files but does
  not carry them.

See [Workspaces](workspaces.md) for import and export of a whole workspace, and
[Self-hosting](self-hosting.md) for backing up the machine.

## Limits, in one place

| | |
| --- | --- |
| Per-file upload cap | 50 MB by default, 1–2048 MB configurable |
| Slack on top of the cap, for the multipart envelope | 1 MB |
| Editor's own refusal | 50 MB, not configurable |
| PDF text extracted | up to 50 MB of file, 500,000 bytes of text |
| Parallel PDF extractions | 1 to 3, by memory |
| Extension length kept (browser upload) | 12 characters including the dot |
| Depth of a "files in this subtree" walk | 20 levels |
| Formats with a built-in preview | PDF |
| Formats whose text is indexed | PDF |
