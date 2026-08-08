# Single sign-on

salt.md can hand sign-in to **Google** or **Microsoft**. People then press a
button on the sign-in screen instead of remembering another password, and you
stop keeping a second set of credentials. This page is for whoever sets that up:
the exact fields, the address to register with the provider, how an account is
matched or created, how long a sign-in lasts, and every error the flow can
produce.

Two providers, and only two. There is no generic OpenID Connect field, no SAML,
and no way to point salt.md at a different identity provider. Setting it up is
an instance admin's job and takes two values per provider.

## What it does, and what it does not

Single sign-on answers one question: **which email address is this person?**
Everything else stays with salt.md.

- **It does not decide who may have an account.** That is the registration
  policy — see [Who gets an account](#who-gets-an-account) below.
- **It does not sync groups, roles or workspaces.** An account that arrives this
  way is an ordinary member with its own personal space. Nobody becomes an admin
  through SSO, and no directory group becomes a workspace.
- **It does not deactivate anybody.** Removing a person in your tenant does not
  remove them here; deactivate the account in **Manage users** — see
  [Taking access away](#taking-access-away).
  ([Administration](administration.md))
- **It does not replace password sign-in.** There is no switch that turns
  passwords off. The sign-in screen always shows the email and password fields,
  and the provider buttons underneath.
- **It does not ask for a two-factor code.** Two-factor sign-in in salt.md
  applies to password sign-in. On this route the second factor is whatever your
  provider enforces. ([Account](account.md))

## What you enter in salt.md

Account menu (your avatar) → **Instance settings** → the **Access** tab. Admin
only. The section is headed **Sign in with Google / Microsoft (OAuth)** and
holds two cards:

| Card | Field | What goes in it |
| --- | --- | --- |
| Google | **Client ID** | the OAuth client ID, ending in `apps.googleusercontent.com` |
| Google | **Client secret** | the secret created for that client |
| Microsoft | **Client ID (application ID)** | the application (client) ID of the app registration, a UUID |
| Microsoft | **Client secret** | the secret **value**, not its id |

Press **Save**. A toast says *Settings saved* and the dialog closes.

Four things about these fields:

- **Both values are needed before anything appears.** A provider's button shows
  on the sign-in screen only when its client ID *and* its secret are stored. One
  without the other does nothing at all.
- **A stored secret is never sent back to the browser.** The field then shows
  *•••••• (stored)* as its placeholder. Leaving it empty keeps what is stored;
  typing into it replaces it.
- **The two providers are independent.** Configure one, both, or neither.
- **The same client is reused for sending email.** If you later connect a
  mailbox on the **Email** tab, it uses the client ID and secret from here —
  with a different consent, different permissions, and a different address to
  register. See [Sending email](mail.md).

## The address to register with the provider

The provider needs to know where to send the browser back. salt.md shows both
addresses ready to copy in the same section, under the labels **Google** and
**Microsoft** — click one and it selects itself.

They are your instance's address plus:

```
/api/oauth/google/callback
/api/oauth/microsoft/callback
```

So for an instance at `https://notes.example.com`:

```
https://notes.example.com/api/oauth/google/callback
https://notes.example.com/api/oauth/microsoft/callback
```

Register it exactly as salt.md builds it — scheme, host, port, path. salt.md
sends that string twice: once when it sends the browser to the provider, and
again when it exchanges the code for a token. Any difference between what it
sends and what is registered ends the sign-in with *Sign-in failed.*

### Which address salt.md puts in the box

The address shown in the dialog is built from the first of these that exists:

1. the **Public base URL (for links, mail, calendars)** from the **General** tab
2. the URL of a running quick tunnel, if one is up
3. the address you happen to be browsing on right now

Two warnings can appear underneath it, and both are worth reading:

> ⚠ Google and Microsoft accept HTTPS redirect URIs only (localhost aside).
> Start a tunnel (the “Domain & proxy” tab) or enter a public HTTPS base URL
> under “General” — it then appears here on its own.

> ⚠ This is the URL of the running quick tunnel — it changes on every start.
> For OAuth that lasts, use a named tunnel or your own domain and enter it as
> the base URL.

**Set the public base URL.** It is the one setting that makes this predictable:
with it set, salt.md sends the same address to the provider every time, no
matter which host the browser used. Without it, the address salt.md sends is
whatever host the browser is on at that moment — which is exactly how a
registration that matches in the dialog fails in practice. See
[Reaching it from outside](domain.md).

### The scopes

salt.md asks for `openid email profile` and nothing else: who you are, your
address, your name. It cannot read mail, files or calendars with this, and it
never asks for offline access, so it holds nothing after the sign-in is over.

### Where the values come from

The provider consoles rearrange themselves regularly, so this wiki does not
describe their buttons. The dialog carries a one-line reminder for each, and
those are the things to look for:

- **Google** — *console.cloud.google.com → APIs & Services → Credentials →
  “OAuth client ID” (Web application) → enter the redirect URI above.*
- **Microsoft** — *portal.azure.com → App registrations → New (supported account
  types: “Any org + personal accounts”) → Redirect URI (Web): as above but with*
  `/api/oauth/microsoft/callback` *→ Certificates & secrets → client secret.*

One thing about Microsoft is ours to state rather than theirs: **salt.md talks
to the `common` endpoint**, which accepts work, school and personal accounts
alike. salt.md does not check which tenant a person came from. If you want one
tenant only, restrict it in the app registration — that is the only place the
restriction can live.

## What people see

Once a provider is configured, the sign-in screen shows a divider reading **or**
below the **Sign in** button, and then **Sign in with Google**, **Sign in with
Microsoft**, or both.

- **Google always asks which account to use**, even when only one is signed in
  in that browser.
- **Microsoft does not.** If exactly one account is signed in there, the round
  trip can complete without a single click. That is the provider's behaviour,
  not a setting here.

### Where a sign-in comes back to

By default the browser lands at the top of the workspace. A sign-in can carry a
destination instead: hang a `next` value on the start address and the browser
returns to that path when the round trip is over.

```
/api/oauth/google/start?next=/library
```

Only a path on this instance is accepted. An absolute address, one starting
`//`, a backslash after the slash, anything with a line break — all dropped, and
the sign-in ends at the top of the workspace as if nothing had been asked for.
The destination rides in the short-lived cookie salt.md sets when the sign-in
starts, not in the address bar, so nothing the provider echoes back can steer
it, and it is checked a second time on the way out.

The sign-in screen passes on whatever destination it was opened with, so the
provider buttons carry it too. That is what makes the [desktop app](desktop-app.md)
work: it sends people to its approval screen, the screen sends them to sign in,
and the button brings them back to the approval screen rather than dropping them
in the workspace.

### How long people stay signed in

An SSO sign-in creates an ordinary session — exactly the one a password creates.
It lasts as long as **Sign-in session length (days)** on the **General** tab of
**Instance settings**: **90 days** unless changed, anything from 1 to 365.

The provider has its say once, at sign-in. Nothing asks it again until the
session ends, so a shorter session length is the only lever this page has over
how often people go past the provider.

### Pressing the button while somebody is already signed in

The provider buttons work whether or not a session exists. salt.md does not ask
and does not warn: the account the provider returns is signed in, and the
browser now belongs to that account instead of the previous one. Quick if you
keep two accounts, a surprise if you expected a question.

## Who gets an account

The address the provider returns (lower-cased) is the only key. salt.md looks
for an account with that address, and takes it **only if the address is
confirmed and the account is not deactivated**.

If there is no such account, the instance's registration policy decides —
the same **Who may register?** setting that governs ordinary sign-up, on the
same **Access** tab:

| Policy | What an unknown address gets |
| --- | --- |
| **By invitation only** (default) | refused: *no account for … — registration here is by invitation* |
| **Email domain allowed** | an account if the domain is on the list, otherwise *this email address may not register here — ask an admin for an invitation* |
| **Open (anyone)** | an account |

Under **By invitation only** — the default — a colleague with a perfectly valid
company account still gets nowhere until they are invited or an admin creates
them. That surprises people, and it is usually what you want.

### The domain list

Choosing **Email domain allowed** makes a second field appear on the same tab:
**Allowed domains (comma separated)**. It is only there while that policy is
selected.

salt.md compares the part after the **last** `@`, ignoring case, and matches a
whole domain only — an entry of `example.com` does not admit an address at
`mail.example.com`, and there is no wildcard. Spaces around the commas do not
matter. An empty entry matches nothing, so a trailing comma is harmless.

### Creating the accounts in advance

Under **By invitation only** the accounts have to exist before a provider button
can do anything. Two ways to make one, and they differ in whether the person
hears about it.

**Manage users → + User.** Account menu → **Manage users** → **+ User**. The
panel is headed **Create a new user** and states *Creates the account straight
away — no email is sent.* It asks for:

1. **Name**
2. **Email** — this is the whole matching rule, so it has to be the address the
   provider will return
3. **Initial password (min. 8 characters)**
4. **Instance admin (may manage everything)**, a checkbox, off by default
5. a role per workspace — no access anywhere unless you give it

Press **Create user**. The address counts as confirmed from the start, so the
provider button works for that person immediately. The initial password stays
usable as well, which makes it the fallback for a day the provider is
unreachable.

**An invitation.** Workspace switcher → **Workspace settings** → **Members** →
the field *Invite by email (blank = link only)* → **Invite**. Workspace admins
only. It produces a link valid for **14 days**,
built on the instance's public address, and mails it if you filled the address
in and sending is configured ([Sending email](mail.md)).

### Invitations and single sign-on

An invitation and single sign-on combine in the obvious direction: sign in with
Google first, then open the invitation link, and the account you are signed in
as is added to that workspace. No password is asked — the session already proves
who you are.

That holds for an invitation created **without** an address (*blank = link
only*), or one created for the same address the signed-in account carries. An
invitation bound to a different address refuses: *this invite is for a different
account — sign out to accept it*. Since the invitation field asks for an address
first, that is the case you will meet — so for people who will arrive
through a provider button, either leave the address blank or type exactly the
address the provider returns.

### What a new account looks like

When the policy does allow it, the account is created immediately:

- **Name** from the provider, or the part of the address before the `@` if the
  provider sent none. Longer than 80 characters is cut.
- **Not an admin**, ever.
- **A colour** from the palette of ten, handed out in turn by how many accounts
  the instance already has.
- **Its own personal space**, named after the person and cut at **60**
  characters — so a long display name produces a workspace title shorter than
  the account name — plus every workspace marked open to every new user. Nothing
  else. ([Workspaces](workspaces.md))
- **A confirmed address**, which is what lets the button work the second time.
- **No usable password.** The account is created with a random one that nobody
  knows, and salt.md has no password-reset flow. Until somebody sets one, the
  provider button is the only way in.

Setting a password on such an account is possible in exactly one place, and it
is not the interface: only the instance **owner** can do it, and only over the
API — `PATCH /api/users/{id}` with a `password` field ([The API](api.md)). The
**Manage users** dialog offers no password field for an existing account; the
only password it can set is the **Initial password (min. 8 characters)** on
**+ User**, at creation time. An admin who is not the owner is refused outright:
*Only the owner can change another account's password or email. As an admin you
can send an invitation.*

Two consequences of the address being the key:

- **Whoever controls that mailbox at the provider can sign in as that account.**
  No password is checked, and no two-factor code is asked for.
- **Any change to an address stops it counting.** Changing your own address in
  **Profile**, or the owner changing somebody else's over the API, marks the
  address unconfirmed — and nothing in the product ever confirms it again. The
  provider button then fails for that account with the message about an
  unconfirmed address, below. On an SSO instance, create the account with the
  right address rather than correcting it afterwards. ([Account](account.md))

## Taking access away

Removing somebody in your tenant at the provider does not remove them here.
What does is **Manage users** → pick the person → **Deactivate account**: their
sessions and API tokens end at once, and the provider button stops signing them
in.

**The message they get then names the wrong cause.** A deactivated account and
an account with an unconfirmed address produce the same sentence — *This address
belongs to an account that has not confirmed it. Please sign in with a password
or contact your administrator.* If somebody reports that message and their
address is obviously fine, look for the **deactivated** badge beside their name
in **Manage users** before looking anywhere else.

Neither you nor the owner can be deactivated this way — hand the owner role on
first. **Delete user** is the owner's alone and takes the person's personal
space with it; deactivation loses nothing and is reversible with **Reactivate**.

## Errors, and what each one means

A failed sign-in returns to the sign-in screen with a message above the form. If
the provider supplied wording of its own, it follows in brackets, untranslated —
nobody can translate a sentence somebody else wrote. The error is then cleared
out of the address bar, so a reload does not show it again.

| Message | What actually happened |
| --- | --- |
| *This sign-in method is not configured.* | that provider has no client ID or no secret stored — typically a bookmarked link after the credentials were cleared |
| *Sign-in was cancelled.* | the provider sent an error back: somebody pressed cancel, or consent was refused. The provider's own code is in the brackets |
| *Sign-in expired — please try again.* | the sign-in was not finished within **10 minutes**, or the browser did not send back the cookie salt.md set when it started |
| *Sign-in could not be verified — please try again.* | the cookie came back but does not fit this attempt — a different provider, a stale tab, a mismatched state |
| *No authorization code received.* | the provider returned neither a code nor an error |
| *Token exchange failed.* | salt.md could not reach the provider (15-second limit): no outbound internet, DNS, or a firewall |
| *Sign-in failed.* | the provider refused the exchange — wrong client secret, or a redirect address that does not match the registered one. Its explanation is in the brackets, or *token response unreadable* if it sent something salt.md could not parse |
| *The provider did not supply an email address.* | no address in the token and none from the provider's userinfo endpoint |
| *This Google address is not verified.* | Google only, and Google's own verdict on the address |
| *This address belongs to an account that has not confirmed it. Please sign in with a password or contact your administrator.* | an account holds this address but it is not confirmed — **or the account is deactivated** |
| *This address cannot create an account here.* | no account, and either the registration policy refuses or the account could not be written. The reason is in the brackets: *…registration here is by invitation* and *…may not register here* are the policy, *the account could not be created* is a database problem for the server log |
| *The session could not be created.* | the sign-in worked and writing the session did not — a database problem, one for the server log |

A link naming a provider that does not exist (anything but `google` and
`microsoft`) does not come back to the sign-in screen at all: it answers 404
with the JSON body `{"error":"unknown provider"}`.

**None of this is written to the audit log.** Sign-ins are not recorded there,
successful or not. ([History and audit](history-and-audit.md))

## The one failure that wastes an afternoon

**The whole round trip has to happen on one address.**

When a sign-in starts, salt.md sets a short-lived cookie in the browser and
checks it when the provider sends the browser back. That cookie belongs to the
host that set it. Start on `https://notes.example.com` and come back on
`http://192.0.2.10:8420` — or the other way round — and the cookie is not there.

The symptom is *Sign-in expired — please try again.* or *Sign-in could not be
verified — please try again.*, on the first attempt, with credentials that are
completely correct.

The fix is always the same three things, in this order:

1. Set the **Public base URL** on the **General** tab to the address people
   actually use.
2. Register exactly that address plus `/api/oauth/google/callback` (or
   `/api/oauth/microsoft/callback`) with the provider.
3. Reach the instance under it.

With a public base URL set, salt.md helps: starting a sign-in from any other
address sends the browser to the canonical one first, so the round trip runs
where the registration says it does.

One catch is worth knowing, because it looks like a bug from the outside: that
hop rebuilds the start address and drops whatever the sign-in was carrying, so
a destination given with `next` does not survive it. A [desktop app](desktop-app.md)
sign-in begun on a LAN address, on an instance that has a public base URL, ends
in the workspace instead of on the app's approval screen. Reach the instance
under its public base URL and the hop never happens.

## Turning it off

Clear the **Client ID** field for that provider and press **Save**. The button
disappears from the sign-in screen at once — a provider counts as configured
only when both values are there.

The stored secret stays in the database: an empty secret field means *leave it
alone*, and the dialog has no way to erase one. That is harmless without an ID,
and typing a new ID brings the old secret back into use — so replace the secret
too if the point was to retire it.

Accounts created through SSO keep existing, with all their pages. What they lose
is their way in: no usable password, no reset. Before you switch a provider off,
the owner has to give those people a password with `PATCH /api/users/{id}` —
there is no field for it anywhere in the interface. The alternative is deleting
the account and creating it again with **+ User**, which takes the person's
personal space with it.

## Related

- [Administration](administration.md) — the Access tab in full, users, policies
- [Account](account.md) — sessions, two-factor, changing your address
- [Reaching it from outside](domain.md) — the public base URL and tunnels
- [Sending email](mail.md) — the other thing these credentials are used for
- [The API](api.md) — where a password can be set on an existing account
- [Agent access](agent-access.md) — agents signing in, which is a different flow
  with the same word in its name
