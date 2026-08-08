# Concepts

salt.md is built out of about fifteen nouns, and most questions about how it
behaves turn out to be questions about which noun owns which. This page defines
every one of them: what it is, where it lives, what it can contain, and what
happens to it when the thing above it goes away. Each section points at the page
that covers the subject in depth, so this one can stay a dictionary.

Two facts explain more than any other. **Everything you can open is a page** — a
document, a database, a row in a database. And **a page belongs to exactly one
workspace**, which is where access is decided.

## The nesting, in one picture

```
Instance                          one name, one owner
└── Workspace  "Northwind"        membership lives here
    ├── Page  "Field handbook"    a document
    │   ├── Page "Safety checks"  a sub-page
    │   └── Collection "Depots"   a database, filed under the document
    │       ├── Row "Depot North" a page with property values
    │       │   └── Page "Uplink" a sub-page of a row
    │       └── Row "Depot South"
    └── Page  "Suppliers"
```

Everything from the workspace down is a page. What makes one a document, a
database or a row is a single setting plus where it is filed — which is why a
row can carry its own text, its own sub-pages and its own files, exactly like
the handbook above it.

## Instance

One running copy of salt.md — one binary, one database file, one address. The
whole of the tree above lives inside it.

Internally the instance is recorded as an **organisation**, which exists so that
"who runs this" is a stored answer rather than an assumption. You never see the
word. What you see is the **instance name**, set by an admin under
**Instance name (sign-in page & title)**. It appears on the sign-in page, and in
the browser tab while that page is showing. Once you are signed in the tab keeps
the built-in title, so a reader who never sees the sign-in screen never sees the
instance name there.

Every account holds one of three instance roles. The two elevated ones are
separate from any workspace role — being an instance admin gives you no page to
read anywhere.

| Role | What it means |
| --- | --- |
| **owner** | Runs the instance. Instance configuration, the account lifecycle including password resets, and emergency access. Exactly one account holds it. |
| **admin** | Manages people, not content. Creates accounts, invites, sees the account list. Shown as the badge **Instance admin**. |
| **member** | An ordinary account. No instance-level rights at all; everything it can do it can do because a workspace lets it. |

An instance admin deliberately **cannot** set somebody else's password, add
themselves to somebody else's workspace, or export a workspace they are not in.
Without those three prohibitions the boundary would be decoration: whoever can
set a password can sign in and read everything.

The owner role moves by handover, not by promotion. The current owner hands it
to an account that is **already** an instance admin and is not deactivated;
afterwards the old owner is an ordinary admin. There is never a second owner.

One instance setting decides how much of this page applies to an ordinary
account: **Users may create their own workspaces**. With it on — the default —
anyone can create a workspace and becomes its admin. With it off, only instance
admins can, and **New workspace** is simply absent from the workspace menu for
everybody else.

See [Administration](administration.md) and [Permissions](permissions.md).

## Account

One person, one email address, one password. An account carries a name, a
colour, an optional picture, its instance role, and its own language and time
settings — those live on the account rather than in the browser, so a phone and
a laptop agree. See [Your account](account.md) and
[Language and time](language-and-time.md).

An account comes into being in one of two ways. An instance admin uses
**Create a new user**, which makes the account straight away with an initial
password and sends no email. Or a workspace admin **invites** somebody: in
**Workspace members**, an email address, a role, and the **Invite** button. If
the address is left blank you get a link to pass on yourself; either way the
invitation is **valid for 14 days**, and accepting it creates the account and
drops it into that workspace with that role.

An account can be **deactivated** instead of deleted: sign-in closes, its
sessions end, its API tokens and its calendar subscription link are revoked,
any editor it still has open is dropped, and everything they wrote stays
attributed to them. That is the normal case when somebody leaves. The badge in
the account list reads **deactivated**. Worth knowing before you flip it: an
agent running on that person's token stops working the same second.

## Workspace

The unit of access, and the only one. Membership is per workspace — there is no
per-page sharing between colleagues. Everything in a workspace is readable by
its members, except pages marked private.

A workspace carries: a name, an emoji or an uploaded logo, its members, its
**rules**, a setting for **What agents may do here**, a setting for
**How the sidebar is arranged**, and its own tag colours. Everything but the tag
colours sits behind **Workspace settings** in the workspace menu — the entry
appears there only for a workspace admin. A tag is recoloured by clicking its
chip on any page.

