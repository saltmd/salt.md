# The REST API

Everything the salt.md interface does, it does over an HTTP API that you can
call yourself: create pages, read and write database rows, search, upload files,
export Markdown, watch changes live. This page is for people writing scripts —
a backup job, a nightly import, a small internal tool. It covers how to
authenticate, what an answer and an error look like, the limits you will hit,
and a grouped list of the endpoints worth calling.

**If you are connecting an AI agent, use the MCP endpoint instead.** It speaks
the same data through 33 purpose-built tools, with descriptions the agent reads
before it acts, and it needs no glue code at all — see
[Agents](agents.md) and the [tool reference](mcp-tools.md). The REST API is for
code you write; MCP is for models. Both accept the same credentials.

## Authenticating

Three credentials work:

| Credential | How it travels | Lives |
| --- | --- | --- |
| Browser session | the `salt_session` cookie, set by signing in | 90 days by default; an admin can set 1–365 |
| **API token** | `Authorization: Bearer salt_…` | until it is revoked or its owner changes their password |
| **OAuth access token** | `Authorization: Bearer …` | one hour, renewed in the background from a refresh token |

The last two are both bearer headers, and a bearer is tried as an OAuth access
token first, then looked up in the token table. They carry the same two
narrowings — read/write, and a list of workspaces — so every endpoint below
behaves the same whichever one you send.

**A script should use an API token.** It is one string you can put in an
environment variable. The OAuth route exists for agents and apps, where a
permanent key in a config file is the thing to avoid; it is described further
down.

### Creating a token

1. Open the menu at the bottom of the sidebar — your avatar and name.
2. Choose **API tokens**.
3. Give it a name, for example `backup-script`. The field's placeholder is
   "Token name (e.g. claude-code)".
4. Choose **Read-write** or **Read-only**.
5. Choose **All workspaces** or **Specific workspaces…** and tick the ones it
   may reach. Pick "Specific workspaces…" and tick nothing and the dialog
   refuses to send it: "Pick at least one workspace (or “All workspaces”)."
6. Press **Create token**.

The token appears once, under the line "Copy this token now — it will not be
shown again:". It looks like `salt_` followed by 48 hexadecimal characters. Only
its hash is stored, so a lost token cannot be recovered — create a new one and
press **Revoke** on the old.

The same dialog lists every token you own with its scope, its workspaces, when
it was last used and **the address it was last used from**. That last column is
the point: a token that rides in a URL cannot be kept secret, so the defence is
noticing. An address you do not recognise is worth one click on **Revoke**.

Minting a token over the API is deliberately not possible from a token: `GET`,
`POST` and `DELETE /api/tokens` all exist as endpoints, and all three require a
browser sign-in. A key that can mint keys is not a limit.

**Over the API, the workspace list works the other way round from the dialog.**
An empty or absent `workspaces` array means ALL of them, now and in the future.
A list that names only workspaces you are not a member of is refused with `400`
rather than quietly widened to everything — that is the one case the server
turns down.

### Using it

```
curl -H "Authorization: Bearer salt_…" https://salt.example.com/api/pages
```

`GET /api/health` and `GET /api/me` are the two calls to start with.

`/api/health` needs no credential. It answers `{"status":"ok","version":"…"}`,
or `503` with `{"status":"unavailable"}` and **no version field** when the
database does not answer. A monitoring script has to handle both.

`/api/me` answers for anybody, signed in or not, and tells you whether your
credential was accepted:

```json
{ "setupRequired": false,
  "authenticated": true,
  "user": { "id": "…", "email": "ada@example.com", "name": "Ada Lovelace",
            "color": "#2f7d4f", "avatar": "", "isAdmin": false,
            "disabled": false, "orgRole": "member" },
  "prefs": { "language": "", "region": "", "timeZone": "",
             "clock": "", "weekStart": "" },
  "version": "…",
  "allowUserWorkspaces": true }
```

`prefs` is where the account's language, regional format, timezone, clock and
first weekday live — an empty string in any of them means automatic, which is
the normal state ([Language and time](language-and-time.md)). They sit beside
the user object rather than inside it because that object also goes out in
member lists, and somebody else's timezone is nobody's business.

