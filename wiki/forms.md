# Forms

A **form** is one of the seven view types a collection can have. It draws the
collection's properties as fields to fill in, and submitting it creates a row.
Published as a link, it lets somebody with no account create a row in one
collection of your instance — a contact form, a leave request, a bug report, a
signup list.

This page covers how to turn a collection into a form, what you can and cannot
put on it, how to publish and withdraw the link, what a visitor sees, what the
server checks before it accepts an entry, and where the entries end up. The
collection itself is covered in [Collections](collections.md), the other view
types in [Views](views.md), and read-only page sharing — a different thing
entirely — in [Sharing](sharing.md).

## What a form is, exactly

A form is not a separate object. It is a view, stored beside the table and the
board on the same collection, and it has two faces:

- **Inside the app**, a member opens the form tab and fills it in like any other
  view. The row is created as that member, with their permissions.
- **On a public link**, anybody who has the URL fills in the same fields without
  signing in. The row is created anonymously.

The public half is optional. A form view with no link published is a perfectly
useful thing on its own — a tidy entry screen for a database with fifteen
columns, where the table is unpleasant to type into.

One thing to hold on to before anything else: **the public link belongs to the
collection, not to the view.** A collection can have several form views, but at
most one live link, and that link always serves the *first* form view in the tab
strip. If you keep two forms on one collection, only the left-hand one is ever
published.

A form view behaves the same wherever the collection is drawn: as a page of its
own, as a database embedded in a document, or as a database nested inside
another one. It is the same view machinery in all three places.

## Adding a form view

1. Open the collection.
2. Press the `+` at the right end of the view tabs (tooltip **Add view**).
3. Choose **Form** from the menu.

The new view is called *Form* and appears as the last tab.

To rename it, double-click the tab, or open the `⋯` button on the view bar
(tooltip **View options**) and choose **Rename view**. That opens a small dialog
with the current name filled in; its confirm button reads **Rename**.

The same `⋯` menu holds **Move left** and **Move right**, which change the tab
order — and, since the public link serves the first form view, they also decide
which form the world sees. It also holds **Remove view**, which deletes the view
you are on. All four of these appear only when the collection has more than one
view: a collection with no view has nothing to render, so the last one cannot be
removed and has nowhere to move to.

Over MCP the same view is created with `set_view`:

```
set_view(page_id: "<collection id>", type: "form", name: "Intake")
```

## Building the form

The form itself is the card in the middle of the screen. Two things on it are
editable in place — the heading and the description — and every change saves as
you type.

### Heading and description

The large text at the top is the form's heading. Its placeholder reads **Form**;
leave it blank and the public page falls back to the collection's own title, and
to the word *Form* if the collection has no title either. The box under it is the
description, placeholder **Description (optional) — explains what the form is
for.** Both are shown to visitors exactly as written, and the description is left
out of the public page entirely when it is empty.

Neither can be set over MCP — `set_view` does not carry them. An agent can make
a form view; a person writes what it says.

Both boxes accept typing from anybody who can open the collection, including
somebody with read-only access — and nothing on screen says the change was
refused. What you typed stays in the box until you reload the page, at which
point the saved text comes back. If you have read access and not write access,
treat the heading and description as read-only regardless of what the cursor
does.

### The Title field

Every form starts with a field labelled **Title**, marked with a red `*`, with
the placeholder **Name of the entry**. It is not a property and it cannot be
removed or renamed. It is the row's title, and it is the only required field on
the whole form: the submit button stays disabled until something is typed into
it. Pressing Enter in the Title field submits the form.

### Which fields appear

Every property of the collection whose type can be filled in appears as a field,
in schema order, labelled with the property's name. Seven of the thirteen
property types qualify.

| Property type | On a form | What the field is |
| --- | --- | --- |
| `text` | yes | a single-line text box |
| `number` | yes | a number box |
| `select` | yes | a dropdown, with `—` for "no value" |
| `multiselect` | yes | the options as chips; click to toggle |
| `date` | yes | the browser's own date picker |
| `checkbox` | yes | a checkbox |
| `person` | yes | a single-line text box (see below) |
| `url` | no | — |
| `checklist` | no | — |
| `relation` | no | — |
| `backrelation` | no | — |
| `rollup` | no | — |
| `formula` | no | — |

