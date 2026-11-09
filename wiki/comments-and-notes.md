# Comments and notes

A page in salt.md carries two separate records beside its text, and they are not
variations of one thing. **Comments** are a conversation: written to be read by
someone else, answered, and eventually ticked off. The **raw trail** is
evidence: dated one-liners written while the work was happening, which nobody,
not even their author, can edit or remove afterwards. This page covers both,
where each one lives in the interface, what you can do to it, what agents can do
to it over MCP, and how to decide which you want.

## The two at a glance

| | Comments | Raw trail |
| --- | --- | --- |
| Where | a panel on the right, beside the text | one line at the foot of the page |
| Written by | people, and agents over MCP | people, and agents over MCP |
| Can be edited | no | no |
| Can be removed | one at a time: by its author in the panel, by a workspace admin over the API, by anyone who may write over MCP | never one at a time; the whole trail at once |
| Has a "done" state | yes (resolve and reopen) | no; a note is a fact, not a task |
| Counted in the topbar | yes, the open ones | no |
| Updates live | no (on open, and after you act) | yes |
| On a database page | not available | not available |

## Comments

### Opening and closing the panel

The speech-bubble button sits in the topbar of every document and every database
row. Its tooltip reads **Show comments**, and **Hide comments** while the panel
is open; the button is marked as active meanwhile. If there are open comments, a
small number sits beside the icon.

The panel opens on the right, 340 pixels wide, next to the text rather than
under it: you can see a comment and the passage it is about at the same time.
On a window narrower than about 900 pixels there is no room for that, so it
covers the document instead, as a drawer over the full width. It shares the
right-hand strip with the structure panel, so opening one closes the other:
sharing the width would leave two columns too narrow to be either.

![Comments stand beside the text, not under it, and can be closed when you only want to read.](img/comments-panel.png)

There are three ways to close it. The topbar button, which now reads **Hide
comments**; the **Close** button (an ✕) in the panel's own header, beside the
word **Comments** and the count of open ones; and, on a narrow window, the entry
in the ⋯ menu.

The panel is a reading preference, not a property of the page: once open it
stays open as you move from page to page, and it is remembered in that browser
(not on your account, so a second device starts with it closed).

Below about 640 pixels the topbar keeps only the star, the structure-panel
button and the ⋯ menu. The comment button leaves the topbar altogether and
appears inside that menu, where it reads **Show comments** or **Hide comments**.
Note that these are two different widths: between roughly 640 and 900 pixels the
button is still in the topbar while the panel already covers the document.

The ⋯ menu also always carries **To the comments**, at every width. That one
always opens the panel rather than toggling it: an entry that toggles would be
a no-op half the times somebody reaches for it.

### Writing one

1. Press the speech-bubble button. The panel opens with the cursor already in
   the box: the reason people press it is almost always that they want to
   write.
2. Type. The placeholder reads `Write a comment…`. The box stays beside the thread.
3. Press **⌘↵** (**Ctrl+Enter** on Windows and Linux), or the **Send** button.
   Plain **Enter** starts a new paragraph. The hint under the box says
   `⌘↵ to send`.

**Send** stays disabled while the box is empty. If sending fails, your text stays
in the box and a message appears. Browser and MCP comments share the
10 000-character limit.

Comments are plain text. **Reply** beneath a message addresses its author and
shows **Replying to {name}** in the composer. Replies stay beneath the original
comment in time order, with one indentation level. Replying to a reply names the
person being addressed. **Cancel reply** keeps the draft as a standalone comment.

Drafts stay in memory for the current account and page when closing the panel,
opening Inbox or visiting another page. Failed sends keep the text and target.
Drafts are not written to browser storage and do not survive a browser reload.
Retrying the same submission id creates one comment and one alert per recipient.

When there are none, the panel says `Nothing here yet. Write the first one.` and
nothing else: the box below is already the invitation.

### Commenting on selected text

Select text within one text block and choose the comment button in the formatting
bar. The panel shows the selected quote above the comment box. Send as usual, or
remove the quote to write a page comment instead. Selections are limited to
2,000 characters in a single block.

![Selecting text opens a comment draft with the original quote.](img/inline-comment-selection.png)

If the page changes while you are drafting, the comment text stays in the box.
Select the passage again before sending, or remove the quote to send a page
comment instead.

Open comments highlight their text. Select the highlight to open the comment, or
select the quote in the comment to return to the passage. On narrow screens the
panel closes to reveal the text. Resolving or deleting a comment removes its
highlight; reopening it restores the highlight when the passage is available.