### The session lifecycle, for a cookie-based script

| Call | What it does |
| --- | --- |
| `GET /api/signup-policy` | whether self-registration is open, and the instance name |
| `POST /api/setup` | create the first account — refused with `403` once one exists |
| `POST /api/signup` | register yourself, when the policy allows it |
| `POST /api/login` | `{"email":…,"password":…}`, plus `"code"` when two-factor is on |
| `POST /api/logout` | delete this session and clear the cookie |

`POST /api/login` returns the user object and sets the cookie. Sign-in is
throttled per address, and a deactivated account is refused only after the
password has been checked, so the answer never gives away that an address
exists.

### Signing an agent in instead

salt.md is also an OAuth authorization server, so an agent can sign in rather
than carry a key that never dies. The whole flow is standard OAuth 2.1 with
PKCE, and a client that already speaks it needs no special handling:

| Endpoint | What it is |
| --- | --- |
| `/.well-known/oauth-protected-resource` | the signpost — where the doors are |
| `/.well-known/oauth-authorization-server` | endpoints, scopes and methods |
| `POST /oauth/register` | a client introduces itself and gets a `client_id` |
| `GET /oauth/authorize` | sends the human to the consent screen in their browser |
| `POST /oauth/token` | exchange the code, or refresh |
| `POST /oauth/revoke` | disconnect |

Four things about it are not negotiable and will fail your client if you skip
them. `code_challenge` with `code_challenge_method=S256` is required — absent or
`plain` is refused. `redirect_uri` is compared exactly, at registration and
again at every step; `http` is allowed only for loopback addresses, and a
fragment is rejected outright. An authorization code is single-use and lives 60
seconds, and redeeming one twice does not hand out a second token, it destroys
the grant. And authorizing needs a **browser session** — an API token presented
at `/oauth/authorize` is bounced to the sign-in screen, because a key approving
a better key is not a boundary.

The two scopes are read and write, the same pair a token has. Scope is a
space-separated list, unknown entries are ignored rather than refused, and
asking for nothing recognisable lands on read — the weaker of the two, never on
the stronger. The consent screen is where the
human picks the workspaces; "all workspaces" is stored as an empty list, so a
workspace created next month is covered too.

`POST /oauth/token` answers `access_token`, `token_type`, `expires_in`
(3600), `refresh_token` and `scope`. **The refresh token rotates**: each refresh
replaces the grant, so the previous refresh token stops working.

### Connections you have granted

| Method and path | What it does |
| --- | --- |
| `GET /api/oauth/grants` | what is connected to your account: client name, scope, workspaces, when and from where it was last used |
| `DELETE /api/oauth/grants/{id}` | disconnect — the grant and every access token minted from it go at once |

Both need a browser sign-in. This is the counterpart to the token list, and it
is the only way to cut off an agent that signed in: revoking a token does
nothing to a grant.

### What a token is, and what it is not

A token is **a second key to the content its human can already reach**. It
carries their full identity and narrows in exactly two ways: read/write, and a
list of workspaces. It is not an administrator's pass.

- A **read-only** token gets `403` with the message "token is read-only" on any
  POST, PUT, PATCH or DELETE. Reads are unaffected.
- A **workspace-scoped** token cannot touch a page outside its list even if you
  name that page's id directly, and it cannot create a workspace at all — the
  new one would not be on its list. That refusal carries the code
  `workspace_scoped`.
- **Administration needs a browser.** Account management, *changing* instance
  settings, the instance backup, invitations, two-factor, minting further
  tokens, workspace rules, emergency access and discarding a page's raw trail
  all refuse a token with `403` and the code `session_required`. Without that
  rule a token handed to an agent could issue itself a wider one.
- **Reading the admin views is the exception.** `GET /api/settings` and
  `GET /api/admin/info` check the admin flag and nothing else, so an admin's
  token — read-only included — gets the settings object and the instance figures.
  If that matters to you, do not give an admin account's token away.
