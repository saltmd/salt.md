package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Workspaces are the isolation boundary: every page belongs to exactly one
// workspace, and a user only ever sees pages in workspaces they are a member
// of. Within a workspace a page is visibility='workspace' (all members) or
// 'private' (owner + workspace admins only); private-ness is inherited by the
// whole subtree. Public read-only sharing is a separate token (share_links).

// migrateWorkspaces runs once: if any page lacks a workspace, create a default
// workspace, assign every existing page/user to it, and make admins its admins.
// Idempotent and additive — safe on an already-migrated DB.
func (s *Server) migrateWorkspaces() error {
	var orphan int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ''`).Scan(&orphan); err != nil {
		return err
	}
	var userCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	var wsCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspaces`).Scan(&wsCount)
	// Fresh install (no users yet): handleSetup creates the first workspace and
	// membership. Don't pre-create an ownerless empty workspace here.
	if userCount == 0 && wsCount == 0 {
		return nil
	}
	// Upgrade path: users/pages exist but no workspace — create the default one.
	if wsCount == 0 {
		wsID := newID()
		if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'Workspace', ?)`, wsID, now()); err != nil {
			return err
		}
		// All existing users become members; admins become workspace admins.
		rows, err := s.db.Query(`SELECT id, is_admin FROM users`)
		if err != nil {
			return err
		}
		type u struct {
			id    string
			admin int
		}
		var users []u
		for rows.Next() {
			var x u
			rows.Scan(&x.id, &x.admin)
			users = append(users, x)
		}
		rows.Close()
		for _, x := range users {
			role := "member"
			if x.admin != 0 {
				role = "admin"
			}
			s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, wsID, x.id, role)
		}
		// Assign all pages to this workspace, owned by the first admin.
		var firstAdmin string
		s.db.QueryRow(`SELECT id FROM users WHERE is_admin = 1 ORDER BY created_at LIMIT 1`).Scan(&firstAdmin)
		s.db.Exec(`UPDATE pages SET workspace_id = ?, owner_id = COALESCE(NULLIF(owner_id,''), ?) WHERE workspace_id = ''`, wsID, firstAdmin)
	} else if orphan > 0 {
		// Workspaces exist but some pages are orphaned (shouldn't normally
		// happen): attach them to the oldest workspace.
		var wsID string
		s.db.QueryRow(`SELECT id FROM workspaces ORDER BY created_at LIMIT 1`).Scan(&wsID)
		s.db.Exec(`UPDATE pages SET workspace_id = ? WHERE workspace_id = ''`, wsID)
	}
	return nil
}

// defaultWorkspace returns the workspace a new page from this user should land
// in (their first membership). Empty string if the user has none.
// defaultWorkspaceFor picks a workspace the CALLER can actually reach — which
// is not the same as the account's default.
//
// The bug it fixes was reported from a real connection: somebody granted an
// agent ONE workspace, and the agent's very first call failed with
// `workspace "…" not found`. The id in that message was a workspace it had
// never been given: every "no workspace named, use the default" path asked the
// ACCOUNT for its default and then checked the credential against it. For a
// narrowed credential that check fails by construction, and the message names
// an id nobody recognises.
//
// Belongs here rather than at each call site: there are a dozen of those, and
// the next one added would be written the same way.
func (s *Server) defaultWorkspaceFor(u *user) string {
	if u == nil {
		return ""
	}
	if ws := s.userDefaultWorkspace(u.ID); ws != "" && u.tokenCanReach(ws) {
		return ws
	}
	// Narrowed, and the account's default is outside it: take the first granted
	// one they are still a member of. Sorted, so the answer does not depend on
	// the order somebody happened to tick boxes in.
	granted := append([]string(nil), u.TokenWorkspaces...)
	sort.Strings(granted)
	for _, w := range granted {
		if s.isMember(u.ID, w) {
			return w
		}
	}
	return ""
}

func (s *Server) userDefaultWorkspace(userID string) string {
	var ws string
	s.db.QueryRow(`SELECT workspace_id FROM workspace_members WHERE user_id = ? ORDER BY workspace_id LIMIT 1`, userID).Scan(&ws)
	return ws
}

func (s *Server) isMember(userID, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE user_id = ? AND workspace_id = ?`, userID, workspaceID).Scan(&n)
	return n > 0
}

// workspaceRole returns the caller's role in a workspace ("admin"|"member"|
// "viewer"), or "" if they are not a member.
func (s *Server) workspaceRole(userID, workspaceID string) string {
	var role string
	s.db.QueryRow(`SELECT role FROM workspace_members WHERE user_id = ? AND workspace_id = ?`, userID, workspaceID).Scan(&role)
	return role
}

func (s *Server) isWorkspaceAdmin(userID, workspaceID string) bool {
	return s.workspaceRole(userID, workspaceID) == "admin"
}

// forbiddenPrivateAncestor reports whether any ancestor-or-self of pageID is
// private and owned by someone other than userID (making the subtree off-limits
// unless the user is a workspace admin).
func (s *Server) forbiddenPrivateAncestor(userID, pageID, ws string) bool {
	if s.isWorkspaceAdmin(userID, ws) {
		return false
	}
	var n int
	s.db.QueryRow(`
		WITH RECURSIVE anc(id, parent_id, visibility, owner_id) AS (
			SELECT id, parent_id, visibility, owner_id FROM pages WHERE id = ?
			UNION
			SELECT p.id, p.parent_id, p.visibility, p.owner_id
			FROM pages p JOIN anc ON p.id = anc.parent_id
		) SELECT COUNT(*) FROM anc WHERE visibility = 'private' AND owner_id != ?`, pageID, userID).Scan(&n)
	return n > 0
}

