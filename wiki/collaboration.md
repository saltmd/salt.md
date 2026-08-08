# Working at the same time

Two people can open the same document and type in it at once. Nothing locks,
nothing asks who wins, and there is no save button. This page describes what
that looks like on screen, who you see and where, what happens when the
connection drops or the page is replaced under you, and what an agent's badge in
the topbar means. It also says where the live part stops — it does not cover
everything a page has, and the places it does not cover are the ones people trip
over.

## Live editing

### What is shared, and how it merges

The **body of a document** is shared in the literal sense: the text you type
appears on the other person's screen as you type it, letter by letter. Two people
writing in the same paragraph both keep their text. There is no "someone else is
editing this" dialog, no lock, and no conflict to resolve afterwards.

You see the other person's cursor inside the text: a coloured caret. Their name
sits on it while they are typing, fades a couple of seconds after they stop, and
comes back when you point at the caret. The colour is theirs, from their account
— open the user menu, choose **Profile**, and pick one of the ten swatches under
**Colour**. The same colour carries their circle in the topbar.

Two separate things happen while you type:

- Every change goes to everyone else on that page immediately, over a live
  connection that stays open as long as the page is.
- About one and a half seconds after you stop typing, a finished copy of the
  text is written back to the page itself. That copy is what
  [search](search.md), export, backlinks, the Markdown API and agents read. A
  paragraph you wrote a moment ago is findable a second or two later, not on the
  next reload.

If that second write fails, a message reads **Page content not saved** and the
text is written again on your next change, or when you leave the page. The live
connection and that copy are independent: the other person has your sentence
either way.

### What is not shared live

Not everything on a page travels with the text.

| Part of the page | Reaches the others |
| --- | --- |
| Document body | immediately, character by character |
| Title, icon, cover, tags, description | saved half a second after you stop; the others' sidebar updates, an open page's own title field does not |
| Properties of a database row | in every open view of that collection, within half a second; on the row's own open page not until you reload it |
| Comments | not live — see [Comments and notes](comments-and-notes.md) |
| Raw trail (notes) | live; a note added by anyone appears under the page |
| Rows of a collection | the open view reloads itself, see below |

The title is the one worth knowing about. If a colleague renames the page you
both have open, the name changes in your sidebar, and in the crumbs for the pages
*above* this one — but the crumb for the page itself and the big title at the top
keep the text you loaded until you leave and come back. Two people renaming a
page at the same second is last-write-wins, not a merge.

### Collections are not shared documents

A collection — a database, in the [MCP](mcp-tools.md) vocabulary — has no shared
text. Its rows are ordinary records, and the open view refreshes itself instead:
when anybody, a person in another browser or an agent, changes a row of **that**
collection, every open view of it reloads about half a second later. A card
pushed into another column moves while you are looking at the board, and so does
a property somebody edits in a table cell.

Only that collection reloads, not every view you have open. An agent writing
fifty rows sends fifty signals and causes one reload, not fifty.

The one place that does not follow is the property panel on a **row's own page**.
It shows the values that were fetched with the page and listens for nothing, so a
status changed by somebody else while you have the row open appears on the board
and not in front of you. Reload the page to see it.

Because a collection has no shared document, it also has no cursors and no
presence circles (below). A **row** of a collection, opened as its own page, is a
document like any other and has all of it.

## The presence row

At the top right of a page, left of the icon buttons, sit small round faces: the
other people who have this page open right now.

- Each is that person's picture, or their initials — the first letter of their
  first two words — on their profile colour.
- At most three are drawn, overlapping slightly. A fourth person turns the row
  into three faces and a **+1**, a fifth into **+2**, and so on.
- Hovering the row gives **Also here: Ada Lovelace, Grace Hopper** — every name,
  including the ones behind the counter.

Four things about it are easy to be surprised by:

**A hidden tab is not "here".** Presence is withdrawn as soon as this tab stops
being the visible one — you switched to another tab, or minimised the window —
and comes back when you return to it. The document connection itself stays up the
whole time: you are still receiving their edits, you are just not announcing
yourself.

**Reading counts.** Somebody who cannot write the page (a viewer, or an
[emergency access](permissions.md)) is in the presence row like anybody else, and
follows the text as it changes. Presence says "is looking at this", not "is
typing".

**A closed tab disappears at once.** Closing the tab or quitting the browser
sends a goodbye, and the server passes it straight on. A sleeping laptop or a
connection that vanishes without one takes longer: the server notices through its
own heartbeat after roughly forty seconds, and the other browsers drop a face
that has gone silent for thirty seconds by themselves. So half a minute of ghost
is possible — but only for a machine that went away without saying so.

**Your own second window shows up as a peer.** Two visible windows of the same
page are two connections, and each sees the other. That is your own name in the
row, not a duplicate account.

The presence row is empty on a collection page, always: the faces come from the
shared document, which a collection does not have.

## Losing the connection, and coming back

The connection to a page is a WebSocket (`/collab/{id}`). It can be interrupted
by a laptop lid, a phone going to sleep, a Wi-Fi change, or a reverse proxy that
kills idle connections. All of that is handled without you doing anything, and it
is worth knowing what to expect.

