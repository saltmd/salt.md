# Workspaces

A workspace is the boundary around content. Every page in salt.md belongs to
exactly one workspace, and you only see pages in workspaces you are a member of.
There is no per-page invitation list: if you need "these four people and nobody
else", the answer is a workspace, not a setting on a page. This page covers
creating one, its name and picture, members and their roles, the rules text,
what agents may do inside it, leaving, and what happens to a workspace nobody is
left to look after.

## The switcher

The button at the top of the sidebar shows the current workspace — its logo, or
its emoji, or the first letter of its name in a colour derived from that name.
Clicking it opens the list of every workspace you belong to. Your own space
comes first, the rest in the order they were created. Two tags can appear beside
a name: **own space** for your personal one, **open to all** for a workspace
every new account joins automatically.

Below the list:

| Entry | Who sees it |
| --- | --- |
| **Workspace settings** | admins of the workspace you are currently in |
| **New workspace** | everyone, unless the instance has switched it off |
| **With nobody in charge…** | the instance owner |

Everything about one workspace lives behind **Workspace settings** — members,
rules, files, export, delete. If you are a member but not an admin, that entry
is absent, and so is every dialog behind it: the member list, the rules, the
file list and both exports are reachable in the browser only through it. An
agent connected to the workspace still receives its rules over MCP.

## Your personal space

Every account created after the instance was set up gets one: signing up,
accepting an invitation, being created in user management, or signing in through
SSO for the first time. The account that ran the setup screen is the exception —
it starts with the instance's shared workspace (named `Workspace`, and marked
**open to all**) and has no personal space of its own. On a self-hosted instance
that is usually the owner, which is worth knowing before going looking for one.

The space carries the person's name, or `Personal` if the account has none, and
that person is both its owner and its admin. It behaves like any other workspace
with a handful of exceptions, all of them guards rather than missing features:

- It cannot be opened to every new user.
- Its person cannot be demoted or removed from it, by anybody.
- It is not handed out from user management — an instance admin cannot seat
  somebody in it. Whoever wants to let a guest in does it themselves, from the
  member list of their own space.
- Emergency access does not reach it. See [Administration](administration.md).
- It is not offered as a starting point for a new workspace. The shelf under
  **Or like one you already have** lists your shared workspaces only.
- If it is ever left with nobody in it, it can be cleaned up but never adopted —
  the clean-up view offers **Delete** and nothing else.

A personal space may have guests: its owner invites them like anywhere else. If
that account is later deleted, the space does not go with it — it becomes an
ordinary workspace and passes to one of the people left in it, while the deleted
account's private pages are removed. The heir is picked by role first (an admin
before a member, a member before a viewer) and only then by age of account.
Deactivated accounts are passed over.

## Creating a workspace

**New workspace** opens a shelf, not a name prompt: *Start with a ready-made
workspace*, and under it *Each one brings its databases, views and house rules —
and no data. You fill it.*

1. Open the workspace switcher and choose **New workspace**.
2. Pick a card: **Empty workspace**, one of the ready-made blueprints, or — under
   **Or like one you already have** — one of your shared workspaces.
3. Read the preview. For a blueprint it lists every database with its columns,
   the real option colours and the views you will get; the card's fact line
   counts databases, columns, views and whether it carries house rules.
4. Type a **Name**.
5. Press **Create workspace**.

Three blueprints ship with salt.md:

| Blueprint | What it is |
| --- | --- |
| **Software team** | What we run, and what still has to be done to it. |
| **Sales pipeline** | Companies on one side, deals on the other, and a board you can drag. |
| **Content calendar** | Every channel, every piece, and the date it goes out. |

The preview is read out of the blueprint the server is about to import, not from
a stored screenshot, so it cannot advertise a column that is not in there.

