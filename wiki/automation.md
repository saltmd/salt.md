# Automation

salt.md connects to things outside itself in several ways: it **calls** an
address of yours when a page changes, your calendar app **subscribes** to a feed
of your dates, content **comes in** from Markdown files, Notion exports, JSON
sources and public forms, content **goes out** as Markdown, HTML, a native
archive or a public link, and agents work over **MCP**. This page is the map —
each part is summarised here and most have a page of their own.

What salt.md does not have is a rule engine or a scheduler. Nothing inside it
says "when Status becomes Done, send an email". The pieces below are the wires;
the logic lives at the other end — in a script, in Zapier, Make or n8n, or in an
agent working over MCP (see [Agents](agents.md)).

## Webhooks — salt.md calls you

A webhook is an address salt.md posts to when a page is created, changed or
thrown away. It is the only thing that calls **you** when content changes;
without it, every integration has to ask over and over whether anything is new.

Set up in the user menu under **Instance settings → Webhooks**. This is an
instance-wide setting and **only an administrator sees it** — a hook belongs to
the server, not to a workspace or a person. It also needs a browser: the webhook
endpoints refuse an API token, an administrator's own included, with
`session_required`.

| Event | The checkbox says | Fires when |
| --- | --- | --- |
| `page.created` | a page is created | a page, row or collection is created in the browser, or an agent calls `create_page` |
| `page.updated` | a page is changed | any save of the page — title, content, icon, cover, tags, description, visibility, template flag or properties — and also a move to another parent or a reorder in the sidebar |
| `page.trashed` | a page is thrown away | a page goes to the trash, **or is deleted permanently** — both send this same event — once **per page in the subtree**, so a receiver watching one page hears about it even when a parent was thrown away |

### What does not fire

Only a handful of places in the server send an event at all. Everything else
writes to the database in silence:

- importing a Markdown file, a Notion `.zip` or a native workspace archive
- an `import_url` job, however many records it writes
- an agent calling `create_rows`
- duplicating a page, or creating a workspace from the [library](library.md)
- restoring a page from the trash, or restoring an older revision
- a row created by a visitor filling a [public form](forms.md)

Over MCP the surface is narrower still: only `create_page` and `write_content`
in its `replace` mode send anything. An agent renaming a page, setting an icon,
writing properties or changing a schema fires nothing — while the same edits
made in the browser fire `page.updated`. Appending or prepending content is
silent too.

Bulk work coming **in** therefore never announces itself page by page, which is
deliberate: a two-thousand-page import would otherwise turn into two thousand
outbound calls. There is one exception in the other direction — trashing a tree
fires once per page in it, so throwing away a large section does produce a burst
of calls. Treat webhooks as a signal about everyday edits, not as a change log
you can reconcile against.

### Adding a hook

1. Enter the address under **Address to call**. It has to start with `https://`
   or `http://`, name a host, and be no longer than 500 characters; anything
   else is refused as you add it.
2. Tick at least one box under **When should we call?**
3. Press **Add**.

salt.md then shows the signing secret once, under the line *"Copy this secret
now — it is shown only once."* There is no way to see it again; if you lose it,
**Remove** the hook and add it back.

The same goes for changing a hook: there is no edit and no on/off switch in the
interface, only **Remove**. A new address or a different set of events means a
new hook — and a new secret, so every receiver has to be re-keyed. Adding and
removing a hook are both written to the audit log, which is where an
administrator finds out who pointed the instance at an address (see
[History and audit](history-and-audit.md)).

### What a delivery looks like

- **The message names a page and never carries it.** You get the id, the title,
  the workspace and a path — never the content. A receiver that is allowed to
  read the page fetches it with its own credential, through the normal
  permission checks. After a **permanent** deletion the title and the workspace
  come through empty: the page is already gone when the message is built.
- **Every delivery is signed**, as `X-Salt-Signature: sha256=…` over the raw
  body. Verify it: without that check, anybody who learns the URL can forge a
  message. The request also carries `Content-Type: application/json` and a
  `User-Agent` of `salt.md/<version>`.
- **A failed delivery never fails your save.** A page that saved correctly is
  not reported as an error because somebody's endpoint is down. The result of
  the last attempt is shown beside the hook instead — `HTTP 200`, `failed: …`
  (the reason, cut off at 120 characters), or **not called yet**.
- **One attempt, ten seconds, no redirects.** There is no retry queue, and an
  endpoint that answers with a redirect is treated as a failure.