Three roles inside a workspace:

| Role | Read | Write | Members, settings, rules |
| --- | --- | --- | --- |
| **Admin** | yes | yes | yes |
| **Member** | yes | yes | no |
| **Viewer** | yes | no | no |

A workspace can never be left without an active admin: the last one cannot be
demoted and cannot be removed. If you try to leave and you own private pages
there, the server tells you how many before it lets you go — they stay in the
workspace and become visible to its admins only.

Two kinds of workspace are marked in the switcher:

- **own space** — a personal workspace, created with the account and named after
  the person. It cannot be opened to everyone, cannot be handed out by an admin
  from the outside, and its owner's role in it cannot be changed or removed. Not
  even emergency access reaches into it.
- **open to all** — every newly created account becomes a member automatically.
  The switch behind it is **Open to every new user** in Workspace settings; only
  the instance owner may flip it, and it is not offered at all for a personal
  space.

### What is in Workspace settings

Six things you can only do there, listed because the dialog is the answer to
several "where is that" questions:

| Section | What it offers |
| --- | --- |
| General | **Name** (rename), **Picture** |
| Access | **Members**, **What agents may do here**, **Open to every new user**, **Emergency access log** (owner only) |
| Layout | **How the sidebar is arranged** — *Documents and collections apart*, or *One tree, filed where you put it* |
| Conventions | **Workspace rules** |
| Data | **Files**, **Export workspace**, **Export as Markdown**, **Import workspace…** |
| Data | **Delete workspace** — you have to type the workspace name back to confirm |

**Export workspace** writes a native archive that **Import workspace…** reads
back one to one, on this instance or another. **Export as Markdown** is readable
anywhere and leaves the databases behind. See
[Import and export](import-export.md).

### Workspace rules

