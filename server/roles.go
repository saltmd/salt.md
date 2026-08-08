package server

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// Permission model (W101).
//
// Four levels, outside in:
//
//	Owner            runs the instance. Has access to the SQLite file anyway —
//	                 that is the honest boundary. Inside the app it means:
//	                 instance configuration, the account lifecycle (password
//	                 resets included) and emergency access (break_glass), but
//	                 every one of those acts leaves a trace.
//	Admin            manages people, not content. Create and maintain accounts,
//	                 invite, see the user list. Expressly NOT: set somebody
//	                 else's password, add themselves to somebody else's
//	                 workspace, export somebody else's workspace. Without those
//	                 three prohibitions the boundary would be theatre — whoever
//	                 can set a password can sign in and read everything.
//	Workspace admin  everything inside their own workspace, nothing outside it.
//	Member/viewer
//
// The roles live in org_members, deliberately mirroring workspace_members.
// Today exactly one organisation exists (this instance); should this ever
// become a hosted multi-tenant version, org_id is already the boundary.

const (
	roleOwner  = "owner"
	roleAdmin  = "admin"
	roleMember = "member"

	// breakGlassTTL: long enough for a review, short enough that a forgotten
	// grant does not become a permanent state.
	breakGlassTTL = 2 * time.Hour
)

// tsFixed is RFC3339 with ALWAYS nine decimal places. now() uses RFC3339Nano,
// which trims trailing zeroes — that makes a shorter timestamp a prefix of a
// longer one, and in SQL's string comparison the 'Z' (90) then wrongly beats
// any digit (48-57). An expired emergency grant could go on counting as valid.
// Fixed width means: lexicographic == chronological.
const tsFixed = "2006-01-02T15:04:05.000000000Z07:00"

func nowFixed() string { return time.Now().UTC().Format(tsFixed) }

// headerSafe strips line breaks out of a mail header value.
func headerSafe(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}

// defaultOrg returns the (today only) organisation. The value does not change
// at runtime but is deliberately not cached globally: the call is cheap, and a
// cache would be the first thing to go wrong the day this becomes
// multi-tenant.
func (s *Server) defaultOrg() string {
	var id string
	s.db.QueryRow(`SELECT id FROM organizations ORDER BY created_at LIMIT 1`).Scan(&id)
	return id
}