Starting from **one of your own** copies the databases with their columns, their
option ids and their views, plus the workspace rules — and no rows, no
documents. The workspace you point at is the template; there is no separate
template object to drift out of step with it. Two consequences worth knowing: a
workspace with no databases cannot be used this way (there is nothing to copy),
and a relation pointing at a database outside the copied workspace loses its
target rather than quietly reading rows from the original.

Whoever creates a workspace becomes its **admin** and its **owner**. Two things
can stop you:

- An instance can switch off workspace creation for non-admins — the setting is
  *Users may create their own workspaces* in the admin dialog. With it off, the
  **New workspace** entry is hidden for everyone but instance admins, and the
  server refuses on every route that would make one.
- A connection limited to particular workspaces (a scoped API token, or an OAuth
  grant where somebody ticked individual workspaces) is refused when it asks for
  a new workspace over MCP or over `/api/workspaces`. It would not be able to
  open what it made, and widening its own list is exactly what a boundary must
  not do.

Importing a workspace archive also creates one — see
[Import and export](import-export.md).

## Name and picture

Both sit under **General** in the settings dialog.

**Name** opens *Rename workspace* with the current name filled in; **Rename**
saves it. A blank name is refused. The browser sets no length limit of its own;
over MCP a name is capped at 80 characters, when creating as well as when
renaming.

**Picture** — *Pick an emoji or upload a logo (a company or project logo, say).*
An emoji and a logo are one identity, not two: setting the emoji clears the
image and uploading an image clears the emoji. **Remove** clears both and the
switcher falls back to the coloured initial. The picture is shown in the
switcher and on cards. Only an uploaded file is accepted — pasting an external
image address is refused, because a remote picture would turn every sidebar into
a request to somebody else's server.

## Members and roles

Three roles, per workspace:

| Role | May |
| --- | --- |
| **Admin** | everything a member may, plus: members and their roles, the rules, the settings, invitations, delete. Sees private pages belonging to other members. |
| **Member (can edit)** | read and write every page that is not somebody else's private page |
| **Viewer (read only)** | read the same pages, change nothing anywhere in the workspace |

A viewer's limit is wider than "cannot edit". They cannot create a page in that
workspace at all (*you are a viewer in that workspace and cannot create pages
there*), and nobody can move a page into it on their behalf — the move is
refused with *you are a viewer in the target workspace and cannot add pages
there*.

Above these sits the instance owner and instance admins, who administer the
server rather than its content — see [Administration](administration.md) and
[Permissions](permissions.md).

Under **Access → Members** the dialog *Workspace members* shows the roster by
name, email and role; your own row is marked **(you)**. An admin gets a dropdown
on every other row (**Admin**, **Member**, **Viewer**) and a ✕ to remove. The
dialog opens from the settings dialog, so in the browser it is an admin who
looks at it, even though the underlying route serves any member.

### Inviting somebody

At the bottom of the member dialog: an email field labelled *Invite by email
(blank = link only)*, a role, and **Invite**.

- With an address, the invitation is emailed if the instance can send mail
  (*Invitation sent by email*).
- With the field blank, or when no mail is configured, you get a link and it is
  copied to your clipboard: *Invitation link (valid 14 days, copied):*.
- The recipient opens the link and sets their own password. If they already have
  an account, they sign in with it — password, and the 6-digit code if they use
  two-factor — and are added. If they are already signed in as that account, the
  link simply joins them. An invitation bound to an address refuses a different
  account: *this invite is for a different account — sign out to accept it*.

Instance admins have a second route: in user management, each account shows one
pill per workspace with **No access / Viewer / Member / Admin**. The same pills
appear while an account is being created, under **Workspace access** in the
**Create a new user** panel, so a new account can be seated in its workspaces
with a role each from the start. Two rules hold there — nobody grants themselves
access this way (emergency access exists for that, and it is logged), and an
admin can only assign where they are a workspace admin themselves.

