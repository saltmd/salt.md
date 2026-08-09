package server

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS pages (
	id TEXT PRIMARY KEY,
	parent_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
	title TEXT NOT NULL DEFAULT '',
	icon TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '[]',
	position REAL NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	trashed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_pages_parent ON pages(parent_id);
-- remove_diacritics 2 folds ä→a, ü→u, ß→ss before indexing (i18n-ok: the
-- folded characters are the subject). Together with the prefix search this
-- removes a large part of German inflection on its own: the plural of
-- "Vertrag" is stored as "vertrage" and "vertrag*" reaches it.
-- Changes here need a new ftsVersion in searchindex.go.
CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
	id UNINDEXED, title, body,
	tokenize = "unicode61 remove_diacritics 2"
);
-- Passages of a page (W110): the search unit below the page. Hangs off pages
-- by cascade; chunks_fts is carried along by hand, because a virtual table
-- knows no foreign keys.
CREATE TABLE IF NOT EXISTS page_chunks (
	id TEXT PRIMARY KEY,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	workspace_id TEXT NOT NULL DEFAULT '',
	ord INTEGER NOT NULL DEFAULT 0,
	heading TEXT NOT NULL DEFAULT '',
	text TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunk_page ON page_chunks(page_id);
CREATE INDEX IF NOT EXISTS idx_chunk_ws ON page_chunks(workspace_id);
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
	chunk_id UNINDEXED, title, heading, text,
	tokenize = "unicode61 remove_diacritics 2"
);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	name TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '#2f7d4f',
	password_hash TEXT NOT NULL,
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS api_tokens (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	created_at TEXT NOT NULL,
	last_used_at TEXT
);
CREATE TABLE IF NOT EXISTS oauth_clients (
	id TEXT PRIMARY KEY,
	secret_hash TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	redirect_uris TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_codes (
	code_hash TEXT PRIMARY KEY,
	client_id TEXT NOT NULL,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	redirect_uri TEXT NOT NULL,
	challenge TEXT NOT NULL,
	scope TEXT NOT NULL DEFAULT 'read',
	workspaces TEXT NOT NULL DEFAULT '',
	resource TEXT NOT NULL DEFAULT '',
	expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS oauth_grants (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	client_id TEXT NOT NULL,
	refresh_hash TEXT NOT NULL UNIQUE,
	scope TEXT NOT NULL DEFAULT 'read',
	workspaces TEXT NOT NULL DEFAULT '',
	resource TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	last_used_at TEXT,
	last_used_ip TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS oauth_access (
	token_hash TEXT PRIMARY KEY,
	grant_id TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
	expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS collections (
	page_id TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
	schema TEXT NOT NULL DEFAULT '[]',
	views TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS yjs_state (
	page_id TEXT PRIMARY KEY REFERENCES pages(id) ON DELETE CASCADE,
	snapshot BLOB,
	seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS yjs_updates (
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	seq INTEGER NOT NULL,
	data BLOB NOT NULL,
	PRIMARY KEY (page_id, seq)
);
CREATE TABLE IF NOT EXISTS file_texts (
	file_name TEXT PRIMARY KEY,
	page_id TEXT REFERENCES pages(id) ON DELETE CASCADE,
	text TEXT NOT NULL
);
-- The file index (W125): one row per uploaded file, so that "every document
-- for this customer" is a query rather than a walk through every page's block
-- JSON. It is DERIVED and rebuildable — like the search index, and for the
-- same reason: the truth is the block on the page plus the byte on disk, and
-- an index that claims otherwise would rot. filesVersion drives the rebuild.
--
-- page_id is the carrier page (NULL once the page is gone); display_name is
-- the name a person recognises, which the stored file name deliberately is
-- not (uploads are stored under an opaque id).
CREATE TABLE IF NOT EXISTS files (
	file_name TEXT PRIMARY KEY,
	page_id TEXT REFERENCES pages(id) ON DELETE SET NULL,
	workspace_id TEXT,
	display_name TEXT NOT NULL DEFAULT '',
	ext TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_files_page ON files(page_id);
CREATE INDEX IF NOT EXISTS idx_files_ws ON files(workspace_id);
-- Which agent says it is working on which page (W126).
--
-- Deliberately a CLAIM, not a fact: an agent names itself, because nothing in
-- the token says which one it is — a token belongs to a human. account_id is
-- the part that is verified, and both are shown together.
--
-- last_seen is refreshed by any call from that account, so an agent working
-- inside salt.md stays fresh without doing anything. It does NOT expire the
-- entry: an agent has no clock and cannot wake itself to say "still here", so
-- an entry that vanished after ten minutes would erase a three-hour job. It
-- only fades in the interface, and a sweep removes what has been silent for
-- half a day.
CREATE TABLE IF NOT EXISTS agent_presence (
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	account_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	agent TEXT NOT NULL DEFAULT 'generic',
	label TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL DEFAULT '',
	last_seen TEXT NOT NULL DEFAULT '',
	expected_minutes INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (page_id, account_id, agent)
);
CREATE INDEX IF NOT EXISTS idx_presence_page ON agent_presence(page_id);
CREATE INDEX IF NOT EXISTS idx_presence_seen ON agent_presence(last_seen);
-- The raw trail (see notelog.go). Append-only by rule, not by grant: there is
-- no UPDATE and no single-row DELETE anywhere in the code, only a whole-page
-- clear a person triggers. agent is a claim, exactly like presence's; the
-- author is the verified half and both are shown together.
CREATE TABLE IF NOT EXISTS page_notes (
	id TEXT PRIMARY KEY,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	author_id TEXT REFERENCES users(id) ON DELETE SET NULL,
	author_name TEXT NOT NULL DEFAULT '',
	agent TEXT NOT NULL DEFAULT '',
	label TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_page_notes_page ON page_notes(page_id, created_at);
-- Signing in to the desktop app through the real browser (desktop_auth.go).
-- One row per sign-in in flight, alive for five minutes. The code is stored
-- HASHED, like every other credential here: a dump of this table must not be a
-- set of usable sign-ins. The challenge is stored in the clear on purpose —
-- it is a digest, and its whole job is to be compared against one.
CREATE TABLE IF NOT EXISTS desktop_auth (
	challenge TEXT NOT NULL,
	code_hash TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS favorites (
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	position REAL NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, page_id)
);
CREATE TABLE IF NOT EXISTS links (
	source_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	target_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	PRIMARY KEY (source_id, target_id)
);
CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_id);
CREATE TABLE IF NOT EXISTS workspaces (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS workspace_members (
	workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'member',
	PRIMARY KEY (workspace_id, user_id)
);
-- The organisation is the level ABOVE the workspaces: today exactly one row
-- (this instance), so that "who owns the instance" is a query rather than an
-- assumption. org_members deliberately mirrors workspace_members — if this
-- ever becomes a hosted multi-tenant version, org_id is already the barrier
-- and the work is narrowing queries rather than a rebuild.
CREATE TABLE IF NOT EXISTS organizations (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS org_members (
	org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	role TEXT NOT NULL DEFAULT 'member', -- owner | admin | member
	PRIMARY KEY (org_id, user_id)
);
-- Emergency access ("break-glass"): an owner can deliberately, temporarily
-- and with an audit trail gain read access to a workspace they do not belong
-- to. Without this route the only way in is the quiet back door (reset a
-- password, add yourself) — which is closed for exactly that reason.
CREATE TABLE IF NOT EXISTS break_glass (
	id TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	reason TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	revoked_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_break_glass_ws ON break_glass(workspace_id);
CREATE TABLE IF NOT EXISTS share_links (
	token_hash TEXT PRIMARY KEY,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tag_colors (
	workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
	tag TEXT NOT NULL,
	color TEXT NOT NULL,
	PRIMARY KEY (workspace_id, tag)
);
CREATE TABLE IF NOT EXISTS audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at TEXT NOT NULL,
	actor_type TEXT NOT NULL,
	actor_id TEXT NOT NULL,
	actor_name TEXT NOT NULL,
	action TEXT NOT NULL,
	page_id TEXT,
	workspace_id TEXT,
	detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_ws ON audit_log(workspace_id, id);
CREATE TABLE IF NOT EXISTS idempotency (
	key TEXT PRIMARY KEY,
	result TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS schema_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS app_settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS invites (
	token_hash TEXT PRIMARY KEY,
	email TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'member',
	workspace_id TEXT NOT NULL,
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS page_revisions (
	id TEXT PRIMARY KEY,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	created_at TEXT NOT NULL,
	author_id TEXT NOT NULL DEFAULT '',
	author_name TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rev_page ON page_revisions(page_id, created_at);
CREATE TABLE IF NOT EXISTS comments (
	id TEXT PRIMARY KEY,
	page_id TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
	block_id TEXT NOT NULL DEFAULT '',
	author_id TEXT NOT NULL DEFAULT '',
	author_name TEXT NOT NULL DEFAULT '',
	body TEXT NOT NULL,
	created_at TEXT NOT NULL,
	resolved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_comment_page ON comments(page_id, created_at);
`

// ensureColumn adds a column to an existing table if it is missing
// (SQLite has no ADD COLUMN IF NOT EXISTS).
func ensureColumn(db *sql.DB, table, column, ddl string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + ddl)
	return err
}

func openDB(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Single connection: one writer keeps SQLite trivially consistent and is
	// more than fast enough for a personal/team workspace.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := ensureColumn(db, "pages", "type", `type TEXT NOT NULL DEFAULT 'doc'`); err != nil {
		return nil, fmt.Errorf("migrate pages.type: %w", err)
	}
	if err := ensureColumn(db, "pages", "props", `props TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return nil, fmt.Errorf("migrate pages.props: %w", err)
	}
	if err := ensureColumn(db, "pages", "cover", `cover TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.cover: %w", err)
	}
	if err := ensureColumn(db, "pages", "workspace_id", `workspace_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.workspace_id: %w", err)
	}
	if err := ensureColumn(db, "pages", "owner_id", `owner_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.owner_id: %w", err)
	}
	if err := ensureColumn(db, "pages", "visibility", `visibility TEXT NOT NULL DEFAULT 'workspace'`); err != nil {
		return nil, fmt.Errorf("migrate pages.visibility: %w", err)
	}
	// Existing tokens predate scoping — default them to full write access so an
	// upgrade never silently downgrades a working integration.
	if err := ensureColumn(db, "api_tokens", "scope", `scope TEXT NOT NULL DEFAULT 'write'`); err != nil {
		return nil, fmt.Errorf("migrate api_tokens.scope: %w", err)
	}
	// Optional expiry on public share links (empty/NULL = never expires).
	if err := ensureColumn(db, "share_links", "expires_at", `expires_at TEXT`); err != nil {
		return nil, fmt.Errorf("migrate share_links.expires_at: %w", err)
	}
	// Pages marked as templates (instantiated via duplicate?fromTemplate=1).
	// What an agent changed, so it can be taken back. JSON, one object per
	// property: {"status":{"from":"offen","to":"erledigt"}}. Empty for every
	// action that is not a property change — the column is the difference
	// between a log that says something happened and one you can act on.
	if err := ensureColumn(db, "audit_log", "changes", `changes TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate audit_log.changes: %w", err)
	}
	if err := ensureColumn(db, "pages", "is_template", `is_template INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, fmt.Errorf("migrate pages.is_template: %w", err)
	}
	// Page tags: a JSON array of short labels (Obsidian-style, workspace-scoped).
	if err := ensureColumn(db, "pages", "tags", `tags TEXT NOT NULL DEFAULT '[]'`); err != nil {
		return nil, fmt.Errorf("migrate pages.tags: %w", err)
	}
	// Optional Notion-style page description (shown under the title, toggleable).
	if err := ensureColumn(db, "pages", "description", `description TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.description: %w", err)
	}
	// Workspace icon (emoji) + image (uploaded logo URL) for the workspace switcher.
	if err := ensureColumn(db, "workspaces", "icon", `icon TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.icon: %w", err)
	}
	if err := ensureColumn(db, "workspaces", "image", `image TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.image: %w", err)
	}
	// Workspace rules: working conventions a workspace admin writes down for
	// everyone — especially agents — working in the workspace. Every member
	// reads them; writing is admin-only and browser-only (sessionOnly), because
	// an agent that can rewrite its own guardrails has none.
	if err := ensureColumn(db, "workspaces", "rules", `rules TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.rules: %w", err)
	}
	// A rules PROPOSAL: an agent (or member) may draft rules over MCP, but the
	// draft is inert — it becomes active only when a workspace admin applies it
	// in the browser. One slot per workspace; a new proposal replaces the old.
	if err := ensureColumn(db, "workspaces", "rules_proposal", `rules_proposal TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.rules_proposal: %w", err)
	}
	if err := ensureColumn(db, "workspaces", "rules_proposal_by", `rules_proposal_by TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.rules_proposal_by: %w", err)
	}
	if err := ensureColumn(db, "workspaces", "rules_proposal_at", `rules_proposal_at TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.rules_proposal_at: %w", err)
	}
	// Optional password on public share links.
	if err := ensureColumn(db, "share_links", "password_hash", `password_hash TEXT`); err != nil {
		return nil, fmt.Errorf("migrate share_links.password_hash: %w", err)
	}
	// Share mode: '' / 'read' = read-only page view (default); 'form' = a public
	// form-submission link on a collection (anyone can create a row, no account).
	if err := ensureColumn(db, "share_links", "mode", `mode TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate share_links.mode: %w", err)
	}
	// Notes-list preview metadata, derived from content on save (notes.go).
	if err := ensureColumn(db, "pages", "snippet", `snippet TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.snippet: %w", err)
	}
	if err := ensureColumn(db, "pages", "thumb", `thumb TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate pages.thumb: %w", err)
	}
	// API token workspace scope (empty = all the user's workspaces; else a
	// comma-separated allow-list of workspace ids the token may reach).
	if err := ensureColumn(db, "api_tokens", "workspace_scope", `workspace_scope TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate api_tokens.workspace_scope: %w", err)
	}
	// Where a token was last used from. A token that rides in a URL cannot be
	// kept secret, so the defence is noticing: "last used yesterday from an
	// address in another country" is a question somebody can actually answer.
	if err := ensureColumn(db, "api_tokens", "last_used_ip", `last_used_ip TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate api_tokens.last_used_ip: %w", err)
	}
	// What a workspace allows an AGENT to do — open (the default, unchanged) |
	// strict (signed-in connections only) | closed (none). Opt-in: an empty
	// value reads as open, so nothing changes for anybody who does not set it.
	if err := ensureColumn(db, "workspaces", "agent_access", `agent_access TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.agent_access: %w", err)
	}
	// How the sidebar shows this workspace: split (Documents and Collections as
	// separate sections, the default) | mixed (one tree, a database sits where
	// it was filed). A documentation workspace wants the second — there the
	// databases genuinely belong under their document.
	if err := ensureColumn(db, "workspaces", "tree_mode", `tree_mode TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.tree_mode: %w", err)
	}
	// TOTP two-factor auth (secret stored on setup, enforced once enabled).
	if err := ensureColumn(db, "users", "totp_secret", `totp_secret TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate users.totp_secret: %w", err)
	}
	if err := ensureColumn(db, "users", "totp_enabled", `totp_enabled INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, fmt.Errorf("migrate users.totp_enabled: %w", err)
	}
	// W96: profile picture — an uploaded /files/ path, empty = initial+colour.
	if err := ensureColumn(db, "users", "avatar", `avatar TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate users.avatar: %w", err)
	}
	// W96: is this account's email confirmed? Existing accounts (created by
	// setup, invitation or OAuth) count as confirmed (DEFAULT 1). An email
	// changed BY THE ACCOUNT ITSELF sets this to 0 — and OAuth only signs in
	// over confirmed addresses, or somebody could claim a colleague's future
	// SSO identity by editing their own.
	if err := ensureColumn(db, "users", "email_verified", `email_verified INTEGER NOT NULL DEFAULT 1`); err != nil {
		return nil, fmt.Errorf("migrate users.email_verified: %w", err)
	}
	// W101: a workspace has an owner, not just members with roles — otherwise
	// there is no answer to "who does it fall to when the last one leaves" and
	// it can be left ownerless.
	if err := ensureColumn(db, "workspaces", "owner_id", `owner_id TEXT NOT NULL DEFAULT ''`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.owner_id: %w", err)
	}
	// W102: an account's personal workspace — the place somebody can work
	// without anyone having to grant them access. Kept apart from owner_id,
	// because a shared workspace has an owner too.
	if err := ensureColumn(db, "workspaces", "is_personal", `is_personal INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.is_personal: %w", err)
	}
	// W102: "every new user gets this one". Until now every arrival landed
	// quietly in the OLDEST workspace — an assumption, not a decision. Now the
	// owner decides which workspaces (none, one, several) stand open to all.
	if err := ensureColumn(db, "workspaces", "auto_join", `auto_join INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, fmt.Errorf("migrate workspaces.auto_join: %w", err)
	}
	// W105: deactivate an account instead of deleting it. For offboarding that
	// is the normal case — sign-in closed, sessions ended, but everything stays
	// attributable and nothing is orphaned. Deleting stays the deliberate
	// exception.
	if err := ensureColumn(db, "users", "disabled", `disabled INTEGER NOT NULL DEFAULT 0`); err != nil {
		return nil, fmt.Errorf("migrate users.disabled: %w", err)
	}
	// W114: outbound webhooks. Instance-wide and admin-managed: a hook is a
	// standing arrangement to call a foreign host, which is instance
	// configuration rather than content.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS webhooks (
		id TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		secret TEXT NOT NULL,
		events TEXT NOT NULL,
		active INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		last_status TEXT,
		last_at TEXT
	)`); err != nil {
		return nil, fmt.Errorf("create webhooks: %w", err)
	}
	// W112: language and time preferences, per account rather than per browser.
	// Empty means AUTOMATIC — follow the browser, which is what happened before
	// there was a setting and stays the default. prefs.go says why the absence
	// of a decision and the automatic mode share one representation.
	for _, c := range [][2]string{
		{"pref_language", `pref_language TEXT NOT NULL DEFAULT ''`},
		{"pref_region", `pref_region TEXT NOT NULL DEFAULT ''`},
		{"pref_timezone", `pref_timezone TEXT NOT NULL DEFAULT ''`},
		{"pref_clock", `pref_clock TEXT NOT NULL DEFAULT ''`},
		{"pref_week_start", `pref_week_start TEXT NOT NULL DEFAULT ''`},
	} {
		if err := ensureColumn(db, "users", c[0], c[1]); err != nil {
			return nil, fmt.Errorf("migrate users.%s: %w", c[0], err)
		}
	}
	// Record the schema/app version so an operator (and future migrations) can
	// see what a data dir was last written by. Additive, idempotent.
	db.Exec(`INSERT INTO schema_meta (key, value) VALUES ('version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, Version)
	return db, nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

const welcomeContent = `[
 {"type":"paragraph","content":[{"type":"text","text":"Welcome to ","styles":{}},{"type":"text","text":"salt.md","styles":{"bold":true}},{"type":"text","text":" — a fast, lightweight, open-source workspace for your notes, docs and ideas. 🧂","styles":{}}]},
 {"type":"heading","props":{"level":2},"content":[{"type":"text","text":"Everything is a block","styles":{}}]},
 {"type":"paragraph","content":[{"type":"text","text":"Type ","styles":{}},{"type":"text","text":"/","styles":{"code":true}},{"type":"text","text":" anywhere to insert headings, lists, quotes, code blocks, images, tables and more. Drag blocks by their handle to rearrange them.","styles":{}}]},
 {"type":"checkListItem","props":{"checked":true},"content":[{"type":"text","text":"Install salt.md","styles":{}}]},
 {"type":"checkListItem","props":{"checked":false},"content":[{"type":"text","text":"Create your first page (button in the sidebar)","styles":{}}]},
 {"type":"checkListItem","props":{"checked":false},"content":[{"type":"text","text":"Press Ctrl/Cmd + K to search everything","styles":{}}]},
 {"type":"heading","props":{"level":2},"content":[{"type":"text","text":"Your data stays yours","styles":{}}]},
 {"type":"bulletListItem","content":[{"type":"text","text":"Single binary, single SQLite file — trivial to back up","styles":{}}]},
 {"type":"bulletListItem","content":[{"type":"text","text":"Export any page — or your whole workspace — as Markdown","styles":{}}]},
 {"type":"bulletListItem","content":[{"type":"text","text":"Clean REST API — build your own clients on top","styles":{}}]}
]`

func (s *Server) seed() error {
	// A persistent marker (not the page count) decides whether to seed, so
	// a user who deletes everything doesn't get the welcome page back on restart.
	var seeded string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'seeded'`).Scan(&seeded)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	if _, err := s.db.Exec(`INSERT INTO settings (key, value) VALUES ('seeded', '1')`); err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pages`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	ts := now()
	id := newID()
	_, err = s.db.Exec(
		`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at) VALUES (?, NULL, ?, ?, ?, 1, ?, ?)`,
		id, "Welcome to salt.md", "🧂", welcomeContent, ts, ts,
	)
	if err != nil {
		return err
	}
	return s.reindexPage(id)
}