// migrateOrg creates the organisation and derives the roles from what is
// already there. Idempotent: runs on every start, changes nothing after the
// first time.
func (s *Server) migrateOrg() error {
	var userCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&userCount)
	if userCount == 0 {
		// Fresh installation: handleSetup creates the organisation and owner.
		return nil
	}
	orgID := s.defaultOrg()
	if orgID == "" {
		orgID = newID()
		name := s.setting("instance_name", "")
		if strings.TrimSpace(name) == "" {
			name = "salt.md"
		}
		if _, err := s.db.Exec(`INSERT INTO organizations (id, name, created_at) VALUES (?, ?, ?)`,
			orgID, name, now()); err != nil {
			return fmt.Errorf("create organization: %w", err)
		}
	}
	// If an owner already exists they stay one — the election below only fires
	// while there is none.
	var existingOwner string
	s.db.QueryRow(`SELECT user_id FROM org_members WHERE org_id = ? AND role = ?`, orgID, roleOwner).Scan(&existingOwner)

	// The longest-serving admin becomes owner — they set the instance up. With
	// no admin at all (which should not happen) it falls back to the
	// longest-serving user, so the instance is never ownerless. disabled = 0:
	// making a deactivated account the owner is a dead end — it cannot sign in,
	// and owners cannot be deactivated, so it cannot be reactivated either.
	var ownerID string
	s.db.QueryRow(`SELECT id FROM users WHERE is_admin = 1 AND disabled = 0 ORDER BY created_at LIMIT 1`).Scan(&ownerID)
	if ownerID == "" {
		s.db.QueryRow(`SELECT id FROM users WHERE disabled = 0 ORDER BY created_at LIMIT 1`).Scan(&ownerID)
	}
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
		if rows.Scan(&x.id, &x.admin) == nil {
			users = append(users, x)
		}
	}
	rows.Close()
	for _, x := range users {
		role := roleMember
		if x.admin != 0 {
			role = roleAdmin
		}
		if x.id == ownerID && existingOwner == "" {
			role = roleOwner
		}
		// ON CONFLICT DO NOTHING: a role granted by hand later must not be
		// overwritten by a restart.
		s.db.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			orgID, x.id, role)
	}
	// By-election: if the instance has no owner (because the account was removed
	// outside the app, say), the longest-serving admin is promoted — otherwise
	// it would be left with nobody in charge, and the role cannot be granted
	// anywhere in the app. The UPDATE is needed because their DO-NOTHING insert
	// above bounces off the existing admin row.
	if existingOwner == "" && ownerID != "" {
		s.db.Exec(`UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ?`, roleOwner, orgID, ownerID)
	}
	// Existing instances: every newcomer used to land in the oldest workspace.
	// So the entry point does not change SILENTLY after the update, exactly that
	// one is marked "open to all" — visible in the workspace menu and switchable
	// off. Only when none is marked yet, or a deliberate decision by the owner
	// would be overwritten on every restart.
	var marked int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE auto_join = 1`).Scan(&marked)
	if marked == 0 {
		var oldest string
		s.db.QueryRow(`SELECT id FROM workspaces WHERE is_personal = 0 ORDER BY created_at LIMIT 1`).Scan(&oldest)
		if oldest != "" {
			s.db.Exec(`UPDATE workspaces SET auto_join = 1 WHERE id = ?`, oldest)
		}
	}

	// Workspaces with no owner: the longest-serving workspace admin takes over,
	// failing that the instance owner. A workspace without an owner would be
	// precisely the ownerless state W101 does away with.
	wsRows, err := s.db.Query(`SELECT id FROM workspaces WHERE owner_id = ''`)
	if err != nil {
		return err
	}
	var wsIDs []string
	for wsRows.Next() {
		var id string
		if wsRows.Scan(&id) == nil {
			wsIDs = append(wsIDs, id)
		}
	}
	wsRows.Close()
	for _, wsID := range wsIDs {
		var owner string
		s.db.QueryRow(`SELECT m.user_id FROM workspace_members m
			JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id = ? AND m.role = 'admin'
			ORDER BY u.created_at LIMIT 1`, wsID).Scan(&owner)
		if owner == "" {
			owner = ownerID
		}
		if owner != "" {
			s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ?`, owner, wsID)
		}
	}
	return nil
}

// ---- Where a new account lands (W102) ------------------------------------
//
// Two paths, and only these: a workspace that belongs to it, and the ones the
// owner has expressly opened to everyone. Before this, a newly created account
// inherited every workspace of the admin who created it, and anyone who
// registered themselves landed in the instance's oldest workspace — both
// assumptions nobody had ever actually made.

// personalWorkspaceName: the space carries the person's name.
func personalWorkspaceName(userName string) string {
	userName = strings.TrimSpace(userName)
	if userName == "" {
		return "Personal"
	}
	if len([]rune(userName)) > 60 {
		userName = string([]rune(userName)[:60])
	}
	return userName
}

// createPersonalWorkspace creates an account's own space: the person is both
// its owner and its admin. Returns "" when that fails — the account is then
// left without a space of its own, which the interface handles.
func (s *Server) createPersonalWorkspace(userID, userName string) string {
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id, is_personal) VALUES (?, ?, ?, ?, 1)`,
		id, personalWorkspaceName(userName), now(), userID); err != nil {
		log.Printf("personal workspace for %s: %v", userID, err)
		return ""
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, id, userID); err != nil {
		log.Printf("personal workspace membership for %s: %v", userID, err)
	}
	return id
}

// joinAutoWorkspaces adds an account to every workspace that is open to all.
// Role: member — anyone needing more gets it granted.
func (s *Server) joinAutoWorkspaces(userID string) int {
	rows, err := s.db.Query(`SELECT id FROM workspaces WHERE auto_join = 1`)
	if err != nil {
		return 0
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member') ON CONFLICT DO NOTHING`, id, userID)
	}
	return len(ids)
}

