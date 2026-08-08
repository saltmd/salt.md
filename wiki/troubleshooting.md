# Troubleshooting

Real failures, what they look like from the outside, and what actually fixes
them. Every entry names a symptom first, because that is all you have when
something goes wrong. If you know which area you are in, the other pages go
deeper: [Agent access](agent-access.md), [Sharing](sharing.md),
[Search](search.md), [Files](files.md), [Single sign-on](sso.md),
[Trash and recovery](trash-and-recovery.md),
[Self-hosting](self-hosting.md).

## How salt.md tells you something failed

Three places, and it is worth knowing which one you are looking at.

- **Toasts** — a line at the bottom of the window, prefixed with ⚠, gone after
  four seconds. This is the app's only feedback for a failed save, a failed
  upload or a failed background call. If you looked away, you missed it.
- **The line just above the Sign in button** — everything that goes wrong while
  signing in, including failures handed back by Google or Microsoft. It is
  inside the form, below the email, password and 2FA fields, so a short window
  can hide it under the fold.
- **The text an agent gets back** — over MCP a failure comes back as the tool's
  result, marked as an error, in English.

**Many server messages carry a machine-readable code beside the English
sentence — and many do not.** Where there is a code, the browser renders the
sentence in your language while a script, `curl` or an agent sees the English;
the code is the stable part, because it does not change when the wording does,
and it is the thing to quote when asking for help. Sign-in, permissions,
workspace membership, emergency access and mail carry codes. A great many other
failures — a malformed upload, a page that cannot be read, a refused token —
arrive as a plain English sentence and nothing else. If the message in front of
you has no code, quote the sentence; the table at the end of this page lists the
codes you are most likely to meet.

**"Not found" often means "not allowed".** A page you may not read and a page
that does not exist answer identically, on purpose — otherwise guessing ids
would reveal what exists. So *page not found* from an agent is as often a
permission problem as a typo. `whoami` separates the two.

## Signing in

### Wrong email or wrong password

Code `bad_credentials`. It is deliberately one message for both cases, so the
answer does not reveal whether an address has an account here. Passwords are at
least 8 characters; nothing else about them is enforced.

### It asks for a 6-digit code

Code `2fa_required`. Two-factor sign-in is on for that account. The form grows a
**2FA code (6 digits)** field and keeps the password you already typed. A wrong
code is `2fa_invalid`.

### You lost the authenticator app

This is the one to plan for, because there is no way out through the interface.
There are no recovery codes, and **nobody — not an admin, not the instance
owner — can clear somebody else's second factor.** Turning it off requires a
valid code from the app that is gone.

Two things still work:

- **Signing in with Google or Microsoft**, if that is configured and the
  account's address is one of them. The provider route does not ask for the
  second factor.
- **Direct access to the database on the server**, which means someone with a
  shell on the machine.

Before turning two-factor on, save the secret shown next to the QR code
somewhere safe. See [Your account](account.md).

### "This account has been deactivated — talk to an admin"

Code `account_disabled`. Deactivation is immediate and total: it ends every
session, deletes every API token belonging to that account, invalidates its
calendar link, and closes its open editors on the spot. Nothing that account had
open keeps working. An admin reactivates it in user management; the tokens do
not come back. See [Administration](administration.md).

### "too many login attempts, please wait"

A 429. Sign-in is throttled per client address: 30 attempts a minute with a
burst of 10.

The trap is a reverse proxy. Without **Run behind a reverse proxy (trust
`X-Forwarded-For`)** switched on, every request appears to come from the proxy,
so the whole organisation shares one bucket and a single person retyping their
password locks out everybody. The switch is on **Instance settings → Domain &
proxy**. Whether it is taking effect is shown on a different tab:
**Instance settings → Maintenance**, in the **Instance** panel, the row **Your
IP (as the server sees it)** reads *proxy headers active* when the header is
being trusted.

Only switch it on when there really is a proxy in front. Without one, the header
is set by whoever is calling and an attacker can invent a new address per
attempt.

Every rejected credential also leaves one line in the server's own log, naming
the address and the kind of credential — never the email and never the token.
That line is what a jail like fail2ban reads. It is not in the
[activity log](history-and-audit.md), which is about who did what after signing
in.

### Signing in with Google or Microsoft fails

[Single sign-on](sso.md) has the full table of messages. The one that catches
people out and is not obviously an SSO problem:

**"This address belongs to an account that has not confirmed it."** — code
`oauth_email_squatter`. An account holds that address but it does not count as
confirmed. Editing the address on an account is what un-confirms it — your own
in your profile, or somebody else's if you are the instance owner — and
**changing it back does not restore it**: an address edited after the account
existed stays unconfirmed. Sign in with a password instead. The same message
also appears for a deactivated account.

### Signed out for no reason

Sessions last as long as **Sign-in session length (days)** on the **General**
tab — 90 days by default, 1 to 365 allowed.

Beyond expiry, a session ends when you sign out, when an admin deactivates the
account, when the server's database is replaced under it, and — the one nobody
expects — **when the account's password is changed**. A password change deletes
every session and every API token that account had. The browser where the change
was made is handed a fresh cookie straight away, so the person who changed it
stays signed in while every other device and every agent is thrown out. That is
exactly the shape of "I was signed out everywhere except here".

Any request that comes back unauthorised sends the whole interface back to the
sign-in screen at once. That is deliberate: an expired session used to mean an
upload landing as an error in the middle of a document while the app went on
pretending you were signed in.

## Agents that cannot connect

### The client asks for a token, but you wanted it to sign in

Paste the address without a token — the plain `/mcp` form. A client that can
sign in discovers the authorization server from the refusal itself and sends you
to a consent screen in the browser. A client that asks for a token instead
cannot sign in yet; use **Token in the address** in the **Connect an agent**
dialog and give it `/mcp/<token>`.

### "missing or invalid API token"

The credential is absent, mistyped, revoked, belongs to a deactivated account,
or was wiped when that account's password was changed — a password change
deletes every API token the account had, so an agent that worked yesterday can
stop without anybody revoking anything by hand.

Note that **wrong tokens are throttled by address**: about twenty bad attempts
in quick succession from one address make the server stop looking tokens up at
all for a moment, and during that moment a *correct* token is refused too. It
refills within seconds. A working token never feeds the limit.

### Checking, and cutting off, what is connected

Two different things can hold access to your account, and they are listed in
different places.

**API tokens** — the menu under your name, **API tokens**. Each row shows the
token's name, whether it is *read-only* or *read-write*, which workspaces it
reaches (or *all workspaces*), when it was last used, and **the address it was
last used from**. That last one is the whole defence for a token that travels
inside a URL like `/mcp/<token>`: such a token cannot be kept secret, so the
protection is noticing. An address nobody recognises is a question worth asking,
and the answer is the ✕ (**Revoke**) button at the end of the row. Revoking is
immediate and cannot be undone — issue a new token instead.