- A workspace can additionally **refuse agents** — see
  [agent access](agent-access.md). Where it is set to strict, a permanent API
  token is turned away even when it names that workspace; only an OAuth grant
  somebody signed in for gets through. Where it is closed, neither does.
- **Changing an account's password deletes all of its sessions and all of its
  API tokens.** Scripts stop working the moment their owner changes their
  password. This is deliberate and it is the most common cause of a job that
  "suddenly gets 401".

## Answers and errors

**Every failure is JSON, and most successful answers are too.** The exceptions
are the calls that hand you a document:

| Call | What comes back |
| --- | --- |
| `GET /api/export/{id}` | `text/markdown`, or `text/html` with `?format=html` |
| `GET /api/export` | `application/zip` |
| `GET /api/workspaces/{id}/export` | `application/zip` |
| `GET /api/skill` | `application/zip` |
| `GET /api/admin/backup` | `application/gzip` |
| `GET /api/events` | `text/event-stream` |
| `/files/<name>` | the file itself |
| `/ics/{token}.ics` | `text/calendar` |

Failures carry an English sentence and, usually, a machine-readable code:

```json
{ "error": "This action requires signing in through a browser — an API token is not enough.",
  "code": "session_required" }
```

Read the `code`, never the sentence. The English exists so that curl and scripts
get something readable; the browser ignores it and renders the reader's own
language from the code. The sentence can be reworded at any time. Some failures
carry extra fields beside the two — a `detail` written by an outside provider, or
a count the message needs.

Not every error has a code yet. Where there is none, only `error` is present.

| Status | What it means |
| --- | --- |
| `200` | done — the body is the result |
| `400` | the request was wrong: bad JSON, a value out of range, an impossible move |
| `401` | no credential, or one that was not accepted |
| `403` | recognised, but not allowed — read-only token, session required, not an admin |
| `404` | not there, **or** not yours to see |
| `409` | a conflict: writing to a trashed page, or an email already in use |
| `413` | the upload is over the instance's file limit |
| `429` | too many sign-in attempts, or too many public form submissions |
| `500` | the server failed |
| `503` | `/api/health` only: the database did not answer |

**`404` is also the answer for "you may not".** A page in a workspace you do not
belong to reports "page not found", so nobody can tell an existing private page
from a made-up id.

Codes you are likely to meet: `bad_credentials`, `2fa_required`, `2fa_invalid`,
`account_disabled`, `session_required`, `owner_only`, `workspace_scoped`,
`not_workspace_admin`, `last_admin`, `already_member`.

## Limits

| Limit | Value |
| --- | --- |
| JSON request body | 8 MiB |
| One uploaded file | 50 MB by default; an admin can set 1–2048 MB |
| An imported archive | 100 MB, and at most 2000 pages out of one archive |
| Page title | 2000 characters |
| Comment | 10 000 characters |
| A note in the raw trail | 2000 characters, truncated rather than refused |
| Rows per request | 100 by default, 500 maximum — **a larger `limit` is ignored**, so asking for 1000 gives you 100, not 500 |
| Audit entries per request | 50 by default, 200 maximum — same rule: over 200 falls back to 50 |
| Sign-in attempts | 30 per minute per address, burst of 10 |
| Public form submissions | 20 per minute per address, burst of 8 |

There is no general rate limit on authenticated REST calls. There is one on
**rejected** tokens: 60 a minute per address, burst 20, fed by failures alone.
A working script never touches it — but while an address is guessing, a correct
token from that same address is also answered `401` until the budget refills a
second later.

## Pages

| Method and path | What it does |
| --- | --- |
| `GET /api/pages` | every page you can see, as metadata |
| `POST /api/pages` | create a page (`type` `"doc"`, the default) or a database (`"collection"`) |
| `GET /api/pages/{id}` | one page, including its block content |
| `PATCH /api/pages/{id}` | change anything about it |
| `DELETE /api/pages/{id}` | move to the trash; `?permanent=1` deletes for good |
| `POST /api/pages/{id}/restore` | bring it back out of the trash |
| `POST /api/pages/{id}/duplicate` | deep-copy the page and everything under it |
| `GET /api/pages/{id}/backlinks` | the pages that mention this one |
| `GET /api/graph` | every link between pages you can read, as source/target pairs |
| `GET /api/favorites` · `POST`/`DELETE /api/favorites/{id}` | your own favourites |
| `GET /api/tags` · `GET`/`PUT /api/tag-colors` | tags in use, and their colours |

