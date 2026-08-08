# Agent access

An agent reaches salt.md with a credential that belongs to a person. This page
covers the two kinds of credential — a permanent API token and a connection
somebody signed in for — what each can be narrowed to, what a workspace may say
about agents regardless of who issued the credential, what an agent inherits
from the human it belongs to, and how to take access away again.

If you only want to plug an agent in and start working, see
[Agents](agents.md) and [MCP tools](mcp-tools.md). This page is about the
boundary around that.

## The two ways in

Both end up at the same MCP endpoint and the same REST API. The difference is
what the credential is and how long it lives.

| | API token | Signing in |
| --- | --- | --- |
| What it is | A secret string starting `salt_`, created once and valid until revoked | A short-lived access token the client renews by itself |
| Where it travels | An `Authorization: Bearer` header, or inside the address as `/mcp/<token>` | A header only — nothing secret is ever in the address |
| Lifetime | Until somebody revokes it | The access token lasts one hour; the connection behind it stays until it is ended |
| Who chooses the reach | Whoever creates the token, in a settings dialog | The person, on a consent screen, while looking at what they are granting |
| Set up in | Account menu → **API tokens** | The client asks; you approve in the browser |
| Works with | Every client, including ones with nothing but a URL field | Clients that support OAuth sign-in for MCP |