**OAuth grants** — an agent that signed in through the browser instead of
pasting a token holds a grant rather than a token, and it will not appear in the
token list. There is no dialog for these yet; they are read and removed over the
API, signed in with a browser session: `GET /api/oauth/grants` lists them (the
client's name, the scope, the workspaces, when and from where it was last used)
and `DELETE /api/oauth/grants/{id}` disconnects one. That deletes the grant and
every access token minted from it at once.

### A workspace is missing from the agent's list

Three separate gates, in this order:

1. **The account** is not a member of it. Nothing else can help.
2. **The credential** was narrowed to particular workspaces. A workspace-scoped
   token or an OAuth grant where somebody ticked specific workspaces sees only
   those; the agent is told how many it cannot see, never which.
3. **The workspace itself** decides what agents may do there — **Workspace
   settings → What agents may do here**. *Only signed-in connections* refuses a
   permanent token even when the token names that workspace. *No agents at all*
   refuses every credential, browser sessions only.

`whoami` answers the first two: it reports the token scope, the workspaces the
credential may reach, and what is deliberately closed to agents. See
[Agent access](agent-access.md).

### A write is refused

- *"this API token is read-only; … requires a write token"* — the credential is
  read-only. Create a new one with **Read & write**, or approve the connection
  with write scope on the consent screen.
- *"This action requires signing in through a browser — an API token is not
  enough."* (`session_required`) — account management, backups, tokens,
  emergency access and the instance settings are closed to every credential on
  purpose. An API token is a second key to content, not an admin pass.
- *"This connection is limited to particular workspaces, so it cannot create new
  ones"* (`workspace_scoped`) — a narrowed credential cannot make a workspace it
  would then be unable to open.

### "rate limit exceeded — too many requests, slow down"

240 tool calls a minute per account, with a burst of 60. It comes back as a tool
error rather than a transport failure, so a well-behaved agent can simply wait
and retry.

### "request is … MB — the limit is … MB"

An MCP request is refused before it is read, by its declared size. Base64
inflates a file by a third, so the ceiling for a whole tool call is the
instance's upload limit plus that overhead plus a megabyte for the JSON around
it — with the default 50 MB upload limit, 67 MB. The message says what to do
instead: use the HTTP upload at `/api/upload` for a file that big.

A file that fits inside the request but is still over the upload limit gets a
different sentence from `upload_file` itself — *"file is … MB — the limit is …
MB"* — naming both the file and the limit as it currently stands.

### The agent said it was importing and nothing appeared

An import started with `import_url` runs as a job, and the tool that answers
"how far has it got" is `get_import_status`: it reports how many records are
written, whether it has finished, and any errors along the way. Poll it every
few seconds rather than re-running the import — a second import writes the
records a second time. See [Import and export](import-export.md).

### The agent insists a tool does not exist

Or calls one that was renamed and gets *unknown tool*. **A connected client keeps
the catalogue it fetched when it connected.** salt.md does not announce
catalogue changes, so a session that has been open across an update is working
from the old list. Reconnect the client. Calling the old name again only proves
the client is stale.

## Share links that do not work

### "This link is invalid or has expired"

A visitor gets a plain page with that sentence. Three things produce it:

- the link was revoked with **Stop sharing**;
- its expiry passed — an expired link is deleted the first time it is opened;
- **the page was shared again**. There is only ever one live read link per page.
  Changing the expiry or setting a password mints a new token and kills the old
  one, so a link already sent out stops working.

A page that has been moved to the trash or deleted gives a shorter page — just
*Not found*, with no sentence about the link. The link itself is still live, so
restoring the page brings it back. See
[Trash and recovery](trash-and-recovery.md).

### The link points at an address nobody outside can open

A share link is built from the instance's external address: an explicit **Public
base URL** first, then the built-in HTTPS domain, then a running Cloudflare
tunnel, and only if none of those exist, whatever address your own browser
happens to be using. If the link you copied contains a LAN address, none of the
first three is configured.

If you are relying on the built-in tunnel, its state is on **Instance settings →
Domain & proxy**, above the manual proxy section: *Tunnel starting…* while it
comes up, *Publicly reachable:* with the URL once it is up (with **Copy** beside
it), or *Tunnel error* carrying the reason it failed. **Stop** takes it down and
**Reset** clears an error so it can be started again. A quick tunnel gets a new
URL every time it starts, so every link generated under the old one is dead.
See [Reaching your instance from outside](domain.md).

### Images and attachments do not show for a visitor

A shared page renders as standalone HTML with no sign-in required, but the files
it references are served from a path that **does** require one. A visitor
without an account therefore sees the text and layout of the page and broken
images where the pictures are.

Exporting the single page does not solve this: both the HTML and the Markdown
export write the same file addresses the shared page writes, and neither packs
the bytes. The one export that carries the uploaded files with it is
**Workspace settings → Export workspace** (*Native archive — importable one to
one*), which puts the files inside the archive. See
[Import and export](import-export.md).

### A password-protected page keeps saying "Wrong password."

The password is checked against the token in the link, so it only works through
the exact link it was set with. If the link was re-created, its password went
with it.

### "Form not found" on a public form

The page says *This link is not valid or has been switched off*, and it is one
screen for several causes: the token is unknown or was revoked, the collection
is in the trash, the page behind the link is not a collection, or the collection
has no `form` view any more. A form link needs a live form view on that
collection to render — add one and the same link works again. See
[Forms](forms.md).

### A collection shared to the web shows a plain text table

That is what it is: a shared database renders as its Markdown table, rows only.
Sub-pages of rows are never included. Shared pages also carry a *noindex*
header, so search engines are asked not to list them. See
[Sharing](sharing.md).

## Search that finds nothing

Work through this in order.

1. **Is the page in the trash?** Trashed pages are removed from the index
   immediately, including everything under them. The **Trash** section sits at
   the bottom of the sidebar with a count beside it; the ↺ button on a row puts
   the page back, index and all. See
   [Trash and recovery](trash-and-recovery.md).
2. **Is it private to somebody else?** Search checks the workspace first and
   then every single hit again. The second check is what hides other people's
   private pages inside a workspace you are in.
3. **Are you in the right workspace?** Only workspaces you are a member of are
   searched at all.
4. **Is this an agent?** An agent is narrowed further, by its credential and by
   each workspace's agent rule.
5. **Edit the page.** Any write re-indexes it. If a page really did fall out of
   the index, typing a character and waiting a moment puts it back — the editor
   saves 1.5 seconds after you stop typing and again when you leave the page.

**The text inside a PDF is a separate matter.** It is only indexed when the file
is attached to a page, and only up to a size limit derived from the memory the
server believes it has. Exceeding it costs indexing and nothing else: the file
is still stored, listed and downloadable. The server log names the file when it
skips one. [Search](search.md) has the table of limits.

## Uploads that fail

### "File too large (…) — 50 MB max."

This one comes from the browser, before anything is sent, and **the 50 MB in it
is fixed**. The instance's own limit — **Max. file size per upload (MB)** on the
**General** tab — can be set anywhere from 1 to 2048 MB, but raising it above 50
does not raise what the browser will attempt. Files over 50 MB have to go
through an agent or the API (`/api/upload`) instead.

This is also the one message with a code the server never sends. The browser
mints `file_too_large` itself for its own cap, so an agent or a `curl` user will
never see that code come back — do not go looking for it in a response.

### "file too large — max 50 MB"

The server's own refusal, a 413, naming the instance limit as it currently
stands. The file was larger than **Max. file size per upload (MB)**. Raise the
setting, or send a smaller file.

### "The file is too large for this instance."

This one appears when the refusal did **not** come from salt.md: the browser
falls back to this sentence when the 413 it received is not the server's JSON.
In practice that means a reverse proxy in front, refusing with its own HTML
error page.

The usual cause is a stale body limit. The nginx configuration salt.md generates
writes `client_max_body_size` from the upload limit as it stood when you copied
it. Raise the limit in salt.md later and the proxy still refuses at the old
size — and salt.md never sees the request at all.

### "Upload failed" or "…" was not uploaded

Anything else: a lost connection, a full disk, a write that failed on the
server. Dropping several files at once uploads them one at a time and a failure
does not stop the rest, so four out of five landing plus one named failure is
the expected shape rather than a bug.

### The file uploaded but its contents are not searchable

An upload is indexed under the page it was attached to. A file with no page —
a cover image, a workspace logo, an avatar — has nowhere to be indexed and
counts as unreferenced in the file list. See [Files](files.md).

### A count in the file list looks wrong

The file index is derived: the truth is the block on the page and the byte on
disk. It is rebuilt from scratch at startup whenever a release changes how it is
built, and the startup log says so. There is no button for it — being derived is
what makes the rebuild safe, not something you have to ask for.

## Mail that never arrives

Invitations, and anything else the instance sends, go out through SMTP or
through a connected Google or Microsoft mailbox. Before hunting anywhere else,
press **Send test mail** — it is on **Instance settings → Email**, once beside
the connected provider and once above the SMTP fields. It sends to *your own*
address, so a success toast (*Test mail sent to … ✓*) proves the whole path in
one click.

A failure comes back in the same toast, and it is the most useful text you will
get: where the provider wrote the reason, that reason travels with the message
rather than being flattened into "sending failed". Two codes are worth knowing:
`mail_not_configured` means nothing is set up at all, and `mail_refresh_failed`
means the connected mailbox needs connecting again — **Disconnect**, then
connect it a second time. See [Email](mail.md).

## Webhooks that stopped firing

Every configured webhook is listed on **Instance settings → Webhooks** with its
address, the events it is subscribed to, and its last result: *last call:
`<status>` · `<time>`*, or *not called yet* if it has never fired.

That line answers the question directly. *Not called yet* on a hook you expected
to be busy means the events never matched, not that delivery failed. A status in
the 400s or 500s means the receiving end refused it. See
[Webhooks](webhooks.md).

## "A new version is available — reload the page"

The server was updated while your tab was open. The message arrives twice over:
once from the first request the page makes, and once from the live change feed,
so a tab that has been open for days still learns about a deploy. It is a toast,
so it disappears after four seconds — reload when you see it.

**If it appears on every load and never stops**, the frontend and the server
were built with different version strings. That is a build problem, not a
browser problem; the two are stamped from one value on purpose. See
[Self-hosting](self-hosting.md).

**If the interface stays old across reloads**, the browser is holding a cached
copy of the document that names the previous build's files. The document itself
is served as `no-cache`, so this resolves on the next load; a hard reload forces
it. The service worker keeps only the app shell — no API responses, no files, no
shared pages — so nothing you see as *data* can be stale that way.

## Live editing

**The faces of your colleagues vanished.** That is your connection, not theirs.
Editing continues into your own copy and is pushed across when the socket comes
back — automatically, with a backoff that starts inside a second and tops out at
thirty. The one way to lose that work is closing the tab while it is
disconnected.

**The editor reloaded itself in the middle of a sentence.** Something replaced
the page rather than merging into it: a restored version, an agent writing the
whole body with `write_content`, an import, or the page being trashed. An edit
typed in the same second can be lost. `working_on` exists so a person can see an
agent coming. See [Working at the same time](collaboration.md).

**Getting the old text back.** Every path that replaces a page body saves the
state from before as a version first — an agent's rewrite, an import, and a
restore itself, which means restoring is reversible too. Open **⋯ → Version
history** on the page, pick the entry you want and press **Restore**; you are
asked to confirm, and the current state is saved as a version before it is
replaced. An agent does the same through `revisions`. Two limits worth knowing:
at most one snapshot is taken per page every two minutes, so a burst of rewrites
leaves one version from before the burst rather than one per write, and only the
newest 50 versions of a page are kept. See
[History and audit](history-and-audit.md).

**"Page content not saved."** The debounced save failed. It is retried on the
next change and when the editor closes; the live document in the browser is
unaffected. Repeated occurrences mean the server is refusing writes — check
whether you are still signed in.

**"Something went wrong. This view hit an error."** A render error inside the
interface, caught so it does not take the whole window down. **Try again** keeps
your place; **Reload** starts over. The message ends with the technical detail,
which is what to quote in a report. Nothing is lost — the data is on the server.

**"Cannot reach the server."** The full-screen state with a **Retry** button:
the very first request failed. The server is down, or the address is wrong, or a
proxy is between you and it and is not forwarding. `/api/health` answers
`{"status":"ok"}` and the version when the server is alive, without a sign-in.

## Getting into a workspace you are not in

The instance owner can open a workspace they are not a member of, but only
through the front door. In **Manage users**, select your own account, find the
workspace under **Workspace access**, and press **Emergency access**. It asks
why, and the reason is mandatory: under 10 characters it is refused with
`reason_too_short`. Access is read-only, expires after two hours, and can be
ended early.

The record is not hidden. Anyone running that workspace sees it under
**Workspace settings → Emergency access log** — who looked in, when, until when,
and the reason in their own words, with **End it now** while it is still
running. It also lands in the [activity log](history-and-audit.md).

Two refusals belong to this route. `no_self_grant` — you cannot simply give
yourself a role in a workspace; emergency access is the logged way in.
`personal_no_break_glass` — a personal space cannot be looked into at all, in
an emergency or otherwise, because it belongs to exactly one account. See
[Permissions](permissions.md).

## Before reporting anything: the numbers

**Instance settings → Maintenance** has an **Instance** panel, and it is the
fastest answer to most "what have you actually got" questions: the version, the
Go version and operating system, uptime, how many users and workspaces, how many
pages and how many of those are trashed, the size of the database, the size of
the uploads, the data directory on disk, and the address the server sees you
coming from. Quote that panel rather than guessing.

The same tab has **Download backup (.tar.gz)** — the whole database as a
consistent snapshot plus every upload, in one file, without a shell on the
machine. Only the instance owner may take it (`owner_only_backup`), because it
contains every workspace.

## When the server itself is the problem

**Read the startup log first.** It is a handful of lines and it says what the
server decided about its memory, whether it rebuilt the search index, whether it
rebuilt the file index, and what address it is listening on. Most answers are
there.

**A restore refuses to run.** `salt restore` will not overwrite an existing
database. Empty the data directory, or set `SALT_RESTORE_FORCE=1`. The guard is
deliberate — restoring over a live instance is the mistake it exists to prevent.

**A backup restored, but did the schema survive?** The proof is the *absence* of
a search-index rebuild line at the next start: the binary recognising its own
schema. A rebuild line means it migrated the data forward, which is normal after
an upgrade and suspicious after a plain restore.

**You copied the database out and the schema looks old.** SQLite runs in WAL
mode. The `.db` file alone is stale; recent changes are in the `-wal` file next
to it. Copy all three, or stop the server first. `salt backup` does this
properly — it takes a transactionally consistent snapshot, so it is safe against
a running instance.

**The process gets killed under load.** A container with no memory limit is
treated as a small machine on purpose, because the host's figure is not a
promise about what the container will be given. That reading does two things: it
sizes what gets indexed, and it tells the garbage collector where the ceiling
is. Set `--memory=` on the container or `SALT_MEMORY_MB` to tell it the truth,
and note which direction is dangerous — reading **too high** is, because the
collector then aims at memory the container will never get and the kernel
arrives first. Reading too low costs indexing and nothing else: the file is
still stored, listed and downloadable.

**Pages imported from Notion carry a repeated preamble.** With the server
stopped, `salt fix-notion-rows` strips the "# title + Property: value" block
that Notion writes into every database row and reports how many bodies it
cleaned. It takes the sole database connection, so it will not run alongside a
live instance.

**Do not ask the binary for its version with a flag.** `salt version` prints it.
An unrecognised flag is not a subcommand, so the binary starts a second server
instead — beside the one already running.

**And do not trust the version string to prove a deploy.** A mislabelled build
reads exactly like a correct one. Check for something the new code has and the
old does not: a route that answers, a marker in the served files, a behaviour
you changed.

## Error codes you may meet

These are codes the server sends. The code is the same in every language; the
sentence is what you see in yours. Messages outside this table generally carry
no code at all — quote their English sentence instead.

| Code | What it means |
| --- | --- |
| `bad_credentials` | Wrong email or wrong password — deliberately one message for both |
| `2fa_required` | The account has two-factor sign-in; enter the 6-digit code |
| `2fa_invalid` | That code was wrong or has already rolled over |
| `account_disabled` | The account was deactivated; sessions and tokens are already gone |
| `signup_not_allowed` | Self-registration is off for that address; ask for an invitation |
| `oauth_email_squatter` | An account holds the address but it counts as unconfirmed — or it is deactivated |
| `oauth_expired` / `oauth_bad_state` | The sign-in was not carried through: the round trip took longer than ten minutes, or what came back did not match what was sent ([SSO](sso.md)) |
| `session_required` | Administration; a credential of any kind is refused, browser sign-in only |
| `owner_only` | Reserved to the instance owner |
| `owner_only_backup` | Only the owner may download an instance backup — it contains every workspace |
| `owner_only_credentials` | Only the owner may change another account's password or email |
| `workspace_scoped` | A credential tied to particular workspaces cannot create new ones |
| `last_admin` / `last_admin_other` | The last admin of a workspace cannot be removed; appoint another first |
| `no_self_grant` | You cannot grant yourself access to a workspace; emergency access is the logged route |
| `personal_no_break_glass` | A personal space cannot be looked into, even in an emergency |
| `private_pages_left_self` / `private_pages_left_other` | Removing a member would leave private pages behind; the count travels with the message |
| `mail_not_configured` | No mail delivery is set up — SMTP, or a connected Google or Microsoft account |
| `mail_refresh_failed` | The connected mailbox needs connecting again; the provider's own words follow in brackets |
| `rules_too_long` | Workspace rules are capped at 16,000 characters |
| `reason_too_short` | Emergency access needs a reason of at least 10 characters — it is logged and shown |

A code with no translation falls back to the server's English sentence. That is
the intended behaviour, not a fault: a correct sentence in the wrong language
beats a broken one.

## See also

- [Agent access](agent-access.md) — credentials, scopes, what a workspace allows
- [Sharing](sharing.md) — public links, passwords, expiry, forms
- [Search](search.md) — what is indexed and when
- [Files](files.md) — uploads, limits, the file index
- [Trash and recovery](trash-and-recovery.md) — restoring a page, deleting for good
- [History and audit](history-and-audit.md) — versions, the activity log
- [Single sign-on](sso.md) — the full table of provider failures
- [Email](mail.md) — SMTP, connected mailboxes, the test message
- [Webhooks](webhooks.md) — what fires, what is delivered, what is signed
- [Self-hosting](self-hosting.md) — the startup log, backups, updating
- [Working at the same time](collaboration.md) — losing and regaining the connection