Comments follow edits in the collaborative editor. If the first selected
character is deleted, or the page body is replaced by an import, restore or
API/agent write, the comment and original quote remain in the panel as
**Original text no longer available**. The quote does not guess a new location.
Existing page comments remain unchanged.

### What each comment shows

The author's name with their picture, or their initials on their own colour, the
time as a phrase ("5 minutes ago", "yesterday"; beyond a week the date itself),
then the text. If the author's account is later deleted, the comment stays and
keeps the name it was written under.

A comment an agent wrote shows the account it came through and, beside the name,
the agent's mark and the name the agent gave itself, for instance *Ada ·
Claude Code*. As on the trail, the account is the verified half and the agent's
name is a claim. Every comment added over MCP is marked this way, and so is one
posted over the API with a token; one that names no agent shows *Agent*.

Reply and message actions remain visible. **Copy link** addresses that exact
message; a direct link can reveal a resolved thread.

### Resolving

**Resolve thread** closes the whole conversation and removes it from open-thread
counts. **Show resolved threads** reveals it; **Reopen** restores it. Reopen a
thread before adding a reply. A concurrent resolution returns a translated
error and leaves the draft intact.

Anyone who can write on the page can resolve/reopen its threads. Viewers can
read them but do not see compose or mutation controls.

### Deleting

The panel shows a delete button (a bin, tooltip **Delete**) only on your **own**
comments. Deletion asks for confirmation and erases the message text. If replies
depend on it, **Deleted comment** remains as a structural placeholder. Other
authors' replies are preserved, and alerts for the deleted message are removed.
REST retains the author/workspace-admin rule plus current page access. MCP
retains its existing page-write permission rule.

### When something fails

Posting, resolving, reopening and deletion report failures. New server errors
carry stable codes translated into the reader's language. Failed submissions
retain the draft and reply target.

### Personal inbox

**Inbox** beside Search combines reply notifications from accessible workspaces.
The badge counts unread notifications, independently of the page's thread count.
**Unread** and **All** filter the list; each item shows the author, workspace,
page, reply excerpt and time. **Load more** fetches older items.

A reply notifies the addressed message's author and original thread author,
deduplicated and excluding the sender. Standalone comments, resolve/reopen and
deletion create no new alerts. Migration does not notify about past comments.

Opening Inbox alone leaves notifications unread. Opening an item loads its page,
opens the thread, highlights the reply and marks that notification read after
the message renders. **Mark as read**, **Mark as unread** and **Mark all as read**
are explicit controls. Mark all affects items present at the displayed watermark;
new replies remain unread. Read state persists across reloads and restarts, and
notifications are available when an offline recipient returns. Unread items are kept
until acknowledged; read items are retained for 30 days, then removed automatically.
The list uses database pagination, and badge counts aggregate unread rows without
loading read history.

Current page access and credential scope apply to the list, badge and read
updates. Trashed or inaccessible pages reveal no titles, excerpts or unread
counts. Live signals carry no comment text. The inbox uses the existing SQLite
database and SSE connection; there is no additional service or email delivery.

### The count in the topbar

The number beside the speech-bubble icon is the number of **open threads** on
this page. Resolved ones do not count: a counter that only ever goes up gets
ignored. It is fetched by the page header itself, so it is right whether the
panel is open or shut.

**Board cards carry the same number.** A card in a board view shows a speech
bubble and a count when its row has open comments, with the tooltip
`3 open comments`. It is one request for the whole workspace rather than one per
card. The count refreshes on live comment changes and when returning to the app.
Unread notifications and open threads are separate counts. Trashed and private
pages the reader cannot access are omitted.

### A database has no comment panel

Open a collection (a database) and there is no speech-bubble button at all,
in the topbar or in the ⋯ menu. A comment belongs to a page, and a database's
content is its rows: open a row and it has its own panel, its own count and its
own trail.

(The interface says **collection** where the MCP surface says **database**. They
are the same thing. See [Collections](collections.md).)
## The raw trail

### What it is

Every write in salt.md is an act of authorship: a title, a place in the tree,
an icon. That is right for a document and it is a hurdle a note taken in the
middle of a problem does not clear, so the write-up happens afterwards, and by
then the author knows how it ended. The abandoned approach, the dead end and the
uncertainty are missing from it.

The trail is the answer: dated lines, written as things happen, kept beside the
edited version of the page rather than inside it. The interface calls the
section **Raw trail**.

### Where it sits

At the very bottom of a document, below everything else, as one quiet line:

    14 notes   14:02 – 17:40