The last four are derived or linked: there is nothing to type into a rollup, and
a stranger cannot be asked to pick a row out of a database they cannot see.
`url` and `checklist` are simply not offered — if you need a link from a form,
collect it in a `text` property.

Two details worth knowing before you design the schema around a form:

- **A `person` field on a form is a plain text box**, not a member picker. What
  the visitor types is stored as written. If it matches a member's name, ignoring
  capitalisation, the value renders afterwards as that member's chip; anything
  else renders as a chip with the typed text. (A member's internal id matches
  first, which is what an agent writing the same property usually stores.) That
  is fine for "who should we contact" and wrong for "assign this to somebody".
- **A `multiselect` property with no options defined** shows the line
  **No options defined** instead of a field, and nothing can be submitted for it.
  The fix is the **Properties** button on the view bar: open the property there
  and type option names into the **+ Option** box, one per Enter, then press
  **Save**. The same box is where a `select` gets its options.

### The order the fields appear in

Fields follow the collection's schema order — the order the properties are listed
in the **Properties** dialog — both on the form inside the app and on the public
page. Nothing in the interface reorders that list, and no MCP tool does either:
`set_view` carries no field order, and `update_schema` merges into the properties
it finds and appends new ones at the end. The one way to move a property is to
remove it and add it again with the same id, which puts it last and keeps the
values already in the rows.

### Hiding fields

The **Columns** button on the view bar works on a form view the same way it works
on a table: the properties under **Shown** appear on the form, the ones under
**Hidden** do not. **Hide all** and **Show all** move the whole list at once. This
is how you keep a form short when the collection has columns that only the team
fills in — a status, an internal note, an owner.

Once anything is hidden, the button reads **Columns** followed by a count in
brackets — `(4/7)`, shown over total. That count is the only sign on the bar that
the form has been shortened.

Hiding is per view, so hiding a property on the form leaves the table untouched,
and a field you hide is not sent to the visitor's browser at all. The server does
not re-check it when an entry arrives, though: a value posted for a hidden
property by hand is still stored. **Hiding shortens the form; it does not seal
the property off.** For a field nobody outside may ever touch, that means a
hidden column is a tidiness measure, not a lock.

**Properties** on the same bar opens the schema editor, for adding or retyping a
property ([Collections](collections.md)). A form view has no filter, no sort and
no **New** button — it creates rows rather than listing them.

## Filling it in from inside

Fill the fields, press the submit button. The card is replaced by a confirmation
reading **Sent** / **Your answer has been saved.**, with a **Send another answer**
button that clears the form for the next entry.

An entry made this way is an ordinary row creation by the signed-in member: it
appears immediately in everyone else's open table or board, and it triggers the
`page.created` webhook if one is configured ([Webhooks](webhooks.md)). None of
the coercion rules described below apply to it — it goes down the same path as
typing into a table cell.

## Publishing the link

The bar at the top of the form card holds the sharing controls. Everybody who can
open the collection sees them, but only somebody who may edit it can use them:
for a reader the buttons answer with a **Sharing failed** toast and nothing
changes.

1. Press **Share publicly**.
2. The link is created and copied to your clipboard — a toast says **Public link
   copied** (or **Public link created** if the browser refused the clipboard).
3. The URL also appears in a read-only box under the bar. Clicking it selects the
   whole thing.

The bar then reads **Public** with a globe, and carries two buttons: **Copy link**
and **Revoke**.

The address looks like this:

```
https://salt.example.com/form/8c1f4a2e9b7d05c3a6e1f8b4d29c7053ab6e
```

The token is 36 hexadecimal characters from random bytes. The host part is not
your browser's address bar — the server builds the link from the instance's
configured public address, so a link created on the local network still works for
somebody outside it. If no public address is configured, the link is built from
the address you are on, which will be the local one. See [Domain](domain.md).

### The link is on screen once, and only once

The URL box disappears the moment you leave the page. Come back to the form later
and the bar reads **Public** / **Copy link** / **Revoke** with no URL under it:
the instance can confirm that a live link exists, and cannot tell you what it is.

