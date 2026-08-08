package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Agent parity, part 4: self-description, workspaces, sharing.
//
// The security boundary — deliberately NOT reachable over MCP, even where a
// human can do it in the interface:
//   • setting up and switching off two-factor authentication
//   • creating or deleting API tokens (an agent could otherwise issue itself
//     permanent access carrying wider permissions)
//   • creating or deleting user accounts, setting passwords
//   • backup, restore, tunnel, SMTP, instance settings
// Those are permissions over the instance, not over content. A compromised or
// misdirected agent should be able to touch content — not the way in.

// mcpWhoami: "an agent has to know who it is and what it may do." Without this
// answer an agent is left guessing whether a failure was missing permissions or
// a wrong id.
func (s *Server) mcpWhoami(u *user) (string, error) {
	scope := u.TokenScope
	if scope == "" {
		scope = "write"
	}
	out := map[string]any{
		"user_id":     u.ID,
		"name":        u.Name,
		"email":       u.Email,
		"token_scope": scope,
		"can_write":   scope != "read",
		// What this access deliberately cannot do — so an agent does not even try
		// and reads a failure correctly. This list used to say "user accounts"
		// although list with kind="users" exists, and "backup/restore" although the same token
		// reached the backup through the REST interface. Both are accurate now:
		// administration requires a sign-in in the browser.
		"not_available_via_mcp": []string{
			"two-factor settings", "API tokens", "creating or deleting accounts",
			"backup/restore", "tunnel and instance settings",
			"workspace membership and roles",
			"applying workspace rules (workspace admins may submit a draft via propose_workspace_rules; applying it stays in the browser)",
		},
		"note": "list with kind=\"users\" names only the people you share a workspace with; " +
			"account administration needs a signed-in browser session.",
		// A tool nobody thinks of is a tool nobody uses. This is the one place an
		// agent reliably looks BEFORE it starts — its own description says "call
		// this first" — so the reminder to announce work belongs here rather than
		// only in the working_on description, which is read by whoever already
		// decided to use it.
		"before_you_start": "If you are about to work on a page or a board task for more than a moment, " +
			"call working_on(page_id, agent, note) first — a person watching then sees who is on it and what for, " +
			"live. Call it again with done: true when you finish. Nothing expires on you in between.",
	}
	if u.TokenWorkspaces == nil {
		out["workspace_scope"] = "all workspaces you are a member of"
	} else {
		out["workspace_scope"] = u.TokenWorkspaces
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpListWorkspaces: which workspaces this connection sees, and in which role.
// The workspace used to hang off the token implicitly and was invisible.
//
// A NARROWED connection sees only what it was granted. This listed everything
// and merely flagged each entry with in_token_scope — defensible while the
// scope was a convenience, wrong the moment it is a boundary. It handed an
// agent the name, the id and the role of every workspace on the account:
// "Privat", "Sales", a customer's name. Names alone are information, and an
// agent that was deliberately given ONE workspace should not come away with a
// directory of the rest.
//
// What it still gets is a COUNT of what it was not given. That keeps the one
// useful thing — "there is more here, ask to be added" — without naming any of
// it. A count answers the question; a list answers a different one nobody asked.
func (s *Server) mcpListWorkspaces(u *user) (string, error) {
	rows, err := s.db.Query(`SELECT w.id, w.name, m.role, w.rules != '' FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id
		WHERE m.user_id = ? ORDER BY w.name`, u.ID)
	if err != nil {
		return "", err
	}
	type ws struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Role    string `json:"role"`
		InScope bool   `json:"in_token_scope"`
		// HasRules says "read them via get_workspace before you write here" —
		// the rules themselves stay out of the list so they are delivered once,
		// with their framing, not scattered through an untrusted-content block.
		HasRules bool `json:"has_rules"`
	}
	// DRAIN FIRST, then judge. The reach check reads the workspace's own rule
	// from the database, and a query issued inside an open cursor blocks the
	// whole server on a single connection — the deadlock this codebase warns
	// about, walked straight into by filtering in the loop.
	all := []ws{}
	for rows.Next() {
		var w ws
		if err := rows.Scan(&w.ID, &w.Name, &w.Role, &w.HasRules); err != nil {
			rows.Close()
			return "", err
		}
		all = append(all, w)
	}
	rows.Close()

	out := []ws{}
	withheld := 0
	for _, w := range all {
		if !s.credentialMayEnter(u, w.ID) {
			withheld++
			continue
		}
		w.InScope = true
		out = append(out, w)
	}
	res := map[string]any{"workspaces": out}
	if withheld > 0 {
		res["not_granted"] = withheld
		res["note"] = "Further workspaces exist on this account that this connection was not granted. " +
			"Their names are deliberately not listed. Ask the account holder to add one if you need it."
	}
	b, err := json.Marshal(res)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpGetWorkspace returns context and members — needed to fill person fields
// or to assign work. The second return is an addendum for OUTSIDE the
// untrusted-content block: the active rules with their follow-this framing,
// or a server-authored hint about missing/proposed rules. It must never carry
// user-authored text apart from the admin's rules themselves — a member name
// out there would be an injection surface with a server voice.
func (s *Server) mcpGetWorkspace(u *user, wsID string) (string, string, error) {
	if wsID == "" {
		wsID = s.defaultWorkspaceFor(u)
	}
	if wsID == "" || !s.isMember(u.ID, wsID) {
		return "", "", fmt.Errorf("workspace %q not found", wsID)
	}
	// "Not found" for something that exists and is theirs, merely out of this
	// connection's reach, sends an agent looking for a typo. Saying which of
	// the two it is costs nothing — the caller already knows the workspace
	// exists, it is their own account — and it tells them what to do instead.
	if !s.credentialMayEnter(u, wsID) {
		return "", "", fmt.Errorf("workspace %q is outside what this connection was granted — ask for it to be added, or name one it can reach", wsID)
	}
	var name, rules, proposal string
	if err := s.db.QueryRow(`SELECT name, rules, rules_proposal FROM workspaces WHERE id = ?`, wsID).Scan(&name, &rules, &proposal); err != nil {
		return "", "", fmt.Errorf("workspace %q not found", wsID)
	}
	rows, err := s.db.Query(`SELECT u.id, u.name, u.email, m.role FROM workspace_members m
		JOIN users u ON u.id = m.user_id WHERE m.workspace_id = ? ORDER BY u.name`, wsID)
	if err != nil {
		return "", "", err
	}
	type member struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	members := []member{}
	for rows.Next() {
		var m member
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Role); err != nil {
			rows.Close()
			return "", "", err
		}
		members = append(members, m)
	}
	rows.Close()
	var pages, dbs int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ? AND trashed_at IS NULL AND type != 'collection'`, wsID).Scan(&pages)
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ? AND trashed_at IS NULL AND type = 'collection'`, wsID).Scan(&dbs)

	role := s.workspaceRole(u.ID, wsID)
	b, err := json.Marshal(map[string]any{
		"id": wsID, "name": name, "my_role": role,
		"members": members, "page_count": pages, "database_count": dbs,
		"has_rules": rules != "", "has_pending_rules_proposal": proposal != "",
	})
	if err != nil {
		return "", "", err
	}
	return string(b), rulesAddendum(rules, proposal, role == "admin"), nil
}