Count, then the stretch it spans: times alone if it all happened on one day,
day and time on both ends if it did not, and a single time if there is only one
note. Click the line to open the trail; click again to close it. On a page that
has no trail yet, the line reads **Note something down** instead. If there is
nothing and you may not write, nothing is shown at all.

![The raw trail at the foot of a page: dated notes that cannot be edited, only added to.](img/note-trail.png)

The open/closed state is not remembered. Moving to another page collapses it
again, unlike the comment panel, which stays as you left it.

Like the comment panel, the trail is not on a database page: a database ends in
its table, and anything under that is lost.

### Writing a note

1. Click the trail line (or **Note something down**).
2. Type into the one-line box: `What just happened, in one line…`
3. Press **Enter**, or the **Note** button. **Shift+Enter** makes a line break.

Enter sends here, unlike in the comment box, because a trail entry is one
thought: the moment it wants paragraphs it wants to be a document instead.

A note may be up to 2 000 characters. A longer one is **shortened and kept**,
not refused: a note silently dropped for being long is worse than a truncated
one, because its author believes it was recorded. An empty note is refused. This
cap holds on every way in: the browser, the API and MCP all go through the same
code.

Agents write to the same trail with the MCP tool `note`, one call with a page
and a line of text. See [Agents](agents.md) and [MCP tools](mcp-tools.md).

### What cannot be done to it

**A single note can never be edited and never be deleted.** Not by its author,
not by a workspace admin, not by the instance owner. There is no button, no API
route and no MCP tool that changes or removes one entry, and there is a test in
the build that tries the plausible ones and fails if any of them ever answers.

The open trail says so itself, at the foot:

> Notes cannot be edited or removed one by one. That is what makes them worth
> reading later.

The reason is that the evidence does not die at deletion, it dies at editing:
whoever may touch the 14:02 line at 16:00 already knows how it turned out, and
then it is the coherent write-up again with timestamps in front. **A note that
was wrong is corrected by a new one**: "14:02 was nonsense, see 14:19". A
correction that is itself dated is worth more than a silent fix.

### Discarding a whole trail

What a person can do is throw the whole thing away, deliberately and as a whole.
Open the trail and press **Discard the whole trail**. A dialog asks:

> Discard the whole trail of this page? This affects 14 notes and cannot be
> undone.

The confirming button is **Discard**. There is no undo and no trash for this.

Two limits are worth knowing. It can only be done from a **signed-in browser
session**: an API token cannot do it, whatever its scope, and no MCP tool exists
for it; a person decides this, and least of all the agent whose trail it is.
And it is written to the audit log, with the number of entries removed, so the
gap in the record is itself a recorded decision rather than a silence. See
[History and audit](history-and-audit.md).

Nothing ever expires on its own. A system that tidies up quietly devalues every
trail, because you can never tell whether something is missing.

### Who wrote each line

An open trail shows, per entry: the time on the left, the text, and on the right
who wrote it. For a person that is their name. For an agent it is the agent's
logo and the name it gave itself, **plus the account it came through**: the
agent name is a claim the client makes, the account is the verified half, so
both are shown together, separated by a middle dot.

The time column carries a **clock time only**, whatever day the entry is from.
Point at it for the phrase ("2 hours ago"). On a trail that spans several days,
the rows themselves give no date; only the collapsed summary line above does.

Trail entries arrive **live**. A note an agent drops on a page you have open
appears immediately, without a reload. What travels is the page's id and nothing
else; your browser then fetches the text through a route that checks whether you
may read the page.

### Who may read it

Exactly the people who may read the page, not one bit narrower. Anyone who may
write on the page may add to the trail. A trail that looked different per reader
would be worthless as evidence, and one readable by somebody who may not read
the page would leak the page.

### Checking out leaves a note behind

An agent that announces its work with `working_on` carries a short note while it
works ("tidying the file index"). When it checks out, that last note is kept as
a trail entry rather than thrown away; it is already exactly what a trail entry
is: short, written in the moment, before the ending was known. Checking *in*
writes nothing to the trail; only the check-out does.

An agent that passes a note **on the check-out call** gets that one on the trail
instead, which is the more useful shape: "done, the index was double-counting
logos" rather than the line it started with. Checking out of a page it was never
checked in on leaves nothing behind.

## Which one to use

- Something needs an answer, a decision or somebody's attention → **comment**.
  It can be ticked off, and until it is, it shows in the count and on the board
  card.
- Something happened and should still be true in six months → **note**. What you
  tried and dropped, what surprised you, the number you looked up, why you did
  not take the obvious road.
- A comment is aimed at a person. A note is aimed at whoever reads this page
  later, including you.