// onboardUser gives a freshly created account its place: its own space plus
// the workspaces open to everyone.
func (s *Server) onboardUser(userID, userName string) {
	s.createPersonalWorkspace(userID, userName)
	s.joinAutoWorkspaces(userID)
}

// orgRole returns the instance role: owner | admin | member (empty = unknown).
func (s *Server) orgRole(userID string) string {
	var role string
	// With org_id, even though only one organisation exists today: the primary
	// key is (org_id, user_id), so a user can have several rows. Without the
	// condition QueryRow would pick an arbitrary one — and this is exactly the
	// boundary meant to separate tenants later.
	s.db.QueryRow(`SELECT role FROM org_members WHERE org_id = ? AND user_id = ?`, s.defaultOrg(), userID).Scan(&role)
	if role == "" {
		// Fallback for accounts created before the migration, or whose row is
		// missing: the old is_admin column decides. Never more rights than
		// before, only never fewer.
		if u := s.userByID(userID); u != nil && u.IsAdmin {
			return roleAdmin
		}
		return roleMember
	}
	return role
}

func (s *Server) isOwner(userID string) bool { return s.orgRole(userID) == roleOwner }

// addOrgMember records a freshly created account in the organisation. Without
// the row the is_admin fallback in orgRole would still work, but the table
// would stay full of holes — and that table is the tenant boundary later on.
func (s *Server) addOrgMember(userID string, isAdmin bool) {
	role := roleMember
	if isAdmin {
		role = roleAdmin
	}
	if org := s.defaultOrg(); org != "" {
		s.db.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, org, userID, role)
	}
}

// sessionOnly requires a browser sign-in — an API token is turned away.
//
// The reason is the boundary the whole permission model rests on: a token is a
// SECOND KEY TO CONTENT, not a pass for administration. It carries its human's
// full identity (see currentUser) and can be narrowed in only two ways — "read
// only" and "these workspaces only". Both act on pages and nothing else.
// Without this guard, a token had the run of:
//
//   - the instance backup — as a GET, so even with a READ-ONLY token: every
//     workspace, every file, every password hash
//   - the account list including email addresses, workspace limit or not
//   - issuing itself a new, UNLIMITED token, which made the workspace limit
//     pure decoration
//
// Handing an agent access hands it content. Accounts, backups, emergency
// access and owner handover stay with a human at a keyboard.
func (s *Server) sessionOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TokenScope is empty for a cookie session and set for every token
		// ("read" or "write").
		if requestUser(r).TokenScope != "" {
			httpErrorCode(w, http.StatusForbidden, "session_required",
				"This action requires signing in through a browser — an API token is not enough.")
			return
		}
		next(w, r)
	}
}

// ownerOnly guards endpoints open to the instance owner alone.
func (s *Server) ownerOnly(next http.HandlerFunc) http.HandlerFunc {
	return s.auth(s.sessionOnly(func(w http.ResponseWriter, r *http.Request) {
		if !s.isOwner(requestUser(r).ID) {
			httpErrorCode(w, http.StatusForbidden, "owner_only", "Only the owner of this instance can do that.")
			return
		}
		next(w, r)
	}))
}