// pageExists reports whether the page is still in the database (trash
// included). canRead does not distinguish "does not exist" from "you may not"
// — but for the log the difference matters.
func (s *Server) pageExists(pageID string) bool {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ?`, pageID).Scan(&n)
	return n > 0
}

// canRead reports whether userID may read pageID.
func (s *Server) canRead(userID, pageID string) bool {
	var ws string
	if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pageID).Scan(&ws); err != nil {
		return false
	}
	if !s.isMember(userID, ws) && !s.hasBreakGlass(userID, ws) {
		return false
	}
	return !s.forbiddenPrivateAncestor(userID, pageID, ws)
}

// canWrite reports whether userID may modify pageID: they must be able to read
// it AND not be a read-only ("viewer") member of its workspace. Workspace admins
// and regular members can write; viewers cannot.
func (s *Server) canWrite(userID, pageID string) bool {
	if !s.canRead(userID, pageID) {
		return false
	}
	var ws string
	if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pageID).Scan(&ws); err != nil {
		return false
	}
	// Demand real membership, not merely "not a viewer": an emergency grant
	// (break_glass) passes canRead but has no role at all — and "" is not equal
	// to "viewer", which would have made it accidentally able to write.
	// Emergency access expressly means read only.
	role := s.workspaceRole(userID, ws)
	return role != "" && role != "viewer"
}

// canReadReq / canWriteReq are canRead / canWrite PLUS the request's API-token
// workspace scope. Every REST handler that reaches a page by an id taken from
// the request MUST use these (not the bare canRead/canWrite), otherwise a
// workspace-scoped token could name an out-of-scope page's id directly and
// bypass the scope that the enumeration endpoints already enforce. The token
// check is skipped for session / unrestricted-token callers (TokenWorkspaces
// == nil) so it costs no extra query in the common case.
func (s *Server) canReadReq(r *http.Request, pageID string) bool {
	if !s.canRead(requestUser(r).ID, pageID) {
		return false
	}
	u := requestUser(r)
	return s.credentialMayEnter(u, s.pageWorkspace(pageID))
}

func (s *Server) canWriteReq(r *http.Request, pageID string) bool {
	if !s.canWrite(requestUser(r).ID, pageID) {
		return false
	}
	u := requestUser(r)
	return u.TokenWorkspaces == nil || u.tokenCanReach(s.pageWorkspace(pageID))
}

// ---- what a workspace itself allows (W-opt-in) ------------------------------
//
// Until now the reach of an agent was decided ENTIRELY by whoever issued the
// credential. A workspace holding personal data had no say: it could only hope
// that every token ever minted happened to leave it out. With one agent that is
// manageable; with five agents over two years it is discipline, and discipline
// is not access control.
//
// So the workspace gets a say, and it is OPT-IN — the default is exactly the
// behaviour that exists today, so an instance that updates and changes nothing
// notices nothing.
//
//	open    (default) any credential that was granted this workspace
//	strict  only a credential somebody SIGNED IN for: short-lived, revocable,
//	        and chosen on a consent screen. A permanent API token is refused
//	        here even when it names this workspace.
//	closed  no agent at all — browser sessions only.
//
// "strict" rather than "closed" is the answer for a confidential workspace in an
// agent-first product: closed keeps agents out, strict lets them in on terms.
const (
	agentAccessOpen   = "open"
	agentAccessStrict = "strict"
	agentAccessClosed = "closed"
)

func validAgentAccess(v string) bool {
	return v == agentAccessOpen || v == agentAccessStrict || v == agentAccessClosed
}

func (s *Server) workspaceAgentAccess(wsID string) string {
	var v string
	s.db.QueryRow(`SELECT COALESCE(agent_access, '') FROM workspaces WHERE id = ?`, wsID).Scan(&v)
	if !validAgentAccess(v) {
		return agentAccessOpen
	}
	return v
}

// credentialMayEnter is THE workspace-level gate, and it is one function on
// purpose: thirty-odd places ask "may this caller reach that workspace", and a
// rule spread across thirty of them is a rule with a hole in it by next month.
//
// A browser session is never limited by this. The setting is about what an
// AGENT may reach; a person signing in is the one who decides it.
func (s *Server) credentialMayEnter(u *user, wsID string) bool {
	if u == nil {
		return false
	}
	// The credential's OWN list first. This is a no-op for a session (nil list),
	// so it needs no special case — and taking a shortcut on TokenScope instead
	// was wrong: the marker for "a credential with a list" has always been the
	// list itself, and a test caught the difference immediately.
	if !u.tokenCanReach(wsID) {
		return false
	}
	// The workspace's own rule applies to AGENTS. A person in a browser is the
	// one who sets it, so it is not turned against them.
	if u.TokenKind == "" {
		return true
	}
	switch s.workspaceAgentAccess(wsID) {
	case agentAccessClosed:
		return false
	case agentAccessStrict:
		return u.TokenKind == tokenKindOAuth
	}
	return true
}

// scopeWorkspacesFor is scopeWorkspaces plus the workspace's own rule — the
// list form of credentialMayEnter, for every enumeration.
func (s *Server) scopeWorkspacesFor(u *user, ws []string) []string {
	out := make([]string, 0, len(ws))
	for _, w := range scopeWorkspaces(u, ws) {
		if s.credentialMayEnter(u, w) {
			out = append(out, w)
		}
	}
	return out
}

// narrowedToWorkspaces is true when the caller arrived with a credential tied to
// a FIXED list — a workspace-scoped API token, or an OAuth grant where somebody
// ticked particular workspaces instead of allowing all.
//
// It matters at exactly one place that is easy to miss: creating a workspace.
// Such a caller would make one and then not be able to open it, because the
// list it is bound to was written before that workspace existed. Adding it to
// the list automatically is the obvious fix and the wrong one — a credential
// that widens its own reach is not a boundary. So creating is refused, with the
// reason said out loud.
func narrowedToWorkspaces(u *user) bool {
	return u != nil && u.TokenWorkspaces != nil
}

// scopeWorkspaces intersects a workspace list with a request user's token
// workspace restriction (a workspace-scoped API token). Cookie/session auth and
// unrestricted tokens (TokenWorkspaces == nil) pass everything through.
func scopeWorkspaces(u *user, ws []string) []string {
	if u == nil || u.TokenWorkspaces == nil {
		return ws
	}
	allow := map[string]bool{}
	for _, w := range u.TokenWorkspaces {
		allow[w] = true
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		if allow[w] {
			out = append(out, w)
		}
	}
	return out
}

// tokenReachesWorkspace enforces an API token's workspace scope on a
// workspace-id-keyed REST endpoint: a workspace-scoped token must not act on
// (or read) a workspace outside its allow-list even when the user is a member/
// admin of it. Session auth and unrestricted tokens always pass.
func (s *Server) tokenReachesWorkspace(r *http.Request, wsID string) bool {
	u := requestUser(r)
	return u.TokenWorkspaces == nil || u.tokenCanReach(wsID)
}

// tokenCanReach reports whether a workspace-scoped token may touch a workspace.
func (u *user) tokenCanReach(ws string) bool {
	if u == nil || u.TokenWorkspaces == nil {
		return true
	}
	for _, w := range u.TokenWorkspaces {
		if w == ws {
			return true
		}
	}
	return false
}

// visibleWhere returns a SQL fragment + args that restrict a `pages` query
// (aliased `p`) to pages the user may read. It handles workspace membership and
// filters out private subtrees the user doesn't own. Private inheritance is
// approximated at the SQL layer by checking the page and NOT having any private
// ancestor owned by someone else; the exact per-page check is canRead.
func (s *Server) visibleWorkspaces(userID string) []string {
	// Memberships plus running emergency grants — otherwise an owner would pass
	// canRead but see the pages in no list at all.
	rows, err := s.db.Query(`SELECT workspace_id FROM workspace_members WHERE user_id = ?
		UNION
		SELECT workspace_id FROM break_glass
		WHERE user_id = ? AND revoked_at IS NULL AND expires_at > ?`, userID, userID, nowFixed())
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		if rows.Scan(&w) == nil {
			out = append(out, w)
		}
	}
	return out
}

// filterReadable keeps only the pages the user may read (workspace + private
// subtree rules), computed in-memory from the already-workspace-scoped set.
func (s *Server) filterReadable(userID string, all []pageMeta) []pageMeta {
	byID := map[string]*pageMeta{}
	for i := range all {
		byID[all[i].ID] = &all[i]
	}
	adminOf := map[string]bool{}
	isAdmin := func(ws string) bool {
		if v, ok := adminOf[ws]; ok {
			return v
		}
		v := s.isWorkspaceAdmin(userID, ws)
		adminOf[ws] = v
		return v
	}
	// A page is hidden if ANY ancestor-or-self is private and owned by someone
	// else (unless the user is a workspace admin).
	blocked := func(p *pageMeta) bool {
		if isAdmin(p.WorkspaceID) {
			return false
		}
		cur := p
		guard := 0
		for cur != nil && guard < 1000 {
			guard++
			if cur.Visibility == "private" && cur.OwnerID != userID {
				return true
			}
			if cur.ParentID == nil {
				break
			}
			cur = byID[*cur.ParentID]
		}
		return false
	}
	out := make([]pageMeta, 0, len(all))
	for i := range all {
		if !blocked(&all[i]) {
			out = append(out, all[i])
		}
	}
	return out
}

// ---- HTTP: workspaces & members ----

type workspaceJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Icon  string `json:"icon"`
	Image string `json:"image"`
	// Personal: this account's own space. AutoJoin: open to every new account —
	// both drive what the interface offers.
	Personal bool `json:"personal"`
	AutoJoin bool `json:"autoJoin"`
	// Rules: the admin's working conventions for this workspace (see
	// handleWorkspaceRules). Along for the ride here so the dialog needs no
	// second request. A pending proposal (usually from an agent, via MCP)
	// travels with them — inert text until an admin applies or dismisses it.
	Rules           string `json:"rules"`
	RulesProposal   string `json:"rulesProposal"`
	RulesProposalBy string `json:"rulesProposalBy"`
	RulesProposalAt string `json:"rulesProposalAt"`
	// What agents may do here, and how the sidebar shows it. Empty means the
	// default in both cases — the interface reads that as "open" / "split", so
	// there is no third state to handle.
	AgentAccess string `json:"agentAccess"`
	TreeMode    string `json:"treeMode"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT w.id, w.name, m.role, w.icon, w.image, w.is_personal, w.auto_join, w.rules,
		       w.rules_proposal, COALESCE(pu.name, ''), w.rules_proposal_at,
		       COALESCE(w.agent_access, ''), COALESCE(w.tree_mode, '')
		FROM workspace_members m
		JOIN workspaces w ON w.id = m.workspace_id
		LEFT JOIN users pu ON pu.id = w.rules_proposal_by
		WHERE m.user_id = ? ORDER BY w.is_personal DESC, w.created_at`, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []workspaceJSON{}
	for rows.Next() {
		var x workspaceJSON
		var personal, autoJoin int
		rows.Scan(&x.ID, &x.Name, &x.Role, &x.Icon, &x.Image, &personal, &autoJoin, &x.Rules,
			&x.RulesProposal, &x.RulesProposalBy, &x.RulesProposalAt,
			&x.AgentAccess, &x.TreeMode)
		x.Personal, x.AutoJoin = personal != 0, autoJoin != 0
		// A pending proposal is admin business — reviewing it is a governance
		// act, and members have no rights over the rules beyond reading the
		// active ones. So the draft does not even travel to them.
		if x.Role != "admin" {
			x.RulesProposal, x.RulesProposalBy, x.RulesProposalAt = "", "", ""
		}
		list = append(list, x)
	}
	writeJSON(w, list)
}

// handleUpdateWorkspace lets a workspace admin rename it or set its icon (emoji)
// / image (an uploaded logo URL). Empty-string fields clear that attribute.
func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if !s.tokenReachesWorkspace(r, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(requestUser(r).ID, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Icon     *string `json:"icon"`
		Image    *string `json:"image"`
		AutoJoin *bool   `json:"autoJoin"`
		// What agents may do here (open|strict|closed) and how the sidebar shows
		// this workspace (split|mixed). Both are workspace-admin decisions, and
		// both default to today's behaviour when never set.
		AgentAccess *string `json:"agentAccess"`
		TreeMode    *string `json:"treeMode"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	var sets []string
	var args []any
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			httpError(w, 400, "name is required")
			return
		}
		sets = append(sets, "name = ?")
		args = append(args, n)
	}
	if body.AgentAccess != nil {
		v := strings.TrimSpace(*body.AgentAccess)
		if v != "" && !validAgentAccess(v) {
			httpErrorCode(w, 400, "bad_agent_access", "agentAccess must be open, strict or closed")
			return
		}
		// Turning it OFF for agents is a decision that should be findable
		// afterwards — "why can the agent suddenly not read this" is a question
		// somebody will ask weeks later.
		s.audit("human", requestUser(r).ID, requestUser(r).Name, "set_agent_access", "", wsID, v)
		sets = append(sets, "agent_access = ?")
		args = append(args, v)
	}
	if body.TreeMode != nil {
		v := strings.TrimSpace(*body.TreeMode)
		if v != "" && v != "split" && v != "mixed" {
			httpErrorCode(w, 400, "bad_tree_mode", "treeMode must be split or mixed")
			return
		}
		sets = append(sets, "tree_mode = ?")
		args = append(args, v)
	}
	if body.Icon != nil {
		icon := strings.TrimSpace(*body.Icon)
		if r := []rune(icon); len(r) > 8 { // an emoji or two, not arbitrary text
			icon = string(r[:8])
		}
		sets = append(sets, "icon = ?")
		args = append(args, icon)
	}
	if body.Image != nil {
		img := strings.TrimSpace(*body.Image)
		// Only accept an internal upload path or clearing it — never an external
		// URL (which would leak requests / act as a tracking beacon).
		if img != "" && !strings.HasPrefix(img, "/files/") {
			httpError(w, 400, "image must be an uploaded file")
			return
		}
		sets = append(sets, "image = ?")
		args = append(args, img)
	}
	if body.AutoJoin != nil {
		// "Open to everyone": every newly created account becomes a member. That
		// is a decision about the whole instance, not about a single workspace —
		// hence owner rather than workspace admin. A personal space can never be
		// one; it belongs to a person.
		if !s.isOwner(requestUser(r).ID) {
			httpErrorCode(w, 403, "owner_only_autojoin", "Only the owner can open a workspace to everyone.")
			return
		}
		var personal int
		s.db.QueryRow(`SELECT is_personal FROM workspaces WHERE id = ?`, wsID).Scan(&personal)
		if personal != 0 {
			httpErrorCode(w, 400, "personal_no_autojoin", "A personal space cannot be opened to everyone.")
			return
		}
		v := 0
		if *body.AutoJoin {
			v = 1
		}
		sets = append(sets, "auto_join = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		httpError(w, 400, "nothing to update")
		return
	}
	args = append(args, wsID)
	if _, err := s.db.Exec(`UPDATE workspaces SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleWorkspaceRules stores the workspace rules: working conventions an
// admin writes down for everyone — especially agents — working in this
// workspace ("invoices go into Finance/Inbox", "titles start with the date").
// Members read them (they ride along in GET /api/workspaces); MCP hands them
// to agents in get_workspace.
//
// Admin-only AND session-only, and the session gate is the point, not
// decoration: agents are told to FOLLOW these rules, so anything holding a
// mere API token must never be able to write them — otherwise the rules
// channel is the injection channel. Same line as membership and roles, which
// are not reachable over MCP either.
func (s *Server) handleWorkspaceRules(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if !s.isMember(requestUser(r).ID, wsID) {
		// Do not give away that the workspace exists.
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(requestUser(r).ID, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	var body struct {
		Rules string `json:"rules"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	rules := strings.TrimSpace(body.Rules)
	if utf8.RuneCountInString(rules) > 16000 {
		httpErrorCode(w, 400, "rules_too_long", "Workspace rules are limited to 16000 characters.")
		return
	}
	// Writing the rules settles any pending proposal: the admin either loaded
	// it into the editor (accepted) or wrote something else (overruled) —
	// either way it has been reviewed, and keeping it would re-raise a
	// question that was just answered.
	var hadProposal string
	s.db.QueryRow(`SELECT rules_proposal FROM workspaces WHERE id = ?`, wsID).Scan(&hadProposal)
	if _, err := s.db.Exec(`UPDATE workspaces SET rules = ?, rules_proposal = '', rules_proposal_by = '', rules_proposal_at = '' WHERE id = ?`, rules, wsID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	detail := ""
	if hadProposal != "" {
		detail = "settled a pending proposal"
	}
	s.audit("human", requestUser(r).ID, requestUser(r).Name, "workspace_rules_set", "", wsID, detail)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDismissRulesProposal drops a pending rules proposal without changing
// the active rules. Same gates as writing the rules themselves — reviewing a
// proposal IS rules governance, so it happens in the browser, by an admin.
func (s *Server) handleDismissRulesProposal(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if !s.isMember(requestUser(r).ID, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(requestUser(r).ID, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	if _, err := s.db.Exec(`UPDATE workspaces SET rules_proposal = '', rules_proposal_by = '', rules_proposal_at = '' WHERE id = ?`, wsID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.audit("human", requestUser(r).ID, requestUser(r).Name, "workspace_rules_proposal_dismissed", "", wsID, "")
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDeleteWorkspace removes a workspace and everything inside it. This is
// irreversible, so it is fenced in three ways: workspace-admin only, never the
// caller's last workspace (nobody may strand themselves), and the client must
// echo the exact workspace name back in `confirm`.
//
// pages.workspace_id came from a migration and therefore has NO foreign key, so
// pages must be deleted explicitly — everything hanging off a page (collections,
// comments, revisions, links, share links, favourites, file texts) then cascades
// on its own. pages_fts is a virtual table without FKs and is cleared by hand.
func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	uid := requestUser(r).ID
	if !s.tokenReachesWorkspace(r, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(uid, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	decodeJSON(w, r, &body)

	var name string
	if err := s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, wsID).Scan(&name); err != nil {
		httpError(w, 404, "workspace not found")
		return
	}
	if strings.TrimSpace(body.Confirm) != name {
		httpError(w, 400, "confirmation does not match the workspace name")
		return
	}
	// Refuse to leave the admin without any workspace at all.
	var mine int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE user_id = ?`, uid).Scan(&mine)
	if mine <= 1 {
		httpError(w, 409, "this is your last workspace — create another one first")
		return
	}

	// The same path as deleting an account (see purgeWorkspace). There used to
	// be a separate, nearly identical transaction here — only without clearing
	// up the uploads and without disconnecting the open editors. So the most
	// common deletion path was, of all of them, the least complete: uploaded
	// files stayed fetchable under their /files/ address.
	if err := s.purgeWorkspace(wsID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// The audit entry deliberately outlives the workspace it describes.
	// With no workspace reference: the workspace is gone, and letting entries
	// about vanished workspaces through would mean exposing their page titles.
	s.audit("human", uid, requestUser(r).Name, "delete_workspace", "", "", name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		// Start from an existing workspace's structure instead of from nothing.
		// The workspace itself is the template — see workspace_blueprint.go for
		// why there is no separate template object.
		FromWorkspace string `json:"fromWorkspace"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if narrowedToWorkspaces(requestUser(r)) {
		httpErrorCode(w, http.StatusForbidden, "workspace_scoped",
			"This connection is limited to particular workspaces, so it cannot create new ones — it would not be able to open them.")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		httpError(w, 400, "name is required")
		return
	}
	// W97: non-admins only when the instance permits it.
	if !requestUser(r).IsAdmin && !s.loadSettings().AllowUserWorkspaces {
		httpError(w, 403, "creating workspaces is disabled on this instance — ask an admin")
		return
	}
	if body.FromWorkspace != "" {
		u := requestUser(r)
		if _, err := s.blueprintWorkspace(u, body.Name, body.FromWorkspace); err != nil {
			httpErrorCode(w, 400, "blueprint_failed", err.Error())
			return
		}
		// The blueprint path creates the workspace itself; hand back the new one so
		// the browser can switch straight into it.
		var id, name string
		if err := s.db.QueryRow(`SELECT id, name FROM workspaces
			WHERE owner_id = ? AND name = ? ORDER BY created_at DESC LIMIT 1`,
			u.ID, strings.TrimSpace(body.Name)).Scan(&id, &name); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		writeJSON(w, workspaceJSON{ID: id, Name: name, Role: "admin"})
		return
	}
	id := newID()
	// Whoever creates it owns it — not merely as a role, but as its owner. That
	// keeps "who is responsible" answerable even when roles fall away.
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id) VALUES (?, ?, ?, ?)`,
		id, strings.TrimSpace(body.Name), now(), requestUser(r).ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, id, requestUser(r).ID)
	writeJSON(w, workspaceJSON{ID: id, Name: strings.TrimSpace(body.Name), Role: "admin"})
}

// handleAddWorkspaceMember lets a workspace admin add an existing user.
func (s *Server) handleAddWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if !s.tokenReachesWorkspace(r, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(requestUser(r).ID, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	var uid string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, strings.ToLower(strings.TrimSpace(body.Email))).Scan(&uid); err != nil {
		httpError(w, 404, "user not found")
		return
	}
	s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = excluded.role`, wsID, uid, normalizeRole(body.Role))
	writeJSON(w, map[string]bool{"ok": true})
}

// normalizeRole clamps an arbitrary role string to a known value.
func normalizeRole(r string) string {
	switch r {
	case "admin", "viewer":
		return r
	default:
		return "member"
	}
}

type memberJSON struct {
	UserID string `json:"userId"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	// Colour and picture travel too, so a person property can show the same
	// face the presence dots and the comments already show. Nothing new is
	// disclosed: both are visible to fellow members wherever a person appears.
	Color  string `json:"color"`
	Avatar string `json:"avatar"`
}

// handleListMembers lists a workspace's members. Any member may view the roster.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	if !s.tokenReachesWorkspace(r, wsID) || !s.isMember(requestUser(r).ID, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	rows, err := s.db.Query(`
		SELECT u.id, u.name, u.email, m.role, COALESCE(u.color, ''), COALESCE(u.avatar, '')
		FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? ORDER BY m.role, u.name`, wsID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []memberJSON{}
	for rows.Next() {
		var m memberJSON
		rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.Color, &m.Avatar)
		list = append(list, m)
	}
	writeJSON(w, list)
}

// workspaceAdminCount counts admins so we never orphan a workspace.
// workspaceAdminCount counts ACTIVE admins only. A deactivated account is
// nobody in charge — counting it would have deadlocked the workspace: the
// clean-up view reported "nobody in charge" while removing or demoting that
// very account failed on "last admin".
func (s *Server) workspaceAdminCount(wsID string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND m.role = 'admin' AND u.disabled = 0`, wsID).Scan(&n)
	return n
}

// otherActiveAdmins counts the active admins OTHER than the one named.
//
// The question before every removal and demotion is not "how many admins does
// the workspace have" but "will one be left afterwards". The plain count fell
// over at both ends: a deactivated admin used to count and hid the fact that
// nobody could act any more — and once it stopped counting, it suddenly could
// not be removed while exactly one active admin remained, even though its
// departure costs nobody anything.
func (s *Server) otherActiveAdmins(wsID, exceptUser string) int {
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND m.role = 'admin' AND u.disabled = 0 AND m.user_id != ?`,
		wsID, exceptUser).Scan(&n)
	return n
}

// personalOwner names the person a personal space belongs to (empty when it is
// not a personal space).
//
// Their access to that space is untouchable: it can be neither demoted nor
// removed. Without this guard, two permitted calls were enough to lock somebody
// out of their OWN space — it appeared in no list for them afterwards, and it
// does not get created again.
func (s *Server) personalOwner(wsID string) string {
	var owner string
	s.db.QueryRow(`SELECT owner_id FROM workspaces WHERE id = ? AND is_personal = 1`, wsID).Scan(&owner)
	return owner
}

// handleUpdateMember changes a member's role (workspace admin only). It refuses
// to demote the last admin so a workspace can't be left without one.
func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	target := r.PathValue("userId")
	if !s.tokenReachesWorkspace(r, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	if !s.isWorkspaceAdmin(requestUser(r).ID, wsID) {
		httpError(w, 403, "workspace admin only")
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	role := normalizeRole(body.Role)
	if role != "admin" && target == s.personalOwner(wsID) {
		httpErrorCode(w, 403, "personal_role_fixed", "That is this person's personal space — their role in it stays as it is.")
		return
	}
	if role != "admin" && s.workspaceRole(target, wsID) == "admin" && s.otherActiveAdmins(wsID, target) == 0 {
		httpError(w, 400, "cannot demote the last admin")
		return
	}
	res, err := s.db.Exec(`UPDATE workspace_members SET role = ? WHERE workspace_id = ? AND user_id = ?`, role, wsID, target)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpError(w, 404, "member not found")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRemoveMember removes a member from a workspace. A workspace admin may
// remove anyone; a non-admin may remove only themselves (leave). The last admin
// cannot be removed.
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	target := r.PathValue("userId")
	if !s.tokenReachesWorkspace(r, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	me := requestUser(r).ID
	isAdmin := s.isWorkspaceAdmin(me, wsID)
	if !isAdmin && target != me {
		httpError(w, 403, "you can only remove yourself")
		return
	}
	if !s.isMember(target, wsID) {
		httpError(w, 404, "member not found")
		return
	}
	if target == s.personalOwner(wsID) {
		httpErrorCode(w, 403, "personal_no_remove", "That is this person's personal space — they cannot be removed from it.")
		return
	}
	if s.workspaceRole(target, wsID) == "admin" && s.otherActiveAdmins(wsID, target) == 0 {
		// The old message only said it was not possible — not what to do. Both
		// are workable routes, and without saying so people go hunting for them.
		if target == me {
			httpErrorCode(w, 400, "last_admin", "You are the last admin of this workspace. Make somebody else an admin first — or delete the workspace if it should go.")
		} else {
			httpErrorCode(w, 400, "last_admin_other", "That is the last admin of this workspace. Make somebody else an admin first.")
		}
		return
	}
	// Whoever leaves leaves their PRIVATE pages behind: to everyone except the
	// workspace admins they are invisible afterwards. Leaving under your own
	// account, that is an avoidable surprise — the server gives the number and
	// the interface asks with it.
	if r.URL.Query().Get("confirmPrivate") != "1" {
		var privatePages int
		s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ? AND owner_id = ? AND visibility = 'private' AND trashed_at IS NULL`,
			wsID, target).Scan(&privatePages)
		if privatePages > 0 {
			if target == me {
				httpErrorData(w, 409, "private_pages_left_self",
					fmt.Sprintf("You have %d private page(s) here. They stay in the workspace and will only be visible to its admins afterwards.", privatePages),
					map[string]any{"pages": privatePages})
			} else {
				// Removing SOMEBODY ELSE, the person does not make the decision
				// themselves — all the more reason the warning belongs before the
				// click.
				httpErrorData(w, 409, "private_pages_left_other",
					fmt.Sprintf("This person has %d private page(s) here. They stay in the workspace and will only be visible to its admins afterwards.", privatePages),
					map[string]any{"pages": privatePages})
			}
			return
		}
	}
	s.db.Exec(`DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, wsID, target)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- HTTP: public share links (read-only, no auth) ----

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	// Optional expiry (expiresInDays<=0 = never) and optional password.
	var body struct {
		ExpiresInDays int    `json:"expiresInDays"`
		Password      string `json:"password"`
	}
	decodeJSON(w, r, &body)
	var expiresAt any
	if body.ExpiresInDays > 0 {
		expiresAt = time.Now().UTC().AddDate(0, 0, body.ExpiresInDays).Format(time.RFC3339Nano)
	}
	b := make([]byte, 18)
	rand.Read(b)
	token := hex.EncodeToString(b)
	// Password is stored as sha256(token:password) — salted by the 144-bit
	// random token, verifiable from the URL-supplied token without keeping the
	// raw password. (Casual protection on top of an already-unguessable link,
	// not a substitute for real accounts.)
	var pwHash any
	if body.Password != "" {
		pwHash = tokenHash(token + ":" + body.Password)
	}
	// One live read-share per page: replace any existing read link so a re-share
	// with a new expiry/password doesn't leave the old token valid. Form-shares
	// (mode='form') are independent and left untouched.
	s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode != 'form'`, pageID)
	if _, err := s.db.Exec(`INSERT INTO share_links (token_hash, page_id, created_at, expires_at, password_hash) VALUES (?, ?, ?, ?, ?)`, tokenHash(token), pageID, now(), expiresAt, pwHash); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]string{"token": token, "url": s.publicShareBase(r) + "/public/" + token})
}