There is a third route with no button behind it. `POST /api/workspaces/{id}/members`
adds an **existing** account by email address, straight away and without an
invitation. A workspace admin may call it, the role is clamped to admin, member
or viewer, and an unknown address is refused. Nothing in the browser offers it —
the interface deliberately routes people through invitations instead — so it is
a thing scripts and agents can do that a person cannot.

### Leaving, and being removed

The ✕ on your own row is **Leave**; on anybody else's row it is **Remove**, and
only admins have it. Both ask first: *Leave this workspace?* or
*Remove Ada?*, naming the person.

If the person leaving owns private pages here, a second question follows and
names the number: *You have 3 private page(s) here. They stay in the workspace
and will only be visible to its admins afterwards.* — followed by *Leave
anyway?* or *Remove anyway?*, with the confirming button labelled **Leave** or
**Remove**. The pages are not deleted and not moved; they stay where they are,
and the workspace admins are the only ones who can still open them.

A workspace never loses its last admin. Demoting one is refused; removing one
says what to do instead: *You are the last admin of this workspace. Make
somebody else an admin first — or delete the workspace if it should go.* A
deactivated account does not count as an admin for this — otherwise a workspace
whose only admin had left the company could be neither repaired nor cleaned up.

## Workspace rules

Free text an admin writes down once: the working conventions of this workspace.
Up to 16 000 characters. The placeholder says what shape they take — *e.g.
Invoices go into Finance/Inbox. Titles start with the date. Never edit the
Handbook section.*

They exist mainly for agents. `get_workspace` hands them to a connected agent
before it writes anything, framed as conventions to follow; `list` with
`kind: "workspaces"` marks which workspaces have them. Rules never grant
permission beyond what the connection already has, and they never replace the
task the person actually asked for.

Editing stays with workspace admins **and only in the browser**. The server
refuses an API token on the rules routes outright. That is the point rather than
an oversight: agents are told to follow this text, so anything that could write
it would be able to rewrite its own guardrails.

The dialog has a read-only mode for anybody who is not an admin, but its only
entry point is the admin-only settings dialog. So in practice a plain member
cannot read the rules in the browser at all — for them the rules exist over MCP
and nowhere else.

### Proposals

An agent can draft rules instead: `propose_workspace_rules` stores the text
beside the active rules without touching them. Two conditions before anything
else is checked: the connection must be able to write (a read-only API token is
refused even when its account is a workspace admin), and the account must be an
admin of that workspace.

A pending draft appears in the settings dialog (*A rules proposal is waiting for
review*) and at the top of the rules dialog, with who wrote it and when:

- **Load into editor** puts the draft in the text box; you then edit it and
  press **Save**.
- **Dismiss proposal** drops it and leaves the active rules alone.

Saving the rules settles a pending proposal either way — you have just reviewed
it, whether you took it or wrote something else. There is one proposal slot per
workspace: a newer draft replaces the older one, and the reply says so.

An agent withdraws its own draft by proposing an empty string. Somebody else's
it cannot touch: *the pending proposal is not yours to withdraw — an admin can
dismiss it in the browser*. With nothing pending, the same empty proposal
answers *There is no pending proposal to withdraw.*

## What agents may do here

A workspace decides for itself what a connected agent may do inside it. The
setting is under **Access**, and its default is what happens if you never touch
it.

| Choice | What it means |
| --- | --- |
| **Anything they were granted** | Any connection that was given this workspace. (default) |
| **Only signed-in connections** | A permanent token is refused, even one naming this workspace. For confidential material. |
| **No agents at all** | Browser sessions only. |

A person in a browser is never limited by this — they are the one who sets it.

The middle option is the interesting one: it accepts an agent that went through
the sign-in and consent flow (short-lived, revocable, approved on screen) and
refuses a long-lived API key even when that key explicitly names this workspace.