`GET /api/pages` carries no blocks — but it is not content-free: every row
includes `snippet`, the first 240 characters of the page's text with whitespace
collapsed, and `thumb`, the url of the first image in the body. That is what
draws preview cards, and it means the list is not a safe thing to hand to
somebody who may not read the pages.

It deliberately **leaves out database rows** — there can be tens of thousands of
them, and they belong in the row endpoint below. Rows that carry sub-pages of
their own are the exception and do appear, because otherwise their children
would have no parent in the list. Trashed pages are included, marked
`"trashed": true`.

`POST /api/pages` takes `parentId`, `title`, `type`, `props` and `workspaceId`.
`PATCH` accepts `title`, `icon`, `cover`, `content`, `props`, `propsPatch`,
`parentId`, `position`, `visibility` (`"workspace"` or `"private"`),
`isTemplate`, `tags`, `description` and `workspaceId`. Five things about it are
worth knowing before you write a script:

- **`propsPatch` merges, `props` replaces.** Send only the keys you changed;
  a key set to `null` is removed. Two scripts editing different properties of
  the same row then do not overwrite each other.
- **What you read back is not all stored.** Rollups, formulas and backrelations
  are computed when a row is read — by `GET /api/pages/{id}` just as much as by
  the rows endpoint. Do not write the whole props object back.
- **`content` is block JSON, not Markdown.** If you want to write prose, use
  `POST /api/import` (below) or the `write_content` tool over MCP, both of which
  take Markdown.
- **Writing `content` resets the live editing session**, so anybody with the
  page open loses unsaved edits. `PATCH /api/pages/{id}?materialize=1` suppresses
  that reset — it is what the editor itself uses when it saves its own document,
  and it is the wrong flag for a script writing from outside.
- **`parentId` moves within one workspace only.** Moving between workspaces is
  the separate `workspaceId` field, which takes the whole subtree along and
  answers `{"ok":true,"moved":n,"workspaceId":"…"}`.

`POST /api/pages/{id}/duplicate` takes two query flags: `?fromTemplate=1` makes
an ordinary page out of a template, and `?asTemplate=1` marks the copy as a
template. Both are what the ⋯ menu does, and both are reachable over REST — see
[Templates](templates.md).

Trashing takes the whole subtree with it, and restoring brings back exactly the
pages that were trashed in the same act. **Waiting also loses a page**: trashed
pages are purged automatically after the instance's trash setting, 30 days by
default, and an admin can set anything from 0 (never purge) to 3650 days — see
[Trash and recovery](trash-and-recovery.md).

## Databases

The interface calls a database a **collection**, and so do these paths. Only the
MCP surface says database, because that is the word an agent expects. It is one
thing under two names.

| Method and path | What it does |
| --- | --- |
| `GET /api/collections/{id}` | the schema (its properties) and its saved views |
| `PUT /api/collections/{id}` | replace both |
| `GET /api/collections/{id}/rows` | rows, filtered, sorted and paginated |

`PUT` needs **both** `schema` and `views` in the body; sending one alone is a
`400`. Read first, change what you need, send the pair back.

Rows are filtered in the database rather than in your script, so a table with
50 000 rows costs one page of results:

```
GET /api/collections/{id}/rows?filter=status:is:done&sort=due:asc&limit=50
```

`filter` is repeatable and reads `prop:op:value`. The operators are `is`,
`is_not`, `contains`, `gt`, `lt`, `between`, `is_empty` and `is_not_empty`; an
omitted operator means `is`, or `is_not_empty` when the value is empty too. `is`
matches a plain value **or** one element of a multi-value property. A condition
whose value is missing is ignored rather than matching nothing.