That is not an oversight — see the next section for why — but it is the situation
that makes people press **Copy link** to "get the link back", which is the one
button on this page that does not do that.

### Copy link mints a new link

**This is the one thing on this page that can bite you.** Only a hash of the token
is stored, so the existing link cannot be read back out. **Copy link** therefore
does the same thing **Share publicly** did: it revokes the current link and
creates a fresh one. Anybody still holding the old URL gets *Form not found* from
that moment.

So keep the link somewhere the moment you create it — a page in your workspace,
the message you sent it in. Pressing **Copy link** is how you *rotate* a link that
went to the wrong person, not how you look one up.

## What a visitor sees

The link opens a standalone page — no sidebar, no workspace, nothing of your
instance around it. While the form is being fetched, the card reads **Loading…**;
on a fast connection that is a flicker, on a slow one it is what the visitor looks
at first. Then, from top to bottom:

- the collection's icon, if it has one, and the heading;
- the description, if there is one;
- **Title** with its red `*`, then the fields, in schema order;
- the submit button;
- a footer reading **Made with salt.md**.

Submitting replaces the card with **Thank you!** / **Your answer has been
submitted.** and a **Send another answer** button, so one visitor can file several
entries without reloading. If sending fails for any reason, a toast says **Sending
failed** and what they typed stays in the fields.

A link that has been revoked, or that points at a collection now in the trash,
shows **Form not found** — *This link is not valid or has been switched off.* The
same page appears if the form view itself has been deleted while the link is
still live.

The submit button carries the label stored on the view. Nothing in the interface
writes that label, so in practice every form's button reads `Absenden`. It can be
set through the HTTP API, which stores a view's settings as they are given — see
the route table at the end of this page — but there is no MCP tool for it and no
box in the browser.

## What the server checks

A public submission is checked before anything is written. The rules are worth
knowing because **nothing is reported back**: a value that fails is dropped
silently and the rest of the entry is saved.

| Rule | Behaviour |
| --- | --- |
| Title | required; blank is rejected outright, and the visitor sees *Sending failed* |
| Title length | at most 2000 characters, otherwise rejected |
| Unknown property | dropped |
| A property that cannot be filled in | dropped, even if the browser sent it |
| `text`, `person`, `date` | must be text; trimmed; empty is dropped; longer than 4000 characters is cut. A date is not parsed — the browser's picker sends a calendar day, but any text is accepted |
| `select` | must be one of that property's option ids; anything else dropped |
| `multiselect` | must be a list; unknown ids dropped; if none survive, the whole value is dropped |
| `number` | must be a number |
| `checkbox` | must be true or false |

On top of that, submissions are rate-limited to **20 per minute per IP address**,
with a burst of 8 — eight in quick succession, then one every three seconds. Over
the limit, the visitor gets *Sending failed*.

That address is the visitor's only when the instance is told to trust its proxy's
headers. Without that, everybody arriving through the same reverse proxy or
tunnel counts as one visitor and shares the twenty between them — and arriving
through a proxy is the normal way a public form is reachable from outside at all.
The switch is **Run behind a reverse proxy** in the administration dialog; the
built-in Cloudflare tunnel turns it on by itself when it connects
([Administration](administration.md)).

A date collected on a form is a calendar day and stays one: a form filled in on
the 18th of July records the 18th, and it reads as the 18th for a colleague in
another time zone. See [Language and time](language-and-time.md).

## Where the entries land

A submitted entry becomes a row of the collection, appended at the end:

- Its title is the Title field; its properties are the values that survived the
  checks above.
- Its body is empty. Open the row and it is an ordinary page, with an editor, a
  comment panel and a history like any other ([Pages](pages.md)).
- It is visible to the workspace, not private, and belongs to the same workspace
  as the collection.
- Its owner is the collection's owner. Nobody signed in for the entry, so the row
  would otherwise have no owner at all; it inherits one rather than being
  ownerless.
- It is indexed for search right away ([Search](search.md)).
- It is recorded in the audit log as an action by **public form**
  ([History and audit](history-and-audit.md)).

Two things a public entry does **not** do, both of which surprise people:

- **It does not refresh a collection somebody already has open.** A member sitting
  on the table sees the new row after a reload, not the moment it arrives.
  Entries made from the in-app form do appear live.
- **It does not fire a `page.created` webhook.** If you are routing new rows
  somewhere with [Webhooks](webhooks.md), form entries will not be among them.

There is no email notification either. A form fills a database; watching that
database is a separate job.

## Closing a form again

**Revoke** on the share bar deletes the link. The toast reads **Public link
revoked**, and the URL stops working immediately for everybody. There is no way
to bring the same link back — the next **Share publicly** creates a different one.
If revoking is refused — a reader pressing it, for instance — the toast reads
**That did not work** and the link stays live.

Four other things also switch a form off:

| What you do | Effect on the link |
| --- | --- |
| Move the collection to the trash | stops accepting entries at once; restoring the collection brings the same link back |
| Delete the collection permanently, or let the trash empty itself | the link is gone with it, and cannot come back ([Trash and recovery](trash-and-recovery.md)) |
| Delete the form view | the link resolves but has nothing to render, so visitors see *Form not found* |
| Press **Share publicly** or **Copy link** again | the old link dies, a new one takes its place |

And two things that do **not** touch it: sharing the collection as a public page,
and unsharing it. A read-only page share and a form share are separate links with
separate lifetimes, and `set_sharing(public: false)` leaves a live form untouched.

## What a form is not

A form token grants exactly one thing: create one row in this collection. It is
worth being precise about the limits, because a form is the widest door in an
otherwise closed instance.

- **A form cannot read anything back.** The public page receives the collection's
  title, its icon, the form's heading and description, and the definitions of the
  visible fillable fields — each field's internal id, name and type, and for a
  `select` or `multiselect` the option ids, names and colours. It never receives
  rows, not even the visitor's own.
- **A form cannot reach the rest of the workspace.** No other page, no member
  list, no search.
- **A form cannot upload a file.** There is no file property anywhere in a
  collection — attachments live in a page's body, which a form does not write.
  See [Files](files.md).
- **A form has no required fields except the Title.** Every property field may be
  left blank.
- **A form has no captcha and no email verification.** The rate limit is the whole
  of the abuse protection. A form on a link that has been posted publicly will
  collect what public forms collect.
- **A form does not know who filled it in.** No name, no account, no address is
  recorded on the row — only that the entry came through a public form.

If you need any of that, the answer is an account and a workspace invitation
rather than a form ([Permissions](permissions.md)).

## Forms and agents

Rows created by a form are ordinary rows. An agent reads them with `query_rows`,
writes back with `set_properties`, and neither can tell them from a row typed by
a colleague except by the audit log.

What an agent can and cannot do to the form itself:

| Task | Over MCP |
| --- | --- |
| Create a form view | `set_view` with `type: "form"` |
| Rename it, hide fields | `set_view` (`name`, `hidden`) |
| Reorder the fields | not available — a form follows the schema order, and `update_schema` appends rather than reorders |
| Remove it | `delete_view` |
| Set the heading, description or button label | not available |
| Publish or revoke the public link | not available |

There is no MCP tool for publishing: a form link is minted in the browser, in
front of the form somebody is about to hand to the world. The HTTP routes below
do accept an API token, though, so an agent holding a write-scoped one can mint
and revoke a link without a browser ([Agents](agents.md)).

Over the HTTP API the whole feature is five routes ([API](api.md)). The first
three need a credential — reading the status needs read access to the collection,
minting and revoking need write access. The last two need nothing at all, which
is the point of them.

| Route | Does |
| --- | --- |
| `GET /api/collections/{id}/form-share` | says whether a live link exists — never the link itself |
| `POST /api/collections/{id}/form-share` | mints a link, replacing any current one |
| `DELETE /api/collections/{id}/form-share` | revokes it |
| `GET /api/public/form/{token}` | the fields a visitor's browser renders |
| `POST /api/public/form/{token}/submit` | one entry |

A view's settings are stored as one block, so `PUT /api/collections/{id}` — the
route the browser uses when you type in the heading — writes whatever a view
carries, the submit label included. That is the one way to change the label the
button shows.