A workspace an agent may not enter does not appear in the agent's workspace list
at all; it learns only that further workspaces exist, as a count with no names.
Naming its id directly gets a refusal rather than a page. `get_workspace` says
so plainly — *workspace "…" is outside what this connection was granted — ask
for it to be added, or name one it can reach* — because "not found" for
something that exists and is the caller's own sends an agent hunting for a typo.
The other tools and the REST routes are terser and answer *not found*, so
`get_workspace` is the one to ask when the difference matters.

Switching this away from the default is written to the activity log, because
"why can the agent suddenly not read this" is a question somebody asks weeks
later.

See [Agent access](agent-access.md) and [Agents](agents.md).

## Open to every new user

Under **Access**, a switch: *Open to every new user — every newly created account
automatically becomes a member of this workspace.* New accounts join as members.
Only the **instance owner** can set it, because it is a decision about the whole
instance rather than about one workspace, and a personal space can never be one.
A workspace with it on is tagged **open to all** in the switcher.

A fresh instance starts with exactly one such workspace: the shared one the
setup screen creates. On an instance that predates this setting, the oldest
non-personal workspace is marked automatically while none is marked at all, so
the place newcomers used to land in does not change silently. Once the owner has
marked one — or unmarked every one deliberately — nothing is marked again.

## How the sidebar is arranged

Under **Layout**, and it changes only how this workspace is drawn:

- **Documents and collections apart** — two sections. Good when the databases
  are the point. (default)
- **One tree, filed where you put it** — a collection stays under its document.
  Good for documentation.

Both readings are right for different workspaces, which is why it is asked
rather than decided. See [Collections](collections.md).

## Files, export, import

Under **Data**:

- **Files** — every uploaded file in this workspace, with the page carrying it.
  See [Files](files.md).
- **Export workspace** — a native archive, importable one to one. It carries the
  name, icon, logo and rules, the pages with their databases, the workspace's
  tag colours, and the uploaded files those pages reference.
- **Export as Markdown** — readable anywhere, without the databases.
- **Import workspace…** — a native archive from another instance. It always
  creates a **new** workspace with you as its only member and its admin; it
  never merges into an existing one.

**An export holds exactly what the person exporting can see.** Other people's
private pages are dropped from the archive. Because the rows sit in the settings
dialog, it is a workspace admin who does the exporting; the server itself asks
only for membership, which is what lets a running emergency grant produce an
export too.

Importing needs no role anywhere — it makes a new workspace, so the only gate is
the instance-wide *Users may create their own workspaces* setting. Two answers
worth expecting: if the name is already taken, the new workspace gets
*(Import)* appended to it, and an archive written by a newer version of salt.md
is refused with a message telling you to update.

See [Import and export](import-export.md).

## Emergency access

The instance owner can take time-limited read access to a workspace they are not
a member of. It starts in user management: open **your own** account, and beside
every workspace you have no access to there is an **Emergency access** button.
It asks *Emergency access to "…" — why?* with the placeholder *e.g. Legal review
ref. …, approved by …*.

A reason of at least ten characters is mandatory — that is the whole difference
between emergency access and a quiet back door. The grant lasts two hours and
allows reading only. It is written to the activity log the moment it is issued,
and every workspace admin is emailed about it if the instance can send mail; the
mail goes out in the background and a failure is only logged, so on an instance
with no mail configured the log entry is the whole record.

**Emergency access log** in the settings dialog lists what has happened: who
looked in, when, with what reason, and until when. A running grant carries
**End it now**. The row appears in the settings dialog for the instance owner
only, though the routes behind it serve any workspace admin — so a workspace
admin can read the log and end a running grant, just not from that screen.

A personal space cannot be looked into this way at all: *A personal space cannot
be looked into even in an emergency — it belongs to exactly one account.* Nor
can a workspace you are already a member of, where the answer is *You are
already a member of this workspace — emergency access is not needed.*

## Deleting a workspace

**Delete workspace** takes every page in it along, and it cannot be undone.

1. Open **Workspace settings → Delete workspace**.
2. The prompt reads *Irrevocably delete "…" and EVERY page in it?* and asks you
   to type the workspace name.