A set of values and a range do not fit in a colon-separated string, so one
`filter` may also be a JSON object — anything starting with `{`. Both spellings
work, and the short one is not going anywhere:

```
GET /api/collections/{id}/rows?filter={"property":"klasse","op":"is_not","values":["a","h"]}
GET /api/collections/{id}/rows?filter={"property":"due","op":"between","value":"2026-03-01","value2":"2026-05-31"}
```

`values` replaces `value` for `is` / `is_not` and means *any of* / *none of*.
`value2` is the upper bound of `between`, inclusive; without it the condition
does nothing. `sort` is
`prop:asc` or `prop:desc`. The answer is
`{"rows": […], "total": n, "offset": …, "limit": …}`, where `total` counts the
whole filtered set, not the page. Rollups, formulas and backrelations are filled
in per row. Other people's private rows are excluded before the count, so paging
stays honest.

Rows are pages, so you create one with `POST /api/pages` naming the database as
`parentId`, and set its values with `PATCH`. See
[Collections](collections.md) and [Properties](properties.md) for what the
values may contain.

## Search, files and export

| Method and path | What it does |
| --- | --- |
| `GET /api/search?q=…` | full-text search across everything you can read |
| `POST /api/upload?page={id}` | upload a file (multipart, field `file`) |
| `GET /api/files` | the file index; `?workspace=` and `?under={id}` narrow it |
| `GET /api/export/{id}` | one page as Markdown |
| `GET /api/export` | a zip of Markdown files, one per page, in folders |
| `POST /api/import` | a new page from Markdown |
| `POST /api/import-zip` | a zip of Markdown or CSV (multipart, field `file`) |
| `GET /api/workspaces/{id}/export` | a whole workspace as a native archive |
| `POST /api/workspaces/import` | that archive back into a new workspace |

Search returns at most 20 hits as `{id, title, icon, snippet, heading}`. The
`snippet` wraps each match in the control characters U+0001 and U+0002 so a
client can highlight safely without the page's own text injecting markup —
replace them with whatever your output needs. `heading` is the heading path of
the matching passage, for example "Contract › Termination". What is indexed, and
why searching in German finds inflected words, is [Search](search.md).

**Always pass `?page=` when you upload.** The response is
`{"url":"/files/<name>"}`, and that url is only searchable, listable and
attributable once it knows which page it belongs to. A PDF uploaded with a page
id has its text extracted and indexed under that page. The file itself is served
from `/files/<name>` and needs the same credential as everything else; a
directory listing is refused, so the random names cannot be enumerated.

### What to send to the import endpoints

`POST /api/import` takes `{"markdown": …, "title": …, "parentId": …}`. Only
`markdown` really matters:

- **No `title`** and the first Markdown heading becomes the title. No heading
  either, and the page is called "Imported".
- **No `parentId`** and the page lands at the top level of your *default*
  workspace. Unlike `POST /api/pages` there is no `workspaceId` field, so
  naming a parent is the only way to choose where an imported page goes.
- A named parent must be writable and not trashed, and the new page joins that
  parent's workspace.

`POST /api/import-zip` is multipart: the archive in the field `file`, and an
optional `parentId` form field beside it. It answers
`{"created": n, "skipped": n}`. Folders become parent pages, `.md` files become
pages, and a Notion database CSV becomes a real collection with its rows filled
in — [Import and export](import-export.md) covers the shapes it recognises.

`GET /api/export/{id}` returns Markdown by default. For a document page,
`?format=html` returns a standalone HTML file, and `?format=html&print=1`
returns it inline for printing instead of as a download. A database always
exports as a Markdown table of its rows. `GET /api/export` without a workspace
covers everything you can read — pass `?workspace={id}` to keep it to one. The
difference between the Markdown zip and the workspace archive is that the
archive is lossless and can be imported back.

## Comments, notes and history