func (s *Server) handleUnsharePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode != 'form'`, pageID)
	writeJSON(w, map[string]bool{"ok": true})
}

// handlePublicPage serves a shared page read-only WITHOUT auth. It returns ONLY
// that page (title/icon/content) — never children or linked/related pages — so
// a share cannot leak the rest of the workspace.
// resolveShare validates a share token: existence, expiry (expired links are
// deleted on sight) and password. Returns the page id, whether a password is
// required, and whether the supplied password matches.
func (s *Server) resolveShare(token, password string) (pageID string, needPW, pwOK bool, found bool) {
	var expiresAt, pwHash sql.NullString
	if err := s.db.QueryRow(`SELECT page_id, expires_at, password_hash FROM share_links WHERE token_hash = ? AND mode != 'form'`, tokenHash(token)).Scan(&pageID, &expiresAt, &pwHash); err != nil {
		return "", false, false, false
	}
	if expiresAt.Valid && expiresAt.String != "" {
		if exp, err := time.Parse(time.RFC3339Nano, expiresAt.String); err == nil && time.Now().After(exp) {
			s.db.Exec(`DELETE FROM share_links WHERE token_hash = ?`, tokenHash(token))
			return "", false, false, false
		}
	}
	needPW = pwHash.Valid && pwHash.String != ""
	pwOK = !needPW || (password != "" && tokenHash(token+":"+password) == pwHash.String)
	return pageID, needPW, pwOK, true
}

func (s *Server) handlePublicPage(w http.ResponseWriter, r *http.Request) {
	pageID, needPW, pwOK, found := s.resolveShare(r.PathValue("token"), r.Header.Get("X-Share-Password"))
	if !found {
		httpError(w, 404, "not found")
		return
	}
	if needPW && !pwOK {
		httpError(w, 403, "password required")
		return
	}
	p, err := s.getPage(pageID)
	if err != nil || p.Trashed {
		httpError(w, 404, "not found")
		return
	}
	writeJSON(w, map[string]any{
		"title":   p.Title,
		"icon":    p.Icon,
		"cover":   p.Cover,
		"content": p.Content,
		"type":    p.Type,
	})
}

// handleAccessOverview: for user management — which user is in which workspace
// and with what role.
//
// The owner sees every workspace in the instance; an admin only the ones they
// manage themselves. Otherwise the names of every private workspace would sit
// here along with their member lists — knowledge an admin may do nothing with
// anyway, now that they can no longer change roles there.
func (s *Server) handleAccessOverview(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	// Personal spaces stay out — for the owner too. They belong to a person, not
	// to the instance; anyone able to grant roles here would have exactly the
	// handle that is not meant to exist. Whoever wants to let somebody in does
	// it themselves, through their own space's member management.
	query := `SELECT id, name FROM workspaces WHERE is_personal = 0 ORDER BY name`
	args := []any{}
	if !s.isOwner(me.ID) {
		query = `SELECT w.id, w.name FROM workspaces w
			JOIN workspace_members m ON m.workspace_id = w.id
			WHERE m.user_id = ? AND m.role = 'admin' AND w.is_personal = 0 ORDER BY w.name`
		args = append(args, me.ID)
	}
	wsRows, err := s.db.Query(query, args...)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type ws struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	workspaces := []ws{}
	for wsRows.Next() {
		var x ws
		if wsRows.Scan(&x.ID, &x.Name) == nil {
			workspaces = append(workspaces, x)
		}
	}
	wsRows.Close()

	mRows, err := s.db.Query(`SELECT user_id, workspace_id, role FROM workspace_members`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type mem struct {
		UserID      string `json:"userId"`
		WorkspaceID string `json:"workspaceId"`
		Role        string `json:"role"`
	}
	// Only memberships of the workspaces visible above — otherwise the list
	// would give away who sits in the ones not shown.
	shown := map[string]bool{}
	for _, x := range workspaces {
		shown[x.ID] = true
	}
	memberships := []mem{}
	for mRows.Next() {
		var m mem
		if mRows.Scan(&m.UserID, &m.WorkspaceID, &m.Role) == nil && shown[m.WorkspaceID] {
			memberships = append(memberships, m)
		}
	}
	mRows.Close()
	writeJSON(w, map[string]any{"workspaces": workspaces, "memberships": memberships})
}

// handleAdminMembership sets a user's role in ONE workspace — the user
// management route, as opposed to the /api/workspaces/{id}/members endpoints.
// role "none" = remove the membership. Who may do what is in the checks below:
// the owner everywhere, an admin only in the workspaces they manage themselves
// — and nobody for themselves.
func (s *Server) handleAdminMembership(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID      string `json:"userId"`
		WorkspaceID string `json:"workspaceId"`
		Role        string `json:"role"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if s.userByID(body.UserID) == nil {
		httpError(w, 404, "user not found")
		return
	}
	var wsName string
	if s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, body.WorkspaceID).Scan(&wsName) != nil {
		httpError(w, 404, "workspace not found")
		return
	}
	// Granting memberships here means administering OTHER people. Two limits:
	//
	//  1. Nobody grants themselves access along this route. Otherwise "an admin
	//     may not read other people's content" would be undone by a single call.
	//     The honest route for an owner is emergency access — time-limited, with
	//     a reason, logged, shown to the people in charge.
	//  2. An admin (not an owner) may only assign where they are a workspace
	//     admin themselves — or they could seat a stand-in inside somebody
	//     else's workspace.
	me := requestUser(r)
	if body.UserID == me.ID {
		httpErrorCode(w, 403, "no_self_grant", "You cannot grant yourself access here — use emergency access, which is logged.")
		return
	}
	if !s.isOwner(me.ID) && !s.isWorkspaceAdmin(me.ID, body.WorkspaceID) {
		httpErrorCode(w, 403, "not_workspace_admin", "Only the owner or an admin of this workspace can change its members.")
		return
	}
	// A personal space is NEVER handed out here — not even by the owner.
	// Otherwise keeping it out of the access overview would be pure cosmetics: a
	// stand-in account inside Bob's space, signed in, done — permanently, with
	// no time limit and without Bob seeing any of it. Whoever wants to let
	// somebody in does it themselves, through their own space's member
	// management.
	var personalWS int
	s.db.QueryRow(`SELECT is_personal FROM workspaces WHERE id = ?`, body.WorkspaceID).Scan(&personalWS)
	if personalWS != 0 && !s.isWorkspaceAdmin(me.ID, body.WorkspaceID) {
		httpErrorCode(w, 403, "personal_invite_owner_only", "A personal space is not handed out from outside — only its owner invites anyone there.")
		return
	}
	// Never remove or demote a workspace's last admin — the workspace would be
	// left with nobody in charge.
	demoting := body.Role != "admin" && s.workspaceRole(body.UserID, body.WorkspaceID) == "admin"
	if demoting && s.otherActiveAdmins(body.WorkspaceID, body.UserID) == 0 {
		httpError(w, 400, "cannot remove the last admin of "+wsName)
		return
	}
	if body.Role == "none" {
		s.db.Exec(`DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?`, body.WorkspaceID, body.UserID)
	} else {
		s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = excluded.role`,
			body.WorkspaceID, body.UserID, normalizeRole(body.Role))
	}
	writeJSON(w, map[string]bool{"ok": true})
}