3. Type it exactly and press **Delete permanently**.

Anything short of the exact name is refused. So is deleting your own last
workspace — you would be left with nowhere to go. Pages, their databases,
comments, revisions, share links and favourites go with it; uploaded files that
no other page still points at are removed from disk as well. One line survives
in the activity log, naming the workspace but nothing that was in it.

## Moving pages between workspaces

A page's ⋯ menu offers **Move to workspace** when you are in more than one. The
whole subtree comes along — otherwise a database's rows would end up somewhere
other than the database. The page loses its parent and arrives at the top level
of the target, because the old parent stays behind. Agents do the same thing with
`update_page` and a workspace id.

Two refusals: you cannot move into a workspace where you are a viewer, and a
subtree containing somebody else's private pages will not move at all — in the
target you might be an admin, and the move would quietly turn their notes into
something you can read.

Tag colours do not travel with a page. They are set per workspace, so a tag that
is green in one workspace is uncoloured in the next until somebody colours it
there. They do travel with an export and an import, because those carry the
whole workspace.

## A workspace with nobody in charge

Two different states, and only one of them is invisible.

A workspace with **no members left at all** still exists and holds its pages
while appearing in nobody's sidebar. A workspace whose **admins are all gone or
deactivated** is still there for its members — the sidebar lists every workspace
you are a member of and asks nothing about roles. They go on working in it. What
they have lost is anybody who can change its settings, its members or its rules.

The instance owner sees both under **With nobody in charge…**:

*These are workspaces nobody can look after any more. With no members left at
all you can adopt or delete them. Where members remain, only someone in charge is
missing — appoint one of them in user management.*

Each row shows the page count, the member count, how many admins are still
active — a deactivated account does not count, which is exactly why a row can
read "2 members · 0 admins" — and the last known owner. What is offered depends
on what is left:

| State | What you can do |
| --- | --- |
| No members at all | **Adopt** (you become its admin) or **Delete** |
| Members remain, no admin | nothing here — *Still has members: make one of them an admin.* |
| An orphaned personal space | **Delete** only — *Orphaned personal space — clean up only, do not open.* |

**Delete** asks for the name here too, and the prompt names what goes with it:
*Permanently delete "…" and its 41 pages? Type the name to confirm.*

"Still has members" is a deliberate wall rather than a gap. Adopting a workspace
that people are still in would be a master key: deactivate the admins, adopt,
read everything, permanently. As long as somebody is in there, the workspace
belongs to those people; the owner appoints one of them instead, and for a look
inside there is emergency access, which expires and is visible to the people
affected.

Deleting an account is the usual way workspaces change hands, and it says in
advance what will happen to each one: personal spaces go with the person, a
personal space with guests becomes an ordinary workspace and stays with them,
shared workspaces pass to the oldest remaining active admin, and only a
workspace with nobody left at all falls to the instance owner. See
[Account](account.md).

## What agents can do with workspaces

| Tool | What it does |
| --- | --- |
| `list` with `kind: "workspaces"` | the workspaces this connection may reach, with your role and whether rules exist — plus a count, without names, of the ones it was not granted |
| `get_workspace` | name, your role, every member with their id, name, email and role, page and database counts, and the rules |
| `workspace` | create one — optionally with `from_workspace`, copying an existing workspace's rules, databases, schemas and views but no rows and no documents — or rename it / set its icon (workspace admins only) |
| `propose_workspace_rules` | submit a rules draft for an admin to review in the browser |

**Changing** membership and roles is deliberately not reachable over MCP, in any
tool; `whoami` says so in its own list of what this connection cannot do.
Reading them is — `get_workspace` names every member with their role, which is
what lets an agent fill a person property or assign work. Applying the rules is
in the same class as changing membership: a draft can be proposed, and a person
in a browser decides. Those are decisions about who may see what.

See [MCP tools](mcp-tools.md).