// handleTransferOwner hands the instance to another account.
//
// Without this path the owner role was a one-way street: it could not be
// deactivated, not deleted and not granted anywhere. If the owner left the
// company, the only way out was editing the database by hand — while two
// separate error messages advised the admin to "hand the owner role on first".
//
// The conditions are deliberately tight: only the owner themselves, only to an
// active instance-admin account, and the handover is complete — afterwards the
// old owner is an ordinary admin. A second owner must never come into being,
// or "exactly one person carries the responsibility" stops being true.
func (s *Server) handleTransferOwner(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	var body struct {
		UserID string `json:"userId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	target := s.userByID(body.UserID)
	if target == nil {
		httpError(w, 404, "user not found")
		return
	}
	if target.ID == me.ID {
		httpErrorCode(w, 400, "already_owner", "You are already the owner.")
		return
	}
	if target.Disabled {
		httpErrorCode(w, 400, "disabled_cannot_own", "A deactivated account cannot take over the instance.")
		return
	}
	if !target.IsAdmin {
		httpErrorCode(w, 400, "owner_must_be_admin", "Only an account that is already an instance admin can take the instance over — make it one first.")
		return
	}
	orgID := s.defaultOrg()
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()
	// Demote first, then promote: the other way round there would be a moment
	// with two owner rows.
	if _, err := tx.Exec(`UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ?`, roleAdmin, orgID, me.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)
		ON CONFLICT(org_id, user_id) DO UPDATE SET role = excluded.role`, orgID, target.ID, roleOwner); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// The old owner's running emergency grants end with their role — otherwise
	// they would keep read access to other people's workspaces for up to two
	// hours after the handover.
	s.db.Exec(`UPDATE break_glass SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, nowFixed(), me.ID)
	s.audit("human", me.ID, me.Name, "transfer_owner", "", "", target.Name)
	writeJSON(w, map[string]any{"ok": true, "owner": target.Name})
}

// ---- Emergency access -----------------------------------------------------

// hasBreakGlass reports a valid, unrevoked emergency grant.
func (s *Server) hasBreakGlass(userID, workspaceID string) bool {
	if userID == "" || workspaceID == "" {
		return false
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM break_glass
		WHERE user_id = ? AND workspace_id = ? AND revoked_at IS NULL AND expires_at > ?`,
		userID, workspaceID, nowFixed()).Scan(&n)
	return n > 0
}