| Method and path | What it does |
| --- | --- |
| `GET`/`POST /api/pages/{id}/comments` | read and write comments |
| `POST /api/comments/{id}/resolve` | mark resolved or unresolved (`{"resolved":true}`) |
| `DELETE /api/comments/{id}` | the author or a workspace admin |
| `GET /api/comment-counts?workspaceId={id}` | open comments per page, in one call |
| `GET`/`POST /api/pages/{id}/notes` | the raw, append-only trail |
| `DELETE /api/pages/{id}/notes` | discard the whole trail — browser sign-in only |
| `GET /api/pages/{id}/revisions` | the version list |
| `GET /api/pages/{id}/revisions/{revId}` | one older state, in full |
| `POST /api/pages/{id}/revisions/{revId}/restore` | put the page back to it |
| `GET /api/audit` | the activity log; `?limit=` and `?before=` page through it |

A note can never be edited or removed on its own — correct a wrong one by adding
another. A version snapshot is taken at most once every two minutes per page and
the newest 50 are kept, so a script writing content in a loop will not fill the
history. Restoring snapshots the current state first, which makes the restore
itself reversible. [Comments and notes](comments-and-notes.md) and
[History and audit](history-and-audit.md) go into both.

## Workspaces, members and sharing

| Method and path | What it does |
| --- | --- |
| `GET /api/workspaces` | the workspaces you are in, with your role in each |
| `POST /api/workspaces` | create one, optionally from an existing one as a blueprint |
| `PATCH /api/workspaces/{id}` | name, icon, logo, automatic joining, agent access, sidebar mode |
| `DELETE /api/workspaces/{id}` | delete it |
| `GET`/`POST /api/workspaces/{id}/members` | who is in it; add somebody by email |
| `PATCH`/`DELETE /api/workspaces/{id}/members/{userId}` | change or remove a role |
| `PUT /api/workspaces/{id}/rules` | the workspace's written rules — browser only |
| `POST`/`DELETE /api/pages/{id}/share` | mint or revoke a public link |
| `GET`/`POST`/`DELETE /api/collections/{id}/form-share` | whether a public form exists, and minting or revoking it |
| `GET /api/library` · `POST /api/library/{id}` | the blueprint shelf, and using one |

`PATCH /api/workspaces/{id}` accepts `name`, `icon`, `image` (an uploaded
`/files/…` path, never an external url), `autoJoin`, `agentAccess`
(`open`, `strict` or `closed`) and `treeMode` (`split` or `mixed`). Two of them
answer `403` even to a workspace admin: `autoJoin` — every new account becomes a
member — is an instance-wide decision and belongs to the owner, and a personal
space can never be opened to everyone at all.

**`POST /api/workspaces` has two refusals worth expecting.** A workspace-scoped
credential gets `403` with `workspace_scoped`. And when an admin has switched
user-created workspaces off, everybody who is not an admin gets `403` with
"creating workspaces is disabled on this instance — ask an admin".

`POST /api/pages/{id}/share` takes an optional `expiresInDays` and `password`
and answers `{"token":"…","url":"…"}`. There is one read link per page: sharing
again replaces the old token, which is how you rotate a link somebody forwarded.
Form shares are independent of it. `GET /api/collections/{id}/form-share`
answers `{"shared": true|false}` and nothing more — the token itself is stored
only as a hash, so re-sharing always mints a new one.
[Sharing](sharing.md) covers what an anonymous visitor actually sees, and
[Forms](forms.md) what a public form does with a submission.

### Emergency access

| Method and path | What it does |
| --- | --- |
| `POST /api/workspaces/{id}/break-glass` | the instance owner takes time-limited read access to a workspace they are not in |
| `GET /api/workspaces/{id}/break-glass` | that workspace's grants — for its admins and for the owner |
| `DELETE /api/workspaces/{id}/break-glass/{grantId}` | end one early |

The `POST` needs a `reason` of at least 10 characters, and refuses without one:
a reason on the record is the whole difference between emergency access and a
quiet back door. It lasts two hours, it is written to the audit log, and the
workspace's admins are emailed. It grants **reading only**. Asking for a
workspace you are already in answers `400` with `already_member`, and a personal
space is refused outright with `personal_no_break_glass`.
[Permissions](permissions.md) explains why the door exists.

## Watching changes