- **Typing produces repeated calls.** The editor saves a copy of the page about
  one and a half seconds after you stop typing, and every one of those saves is
  a `page.updated`. A session of writing sends many. Debounce on your side if
  you act on the event.

Hooks are not filtered by workspace: every active hook hears about every page on
the instance. Filter on your side, using the workspace id in the message.

**[Webhooks](webhooks.md)** has the exact payload, the signature check in code,
and what a receiver has to do.

## The calendar feed

Every date property, on every row, in every collection you can read, as an
iCalendar feed your calendar app subscribes to. Open it from the user menu:
**Subscribe to calendar**. Every account has this — it is not an admin feature.

One event is written per date value: the summary is the row's title with the
property's name in parentheses — "Kickoff (Due)" — and the description is the
name of the collection it came from. A row with two date properties therefore
produces two events. A plain date becomes an all-day event; a value that carries
a time becomes a timed one, written without a time zone, so it shows at that
clock time wherever the calendar is read. Events have a start and no end.

The dialog offers a **scope** under *What should the calendar contain?*:

| Scope | What lands in the feed |
| --- | --- |
| **Everything I can see** | every date property in every workspace you can see — your memberships, plus any workspace you currently hold emergency access to |
| A workspace | the same, narrowed to one workspace |
| A collection | one collection's dates |

Only collections that actually have a date property are listed — otherwise the
dialog would hand out a permanently empty feed. If the list is empty you will
see *"A collection appears here once it has a date property."*

Below the scope, the field **Subscription link (webcal):** shows the feed
itself, read-only; clicking it selects the whole address so you can copy it by
hand. The buttons are **Open in calendar**, which hands a `webcal://` link to
Apple Calendar, Google Calendar or Outlook; **Copy URL**, which copies the same
feed as an ordinary web address for a calendar that wants one; and **Reset the
link**, which confirms with *"New calendar link created (the old one no longer
works)"*.

```
https://salt.example.com/ics/<token>.ics
https://salt.example.com/ics/<token>.ics?workspace=<id>
https://salt.example.com/ics/<token>.ics?collection=<id>
```

Five things worth knowing before you paste that link anywhere:

- **The token is the credential.** No login, like a share link. Anybody holding
  the URL sees what you see. Do not share it.
- **There is one token per person**, behind every scope. Narrowing the feed is a
  view on what you may read — never a way to see more.
- **Reset the link invalidates every feed at once**, because they all sit behind
  that one token. The button says so on hover: *Invalidates all calendar links*.
  Afterwards you re-subscribe in each calendar app.
- **Permissions are checked on every fetch, not at subscription time.** A
  collection that is moved, made private or trashed simply stops producing
  events. A scope you can no longer read yields an **empty calendar rather than
  an error**, so a stale subscription does not sit there flashing red in
  somebody's calendar app.
- **The feed is read-only, and how often it is refreshed is your calendar app's
  decision** — salt.md sets no refresh interval and no expiry on it. Editing an
  event in your calendar changes nothing in salt.md.

## Content coming in

| Source | Where |
| --- | --- |
| A Markdown file or a Notion/Markdown `.zip` | page ⋯ menu → **Import (.md / .zip)**, or the **Import (.md / .zip)** button on the "No pages yet" screen of an empty workspace |
| A native workspace archive from another instance | workspace settings → **Import workspace…** |
| A JSON API, in bulk | the `import_url` tool, for agents |
| A form filled in by somebody with no account | a public [form](forms.md) link |

**Where an import lands.** Both buttons behave the same way: the new pages go to
the **top level of your default workspace**, not under the page you were
standing on and not necessarily into the workspace you are currently looking at.
Move the result afterwards if it belongs somewhere else. The ⋯ entry only
appears on a page you may edit.

A `.zip` import rebuilds a tree: folders become parent pages, `.md` files become
pages, Notion's 32-character id suffixes are stripped from the titles, and a
Notion database CSV becomes a real collection — columns turned into typed
properties, rows into rows. Whatever real text a paired row file holds becomes
that row's body; Notion repeats the title and the properties at the top of every
row file, and those lines are dropped, so most rows arrive with an empty body
and their values in the property panel instead. Notion also wraps large exports
in nested `Part-N.zip` files: those are unpacked automatically, up to five
levels deep, which is the single most common reason an export that "imported
nothing" in fact imports fine here.

