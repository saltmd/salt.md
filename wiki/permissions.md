# Permissions

Who may see and do what. salt.md has two sets of roles that do not overlap:
**instance roles** decide who administers the server and the accounts on it, and
**workspace roles** decide who reads and writes content. An instance admin is not
automatically allowed into your workspace, and a workspace admin has no power
over the instance. This page has the exact consequence of every role, what a
private page means, what an API token can and cannot do compared with a
signed-in browser, and how the owner gets a look inside a workspace they are
not in.

The short version: **access is per workspace, not per page.** If you need "these
three people and nobody else", the answer is a workspace. See
[Workspaces](workspaces.md).

## The four levels

| Level | Where it lives | What it decides |
| --- | --- | --- |
| **Instance owner** | one account, the whole server | instance configuration, the account lifecycle, emergency access |
| **Instance admin** | any number of accounts | people: create accounts, invite, see the user list |
| **Workspace admin** | per workspace | everything inside that one workspace |
| **Member / viewer** | per workspace | content in that one workspace |

Exactly one account is the owner. The interface shows it as an **Instance owner**
badge in **Manage users**, next to **Instance admin** for the others.

## Instance roles

### Owner

The owner runs the instance. They have the SQLite file on the server anyway —
that is the honest boundary, and everything below is about what happens *inside*
the app, where each act leaves a trace in the [activity log](history-and-audit.md).

Only the owner can:

- delete an account (**Delete user**) — an admin can only deactivate
- change another account's password or email
- take [emergency access](#emergency-access) to a workspace they are not in, and
  read a workspace's **Emergency access log** afterwards
- download the instance backup (`/api/admin/backup`)
- open a workspace to every new account (**Open to every new user**) — the switch
  sits in **Workspace settings**, so it is reachable only in a workspace they
  administer, and it is not offered for a personal space
- hand the instance on (**Hand over the instance**)
- see and clean up workspaces nobody is left in charge of
  (**With nobody in charge…**)
- see the full workspace list in **Manage users** — an admin sees only the
  workspaces they administer themselves

The owner cannot be deactivated or deleted, and their admin rights cannot be
revoked: *"The owner cannot be deactivated — hand the owner role on first."* The
handover goes to an active account that is already an instance admin, and it is
complete — afterwards the previous owner is an ordinary admin, and any emergency
grants they were holding end at that moment.

### Admin

An admin manages **people, not content**. They create accounts, invite, see the
user list, and configure the instance settings, mail, the tunnel and
[webhooks](webhooks.md).

Three things an admin deliberately cannot do, because without them the boundary
would be decoration — whoever can set a password can sign in and read everything:

| An admin cannot | Why |
| --- | --- |
| set another account's password or email | that is signing in as them |
| add themselves to somebody else's workspace | *"You cannot grant yourself access here — use emergency access, which is logged."* |
| export a workspace they are not a member of | export follows membership, not the admin flag |

An admin can change workspace roles only in workspaces where they are a
workspace admin themselves: *"Only the owner or an admin of this workspace can
change this."*

### Member

Everybody else. A plain account with no instance role reads and writes in the
workspaces it belongs to and nothing more. Whether it may create workspaces of
its own is an instance setting — **Users may create their own workspaces** —
and when it is off the answer is *"creating workspaces is disabled on this
instance — ask an admin"*.

## Managing accounts

**Manage users**, under your avatar, is where accounts are made and changed. An
admin sees it; the owner sees more in it.

### Creating an account

1. Open **Manage users** and press **+ User**.
2. Fill in **Name**, **Email** and **Initial password (min. 8 characters)**. The
   panel says what it does: *"Creates the account straight away — no email is
   sent."*
3. Tick **Instance admin (may manage everything)** if this account is to
   administer the instance.
4. Under **Workspace access**, set a role per workspace: **No access**,
   **Viewer**, **Member** or **Admin**. Every workspace starts at **No access**,
   so a role is always something somebody chose.
5. Press **Create user**.

The workspace list there is not the whole instance. An admin sees only the
workspaces they administer; the owner sees all of them — and personal spaces
appear for neither.

The other route is an [invitation](#inviting-somebody), where the person sets
their own password and nobody else ever knows it.

### Making and revoking instance admin

Select the account and press **Make instance admin** or **Revoke admin rights**.
Four guards sit on that one button:

- You cannot revoke your **own** admin rights: *"you cannot remove your own admin
  rights — ask another admin"*. It would lock you out of the dialog you are
  standing in, with no way back.
- The **last** admin cannot be demoted.
- The owner's rights are locked: *"The owner's rights cannot be revoked — hand
  the owner role on first."*
- Neither button is offered for the owner's account, nor for your own.

### Deactivating, reactivating, deleting

| Action | Who | What happens |
| --- | --- | --- |
| Leave a workspace | anyone, for themselves | membership goes; private pages stay behind, admin-visible |
| **Deactivate account** | instance admin | sign-in closed, all sessions and API tokens deleted, the calendar link invalidated, any open editor disconnected — nothing is lost and everything stays attributable |
| **Reactivate** | instance admin | sign-in works again. The sessions, tokens and calendar link that were deleted do not come back; they are issued afresh |
| **Delete user** | owner only | the personal space goes with the account, and the workspaces are settled as below |

Deactivating is the normal case when somebody leaves. Nobody can deactivate
their own account (*"You cannot deactivate your own account."*) and nobody can
delete their own. The delete dialog states the consequences before it asks —
which personal space disappears, which workspaces are kept because others work
in them, which are left with nobody in charge — and ends with *"Deactivating is
usually enough — nothing is lost that way."*

What deletion does with the workspaces:

- **The personal space** is deleted with the account — unless the person had
  invited guests. Then it becomes an ordinary workspace and goes to the most
  senior active member left in it: an admin before a member before a viewer, and
  among equals the longest-serving. The deleted account's private pages in it
  are removed anyway; the guests could never see them, and inheriting the
  workspace must not change that.
- **Shared workspaces** keep the admins they still have, and ownership passes to
  the longest-serving remaining active admin. One that is left with no admin
  **and** no other member at all goes to the instance owner. As long as anybody
  else is a member, the owner does not move in — the right move there is to
  appoint one of the people already inside.
- **Somebody else's personal space** the account had been invited into is
  neither deleted nor handed over. Only the membership goes.

### Changing a password

Changing an account's password deletes **every session and every API token** of
that account. Change your own and the browser you are sitting in gets a fresh
session and stays signed in; everything else — another browser, the
[desktop app](desktop-app.md), every agent — has to sign in again. Changing your
own password or email asks for the current password first.

Only the owner may change **somebody else's**: *"Only the owner can change
another account's password or email. As an admin you can send an invitation."*

## Workspace roles

Set per person per workspace, in **Workspace settings → Members**. The dropdown
offers **Admin**, **Member** and **Viewer**. Where a role is shown but not
editable — your own row, and every row if you are not a workspace admin — it
reads **Member (can edit)** or **Viewer (read only)**.

**Workspace settings opens only for a workspace admin**, which decides more of
the table below than the API alone would suggest.

| Can they… | Viewer | Member | Workspace admin |
| --- | --- | --- | --- |
| read pages, rows and files | yes | yes | yes |
| edit a page, upload, move, trash, restore | no | yes | yes |
| write a comment, resolve one, add a `note` | no | yes | yes |
| create a public share link | no | yes | yes |
| read other people's private pages | no | no | **yes** |
| see the member list | API only | API only | yes |
| add, remove or re-role members | no | no | yes |
| invite somebody who has no account | no | no | yes |
| write or dismiss the [workspace rules](workspaces.md#workspace-rules) | no | no | yes |
| rename it, set the picture, agent access, sidebar layout | no | no | yes |
| export the whole workspace | no | no | yes |
| delete the workspace | no | no | yes |
| view and end emergency grants | no | no | no |

Three of those rows need the reason spelling out, because the server is more
generous than the screens are:

- **See the member list.** `/api/workspaces/{id}/members` answers for any member,
  so an agent or a script can read the roster. The **Workspace members** dialog
  is reached through **Workspace settings**, so in the browser only a workspace
  admin gets to it.
- **Export the whole workspace.** `/api/workspaces/{id}/export` accepts any
  member, but both entries (**Export workspace**, **Export as Markdown**) live in
  **Workspace settings**. A member or a viewer can still export one page at a
  time: ⋯ menu → **Export** → **Markdown (.md)**, **Web page (.html)** or
  **Print / as PDF**. See [Import and export](import-export.md).
- **View and end emergency grants.** The server lets a workspace admin read the
  log and revoke a grant, but the **Emergency access log** row is drawn for the
  **instance owner** alone. No other account has a way to it in the interface.

A viewer sees the page exactly as everyone else does; the editor renders
read-only rather than accepting keystrokes the server would reject. On the live
collaboration socket, every frame that **writes** is checked again, so a
demotion bites without a reload. Reading is checked once, when the socket opens:
a tab that is already open goes on receiving the document live until the page is
reloaded.

Four guards keep a workspace from ending up with nobody in charge:

- The last active admin cannot be demoted or removed: *"You are the last admin of
  this workspace. Make somebody else an admin first — or delete the workspace if
  it should go."*
- A deactivated admin does not count as one — otherwise a workspace could look
  staffed while nobody could act.
- Deleting a workspace needs its name typed back as confirmation, and it is
  refused if it is your last one.
- Someone's own personal space cannot be taken from them: *"That is this person's
  personal space — their role in it stays as it is."*

### Inviting somebody

A workspace admin can bring in somebody who has no account at all, from
**Workspace settings → Members**:

1. Type an address in **Invite by email (blank = link only)**, or leave it blank.
2. Pick the role: **Member**, **Viewer** or **Admin**.
3. Press **Invite**.

The link is copied to your clipboard either way and shown under **Invitation
link (valid 14 days, copied):**. With an address filled in and [mail](mail.md)
configured it is sent as well — the toast says **Invitation sent by email**, or
**Invitation link copied** when it was not. The recipient opens the link and
sets **their own** password. Invitation emails go out in English: they reach
somebody who has no account yet, so the server has no idea what language they
read.

Somebody who already has an account is added the other way, in **Manage users**
under **Workspace access** — or, for a workspace admin working through the API,
at `/api/workspaces/{id}/members`.

### A workspace with nobody in charge

If every admin of a workspace is deactivated or deleted, the workspace and its
pages are still there. The owner finds it under **With nobody in charge…** in
the workspace menu, in a dialog called **Workspaces with nobody in charge**.
Each entry offers one of:

- **Adopt** — only when the workspace has **no members left at all**. While
  anybody is still in it: *"This workspace still has members. If nobody is in
  charge, appoint one of them in user management — for a look inside there is
  emergency access."* That rule closes an obvious back door: without it,
  "deactivate the admins, then adopt" would be a master key with no time limit
  and no notification.
- **Delete** — the workspace name has to be typed back, as with an ordinary
  deletion.
- Nothing, for one that still has members but no admin:
  *"Still has members: make one of them an admin."*
- Clean-up only, for an orphaned personal space:
  *"Orphaned personal space — clean up only, do not open."*

## Private pages

Every page is either visible to the workspace or **private**. The padlock in the
page header switches between them; its tooltip says **Private (only you) — click
to share with the workspace** or **Visible to the workspace — click to make it
private**. On a narrow window the same action sits in the ⋯ menu as **Make it
private**.

What private means exactly:

- A private page is readable by **its owner** — the account that created it — and
  by the **workspace admins**. Nobody else.
- **Privacy is inherited by the whole subtree.** A sub-page under a private page
  is private too, whatever its own setting says, and so is everything under that.
- It applies everywhere, not just in the sidebar: [search](search.md) checks the
  workspace first and then every hit again per page, so a private page never
  appears in results; a [relation](relations-and-rollups.md) column checks each
  related row on its own; the file list checks the page carrying each upload;
  and a workspace export contains exactly the pages the person exporting can see.
- The library's **Private** shelf lists every private page **you** can see —
  your own, and in a workspace you administer other people's as well. The owner
  column names who each one belongs to. A private page with **no parent** shows a
  padlock in the source column instead of a parent page; one filed under another
  page shows that parent, like any other page. See [Library](library.md).

Two consequences that surprise people:

**A page keeps its owner when somebody else edits it.** Ownership is set once, at
creation. If a colleague marks a page private that you created, it stays yours
and becomes invisible to them unless they are a workspace admin.

**Private pages stay behind when you leave.** Removing a member — or leaving
yourself — is interrupted with the count: *"You have 3 private page(s) here. They
stay in the workspace and will only be visible to its admins afterwards."* The
question is then **Leave anyway?** or **Remove anyway?**.

## Personal spaces

Every account gets a workspace of its own, named after the person. It is the one
place the permission model treats as untouchable:

- It never appears in the **Workspace access** list in **Manage users**, not even
  for the owner.
- Nobody can be granted a role in it from outside: *"A personal space is not
  handed out from outside — only its owner invites anyone there."* The person
  invites guests themselves, through the members dialog.
- [Emergency access](#emergency-access) is refused for it: *"A personal space
  cannot be looked into even in an emergency — it belongs to exactly one
  account."*
- It cannot be opened to every new account, and it is never adopted by anybody.

If the account is deleted, the personal space is deleted with it — unless the
person had invited guests, in which case it becomes an ordinary workspace and
goes to the most senior active member left in it: an admin before a member
before a viewer, and among equals the longest-serving. The deleted account's
private pages in it are removed anyway.

## Sessions and API tokens

An **API token is a second key to content, not an administration pass.** It
carries the full identity of the person who created it and can be narrowed in
only two ways, both of which act on pages:

| Narrowing | Interface | Effect |
| --- | --- | --- |
| scope | **Read-write** / **Read-only** | a read-only token is refused any POST, PUT, PATCH or DELETE, and any writing MCP tool |
| reach | **All workspaces** / **Specific workspaces…** | the token acts only in the workspaces ticked. Over MCP the others are not even named to it — it is told a number and nothing more. The REST workspace list is not narrowed this way: `/api/workspaces` still answers with every workspace the account belongs to |

Create tokens under your avatar → **API tokens**. The value is shown once
(*"Copy this token now — it will not be shown again"*), and each row records when
and from which address it was last used. That address column earns its place:
the MCP endpoint also takes the token inside the URL, as `/mcp/<token>`, because
many clients offer nowhere to put a header — and a token that travels in a URL
cannot be kept secret. Noticing an address you do not recognise is the defence,
and **Revoke** is one click away.

Anything that is administration of the instance or of an account needs a
**browser sign-in** and refuses a token, with the message *"This action requires
signing in through a browser — an API token is not enough."*:

| Not reachable with a token | Route |
| --- | --- |
| the instance backup | `/api/admin/backup` |
| accounts: list, create, change, deactivate, delete | `/api/users` |
| workspace membership from user management | `/api/admin/membership` |
| two-factor settings | `/api/2fa` |
| issuing or revoking API tokens | `/api/tokens` |
| changing instance settings, mail and the tunnel; webhooks either way | `/api/settings`, `/api/webhooks` |
| invitations | `/api/invites` |
| writing or dismissing workspace rules | `/api/workspaces/{id}/rules`, `/api/workspaces/{id}/rules-proposal` |
| language and time settings | `/api/me/prefs` |
| discarding a page's raw note trail | `/api/pages/{id}/notes` |
| approving an agent's sign-in, and revoking a grant later | `/api/oauth/approve`, `/api/oauth/grants` |

Two edges of that table are worth stating plainly, because "administration needs
a browser" is a summary, not the rule.

**The instance settings can still be read with a token.** Only writing them
needs a session. A token whose account is an instance admin reads the settings
object: instance name, sign-up policy and allowed domains, SMTP host, port, user
and sender, the OIDC client ids. No password or secret is ever in that answer —
only whether one is set. Webhooks are closed in both directions.

**A workspace's own membership is not closed.** `/api/admin/membership`, the
route behind **Workspace access** in **Manage users**, needs a browser. The
workspace's own member endpoints do not: a read-write token whose account is a
workspace admin there can add, re-role and remove members of that workspace.

The backup is the clearest case for the rule: it is a GET, so before it existed
a *read-only* token reached every workspace, every file and every password hash.
The workspace rules are the second: agents are told to follow them, so anything
holding a mere token must not be able to write them, or the rules channel
becomes the injection channel. An agent may **propose** rules over MCP
(`propose_workspace_rules`), but only where its account is a workspace admin — a
member's connection is refused outright. Applying a proposal, and dismissing one
without applying it, stay in the browser. A pending proposal is invisible to
everybody but the workspace admins: the draft is stripped out before the
workspace description leaves the server. An agent can withdraw its **own**
pending draft and not somebody else's.

A token also cannot create a workspace if it is limited to particular ones —
*"This connection is limited to particular workspaces, so it cannot create new
ones — it would not be able to open them."* A credential that widens its own
reach is not a boundary.

### What the workspace itself allows

A workspace can be stricter than the credential that arrives. **Workspace
settings → What agents may do here**:

| Setting | Meaning |
| --- | --- |
| **Anything they were granted** | any connection given this workspace (the default) |
| **Only signed-in connections** | a permanent API token is refused, even one naming this workspace; only a credential somebody signed in for gets through |
| **No agents at all** | browser sessions only |

A browser session is never limited by this — the person setting it is the one it
would be turned against. Over MCP, a workspace an agent may not enter does not
appear in its workspace list at all; the agent is told only how many were
withheld, never their names. See [Agent access](agent-access.md).

### Signing an agent in instead of handing it a key

An agent can sign in through the real browser rather than carry a permanent
token. The approval screen is headed **Grant access?**, names the instance and
the address it belongs to, and states what the connection would be allowed to
do: *read pages*, or *read and change pages*. Under **Where** it asks for
**Every workspace, including ones added later** or **Only the ones I pick** —
nothing is ticked to begin with, and **Allow** stays dead until something is.
The screen also says out loud that the name in front of you is a claim:
*"That name was chosen by whoever set up the connection. Only continue if you
started this yourself."*

This is the credential **Only signed-in connections** is about: it is
short-lived, it was chosen on a screen rather than in a settings dialog, and it
can be taken back. The grants an account has given are listed and revoked
through `/api/oauth/grants`. See [Agent access](agent-access.md).

### Asking before acting

Two MCP tools answer the permission question directly, which is cheaper than
attempting a write and reading the failure:

- `whoami` — the account, the token scope, which workspaces this connection
  reaches, and a list of what is deliberately not available over MCP.
- `get_permissions` — for one page: `can_read`, `can_write`, `can_delete`,
  whether it is in the trash, and `read_only_reason`, which says either
  *"this API token is read-only"* or *"you are a viewer in this workspace"*.

A page you may not read answers `page "…" not found` rather than "forbidden" —
the same answer a page that does not exist gets, so that ids cannot be probed.
See [MCP tools](mcp-tools.md).

## Emergency access

The owner can look inside a workspace they are not a member of — with a reason on
the record, for two hours, read-only, and visible to the people in charge of that
workspace. It is the honest alternative to quietly adding yourself.

1. Open **Manage users** and select **yourself**.
2. Under **Workspace access**, the workspaces you are not in show an **Emergency
   access** button.
3. Give the reason. It is mandatory and at least ten characters: *"Please give a
   reason somebody can follow (at least 10 characters) — it is logged and shown
   to the people in charge of this workspace."*
4. A toast confirms until when: *"Read access to “Northwind” until 16:40 — the
   people in charge have been told."*

What it does and does not do:

- **Read only.** It is not a role: writing, commenting and live editing are
  refused. The collaboration socket refuses every writing frame, so nothing gets
  in that way either — though a socket that is already open goes on receiving
  the document until the page is reloaded.
- **Two hours**, then it lapses on its own.
- **Every workspace admin is emailed**, naming the reason.
- **It is in the log twice**: in the instance
  [activity log](history-and-audit.md) and in the workspace's own **Emergency
  access log** (**Workspace settings → Emergency access log**), which lists who
  looked in, when, why, and whether the grant is still running, with **End it
  now** beside a live one. That row is shown to the instance owner only.
- **Refused for a personal space**, and refused if you are already a member:
  *"You are already a member of this workspace — emergency access is not
  needed."*
- **Ends when the instance is handed over**, so a departing owner does not keep
  reading for another two hours.

Two things about the shape of it that are easy to get wrong. The workspace does
**not** join your switcher while the grant runs — it is not a membership. Its
pages turn up in the [library](library.md) and in [search](search.md), and that
is where you read them. And "read only" means no writing, not "no copy": the
workspace export accepts a running grant exactly as it accepts membership, so a
complete archive can be taken while the two hours last.

## Where the boundary is not

Five honest limits, worth knowing before you rely on the model:

**Uploaded files are served to any credential that gets past sign-in.** A file
lives at `/files/<random-name>`. The name is unguessable, nothing lists it, and
directory listings are refused so the names cannot be enumerated — but the
server does not check which workspace the fetcher belongs to, and a *read-only*
API token is enough. The file *list* (**Workspace settings → Files**) does check,
page by page. Do not treat the URL of an upload as private. See
[Files](files.md).

**A public share link needs no account at all.** Anyone holding it reads that one
page — never its children, never anything it links to. Expiry and a password are
optional. See [Sharing](sharing.md).

**A public form link is a write door.** A shared [form](forms.md) lets anybody
holding the link create a row in that collection with no account whatsoever.
Submissions are rate-limited per address and every value is checked against the
collection's schema, and nothing in the collection can be read back through it —
but data goes IN without a sign-in, which is the opposite direction from every
other public link.

**The calendar subscription link is unauthenticated.** The feed at
`/ics/<token>` serves dates out of the pages the account can read to whoever
holds the address, with no sign-in — that is exactly what makes it work in a
calendar app. It survives a password change and a role change. It is invalidated
by deactivating or deleting the account, or by issuing a new one. See
[Account](account.md).

**Whoever runs the server can read everything.** The database is one SQLite file
on a machine somebody controls, and the backup contains every workspace. The
permission model governs the application, not the filesystem — which is exactly
why emergency access exists as a logged, time-limited, announced act rather than
as a quiet lookup.

## Related

- [Workspaces](workspaces.md) — members, rules, agent access, blueprints
- [Sharing](sharing.md) — public pages and public forms
- [Agent access](agent-access.md) — signing an agent in instead of handing it a key
- [Administration](administration.md) — the instance settings behind these roles
- [History and audit](history-and-audit.md) — where each of these acts is recorded
- [Account](account.md) — two-factor, your own tokens, the calendar link, leaving