`GET /api/events` is a Server-Sent Events stream. It opens with
`{"type":"hello","version":"…"}`, sends a comment line every 25 seconds to keep
the connection alive, and then one small message per change. The messages carry
no content — only what changed, so that a browser can ask for it through a route
that checks permissions:

| Message | Meaning |
| --- | --- |
| `{"type":"pages"}` | the page tree changed somewhere |
| `{"type":"rows","collection":"…"}` | that database's rows changed |
| `{"type":"notes","id":"…"}` | that page's raw trail changed |
| `{"type":"presence"}` | somebody or some agent started or finished working |

`GET /api/presence` lists who is working on what right now, which is what agents
announce with `working_on`.

**Live editing is the one thing this API does not offer.** Two people typing in
the same page are talking over `GET /collab/{id}`, a WebSocket carrying Yjs
update messages, and the server relays those bytes without interpreting them.
There is no HTTP call that inserts a character into an open editor. A script
writes with `PATCH /api/pages/{id}` and accepts that it interrupts whoever has
the page open — see [Collaboration](collaboration.md).

## Calendars and the agent skill

`GET /api/ics` returns your calendar subscription links: one for the whole
account, one per workspace, and one per database that actually has a date
property. Each comes as an `https:` and a `webcal:` url pointing at
`/ics/{token}.ics`.

**That token is a credential in a path.** It is the one url in the product that
carries its own key, because a calendar app has nowhere to put a header.
`GET /api/ics?rotate=1` throws the old token away and mints a new one, which
breaks every subscription already set up — that is what you want when a link
has gone somewhere it should not have.

`GET /api/skill` downloads the agent skill this instance generates for itself,
as a zip ([the skill](skill.md)). Pass `?workspace={id}` to aim it at one
workspace.

## The unauthenticated surface

Three routes answer without any credential, and a script may legitimately call
them:

| Method and path | What it does |
| --- | --- |
| `GET /api/public/{token}` | a shared page: title, icon, cover, content and type — that page alone, never its children |
| `GET /api/public/form/{token}` | a public form's fillable fields, and nothing else about the database |
| `POST /api/public/form/{token}/submit` | create a row from an anonymous submission |

A share protected by a password wants it in the `X-Share-Password` header and
answers `403` without it. An expired link is deleted the moment it is asked for
and answers `404` from then on. A submission needs a `title` and accepts only
`props` that exist in the schema and are fillable — anything else is dropped
rather than refused, and the whole endpoint is capped at 20 submissions a minute
per address.

`GET /api/public-base` needs a credential and is worth one call at startup: it
answers `{"base":"…"}` with the instance's configured public address. Build
share urls and connection strings from that, not from the host you happened to
reach — a link built from an internal address is useless to anybody outside.

## Instance administration

Almost none of these accept a token. `/api/users`, `/api/invites`,
`/api/webhooks`, `/api/2fa`, `/api/me/prefs`, `/api/tokens`,
`/api/admin/backup`, `/api/admin/access`, writing `/api/settings` and every
owner-only endpoint all require a browser sign-in, and most also require the
admin or owner role.

**The read-only admin views are the exception**: `GET /api/settings` and
`GET /api/admin/info` check the admin flag alone and answer an admin's API
token. The first returns the whole settings object — instance name, signup mode,
SMTP host, user and from-address, limits, OAuth client ids; passwords and
secrets travel only as "is one set". The second returns version, uptime, counts,
on-disk sizes and the address the server thinks you are calling from.

What these endpoints do is described where the feature is:
[Administration](administration.md), [Permissions](permissions.md),
[Account](account.md) and [Webhooks](webhooks.md).

## Two habits worth having

**Check by behaviour, not by version.** `GET /api/health` reports a version
string, and a mislabelled build reports it just as confidently. If your script
depends on something recent, call the endpoint and look at the answer.

**Ids are opaque.** A page id is 32 hexadecimal characters and means nothing;
never build one, never parse one. Get it from a list, a search result or the
response to the call that created the page.

If a call is refused and you cannot see why, [Troubleshooting](troubleshooting.md)
lists the usual causes — most of them turn out to be the workspace scope on the
credential or a password that was changed.
