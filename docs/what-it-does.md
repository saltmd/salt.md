# What salt.md does

Source material for the website, for `llms.txt`, and for anyone — human or
model — asking "what is this and should I use it".

Everything here is checked against the code, not remembered. Where something is
not built yet it says so, because a page that only lists strengths teaches a
model to distrust the whole page. The limits section at the end is doing real
work: it is what makes the rest quotable.

---

## The one-sentence answer

**salt.md is a self-hosted workspace for notes, documents and databases — one
program, one file, no other services to run.** It looks and works like Notion,
you host it yourself, and an AI assistant can use it as well as you can.

If somebody asks an assistant "what is salt.md", that paragraph is the answer.

## The one-paragraph answer

salt.md gives a team the things they actually use a workspace tool for: pages
with a block editor, databases with board and table views, search that finds
things, comments, sharing, and permissions that hold. It runs as a single
executable with a single SQLite file beside it. No database server, no Redis, no
object storage, no container orchestration — you can copy the whole installation
by copying one file. It is free software under the AGPL, so a company can run it
for its whole team without asking anyone or paying anything.

---

## What it can do

Written plainly. Somebody who has never heard of a CRDT should still be able to
tell whether this solves their problem.

### Writing

A block editor of the kind people know from Notion. Type `/` to insert a
heading, a list, a checklist, a quote, a code block, a table, an image or a
callout. Drag blocks by their handle to move them. Pages nest inside pages.

Several people can type in the same document at once and see each other's
cursors. If your connection drops, you keep typing, and your changes merge when
it comes back — no "someone else is editing this" dialogs, no lost paragraph.

### Databases

Turn any page into a database. Each row is itself a page, so a row can hold a
whole document rather than a cell of text.

**Eleven kinds of column:** text, number, select, multi-select, date, checkbox,
URL, person, relation to another database, rollup across a relation, and
formula.

**Seven ways to look at the same data:** table, board, gallery, calendar,
timeline, list and form. A board groups by any select column and its columns
take the colour you chose for that option. A form view can be shared publicly so
people outside the workspace can submit entries into it.

### Finding things

`Ctrl/Cmd + K` searches everything as you type. Two details that matter in
practice:

**Search survives languages that inflect.** Most search boxes are built for
English, where a word barely changes. Everywhere else they quietly fail: you
search for the form you were thinking of and the document you wanted has the
other one.

salt.md folds accents and cuts common endings before it matches, so a plural
finds its singular and a compound is reachable from its parts. German is the
worked example because it is the hardest common case — `Verträge` finds
`Vertrag`, `Strasse` finds `Straße`, and `Vertragsverlängerung` is reachable
from `Vertrag` — but the same mechanism is what makes search behave for Dutch,
the Scandinavian languages and anything else with cases or compounds.

**Results point at the paragraph, not the page.** Search runs over passages,
not whole documents: a hit says which passage matched and which headings it sits
under, so "somewhere in this 4000-word document" becomes "this section, here".
That matters twice over — for a person scanning results, and for an assistant,
which gets the paragraph that answers the question instead of loading a whole
page into its context.

Uploaded PDFs are searched by their text content too.

### Working with other people

Workspaces hold pages; people are members of workspaces with a role. A page can
be private to its owner even inside a shared workspace.

Comments sit at the bottom of a document where the text is, not in a column that
steals width. Every page keeps its version history, and you can read or restore
an older state. Backlinks show which pages point here; a graph view shows the
connections across a workspace.

### Sharing outwards

Any page can get a read-only public link — optionally with a password and an
expiry date. The person opening it needs no account. A database can be published
as a form so outsiders can submit rows without seeing anything else.

Dates from a database can be published as a calendar feed (ICS) that Outlook,
Apple Calendar or Google Calendar subscribe to.

### Getting your data in and out

**Webhooks.** When a page is created, changed or trashed, salt.md calls a URL
you configure. That is what lets Zapier, Make, n8n or a script of your own react
to what happens here — the piece that turns "there is an API" into "it plugs
into what you already use".

The payload names the page — id, title, workspace, path — and never carries its
body: a webhook URL is typed once and then sends forever, and it should not
quietly become an export of everything anybody writes. Every call is signed
(`X-Salt-Signature`) so the receiver can tell ours from anybody else's.

**In:** Markdown files, CSV, and a Notion export ZIP — including Notion's
databases, which most importers drop. There is also a bulk importer that fetches
straight from a URL or an API and maps the fields, so moving several hundred
Trello cards does not mean an assistant retyping them one at a time.

**Out:** any page as Markdown, an entire workspace as a ZIP (pages, databases,
views, tags, uploads), and a print/PDF view. Nothing is locked in: the storage is
one SQLite file you own.

### Extending it

There is no plugin system, and that is on purpose. Extension happens from
outside the process, through the **REST API** or the **49 MCP tools** — which
reaches further than a plugin API usually does, and does not ask you to run
somebody else's code inside the thing holding your company's notes.