// handleBreakGlass gives an owner time-limited READ access to a workspace they
// do not belong to — with a stated reason, in the log, and visible to the
// people in charge of that workspace. Writing stays with real members: the
// purpose is a look inside (a review, an orphaned workspace), not taking part.
func (s *Server) handleBreakGlass(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	wsID := r.PathValue("id")
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	reason := strings.TrimSpace(body.Reason)
	// No access without a reason on the record — that is the entire difference
	// between emergency access and a quiet back door.
	if len([]rune(reason)) < 10 {
		httpErrorCode(w, 400, "reason_too_short",
			"Please give a reason somebody can follow (at least 10 characters) — it is logged and shown to the people in charge of this workspace.")
		return
	}
	if len([]rune(reason)) > 500 {
		reason = string([]rune(reason)[:500])
	}
	var wsName string
	if s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, wsID).Scan(&wsName) != nil {
		httpError(w, 404, "workspace not found")
		return
	}
	if s.isMember(u.ID, wsID) {
		httpErrorCode(w, 400, "already_member", "You are already a member of this workspace — emergency access is not needed.")
		return
	}
	// A personal space is off limits to emergency access too. Otherwise the
	// whole promise would be hollow: the owner learns the id anyway (deletion
	// impact, clean-up view), and with emergency access the full export would
	// then be open to them — permanent enough at two hours, on a reason they
	// wrote themselves. Whoever runs the instance can reach everything through
	// the backup if it really comes to that; that is the honest route, and it
	// leaves a trace that does not look like permission.
	var personal int
	s.db.QueryRow(`SELECT is_personal FROM workspaces WHERE id = ?`, wsID).Scan(&personal)
	if personal != 0 {
		httpErrorCode(w, 403, "personal_no_break_glass", "A personal space cannot be looked into even in an emergency — it belongs to exactly one account.")
		return
	}
	expires := time.Now().UTC().Add(breakGlassTTL).Format(tsFixed)
	if _, err := s.db.Exec(`INSERT INTO break_glass (id, workspace_id, user_id, reason, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		newID(), wsID, u.ID, reason, now(), expires); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.audit("human", u.ID, u.Name, "break_glass", "", wsID, wsName+" — "+reason)
	// Concurrent: sending speaks SMTP once per recipient, or the response would
	// hang on the timeout of an unreachable mail server.
	go s.notifyWorkspaceAdmins(wsID, wsName, u.Name, reason)
	writeJSON(w, map[string]any{"ok": true, "expiresAt": expires, "workspace": wsName})
}

// notifyWorkspaceAdmins tells the people in charge by email. If sending fails
// (no SMTP configured), the log entry remains — the access is still on the
// record, it just was not delivered.
func (s *Server) notifyWorkspaceAdmins(wsID, wsName, actor, reason string) {
	rows, err := s.db.Query(`SELECT u.email FROM workspace_members m
		JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND m.role = 'admin'`, wsID)
	if err != nil {
		return
	}
	var mails []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil && e != "" {
			mails = append(mails, e)
		}
	}
	rows.Close()
	body := fmt.Sprintf(
		"%s, the owner of this instance, has taken time-limited read access to the workspace %q.\n\nReason given:\n%s\n\nThe access ends automatically after 2 hours and is recorded in the activity log.",
		actor, wsName, reason)
	for _, to := range mails {
		if err := s.sendMail(to, "Emergency access to "+wsName, body); err != nil {
			log.Printf("break-glass notice to %s: %v", to, err)
		}
	}
}

// handleListBreakGlass shows a workspace's emergency grants — for the people
// in charge of it and for owners. Access you cannot read back is not
// controlled access.
func (s *Server) handleListBreakGlass(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	wsID := r.PathValue("id")
	if !s.isWorkspaceAdmin(u.ID, wsID) && !s.isOwner(u.ID) {
		httpError(w, 403, "workspace admin or owner only")
		return
	}
	rows, err := s.db.Query(`SELECT b.id, u.name, b.reason, b.created_at, b.expires_at, b.revoked_at
		FROM break_glass b JOIN users u ON u.id = b.user_id
		WHERE b.workspace_id = ? ORDER BY b.created_at DESC LIMIT 50`, wsID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	type entry struct {
		ID        string  `json:"id"`
		User      string  `json:"user"`
		Reason    string  `json:"reason"`
		CreatedAt string  `json:"createdAt"`
		ExpiresAt string  `json:"expiresAt"`
		RevokedAt *string `json:"revokedAt"`
		Active    bool    `json:"active"`
	}
	list := []entry{}
	nowStr := nowFixed()
	for rows.Next() {
		var e entry
		if rows.Scan(&e.ID, &e.User, &e.Reason, &e.CreatedAt, &e.ExpiresAt, &e.RevokedAt) == nil {
			e.Active = e.RevokedAt == nil && e.ExpiresAt > nowStr
			list = append(list, e)
		}
	}
	writeJSON(w, list)
}

// handleRevokeBreakGlass ends a running emergency grant at once. The people in
// charge of the workspace can do it themselves — otherwise the notification
// would be a message with no handle on it.
func (s *Server) handleRevokeBreakGlass(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	wsID := r.PathValue("id")
	if !s.isWorkspaceAdmin(u.ID, wsID) && !s.isOwner(u.ID) {
		httpError(w, 403, "workspace admin or owner only")
		return
	}
	res, err := s.db.Exec(`UPDATE break_glass SET revoked_at = ?
		WHERE workspace_id = ? AND id = ? AND revoked_at IS NULL`, now(), wsID, r.PathValue("grantId"))
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpError(w, 404, "no active grant with that id")
		return
	}
	s.audit("human", u.ID, u.Name, "break_glass_revoked", "", wsID, "")
	writeJSON(w, map[string]bool{"ok": true})
}