// rulesAddendum builds what get_workspace appends after the untrusted block:
// the active rules (framed to be followed), and — for ADMINS only — a hint
// that none exist yet (so the agent tells the user and offers to draft some)
// or that a proposal is already waiting (so it does not pile a second one on
// top unasked). Rules are the admin's domain end to end: an agent working for
// a plain member follows them but is never nudged to raise or change them.
func rulesAddendum(rules, proposal string, isAdmin bool) string {
	base := ""
	if rules != "" {
		base = wrapWorkspaceRules(rules)
	}
	if !isAdmin {
		return base
	}
	switch {
	case rules != "" && proposal != "":
		return base +
			"\n\nA rules proposal is also waiting for your human's review in the browser (workspace menu → Workspace rules) — do not submit another unless the user asks for changes."
	case rules == "" && proposal != "":
		return "\n\nThis workspace has no active rules yet, but a rules proposal is already waiting for your human's review in the browser (workspace menu → Workspace rules) — do not submit another unless the user asks for changes."
	case rules == "":
		return "\n\nThis workspace has no rules yet. Mention that to the user; if they want some, draft a short set together (naming, structure, where content goes, what to leave alone) and submit it with propose_workspace_rules — applying it stays in the browser."
	default:
		return base
	}
}

// mcpProposeWorkspaceRules stores a rules DRAFT. It never touches the active
// rules: only a workspace admin can apply the draft, in the browser
// (handleWorkspaceRules) — that is the hard rule the user asked for, enforced
// where the server can actually see the approval. One slot per workspace; a
// newer proposal replaces the older one, and an empty proposal withdraws the
// caller's own pending draft.
//
// Rules are the admin's domain on the WAY IN too: only a token whose human is
// a workspace admin may even propose. A member's agent follows the rules; it
// has no standing to raise them, and no reason to ask its user about them.
func (s *Server) mcpProposeWorkspaceRules(u *user, wsID, rules string) (string, error) {
	if u.TokenScope == "read" {
		// The dispatch's write-gate covers this too; rules deserve the second lock.
		return "", fmt.Errorf("this API token is read-only; proposing rules requires a write token")
	}
	if wsID == "" {
		wsID = s.defaultWorkspaceFor(u)
	}
	if wsID == "" || !s.isMember(u.ID, wsID) || !s.credentialMayEnter(u, wsID) {
		return "", fmt.Errorf("workspace %q not found", wsID)
	}
	if s.workspaceRole(u.ID, wsID) != "admin" {
		return "", fmt.Errorf("workspace rules are managed by workspace admins; your token's account is not one here")
	}
	rules = strings.TrimSpace(rules)
	if utf8.RuneCountInString(rules) > 16000 {
		return "", fmt.Errorf("workspace rules are limited to 16000 characters")
	}
	if rules == "" {
		var by string
		s.db.QueryRow(`SELECT rules_proposal_by FROM workspaces WHERE id = ?`, wsID).Scan(&by)
		if by == "" {
			return "There is no pending proposal to withdraw.", nil
		}
		if by != u.ID {
			return "", fmt.Errorf("the pending proposal is not yours to withdraw — an admin can dismiss it in the browser")
		}
		if _, err := s.db.Exec(`UPDATE workspaces SET rules_proposal = '', rules_proposal_by = '', rules_proposal_at = '' WHERE id = ?`, wsID); err != nil {
			return "", err
		}
		return "Withdrew your pending rules proposal.", nil
	}
	var replaced string
	s.db.QueryRow(`SELECT rules_proposal FROM workspaces WHERE id = ?`, wsID).Scan(&replaced)
	if _, err := s.db.Exec(`UPDATE workspaces SET rules_proposal = ?, rules_proposal_by = ?, rules_proposal_at = ? WHERE id = ?`,
		rules, u.ID, now(), wsID); err != nil {
		return "", err
	}
	note := "Proposed — NOT active yet. A workspace admin reviews and applies it in the browser (workspace menu → Workspace rules). Tell the user it is waiting there."
	if replaced != "" {
		note = "Proposed, replacing the previous pending proposal — NOT active yet. A workspace admin reviews and applies it in the browser (workspace menu → Workspace rules). Tell the user it is waiting there."
	}
	b, err := json.Marshal(map[string]any{"ok": true, "workspace_id": wsID, "note": note})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpGetPermissions answers up front what is allowed on a page — "check
// first rather than write blind and fail".
func (s *Server) mcpGetPermissions(u *user, pageID string) (string, error) {
	var ws string
	var trashed any
	if err := s.db.QueryRow(`SELECT workspace_id, trashed_at FROM pages WHERE id = ?`, pageID).Scan(&ws, &trashed); err != nil {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	canRead := s.canRead(u.ID, pageID) && s.credentialMayEnter(u, ws)
	if !canRead {
		// Do not give away that the page exists.
		return "", fmt.Errorf("page %q not found", pageID)
	}
	role := s.workspaceRole(u.ID, ws)
	canWrite := s.canWrite(u.ID, pageID) && s.credentialMayEnter(u, ws) && u.TokenScope != "read"
	reason := ""
	switch {
	case u.TokenScope == "read":
		reason = "this API token is read-only"
	case role == "viewer":
		reason = "you are a viewer in this workspace"
	}
	b, err := json.Marshal(map[string]any{
		"page_id": pageID, "workspace_id": ws, "my_role": role,
		"can_read": canRead, "can_write": canWrite, "can_delete": canWrite,
		"in_trash": trashed != nil, "read_only_reason": reason,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// --- Public sharing ---------------------------------------------------------

// mcpSharePage creates a public read link. Deliberately identical to the
// interface: one live share per page, and sharing again replaces the old token
// (otherwise a link believed revoked would stay valid).
func (s *Server) mcpSharePage(r requestBase, pageID string, expiresInDays int, password string) (string, error) {
	var expiresAt any
	if expiresInDays > 0 {
		expiresAt = time.Now().UTC().AddDate(0, 0, expiresInDays).Format(time.RFC3339Nano)
	}
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	var pwHash any
	if password != "" {
		pwHash = tokenHash(token + ":" + password)
	}
	s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode != 'form'`, pageID)
	if _, err := s.db.Exec(`INSERT INTO share_links (token_hash, page_id, created_at, expires_at, password_hash) VALUES (?, ?, ?, ?, ?)`,
		tokenHash(token), pageID, now(), expiresAt, pwHash); err != nil {
		return "", err
	}
	out, err := json.Marshal(map[string]any{
		"page_id": pageID,
		"url":     r.base + "/public/" + token,
		"expires": expiresAt,
		"note":    "Anyone with this link can read the page without signing in. Sharing again replaces this link.",
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// mcpUnsharePage revokes the public link.
func (s *Server) mcpUnsharePage(pageID string) (string, error) {
	res, err := s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode != 'form'`, pageID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Sprintf("Page %s was not shared publicly", pageID), nil
	}
	return fmt.Sprintf("Revoked the public link for page %s", pageID), nil
}

// requestBase carries the public base URL into the MCP layer. The link has to
// name the same domain the interface hands out (configured domain, Cloudflare
// tunnel or request host) — otherwise an agent would get a link that cannot be
// reached from outside.
type requestBase struct{ base string }