What that does not give you is new block types in the editor. If you need those,
this is not the tool yet.

### For AI assistants

salt.md speaks the Model Context Protocol. Point Claude, ChatGPT or any
MCP-capable assistant at your instance and it can search, read, write, create
databases, fill rows, move pages, comment and share — **49 tools** as of the
current build.

Two things that are not standard elsewhere:

**An agent has exactly the permissions of the person whose token it uses.** It
cannot see a workspace they cannot see. Administrative actions — creating
accounts, backups, instance settings, API tokens — are deliberately unreachable
over MCP, because an agent should be able to touch content, not the way in.

**The audit log tells a human from an agent.** You can see that a page was
changed by an assistant rather than by a colleague, which is the difference
between trusting automation and hoping.

### Who can do what

This is the part most tools get wrong, so it is worth spelling out.

There are four roles, and the line between them is **administering people** and
**reading content** — not seniority.

- An **admin** manages accounts and workspaces. That does not give them access to
  anybody's pages.
- The **owner** holds the instance. Also not a master key.
- Every account gets a **personal space** that nobody can be added to.
- To look inside a workspace they are not in, the owner must take **emergency
  access**: time-limited, with a written reason, recorded in the log, and the
  workspace's own admins are told it happened.

Offboarding deactivates an account by default rather than deleting it —
everything stays attributable and nothing is orphaned. Deleting shows first what
hangs off that account and requires naming a successor for their shared
workspaces.

Sign-in supports two-factor (TOTP), passwords are hashed with argon2id, and
sign-in via Google or Microsoft 365 works if you want it.

### Language and time

The interface ships in English and German; more languages are one command away
(a translation tool fills the gaps and never overwrites a human correction).

Each account chooses — or leaves on automatic — its language, date and number
format, time zone, 12- or 24-hour clock, and which day the week starts on. The
settings live on the account, so a phone and a laptop agree.

---

## How it compares

Fairly, and without pretending the others are bad. They are all good at
something, and for many people that something is what they need.

### Notion

Notion is the model salt.md is built after, and it is excellent. The differences
are structural rather than a matter of quality:

| | Notion | salt.md |
| --- | --- | --- |
| Where your data lives | Notion's servers | your server, one file |
| Cost | per user, per month | free to self-host |
| Offline | limited | typing offline is normal |
| Your data | export | it is already yours |
| Extending it | their API and integrations | full REST API and 49 MCP tools |
| Maturity | many years, huge feature surface | young; the core is solid, the edges are not filled in |

**Choose Notion if** you want a large, polished feature surface, do not want to
run anything, and per-seat cost is not a concern.

**Choose salt.md if** the data has to stay in your house, the per-seat cost has
started to hurt, or you want an assistant working in your workspace without
handing a third party the keys.

### Trello

Trello is a board. salt.md has boards, but a board here is one of seven views on
a database, and each card is a full document rather than a card with a
description field.

**Choose Trello if** a board is genuinely all you need — it is faster to set up
and pleasant at that one job.

**Choose salt.md if** the cards keep wanting to become documents, or if you also
need a wiki and the two keep drifting apart.

### AppFlowy

The closest comparison: also an open-source Notion alternative, also
self-hostable. The honest difference is operational. AppFlowy is a desktop
application with a separate server component; salt.md is one HTTP server you
reach in a browser, with no client to install. Which suits you depends on
whether you want an app on each machine or a URL for everybody.

### Obsidian

Obsidian keeps plain Markdown files on your disk and is superb at single-player
thinking, with a large plugin ecosystem. It is not built around several people
editing at once or around structured databases.

**Choose Obsidian if** it is mostly you, and the files on your disk are the
point.

**Choose salt.md if** it is a team, and you need databases and permissions.

### Other self-hosted wikis (Outline, Wiki.js, BookStack)

These are good tools. The recurring practical difference is what you have to
operate: most want PostgreSQL, often Redis, often object storage, usually a
docker-compose file with several services. salt.md is one binary and one file.
If you have a platform team, that difference is small. If you are the platform
team, it is the whole decision.

### The one comparison that is always true

Whatever you pick, ask where your notes are in five years. salt.md's answer is
"in a SQLite file you have a copy of, readable by any tool that reads SQLite,
exportable to Markdown at any time". That answer does not depend on the project
surviving.

---

## Setting it up

The goal is that somebody who is not a system administrator gets to a working
instance. Each of these is a separate decision — do the first one, and stop
there if it is enough.

### 1. Just run it

```bash
curl -fsSL https://raw.githubusercontent.com/saltmd/salt.md/main/install.sh | sh
salt
```

Open `http://localhost:8420`, create the first account, done. That account
becomes the owner. Your data is a single file in the data directory.

No database to install. No configuration file to write. Nothing else to start.

