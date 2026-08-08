# The salt.md wiki

salt.md is a self-hosted workspace for documents and structured data. One binary
serves the interface, a REST API, an MCP endpoint for AI agents, a realtime
collaboration relay and a change feed. Your data is one SQLite file and a folder
of uploads, on a machine you control.

This wiki is written to be read two ways. A person reads the page they need and
learns how that part of the product works. An agent connected over MCP reads the
same page and gets exact names, exact limits and exact behaviour — no marketing,
no "should", no invented options.

**Everything here is derived from the code and checked against it.**
`web/scripts/check-wiki.mjs` runs inside the build: every tool this wiki names
must exist, every tool that exists must be documented, every `/api/` path
mentioned must be a real route, every property and view type must have its own
section, every internal link must resolve, and no example may contain a real
address, hostname or mail domain. Documentation that drifts fails the build.

Screenshots are held to the same standard. Each one records which source files
it shows; when one of those changes, the build says which pictures have started
lying and `web/scripts/shoot-wiki.mjs` retakes them.

## Start here

| If you are | Read |
| --- | --- |
| new to salt.md | [Getting started](getting-started.md), then [Concepts](concepts.md) |
| finding your way around | [The interface](interface.md) |
| writing documents | [Pages](pages.md) → [Editor blocks](editor-blocks.md) |
| building a database | [Collections](collections.md) → [Properties](properties.md) → [Views](views.md) |
| an AI agent, or connecting one | [Agents](agents.md) → [MCP tools](mcp-tools.md) |
| running the server | [Self-hosting](self-hosting.md), [Administration](administration.md) |
| setting up SSO, mail or a domain | [Single sign-on](sso.md), [Sending email](mail.md), [Reaching your instance](domain.md) |
| stuck | [Troubleshooting](troubleshooting.md) |

## Everything

### Using it

- [Getting started](getting-started.md) — install it, create the first account, write the first page
- [Concepts](concepts.md) — the nouns this product is made of, and how they nest
- [The interface](interface.md) — sidebar, tabs, topbar, menus, theme, every keyboard shortcut
- [Pages](pages.md) — title, icon, cover, tags, sub-pages, the structure panel, the page menu
- [Editor blocks](editor-blocks.md) — every block type, the slash menu, markdown shortcuts, dropping files in
- [Comments and notes](comments-and-notes.md) — the comment panel, and the append-only trail beside it
- [Search](search.md) — what is indexed, how German words find each other, why a PDF is findable
- [Files](files.md) — uploads, the file index, previews, text extraction
- [The library](library.md) — every page of a workspace, as shelves, as a tree, as a graph
- [Trash and recovery](trash-and-recovery.md) — what trashing does, what comes back, what does not

### Databases

- [Collections](collections.md) — a page whose children are its rows
- [Properties](properties.md) — all 13 types, each with its own section
- [Views](views.md) — `table`, `board`, `list`, `gallery`, `calendar`, `form`, `timeline`
- [Relations and rollups](relations-and-rollups.md) — linking collections, and aggregating across the link
- [Formulas](formulas.md) — a property that computes instead of storing
- [Forms](forms.md) — collecting entries from people who have no account
- [Templates and blueprints](templates.md) — starting from something instead of from nothing

### Working together

- [Workspaces](workspaces.md) — the boundary around content: members, roles, rules, logo
- [Permissions](permissions.md) — two sets of roles, page visibility, what a token may do
- [Sharing](sharing.md) — publishing a page, with a password and an expiry
- [Working at the same time](collaboration.md) — live editing, presence, what happens offline
- [History and audit](history-and-audit.md) — four records, four questions
- [Your account](account.md) — profile, password, two-factor, sessions, API tokens, leaving
- [Language and time](language-and-time.md) — five settings, and why a date never shifts

### Agents

- [Agents](agents.md) — what MCP is, how to connect, what an agent can and cannot do
- [MCP tools](mcp-tools.md) — the complete reference, one section per tool
- [Agent access](agent-access.md) — tokens versus sign-in, and what a workspace allows
- [The agent skill](skill.md) — the instruction bundle an instance writes for itself

### Connecting things

- [Automation](automation.md) — the four ways salt.md reaches outside itself
- [Webhooks](webhooks.md) — calling your address when something changes, and how to verify it
- [Import and export](import-export.md) — every way in and every way out
- [The REST API](api.md) — scripting salt.md without an agent

### Running it

- [Self-hosting](self-hosting.md) — install, environment, memory, updates, backup, restore
- [Administration](administration.md) — instance settings, accounts, who may register
- [Reaching your instance](domain.md) — the public address, the tunnel, your own proxy
- [Single sign-on](sso.md) — Google and Microsoft, and the failure that wastes an afternoon
- [Sending email](mail.md) — a connected mailbox, or plain SMTP
- [The desktop app](desktop-app.md) — a window onto a server you name
- [Troubleshooting](troubleshooting.md) — real failures and what actually fixes them

## For agents reading this

Every page stands on its own: it says what it covers in its first paragraph and
links to what it does not. There is no required reading order.

The names in this wiki are the names in the code. A tool name in backticks is an
MCP tool that exists; a path under `/api/` in backticks is a route that answers.
Both are verified on every build, so you can act on them without checking first.

Where the interface and the MCP surface use different words for one thing, the
wiki says so at the point it matters. The most important case: what the
interface calls a **collection** is what the tools call a **database** —
`create_database`, `database_id`. Neither is a mistake, and neither is going to
be renamed into the other.

## Writing here

Two rules, both learned the hard way.

**It is derived from the code, never from memory.** The mechanical half is in
the build. The half no check can reach — whether a sentence is true — is why
every page is written by reading the source it describes.

**Every example is invented.** The first draft of this wiki named three real
customers of a real instance in a diagram headed for a public website. The build
now refuses real-looking addresses, hostnames and mail domains, but no rule can
recognise a company name. If a name came from somewhere real, change it.