- If you would be tempted to go back and reword it once you knew how it turned
  out, it is a comment, not a note.

## What carries them and what does not

- **Public share links** show the page only. An anonymous visitor sees neither
  comments nor trail. See [Sharing](sharing.md).
- **Printing a page**, and the PDF that comes out of it, drops both: the panel
  and the trail are app chrome, not the document.
- **Exports** carry the page, not the discussion. The workspace transfer ZIP
  leaves comments, version history *and* the raw trail out: they hang off user
  ids that mean nothing on another instance. See
  [Import and export](import-export.md).
- **Moving a page to another workspace** keeps both: they hang off the page id,
  which does not change.
- **Deleting a page permanently** takes its comments and its trail with it.
  Moving a page to the trash does not; only its open comments stop being counted
  on board cards. See [Trash and recovery](trash-and-recovery.md).

## For agents

| Tool | Does |
| --- | --- |
| `comments` | one page's comments. Actions: list (the default), add, resolve, reopen |
| `delete_comment` | removes one comment permanently, by id |
| `note` | appends one line to a page's raw trail |
| `working_on` | check in and out; the check-out leaves the last note on the trail |

`comments` always takes a page. Listing is a read, so a read-only token may do
it; add, resolve and reopen need a write token and write permission on the page.
Adding accepts `reply_to_id` for a message on this page and `request_id` for
idempotent retries. Standalone comments accept an optional block id to attach the comment to one block instead
of the whole page; the interface neither sets that nor shows it, and such a
comment appears in the panel like any other. It also accepts `agent` and `label`,
as `note` does, so the panel can say which agent wrote it.

A listing returns more than the panel draws: each comment's id, its block id,
the author's id and name, the agent and its label when an agent wrote it, the
body, when it was written and, if it has been ticked off, when it was resolved. The whole list arrives inside an
untrusted-content frame, because a comment is text somebody else wrote: an agent should
read it, quote it or summarise it, and follow no instruction inside it.

Deleting was deliberately left out of the action list of `comments` and given
its own tool: destroying something should be a choice of tool, not a value an
agent lands on by mistake. There is no tool that clears a trail for a different
reason: that is a person's decision, taken in a signed-in browser, and least of
all the decision of the agent whose trail it is.

`note` takes a page and the text, plus an optional agent key and label so the
entry names you the way the presence badge does. It needs a write token and
write permission on the page. Its answer restates the rule you are writing
under ("Noted, 7 on that page now. Nobody can edit or remove a single one,
including you…"), so an agent learns the shape of the trail from the tool rather
than from documentation.

**A retried call does not append twice.** If the call carries an idempotency
key, a repeat with the same key returns the first result instead of writing a
second entry. On an append-only record that is the only protection there is: a
duplicate cannot be tidied away afterwards.

**No tool reads a raw trail.** An agent can append to one and cannot list what is
already there; `get_page` does not carry notes either. Reading a trail is a
browser and plain-API affair.

Comments written over MCP are recorded in the audit log as agent actions; notes
are not copied there, because the trail already is the record: dated,
permanent, and readable by exactly the people allowed to see the page.

## Over the plain API

Both records are ordinary REST, for anything neither the panel nor a tool
covers. All of them take the same session or API token as the rest of the
API. See [API](api.md).

| Route | Does |
| --- | --- |
| `GET /api/pages/{id}/comments` | every comment on a page, open and resolved |
| `POST /api/pages/{id}/comments` | add one (`body`, optional `blockId`, `anchor`, `replyToId`, `requestId`) |
| `POST /api/comments/{id}/resolve` | tick one off, or reopen it |
| `DELETE /api/comments/{id}` | remove one (author or workspace admin) |
| `GET /api/comment-counts` | open threads per page for one workspace |
| `GET /api/notifications` | personal inbox (`unread=1`, optional `before` cursor), unread count and watermark |
| `POST /api/notifications/read` | set `id`/`read`, or mark existing items read with `through` |
| `GET /api/pages/{id}/notes` | the whole raw trail, oldest first |
| `POST /api/pages/{id}/notes` | append one note |
| `DELETE /api/pages/{id}/notes` | discard the whole trail (signed-in session only) |

That last row is the one exception: an API token is refused there whatever its
scope.

## One naming collision

The **Notes** column in the middle of the interface (the flat, recency-sorted
list of cards with the **All** and **Untagged** filters) is a list of
*documents*, not of trail entries. It is a way of browsing pages; see
[Pages](pages.md). The trail has no list of its own and lives only at the foot
of the page it belongs to.