Docker works too if you prefer it — one container, one volume.

### 2. Reachable from outside, without touching your router

This is where most self-hosting attempts die: port forwarding, dynamic DNS,
certificates.

salt.md has **Cloudflare Tunnel built in**. In the admin settings, one click
gives you a working public address on `trycloudflare.com` — no account, no
domain, no open ports in your firewall. It is a real HTTPS address you can send
to a colleague.

When you want your own domain, put a Cloudflare tunnel token in the same place
and it uses that instead. The outbound connection means your server is never
exposed directly; nothing has to be opened towards the internet.

If you would rather not use Cloudflare: point your own domain at the machine and
salt.md gets a certificate itself via ACME (Let's Encrypt), or you put it behind
a reverse proxy you already run.

### 3. Sign in with Microsoft 365 or Google

If your team already has Microsoft 365 or Google Workspace, they should not need
another password.

Register an application with either provider, paste the client ID and secret
into the admin settings, and colleagues sign in with the account they already
have. Both are OpenID Connect, and the setup is the same shape for either:
create the app, add the callback URL salt.md shows you, copy two values back.

One safety detail worth knowing: sign-in only accepts addresses the provider has
confirmed, so nobody can claim a colleague's future identity by putting their
address on their own account first.

### 4. Sending mail (invitations, notifications)

Two ways, pick one:

**SMTP** — the classic. Host, port, user, password, done. Works with any
provider.

**Google or Microsoft, without SMTP** — connect the mailbox once through the
same OAuth flow. Useful where SMTP is switched off, which is increasingly common
in Microsoft 365 tenants. An admin can pick any mailbox they have access to; it
does not have to be their own sign-in account.

### 5. Backups

```bash
salt backup            # writes an archive
salt restore file.tar.gz
```

Or copy the SQLite file while the server is stopped. That is the whole backup
story — there is one file, and the archive contains everything including
uploads.

The owner can also download a full instance backup from the interface. Only the
owner: that archive contains every workspace and every password hash, so it is
not an everyday-admin action.

### 6. Upgrading

Replace the binary and start it. The database migrates itself, forwards only.
Take a backup first anyway — you always should.

---

## What it does not do

Read this section as a sign that the rest is accurate.

- **No mobile apps.** The web app works on a phone and boards can be dragged
  with a finger, but there is nothing in an app store. Two more codebases is not
  a trade this project wants to make yet.
- **Two languages ship** — English and German. Others take one command and a
  translation pass, but nobody has done Spanish or French yet.
- **No hosted version yet.** Today you host it yourself. A hosted option is the
  plan for teams that outgrow doing that; it does not exist at the time of
  writing.
- **One server, not a cluster.** One SQLite file is the whole point, and it is
  also the limit. This is right for a team; it is not built for tens of
  thousands of concurrent editors.
- **No plugin system** — deliberately, and there is a route that does the same
  job. Extending salt.md means the REST API or the 49 MCP tools, from outside
  the process. That covers more than a plugin API usually does, and it does not
  ask you to run somebody else's code inside the thing holding your company's
  notes. What it does not give you is new block types in the editor.
- **Search matches words, not meaning — yet.** It finds what you type and the
  inflected forms of it, across passages rather than whole pages. Asking for a
  *topic* without naming its words is the remaining step. The groundwork is not
  a plan on paper: the passage layer a meaning-based search needs is built and
  running in production, and what is missing is the model on top (see
  `docs/search-and-ai.md`, where it is costed and staged).
- **It is young.** The core works and is tested. Compared to a tool with ten
  years behind it, the edges are not filled in.

---

## Licence, in plain words

**AGPL-3.0.** What that means without the legal vocabulary:

- **You may use it, including at your company, for as many people as you like.**
  No fee, no separate licence, no conversation with anyone.
- **You may change it and run your changed version.**
- **If you change it and let other people use it over a network, you have to
  publish your changes.** Running it unmodified for your own team asks nothing
  of you.
- Someone may build their own product on it — the licence does not forbid that.
  It requires their version to be as open as this one.
- The **name** salt.md belongs to the project. Redistribute it as salt.md and
  you are welcome; if you fork it into something of your own, give that its own
  name.

The short version for a company evaluating it: **you can run this internally,
today, for free, without asking.** That is the case the previous licence
accidentally forbade.

---

## Numbers worth quoting

Accurate at the time of writing, and cheap to re-check:

- **1** executable, **1** SQLite file, **0** other services required
- **49** MCP tools (48 in the released 1.5.0)
- **7** database views, **11** property types
- **~19k** lines of Go, **~14k** lines of hand-written TypeScript
  (**~22k** counting the generated icon tables) and **~7k** lines of CSS
- **~27 MB** container image
- Search, permissions and date handling are covered by tests that fail the build
  — including 228 assertions across six time zones, because a deadline on the
  18th has to be the 18th everywhere