The working conventions an admin writes down for everyone in the workspace,
agents included ("invoices go into Finance/Inbox", "titles start with the
date"). Up to 16000 characters.

**Workspace admins read and write them** in Workspace settings → Workspace
rules; there is no screen where an ordinary member reads them. Agents receive
them through `get_workspace`, which is the point of writing them at all.

Writing them requires a workspace admin **in a browser** — an agent holding an
API token cannot rewrite the rules it is told to follow. An agent can
`propose_workspace_rules` instead, and the draft stays inert until an admin
applies or dismisses it. While one is waiting, the Workspace rules row says so.

### Emergency access

The instance owner can look into a workspace they are not a member of, and it
is deliberately awkward. From the account list, on their own row, the
**Emergency access** button next to a workspace they have no role in. It asks
for a reason of **at least 10 characters**, grants **read access for two hours**,
writes the reason into the audit log, and emails the workspace's admins. Any of
those admins can end it early from **Emergency access log** in Workspace
settings, where every past grant is listed with who, when and why.

It does not reach a personal space, and it is refused if you are already a
member.

### Files

Every upload is recorded with the page carrying it, its human name, its type,
its size and its date — so "every document for this customer" is a question with
an answer. A workspace's whole file list is one row in Workspace settings
(**Files**), filterable by name or by type, and each entry opens the page it
sits on. Reading it is permission-checked page by page, so somebody else's
private page keeps its attachments private. See [Files](files.md).

See [Workspaces](workspaces.md).

## Page

A page has a title (up to 2000 characters), an optional icon and cover, an
optional description under the title, tags, a body of blocks, comments, a raw
trail of notes, a version history, and — if it sits in a collection — property
values.

Every page also has an **owner**: the account that created it. It decides
private visibility. It is also what an account's deletion counts before it
happens — the confirmation names how many pages that account owns in shared
workspaces, private or not — and what "you own private pages here" means when
somebody tries to leave a workspace.

**Visibility** has two values. `workspace` means every member can read it.
`private` means only its owner and the workspace admins can, and **the whole
subtree below it inherits that**. Private applies to database rows too: a
private row is missing from other people's row lists entirely, not merely
greyed out.

The lock in the page header switches between the two, and its tooltip says which
way round you are: *Private (only you) — click to share with the workspace*, or
*Visible to the workspace — click to make it private*. On a narrow window the
lock moves into the ⋯ menu, where it is spelled out as *Make it private* /
*Make it visible to the workspace*.

A **sub-page** is a page whose parent is another page. Nothing about it is
special — it is the same object one level down, and it can have sub-pages of its
own. Trash and delete always take the whole subtree; restore brings back exactly
the pages that were thrown away together, and if the old parent is gone — **or
is itself still in the trash** — the page returns to the top level. Restoring a
child before its parent therefore lands it at the top level, not back under the
parent; restore the parent first.

Moving a page is re-parenting it, and it stays inside its own workspace. Moving
it to *another* workspace is a separate action (**Move to workspace** in the ⋯
menu) that takes the whole subtree along and detaches it from its old parent.

See [Pages](pages.md) and [Trash and recovery](trash-and-recovery.md).

### Tags

Short labels on a page, written without a leading `#` and stored per page:
lower-cased for comparison, spaces turned into hyphens, duplicates dropped, at
most 40 characters and 30 tags. Tags are shared across a workspace — any member
can recolour one by clicking the tag chip on a page and picking from nine
colours plus **Default**, and a tag with no colour of its own gets a stable
automatic one derived from its name. The sidebar filters by tag.

### Template

A page marked as a template. It appears in the **Templates** section of the
sidebar, and **New page from this template** copies it, keeping the title.

The important part is that **a template is a snapshot, not a link**. Choosing
**Save as template** duplicates the page and marks the *copy*, so the original
stays an ordinary page and editing it later never changes the template. Copies
made from a template never affect it either. Agents reach the same objects
through `list` with a kind of templates and `save_as_template`.

The **Templates** section also opens a gallery: every template on the left, a
plain-text preview of the selected one on the right, and **Use template** to
copy it. **Remove template flag** turns a template back into an ordinary page —
the page itself survives, it just leaves the section.

See [Templates](templates.md).

### Favourite

A page you starred. The star sits in the page header and in the ⋯ menu of any
tree item (**Add to favorites** / **Remove from favorites**), and starred pages
collect in a **Favourites** section at the top of the sidebar, which appears
only once there is something in it. Favourites belong to the account, not to the
page: yours and a colleague's are different lists over the same pages.

### Public share link

A page published as a read-only link that works without an account. The globe in
the page header mints it and shows it. Two things can be set on it: an **expiry**
(*Never*, *In 1 day*, *In 7 days*, *In 30 days*) and an optional **password**.
**Stop sharing** revokes it, and the link stops working at once.

Anonymous visitors get a standalone page, not the application — no sidebar, no
editing, nothing else of the workspace. A form view has its own separate public
link; see [Sharing](sharing.md) and [Forms](forms.md).

### Version history

A page keeps snapshots of its title and body, taken on save at most once every
two minutes, **the latest 50 per page**. **Version history** in the ⋯ menu lists
them with their time and their author, and **Restore** puts one back — saving
the current state as a version first, so restoring is itself undoable. Agents
read and restore the same snapshots through `revisions`.

### Comment

A remark attached to a page or to one block in it, with an author and a time. It
can be **resolved** (and reopened) and it can be **deleted**. Resolved ones are
hidden until you ask for them with **Show {n} resolved**. Comments live in a
panel beside the page; a collection's own page has no comments, because there is
nothing there to talk about that is not a row.

This is not the same thing as a **note** — see [Agent](#agent) below and
[Comments and notes](comments-and-notes.md).

## Block

The body of a page is a list of blocks: paragraph, heading, bulleted list,
numbered list, check list, toggle list, quote, code, table, divider, image,
video, audio, file — and blocks can be laid out in columns. salt.md adds four
of its own, offered in the `/` menu as **Callout**, **Bookmark / Embed**,
**Table of contents** and **Embed a collection**. Type `/` to insert one; drag a
block by its handle to move it.

Two inline things are not blocks but behave like vocabulary: typing `@` or `[[`
inserts a **page link**, a live mention of another page.

See [Editor and blocks](editor-blocks.md).

## Collection — which the tools call a database

A **collection** is a page whose child pages share a **schema**. Those children
are its **rows**; you look at them through **views**.

This is the one place where the interface and the tools use different words on
purpose:

| The interface says | The MCP tools say |
| --- | --- |
| Collection | database — `create_database`, `embed_database`, `database_id` |

*Collection* covers a table, a board, a calendar and a gallery equally and
promises no SQL to anybody who does not want any. The tools keep *database*
because renaming a tool breaks every agent configuration in existence. Use the
tool names in code and "collection" when writing to a person; do not try to fix
one side into the other.

A new collection starts with one property, **Status** (options *To do*,
*In progress*, *Done*), and two views, **Board** and **Table**.

A collection can be filed anywhere a page can — under a document, or inside
another collection. You build one there rather than dragging it: the ＋ on a
tree item offers **Page** or **Collection**, and the item's ⋯ menu has
**New collection inside**. A collection that ended up nested and should not be
gets out again with **Move to top level** in the same menu.

Embedding one into a document (**Embed a collection** in the editor's `/` menu,
or `embed_database` over MCP) stores a reference only: the collection stays one
object in one place and can appear in several documents at once.

See [Collections](collections.md).

### Row

A page inside a collection. It has everything a page has — text, sub-pages,
files, comments, history — plus its property values. A task on a board is not a
record in a table; it is a page that happens to sit in one. A row carries the
same ＋ as any other tree item, so a dossier under a deal is something you can
build by hand and not only over MCP.

One rule explains a lot of behaviour: **bare rows are not in the sidebar tree.**
A collection with fifty thousand rows would otherwise flood every listing, so
`/api/pages` leaves them out and they are reached through
`/api/collections/{id}/rows` instead. A row does appear in the tree when it has
live sub-pages of its own, because otherwise those sub-pages would have no
parent to hang under. A collection nested inside a collection is always in the
tree — it is not a row, and the counting argument never applied to it.

### Property

A typed field on a collection's schema: *Status*, *Due*, *Owner*. There are 13
types, three of which are **derived** — computed every time a row is read and
never stored: `rollup`, `formula` and `backrelation`.

Every property has an **id** and a **name**. The name is what people see; the id
is what you write:

```
set_properties(page_id: "…", properties: { "status": "in-progress" })
                                            ^ property id  ^ option id
```

**The property key has to be the id.** Writing the visible name there —
`{"Status": …}` — stores a key nothing reads: no view finds it, no filter
matches it, and nothing tells you.

An option's *value* is more forgiving. `set_properties` maps an option written
by its name onto its id before storing, case-insensitively, so
`{"status": "In progress"}` lands as `in-progress`; filters get the same
leniency. That leniency is in the MCP tools. `get_collection` returns both ids
and names, which is the reliable way to find out what a column is actually
called underneath.

See [Properties](properties.md) and
[Relations and rollups](relations-and-rollups.md).

### View

A saved way of looking at one collection: which type, which properties are
hidden, which filters, which sort, what it groups by. Seven types — table,
board, list, gallery, calendar, form and timeline. A collection can have many
views and they are independent of each other; changing one changes nothing else.

A **form** view has a second life: it can be published as a public link that
anybody can fill in without an account, and each submission becomes a row.

See [Views](views.md) and [Forms](forms.md).

## The three kinds of connection

salt.md keeps them apart because they answer different questions.

1. **Filed under** — a page's parent. Structure. This is what the sidebar and
   the tree show, and what the graph draws thin.
2. **Mentioned** — an `@` or `[[wiki link]]` in one page's text pointing at
   another. This is what backlinks and the graph show, and it is the connection
   nobody filed anywhere.
3. **Related** — a relation property between rows of two collections, with its
   reverse side available as a backrelation. This one belongs to a schema, not
   to text.

See [Library and graph](library.md) and
[Relations and rollups](relations-and-rollups.md).

## Blueprint

A ready-made workspace you pick off a shelf when you create a new one, instead
of starting from an empty sheet. The dialog behind **New workspace** offers
three kinds of start: **Empty workspace** ("Start from nothing and build it
yourself") sits first; **Start with a ready-made workspace** is the shelf of
built-in ones, each card naming how many databases, columns and views it brings;
and **Or like one you already have** points at an existing workspace of yours.

A blueprint carries **structure only**: the rules, the databases, their property
schemas with their option ids, and their views. No rows and no documents — a
blueprint full of somebody else's tasks is not a blueprint. When you point at an
existing workspace, that workspace *is* the template; there is no separate saved
template object to drift out of step with it.

See [Library and graph](library.md) and [Workspaces](workspaces.md).

## Credentials: session, token, connection

Three ways to reach the same content, with deliberately different reach.

| | What it is | Can be narrowed by | Reaches administration |
| --- | --- | --- | --- |
| **Session** | a person signed in through a browser | — | yes, per role |
| **API token** | a permanent key, shown once when created | read or write, and a list of workspaces | no, with one exception below |
| **Connection** | an agent that signed in over OAuth; expires on its own and can be ended | workspaces chosen on a consent screen, where the scope the client asked for is shown for approval | no, with the same exception |

**A token is a second key to content, not an admin pass.** It carries the full
identity of the person who made it and narrows only in those two ways. Anything
administrative — the account list, the instance backup, the workspace rules,
issuing another token — requires a browser session, whatever the token says.

**The exception is membership.** A write credential held by somebody who is
admin of a workspace can add a member to it, change a member's role and remove
one, over the API. The last-admin and personal-space protections still hold, and
the credential still cannot reach a workspace outside its own list — but if you
hand out a write token, you are handing out that person's ability to change who
is in their workspaces.

A token can also never mint a workspace it would be unable to open: a connection
tied to a fixed list of workspaces is refused when it tries to create one.

Tokens are created and deleted under **API tokens** in the account menu, where
each one shows its scope, its workspaces, and when and from where it was last
used — a token that rides in a URL cannot be kept secret, so the defence is
noticing.

See [API access](api.md), [Agent access](agent-access.md) and
[Permissions](permissions.md).

## Agent

Any client talking to the instance over MCP — a coding assistant, a chat client,
a script. An agent acts as the account whose credential it holds; there is no
separate agent account.

Two things an agent can leave behind, and they are not the same:

- **`working_on`** — "I am on this page right now", with a note saying what it
  is doing. People see it live in the page header. It does not expire on its
  own, because an agent has no clock to send a heartbeat with; a sweep clears
  what has been silent for half a day.
- **`note`** — one line dropped onto a page's raw trail, in order, with the time.
  It can never be edited or removed, by anybody; a person can discard a whole
  page's trail deliberately, and that is the only way any of it goes. That is
  different again from a **comment**, which is a conversation and can be
  resolved and deleted.

Which agent is calling is a **claim**: nothing in a credential says which one it
is, so the agent names itself and the verified account travels beside it —
"Claude · via Ada Lovelace". The second half is the part you can trust.

A workspace decides for itself what agents may do in it: **Anything they were
granted**, **Only signed-in connections** (a permanent token is refused even
when it names this workspace), or **No agents at all**.

See [Agents](agents.md), [MCP tools](mcp-tools.md) and
[Comments and notes](comments-and-notes.md).

## Which one owns which

| Thing | Belongs to | Goes when that goes |
| --- | --- | --- |
| Page | one workspace, one owner account | the workspace |
| Sub-page | its parent page | the parent, subtree and all |
| Row | its collection | the collection |
| Property, view, schema | its collection | the collection |
| Tag on a page | the page | the page |
| Tag colour | the workspace | the workspace |
| Template flag | the page carrying it | the page |
| Rules, agent setting, sidebar layout | the workspace | the workspace |
| Public share link | the page | the page |
| Uploaded file | the page it was uploaded to | the page |
| Comment, note, revision | the page | the page |
| API token, connection, favourite, language and time settings | the account | the account |
| Instance name, webhooks, mail settings | the instance | — |

## Words that look like synonyms and are not

- **Collection / database** — the same object, two audiences. See above.
- **Page / document** — a document is a page that is not a collection and not a
  row. The sidebar section is called **Documents** when collections have a
  section of their own, and **Pages** when everything sits in one tree.
- **Template / blueprint** — a template copies one page; a blueprint sets up a
  whole workspace's structure.
- **Note / comment** — a note is append-only and never edited; a comment is a
  conversation and can be resolved.
- **Tag / property** — a tag is free text on any page; a property is a typed
  field defined by a collection's schema and only exists on its rows.
- **Invitation / account creation** — an invitation is a link somebody else
  redeems, and it expires; creating a user makes the account immediately and
  tells nobody.
- **Owner** — three different owners live in this product: the instance owner,
  a workspace's owner, and a page's owner account. They are unrelated.

Next: [Getting started](getting-started.md) if you have not set anything up yet,
or [The interface](interface.md) for where all of this appears on screen.