**You can go on typing.** The document lives in your browser. While the
connection is down, your edits go into your copy and nothing is lost as long as
the tab stays open. When the connection returns, everything you wrote is pushed
across in one go and merged with whatever the others wrote meanwhile. Neither
side wins; both texts survive.

**Reconnecting is automatic and backs off.** The first retry comes within a
second, and each following wait is about twice the one before it, up to a ceiling
of thirty seconds. Every delay carries a random part, so twenty open tabs do not
all come back in the same instant and knock the server over again. A clean
reconnection resets the count, so the next hiccup is fast again.

**A dead connection is noticed, not waited out.** The server sends a heartbeat
every twenty seconds. If nothing at all arrives for about a minute the browser
treats the connection as dead, drops it and reconnects — it does not sit there
looking connected. A connection that hangs while being established is given
fifteen seconds and then retried. This exists because of one specific failure:
proxies silently discard idle connections, the browser is never told, and the
symptom is "I only see his edits after I reload".

**A connection that cannot keep up is cut deliberately.** If edits arrive faster
than a browser can take them — a slow line, a machine under load — the server
drops that one connection rather than letting it hold up the room, and the
browser reconnects through the same backoff. On a bad line this shows as the
presence row flickering: the faces go and come back a second later. Nothing is
lost; the document is pushed again on reconnect.

**What you actually see** is the presence row emptying. Nothing announces the
outage in words. If the faces vanish and you know your colleagues are still
sitting there, it is your connection that went, not theirs; they reappear within
seconds of it coming back.

**The one way to lose work** is to close the tab while it is disconnected. The
edits made in that window exist only in that window until the connection returns.
If you have been typing and are not sure the page is live, leave the tab open
until the faces come back.

## When the page is replaced under you

Some changes do not merge into the live document — they replace it. When that
happens, everyone's editor is closed and the page loads itself again. Where there
is still a page to load, the new content simply appears: it takes a moment and
there is no dialog. Where there is not — the page was permanently deleted — you
get an error line and a **Back to workspace** button instead.

This happens when:

- a version is restored from the page history (see
  [History and audit](history-and-audit.md));
- an agent or the API replaces the whole body of the page — `write_content` over
  MCP, a Markdown [import](import-export.md), or a `PATCH /api/pages/{id}`
  carrying new content;
- the page is moved to the trash or deleted, or a page above it is
  ([Trash and recovery](trash-and-recovery.md));
- the whole [workspace](workspaces.md) is deleted, which closes every open editor
  in it for the same reason.

An edit typed in the same second as a replacement can be lost — the stored
document is discarded, and what was in flight has nowhere to land. In practice
this matters when a person and an agent work on the same page at the same time,
which is exactly what the agent badge below is for.

Note that `update_page` is **not** in that list, despite the name. Over MCP it
changes a page's metadata and where it sits — title, icon, cover, description,
tags, visibility, parent, workspace, favourite — and never touches the body, so
it never closes anybody's editor.

Deactivating an account is a different mechanism and belongs beside this rather
than in it. It closes that account's open editors on the spot, so a window that
was left open stops writing. That window does not reload: it starts trying to
reconnect, and goes on trying until the next thing it asks the server for is
refused because the sign-in is gone.

## Agents: who is working on what

An agent connected over [MCP](mcp-tools.md) can announce that it is working on a
page, and people see it live. The tool is `working_on`, and it is the counterpart
to a colleague's face in the presence row.

### What an agent does

```
working_on(page_id: "…", agent: "claude", label: "Claude Code",
           note: "tidying the file index")
```

and, when it finishes:

```
working_on(page_id: "…", agent: "claude", done: true)
```

`agent` is one of `claude`, `chatgpt`, `codex`, `cursor`, `openclaw`, `hermes`,
`gemini` or `generic`. An unknown name is not refused — it becomes `generic`, and
whatever it called itself survives in `label`.

`expected_minutes` is optional and does one thing: it makes a long silence look
expected rather than suspicious. It appears at the end of the badge's tooltip as
**checked in for about 45 min**, and nowhere else.

Checking in again on the same page **keeps the original start time** and replaces
only the note and the expected minutes. An agent that updates what it is doing
every few steps therefore still reads "here for 2 h 14 min" rather than resetting
to zero each time.

Checking out of a page nothing was checked in on is not an error. The answer is
*Nothing to check out of — you were not marked as working on that page.*, so an
agent can call `done: true` defensively at the end of a run without having to
remember whether it ever checked in.

Checking in needs permission to read the page and a credential that may write —
a read-only API token cannot announce itself. The reference for agents is in
[MCP tools](mcp-tools.md) and in the downloadable [skill](skill.md), whose first
standing rule is to check in before starting.

### What you see

On the page, at the top right beside the human faces, a rounded badge: the
agent's own logo, its name, and — when it is the only agent there and the window
is wider than about 1200 pixels — its note. The outline and text carry a colour
per agent, so two of them are still told apart at eleven pixels.

The badge has two states, and the difference is the whole point:

| State | Means |
| --- | --- |
| Solid, gently pulsing | it has called in within the last ten minutes |
| Faded | it has not been heard from since then |

Faded does not mean gone. It means "has not called in a while", which for a
three-hour job is normal. The tooltip says how long it has been here and, once it
has gone quiet, when it last called in — while it is still fresh that second half
reads **active just now**. So nobody has to judge from the shade:

> Claude Code · via Ada Lovelace · tidying the file index · here for 2 h 14 min ·
> last seen 47 min ago

The second name is the account whose credential the agent is using, and it is
**the verified half of the line**. Nothing in a credential says
which agent is calling; the name and the logo are what the agent claims about
itself, the account is what the server knows. They are always shown together for
that reason.

The same mark appears as a small round dot wherever a page is listed rather than
opened: in the corner of a board card (in front of the people assigned to it), in
a table row, and in the sidebar tree. It breathes slowly while fresh, and holds
still for anyone who has asked their system for reduced motion. **In those places
only the first agent is drawn.** If two agents are checked in on one row, the list
shows one dot and nothing says there is a second — open the page to see both.

Everyone who may read the page sees the badge, and only them: the list is fetched
per reader and checked page by page (`/api/presence`), so an agent working in a
private page is not announced to the rest of the workspace.

There is **no screen that lists every agent working across the instance**. You see
a badge on a page, a card, a row or a sidebar entry you are already looking at,
and that is the whole surface. If you want to know whether anything is running
somewhere, you have to go and look.

### Why nothing expires

An agent has no clock and cannot wake itself up to say "still here". A
ten-minute lease would therefore erase a three-hour job halfway through. So:

- a check-in **stays** until the agent checks out;
- any other call the same account makes naming that page counts as a sign of
  life, so an agent working inside salt.md stays fresh without spending a call on
  saying so — including calls the server then refuses, because an agent whose
  write bounced is still alive;
- what has been silent for **twelve hours** is treated as a crashed session and
  removed.

The cost of that design is worth stating plainly: **a person cannot clear a
badge.** There is no button in the interface and no route behind one. An entry
goes away when the agent itself calls `working_on` with `done: true` from the same
account, when the twelve-hour sweep reaches it, or when the page it hangs on is
moved to the trash or deleted. A badge from a crashed run will sit there, fading,
until one of those happens.

Checking out has one side effect worth knowing: a note is kept as an entry in the
page's raw trail — the one passed on the check-out call if there is one,
otherwise the note the agent was carrying while it worked. With neither, nothing
is written and the answer does not mention it. When something was kept, the answer
says so: *Your last note stays on the page as a trail entry.*

Both the check-in and the check-out are recorded in the activity log (user menu →
**Activity log**; see [History and audit](history-and-audit.md)). They read
**started working on:** — with the note quoted after it — and **finished working
on:**, which quotes the agent's own name instead, because at check-out that is
what is recorded.

### The trail is not only for agents

Agents can write to a page's raw trail directly with `note` while they work, and
that is where the check-out note lands. **People write in it too.** At the foot of
a page, under the comments, a line reads **Note something down** on a page that
has no trail yet, or **14 notes · 14:02 – 17:40** on one that has; opening it
gives a box and a **Note** button. A person may also discard a page's whole trail
with **Discard the whole trail** — all of it at once, never a single entry, and
that decision is itself logged. Full details in
[Comments and notes](comments-and-notes.md).

## The honest limits

Things people expect and do not get:

- **No authorship in the text.** The document does not colour or attribute
  passages by who wrote them. Who changed what is answerable per saved version in
  the page history, not per sentence.
- **No follow mode, no typing indicator.** You see where a cursor is, not what
  somebody is about to do.
- **Comments do not arrive live.** The panel loads when you open it and after you
  act in it. A comment written while you are looking at the panel appears the next
  time it reloads.
- **The title of the page you have open does not update**, and neither does its
  own breadcrumb. The sidebar and the crumbs above it do.
- **No live editing on a collection**, only automatic reloading of its rows, and
  no presence circles there.
- **A page shared to the web is a snapshot.** The public link serves plain HTML
  with no live connection, so an anonymous reader sees the page as of its last
  saved copy and has to reload for more ([Sharing](sharing.md)).
- **A viewer follows along and cannot type.** The editor renders read-only.
- **Emergency access is read-only, and the editor does not show it.** Someone
  reading a workspace under an emergency grant gets an editable-looking page whose
  changes the server accepts from nobody — they exist in that browser and are gone
  on reload. The only sign is the **Page content not saved** message a second or
  two after typing; the live connection says nothing at all. Treat an emergency
  look-in as a look, which is what it is meant to be
  ([Permissions](permissions.md)).
- **A person and an agent on the same page can collide.** Live editing merges
  people's keystrokes; an agent rewriting the whole body replaces the document
  instead. The badge exists so this is visible before it happens — if an agent is
  announced on the page you are about to rewrite, wait for it to check out.

If edits seem to arrive only after a reload, or the presence row is empty when it
should not be, [Troubleshooting](troubleshooting.md) covers what to check on the
network path; a proxy that closes idle connections is the usual cause.