**A native archive creates a new workspace**, named after the one in the
archive, rather than merging into an existing one. Any signed-in account can
import one — it is not an admin action — unless creating workspaces has been
switched off for non-admins on this instance.

`import_url` exists because writing several hundred records through
`create_rows` exhausts an agent's context long before the import finishes. The
agent names the source and the mapping, salt.md fetches and writes the records
itself, and none of the content passes through the agent. It answers with a job
id at once; the agent polls `get_import_status` until it says done.

A **public form** is the only way content arrives from somebody with no account:
a visitor fills the fields you published and a real row appears in the
collection, recorded in the audit log as coming from a form. See
[Forms](forms.md).

A Markdown link pointing at a page of this instance becomes a **real page link**
on import, so imported pages show up in backlinks and in the graph instead of
arriving as islands.

Two more ways ready-made content appears in bulk, both silent as far as webhooks
are concerned: the [blueprint library](library.md), which builds a whole
prepared workspace, and [templates](templates.md).

**[Import and export](import-export.md)** has the size limits, what the type
guesser does to each column, and what each format keeps and loses.

## Content going out

| Format | Where | What it is |
| --- | --- | --- |
| Markdown | page ⋯ menu → **Markdown (.md)**, or **Export Markdown** in the sidebar menu | one page, readable anywhere |
| Web page | page ⋯ menu → **Web page (.html)** | one page as standalone HTML |
| Print / PDF | page ⋯ menu → **Print / as PDF** | a print view that saves as a PDF, on the phone too |
| Markdown, whole workspace | workspace settings → **Export as Markdown** | a `.zip` mirroring the page tree, *"Readable anywhere, without the databases"* |
| Native archive | workspace settings → **Export workspace** | *"Native archive — importable one to one"* |
| A public link | page ⋯ menu → **Share to web (read-only link)** | one page, readable by anyone holding the address, optionally with a password and an expiry — see [Sharing](sharing.md) |
| The whole instance | Instance settings → Maintenance → **Download backup (.tar.gz)** | database and uploads, see [Self-hosting](self-hosting.md) |

Three details that decide which of these you want:

- **Exporting a single collection gives you a Markdown table** — a Title column
  plus one column per property, with select options written out as their names
  and checkboxes as a tick. The rows' page bodies are not in it, so a collection
  whose rows carry text loses that text in this format. Exporting a whole
  workspace as Markdown is the other way round: there each row becomes its own
  file under a folder named after the collection, with its body, and the
  properties are left behind. That is what *"without the databases"* means.
- **Only the native archive carries the structure.** Schemas, views, row
  properties, tags, icons, covers, descriptions, positions, template flags,
  private/workspace visibility, the workspace rules and the uploaded files all
  travel with it. It deliberately leaves out members and roles, comments,
  version history and share links — those are tied to one instance and cannot
  follow a workspace to another.
- **HTML and the print view are for documents.** A collection has no HTML
  export: the **Web page (.html)** entry is still in its ⋯ menu, and on a
  collection it quietly gives you the Markdown table under a `.md` name instead.
  **Print / as PDF** on a collection prints the page as it stands on screen,
  while on a document it opens a clean standalone print view in a new tab, which
  also works on a phone where the browser's own print does nothing.

A bulk export contains only what you are allowed to read: private subtrees are
left out of the archive rather than included and hoped over. That holds for the
Markdown `.zip` and the native archive alike.

For the instance backup, the Maintenance panel also names the unattended
version: run `./salt backup` from cron, and restore with
`./salt restore backup.tar.gz`.

## The API, if none of the above fits

Almost everything the interface does, it does over `/api`, with the same bearer
token an agent carries. The exceptions are the account- and instance-level
actions — two-factor, API tokens, invitations, instance settings, the backup
download, account preferences, editing a user, and the webhooks described above.
Those require a browser session and answer a token with `session_required`.

Two endpoints belong on this page in particular:

- `/api/events` is the live change feed — server-sent events for a signed-in
  client that wants to hear about changes without polling. It is the in-browser
  counterpart of a webhook, and it is what keeps a second tab up to date (see
  [Collaboration](collaboration.md)).
- `/api/skill` hands an agent a description of this instance, generated by the
  instance itself, for it to keep in the repository it works in. See
  [Skill](skill.md).

See **[The REST API](api.md)** for the routes and
[Agent access](agent-access.md) for what a token may reach.