Neither is a lesser kind of access: both carry the full identity of the account
they belong to, and both can be narrowed the same two ways (see
[What a credential can be narrowed to](#what-a-credential-can-be-narrowed-to)).
What differs is how easy it is to take back, and whether the secret leaks by
sitting in a URL.

## Connecting an agent

1. Open the account menu at the bottom of the sidebar and choose
   **Agents & MCP**. The dialog is titled **Connect an agent**.
2. Pick one of the two cards at the top: **Sign in** ("Nothing secret in the
   address. Expires and can be ended.") or **Token in the address** ("For
   clients that only have a URL field. Treat it like a password.").
3. Pick your agent from the gallery — **Claude (App & Web)**, Claude Code,
   ChatGPT, OpenAI Codex, Cursor, OpenClaw, Hermes Agent, Gemini CLI, or
   **Other agent**.
4. Press **Copy** and paste the snippet where that client wants it. The hint
   beside the snippet says where that is for the agent you picked.

With **Sign in** the snippet is the plain address, with no secret in it. The
client discovers on its own that it can sign in — the endpoint answers an
unauthenticated call with a pointer to the sign-in service — and sends you to
the browser to approve. If a client asks for a token instead, it cannot sign in
yet; use the other card.

With **Token in the address** the dialog creates the token for you: choose
**Read & write** or **Read only**, and **Only "<workspace>"** or
**All workspaces**, then press **Create token**. The token appears once and is
filled into the snippet. You can also paste an existing token into the field
below.

**The token this dialog makes is an ordinary API token, named `agent`.** It
appears in the account menu → **API tokens** list beside any you made by hand,
and that list is where you revoke it. The two dialogs are two doors into the
same drawer; nothing here is a separate kind of credential.

The dialog warns you if the address it is offering is a plain-HTTP address that
is not on this machine: a cloud agent cannot reach that. Making the instance
publicly reachable is covered in [Domain and proxy](domain.md).

At the bottom, **Download skill** produces a file that teaches the agent how
your team works here — see [The skill](skill.md).

## Signing in: the consent screen

When a client asks to sign in, the browser lands on a screen headed
**Grant access?**. It shows, in order:

- The instance's name and the host you are on, so you can see *which* server you
  are about to hand something to.
- The client's name, as "*<name>* is asking to work in your account."
- A warning that the name is a claim: "That name was chosen by whoever set up
  the connection. Only continue if you started this yourself."
- **It will be allowed to** — "read and change pages" or "read pages".
- **Where** — either "Every workspace, including ones added later" or "Only the
  ones I pick". The list underneath starts with nothing ticked, and **Allow**
  stays dead until you tick something.

The two answers under **Where** are genuinely different, and the difference
bites later. "Only the ones I pick" is a photograph of today: a workspace made
next week — by a colleague, or by the agent itself — is not in it, and the
connection will not cover it. "Every workspace, including ones added later"
follows along.

**Deny** answers the client properly rather than leaving it hanging.

### Where the client's name comes from

Any program can register itself as a client, with no human involved and nobody's
permission: it posts a name and the addresses it wants the answer sent to, and
gets an id back. That is how MCP clients are expected to work, and it is safe
for one reason — a registered client can do **nothing** on its own. It can only
ask a signed-in person for consent, and whatever it gets is bound to that
person's own permissions. Registration creates an applicant, not an account.

Two consequences worth knowing:

- **The name on the consent screen is unverified.** Whoever registers picks it,
  up to 80 characters. That is why the screen says so out loud instead of
  presenting the name as an identity. If you did not start the sign-in yourself,
  the name proves nothing.
- **There is no list of registered clients and no way to delete one.** The
  handle you keep is the *grant* — the connection on your account. Ending that
  ends the access; the applicant may still exist, and still cannot do anything
  without somebody approving it again.

Approving requires being signed in with a browser session. A credential can
never approve a new credential, so an agent cannot enlarge its own reach by
asking itself for consent.

What the client gets back is an access token good for one hour plus a renewal
token it uses in the background, so nobody signs in again every hour. The
renewal token changes on every use — the previous one stops working at that
moment.

The grant is written to the activity log under your name, with the client's
name and the scope. See [History and audit](history-and-audit.md).

## API tokens

Account menu → **API tokens**.

The list shows each token's name, a badge reading **read-only** or
**read-write**, the workspaces it may reach (or "all workspaces"), when it was
last used, and — this is the useful one — **the address it was last used from**.
A token that rides in a URL cannot be kept secret; it sits in the client's
configuration and in the logs of every proxy between. The defence is noticing.
An address you do not recognise beside a token is a question worth asking, and
the answer is the ✕ button (**Revoke**) on the same row.

To create one, fill in the row at the bottom:

1. A name — the placeholder suggests something like the agent's own name.
2. **Read-write** or **Read-only**.
3. **All workspaces** or **Specific workspaces…**, which unfolds a list of
   tick-boxes.
4. **Create token**.

The token is shown exactly once, with **Copy token** beside it and a
ready-to-paste connect command under **Copy MCP command**. It is stored hashed;
nobody, including the owner of the instance, can read it back. Lose it and you
make a new one.

Two rules worth knowing:

- A token can only name workspaces you can currently reach — the ones you are a
  member of, plus any workspace you hold a running emergency grant on. Ids you
  cannot reach are dropped.
- If you asked for specific workspaces and none of them survive that filter, the
  token is **refused** rather than created. Storing an empty list would read back
  as "all workspaces", and a deliberately narrow token must never quietly become
  the widest one.

Tokens can only be created, listed and revoked from a browser session. An agent
cannot mint itself a second, wider key.

## What a credential can be narrowed to

Exactly two dimensions, and they are the same for a token and for a signed-in
connection.

### Scope: reading or reading and writing

A read-only credential is refused at two places. Over the REST API every
POST, PUT, PATCH and DELETE comes back as `token is read-only`. Over MCP the
writing tools are refused by name — `create_page` and the rest answer "this API
token is read-only; … requires a write token" — while the reading tools work
normally.

In the interface the tooltip on the choice spells out what read-only costs:
"Read-only tokens cannot create, edit, delete or upload". **Uploading is the one
people are surprised by** — attaching a file is a write, so a read-only agent
that can search your whole instance still cannot put a PDF into it.

Tools that do both, like page history and comments, are judged per action:
reading a revision is a read, restoring one is a write.

The two ways in default differently when the request is unclear, and it is worth
knowing which is which:

- **Signing in.** A client asks for a scope as a list of words. Unknown words
  are ignored rather than refused, and what is left decides. A request that
  names nothing recognisable lands on read-only, never on read-and-write.
- **An API token.** The other way round: anything that is not exactly `read`
  creates a read-write token. The dialogs always send one of the two, so this
  only bites somebody creating a token by hand over the API — a typo there gives
  you the wider token, not the narrower one.

### Workspaces: all of them, or a fixed list

A credential is either unrestricted (every workspace the account is a member of,
including ones created later) or bound to a list.

For a bound credential:

- Every listing an agent gets over MCP shows only the granted workspaces.
- Naming a page in a workspace outside the list answers "not found" — the same
  answer as for a page that does not exist, because saying "you may not" would
  confirm it exists. This holds over MCP and over the REST API alike, for
  reading and for writing.
- Asking for a workspace by id gives a clearer message, since the account
  already knows that workspace exists: "…is outside what this connection was
  granted — ask for it to be added, or name one it can reach."
- The workspace list an agent gets back says how many further workspaces were
  withheld, without naming them.
- **Creating a workspace is refused**: "This connection is limited to particular
  workspaces, so it cannot create new ones — it would not be able to open them."
  Adding the new workspace to the list automatically would be a credential
  widening its own reach.

One list is not narrowed this way: `GET /api/workspaces`, the plain REST list
the browser uses, returns every workspace the *account* is a member of, with its
name and role, whatever the credential was granted. The MCP list is the one that
was built to withhold — treat the REST list as showing the account, not the
credential.

## What a workspace decides

Everything above is decided by whoever issues the credential. A workspace
holding confidential material had no say in that — it could only hope that every
token ever minted happened to leave it out. It has a say now, and it is opt-in:
the default is exactly the behaviour that existed before, so a workspace nobody
configures behaves as it always did.

A workspace admin sets it in the workspace menu → **Workspace settings** →
**Access** → **What agents may do here**:

| Option | Hint in the dialog | What it does |
| --- | --- | --- |
| **Anything they were granted** | "Any connection that was given this workspace." | The default. Any credential that names this workspace gets in. |
| **Only signed-in connections** | "A permanent token is refused, even one naming this workspace. For confidential material." | A permanent API token is turned away here whatever it says. A connection somebody signed in for is let in. |
| **No agents at all** | "Browser sessions only." | No agent may read this workspace, and it disappears from every list and search an agent gets. |

Three things about it:

- **People in browsers are never affected.** The setting is about agents; the
  person who sets it is not the one it is aimed at.
- **An unrecognised value reads as the default.** A typo in the setting takes
  nothing offline.
- To an agent, a workspace it may not enter is not there in anything it looks
  at: it drops out of the workspace list, out of the page list, and out of
  search results.

Changing the setting is recorded in the activity log, because "why can the agent
suddenly not read this" is a question somebody asks weeks later.

### What the setting does not do yet

The rule governs everything an agent finds by *looking* — lists, search, the
workspace list, over MCP and REST alike — and it also stops a credential bound
to a list of workspaces from naming a page id directly. Two gaps are left, and a
reader deciding where to put confidential material should know them: a
credential granted **all** workspaces is still served a page here when it names
the id, and a write sent straight to a page id over the REST API is still
carried out.

So today the setting is a reliable answer to "an agent must not *find* this
workspace" and not yet a complete answer to "an agent must not touch this page,
even holding its id". The credential's own workspace list is the part that holds
in every case: a token created for one workspace cannot reach another by any
route above. Set both — the workspace's rule and a narrow credential — and the
gap closes.

**Who can change the setting** is worth saying too, because the answer is not
"only a person". It is a workspace-admin decision, and a credential belonging to
a workspace admin carries that role like every other: a read-write token can
switch a workspace from **No agents at all** back to **Anything they were
granted**. The change lands in the activity log either way. If that matters, the
lever is not to give a workspace admin's account a read-write credential — see
[Permissions](permissions.md) for who is an admin of what.

Note that "Only signed-in connections" is usually the better answer than "No
agents at all" for confidential material: **No agents at all** keeps agents out
entirely, while **Only signed-in connections** lets them in on a credential that
expires by itself and can be ended.

## What an agent inherits from its human

A credential carries the **full identity** of the account it belongs to, and can
only ever narrow it. That means:

- The same workspaces, the same role in each, the same private pages. A viewer
  stays a viewer; an agent belonging to a viewer cannot write anywhere in that
  workspace. See [Permissions](permissions.md).
- Pages that are private to somebody else are as invisible to the agent as they
  are to its human.
- **Emergency access flows through too.** While the instance owner holds a
  running emergency grant on a workspace they are not a member of, their agent
  can read pages in it — for the two hours the grant lasts, and read only.
  Emergency access is logged and visible to the people responsible for that
  workspace; it is not a quiet door.
- Everything the agent does is attributed to that person.

### What the activity log actually distinguishes

Work an agent does **over MCP** is marked **agent** in the activity log, so it
is not mistaken for a colleague's edit. The same account working over the
**REST API** is recorded as **human**. The log distinguishes the route, not the
operator — an agent driving the REST API looks exactly like the person it
belongs to.

Two more gaps in the same place, so nobody plans around a record that is not
there: changing a page over REST (`PATCH /api/pages/{id}`, the ordinary "edit"
call) writes no activity entry at all — page history is what records that, see
[History and audit](history-and-audit.md) — and a `note` written over MCP is
deliberately kept out of the log, because the page's own trail already holds it
in front of exactly the people allowed to see the page.

### What is out of reach

Administration is deliberately out of reach of every credential, however wide,
because a key to content must not be a pass for running the instance. These need
a browser sign-in and are refused with "This action requires signing in through
a browser — an API token is not enough.":

- creating, changing, deactivating or deleting accounts, and the account list
  (`/api/users`)
- API tokens themselves — listing, creating, revoking (`/api/tokens`)
- ending signed-in connections (`/api/oauth/grants`)
- two-factor settings (`/api/2fa`)
- the instance backup (`/api/admin/backup`), and **changing** instance settings,
  mail settings, the tunnel, invitations and webhooks
- **applying** workspace rules (`/api/workspaces/{id}/rules`) — an agent may
  submit a draft with `propose_workspace_rules`, but the rules it is told to
  follow must not be writable by whatever holds a key, or the rules channel
  becomes the injection channel
- personal language and time settings (`/api/me/prefs`) — otherwise an admin's
  token could set another person's clock format

Three things that sound like they belong on that list and do not:

- **Reading the instance settings.** An instance admin's credential — read-only
  included — can read `/api/settings` and the instance overview: SMTP host and
  user, the from-address, allowed sign-up domains, the public address, sign-in
  client ids, version and counts. Every secret in them travels as a yes/no, not
  as a value, so nothing that could be used elsewhere is handed over. Changing
  any of it still needs the browser.
- **Workspace membership and roles.** There is no MCP tool for it, and `whoami`
  names it among the things an agent cannot do — true of MCP. Over the REST API
  it is not gated the same way: a read-write credential belonging to a workspace
  admin can add, change and remove that workspace's members, including making
  somebody an admin. Instance-wide membership (`/api/admin/membership`) does
  need a browser.
- **The list of people.** `/api/users` is out of reach, but an agent over MCP
  can call `list` with `kind: "users"` and get everyone it shares a workspace
  with, by name and email address. `whoami` says so itself. Treat colleagues'
  addresses as something every connected agent can see.

### What an agent can ask about itself

`whoami` is the tool an agent is told to call first, and it answers more than
its name suggests:

- the account (id, name, email), the scope, and whether it may write
- which workspaces the credential may reach, or "all workspaces you are a member
  of"
- `not_available_via_mcp` — the list of things this route deliberately cannot
  do, as concrete strings, so an agent reads a refusal correctly instead of
  retrying
- a short instruction to announce its work: call `working_on` before starting on
  a page and again with `done: true` at the end, so a person watching sees who
  is on what, live

`get_permissions` answers the same question for one page — can I read it, can I
write it, is it in the trash, what is my role here — including *why* writing is
refused when it is ("this API token is read-only", or "you are a viewer in this
workspace").

## Ending access

**An API token**: account menu → **API tokens** → the ✕ on its row. It stops
working immediately.

**A signed-in connection**: most clients have a disconnect of their own. It
calls the revocation endpoint (`/oauth/revoke`), which takes the connection and
every token minted from it at once — hand it either the renewal token or a
current access token. It always answers success, whether or not the token
existed: telling a caller which is which would turn it into a way of testing
guesses.

On the salt.md side the connections on your account are readable at
`/api/oauth/grants` and can be ended with a DELETE on
`/api/oauth/grants/{id}` — both from a signed-in browser session only. **The
consent screen promises this lives in your account settings; today it is the
API, not a screen.**

What that list holds, per connection: the client's name, the scope, the
workspaces it was granted, when it was created, when it was last used, and the
address it was last used from. That last pair is the same "notice a stranger"
material the API token list makes a point of — an address you do not recognise
is worth a question.

Two consequences of other actions, and the difference between them matters when
you are ending access in a hurry:

- **Changing your password** ends every session and every API token on the
  account at once. It does **not** end signed-in connections — those survive
  untouched and go on renewing themselves. If you are changing the password
  because you think something leaked, end the connections separately.
- **Deactivating an account** deletes its sessions and API tokens, and every
  credential belonging to it is refused at the door from that moment — over REST
  and over MCP alike. Signed-in connections are refused too, but not deleted:
  reactivating the account brings them back to life, while the API tokens are
  gone for good.

Guessing at tokens is throttled per address, and only failures count: an agent
making hundreds of calls a minute with a good token is never slowed by it.

## Limits an agent runs into

Not permissions, but the same class of surprise — an agent stops and the reason
is not in the answer it expected.

- **The MCP endpoint accepts POST only.** A GET gets "MCP endpoint accepts POST
  only". A client that browses to the address to "check it" learns nothing from
  that.
- **Tool calls are rate limited per account**, generously — a runaway loop is
  what this is for, not normal work. Over the limit the answer is "rate limit
  exceeded — too many requests, slow down".
- **A large upload has to go over HTTP, not MCP.** The MCP endpoint refuses an
  oversized request before reading it, and the refusal names the alternative:
  `/api/upload`. Base64 inflates a file by a third on the way in, so the
  practical ceiling over MCP is well below the instance's upload limit. The
  refusal is deliberate and it is not about permissions — reading such a request
  into memory is what took an instance down once.
- **JSON-RPC batches are not supported**; send one call per request.

## The desktop app is not an agent

The desktop app signs in through your real browser and ends up with an ordinary
browser session, not an agent credential — so none of the workspace rules above
apply to it, and it can do everything you can do.

The flow: the app opens your browser, you sign in as usual (password, or your
company sign-in — see [Signing in with your company account](sso.md)), and a
page appears headed **Sign in to the desktop app?** showing which account it
would use. Press **Allow** and the browser hands control back; **Not now**
cancels. The confirmation step is not ceremony: without it, any page you open
could send your browser through this flow and mint a session for a program
waiting on the other end.

The code that travels back is single use, expires after five minutes, and is
worthless to anybody but the app that started the request. Details in
[The desktop app](desktop-app.md).

## Messages you may see

| What the agent gets | What it means |
| --- | --- |
| `missing or invalid API token` | No credential, or one that is not valid any more. The answer also tells a capable client where to sign in. |
| `token is read-only` | A read-only credential tried to change something over the REST API. |
| "this API token is read-only; … requires a write token" | The same, over MCP, naming the tool. |
| "page … not found" | Either it does not exist, or it is outside what this credential may reach. The two answers are deliberately identical. |
| "workspace … is outside what this connection was granted" | The account has that workspace; this credential was not given it. |
| "This connection is limited to particular workspaces, so it cannot create new ones" | A credential bound to a list tried to make a workspace. |
| "This action requires signing in through a browser — an API token is not enough." | Administration. No credential reaches it. |
| "this account has been deactivated" | The person behind the credential can no longer sign in. |
| "rate limit exceeded — too many requests, slow down" | Too many tool calls in a minute from this account. |
| "…the limit is N MB; for a file this size use the HTTP upload at `/api/upload`" | The request was too big for the MCP endpoint. |

## See also

- [Agents](agents.md) — what an agent does once it is in
- [MCP tools](mcp-tools.md) — the catalogue
- [The API](api.md) — the same permissions over REST
- [Permissions](permissions.md) — roles, private pages, emergency access
- [Workspaces](workspaces.md) — members, rules, settings
- [Your account](account.md) — tokens, two-factor, the activity log
