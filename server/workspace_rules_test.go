package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The guards on PUT /api/workspaces/{id}/rules, exercised through the real
// router. Rules are instructions agents are told to FOLLOW (get_workspace
// hands them out with a follow-this framing), so the write path is the whole
// security story: admin-only, session-only. If an API token could write them,
// the rules channel would be a prompt-injection channel with official
// packaging.

// makeWorkspace creates a workspace directly, sidestepping setup semantics
// (personal spaces, auto-join) that are not under test here.
func makeWorkspace(t *testing.T, s *Server, adminID string) string {
	t.Helper()
	ws := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'Rules WS', ?)`, ws, now()); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, ws, adminID); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	return ws
}

func putRules(t *testing.T, s *Server, wsID, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/workspaces/"+wsID+"/rules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	s.ServeHTTP(rec, req)
	return rec
}

func TestWorkspaceRulesWriteGuards(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, uid)

	// Anonymous: no.
	if rec := putRules(t, s, ws, `{"rules":"x"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous PUT: got %d, want 401", rec.Code)
	}

	// A write-scoped API token of the admin: still no. That is the sessionOnly
	// gate, and the gate is the feature's security story, not decoration.
	raw := newID()
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'probe', ?, 'write', ?)`, newID(), uid, tokenHash(raw), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	if rec := putRules(t, s, ws, `{"rules":"x"}`, map[string]string{"Authorization": "Bearer " + raw}); rec.Code != http.StatusForbidden {
		t.Errorf("token PUT: got %d, want 403 — an API token may not write rules", rec.Code)
	}

	// A plain member: 403. A stranger: 404, not 403 — the workspace's
	// existence is not the stranger's business.
	member, memberCookie := signedIn(t, s, "b@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, member); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if rec := putRules(t, s, ws, `{"rules":"x"}`, map[string]string{"Cookie": memberCookie}); rec.Code != http.StatusForbidden {
		t.Errorf("member PUT: got %d, want 403", rec.Code)
	}
	_, strangerCookie := signedIn(t, s, "c@example.com")
	if rec := putRules(t, s, ws, `{"rules":"x"}`, map[string]string{"Cookie": strangerCookie}); rec.Code != http.StatusNotFound {
		t.Errorf("stranger PUT: got %d, want 404", rec.Code)
	}

	// Nothing above wrote anything.
	var stored string
	s.db.QueryRow(`SELECT rules FROM workspaces WHERE id = ?`, ws).Scan(&stored)
	if stored != "" {
		t.Fatalf("a refused call wrote anyway: %q", stored)
	}

	// The admin over the session cookie: yes — otherwise everything above
	// passes for the wrong reason (a broken route, say).
	if rec := putRules(t, s, ws, `{"rules":"Invoices go into Finance/Inbox."}`, map[string]string{"Cookie": cookie}); rec.Code != http.StatusOK {
		t.Fatalf("admin PUT: got %d, want 200", rec.Code)
	}
	s.db.QueryRow(`SELECT rules FROM workspaces WHERE id = ?`, ws).Scan(&stored)
	if stored != "Invoices go into Finance/Inbox." {
		t.Errorf("stored = %q", stored)
	}

	// Over the length limit: refused with its own code (the dialog translates
	// it), and the stored text stays as it was.
	long := strings.Repeat("a", 16001)
	rec := putRules(t, s, ws, `{"rules":"`+long+`"}`, map[string]string{"Cookie": cookie})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "rules_too_long") {
		t.Errorf("overlong PUT: got %d %q, want 400 rules_too_long", rec.Code, rec.Body.String())
	}
	s.db.QueryRow(`SELECT rules FROM workspaces WHERE id = ?`, ws).Scan(&stored)
	if stored != "Invoices go into Finance/Inbox." {
		t.Errorf("overlong PUT changed the stored rules: %q", stored)
	}
}

func TestWorkspaceRulesReachAgentsOutsideTheUntrustedBlock(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, uid)
	if rec := putRules(t, s, ws, `{"rules":"Titles start with the date."}`, map[string]string{"Cookie": cookie}); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d", rec.Code)
	}

	u := &user{ID: uid}
	out, addendum, err := s.mcpGetWorkspace(u, ws)
	if err != nil {
		t.Fatalf("mcpGetWorkspace: %v", err)
	}
	if !strings.Contains(addendum, "Titles start with the date.") || !strings.Contains(addendum, "BEGIN WORKSPACE RULES") {
		t.Errorf("addendum does not carry the framed rules: %q", addendum)
	}
	// has_rules travels inside the JSON; the text itself must not — the JSON
	// ends up inside the untrusted block, and the rules must sit outside it.
	if !strings.Contains(out, `"has_rules":true`) {
		t.Errorf("workspace JSON does not mark has_rules: %s", out)
	}
	if strings.Contains(out, "Titles start with the date.") {
		t.Errorf("rules text leaked into the untrusted JSON: %s", out)
	}

	// The composed tool answer: untrusted block first, the rules after it,
	// with their own frame.
	full := wrapUntrusted(out) + addendum
	endUntrusted := strings.Index(full, "END UNTRUSTED CONTENT")
	beginRules := strings.Index(full, "BEGIN WORKSPACE RULES")
	if endUntrusted == -1 || beginRules == -1 || beginRules < endUntrusted {
		t.Fatalf("rules are not outside the untrusted block:\n%s", full)
	}
	// And no rules means no frame at all — not an empty one.
	if wrapWorkspaceRules("") != "" {
		t.Errorf("empty rules produced a frame")
	}

	// list_workspaces marks the workspace so an agent knows to fetch them.
	lst, err := s.mcpListWorkspaces(u)
	if err != nil {
		t.Fatalf("mcpListWorkspaces: %v", err)
	}
	var listed struct {
		Workspaces []struct {
			ID       string `json:"id"`
			HasRules bool   `json:"has_rules"`
		} `json:"workspaces"`
	}
	if err := json.Unmarshal([]byte(lst), &listed); err != nil {
		t.Fatalf("unmarshal list_workspaces: %v", err)
	}
	found := false
	for _, w := range listed.Workspaces {
		if w.ID == ws {
			found = w.HasRules
		}
	}
	if !found {
		t.Errorf("list_workspaces does not mark has_rules for %s: %s", ws, lst)
	}
}

// The proposal path (W123b): an agent may DRAFT rules over MCP, and the draft
// is inert — the active rules move only through the admin's browser. That
// asymmetry is the entire point; these tests nail it down.

func TestProposeRulesIsInertUntilAdminApplies(t *testing.T) {
	s := testServer(t)
	admin, adminCookie := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, admin)
	member, _ := signedIn(t, s, "b@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, member); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	// A read-only token cannot even propose.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin, TokenScope: "read"}, ws, "x"); err == nil {
		t.Errorf("read token proposed rules")
	}
	// Rules are the admin's domain end to end: a plain member cannot propose.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: member}, ws, "x"); err == nil {
		t.Errorf("member proposed rules")
	}
	// Neither can a viewer.
	viewer, _ := signedIn(t, s, "c@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'viewer')`, ws, viewer); err != nil {
		t.Fatalf("insert viewer: %v", err)
	}
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: viewer}, ws, "x"); err == nil {
		t.Errorf("viewer proposed rules")
	}
	// A stranger sees no workspace.
	stranger, _ := signedIn(t, s, "d@example.com")
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: stranger}, ws, "x"); err == nil {
		t.Errorf("stranger proposed rules")
	}
	// Overlong drafts are refused.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, strings.Repeat("a", 16001)); err == nil {
		t.Errorf("overlong proposal accepted")
	}

	// The admin's token may propose — and the ACTIVE rules still stay empty.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, "Draft one"); err != nil {
		t.Fatalf("propose: %v", err)
	}
	var rules, proposal, by string
	s.db.QueryRow(`SELECT rules, rules_proposal, rules_proposal_by FROM workspaces WHERE id = ?`, ws).Scan(&rules, &proposal, &by)
	if rules != "" || proposal != "Draft one" || by != admin {
		t.Fatalf("after propose: rules=%q proposal=%q by=%q", rules, proposal, by)
	}

	// A newer draft replaces the older one.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, "Draft two"); err != nil {
		t.Fatalf("second propose: %v", err)
	}
	s.db.QueryRow(`SELECT rules, rules_proposal FROM workspaces WHERE id = ?`, ws).Scan(&rules, &proposal)
	if rules != "" || proposal != "Draft two" {
		t.Fatalf("after second propose: rules=%q proposal=%q", rules, proposal)
	}

	// The admin applies in the browser: rules become active, the slot empties.
	if rec := putRules(t, s, ws, `{"rules":"Draft two"}`, map[string]string{"Cookie": adminCookie}); rec.Code != http.StatusOK {
		t.Fatalf("admin apply: %d", rec.Code)
	}
	s.db.QueryRow(`SELECT rules, rules_proposal, rules_proposal_by FROM workspaces WHERE id = ?`, ws).Scan(&rules, &proposal, &by)
	if rules != "Draft two" || proposal != "" || by != "" {
		t.Fatalf("after apply: rules=%q proposal=%q by=%q", rules, proposal, by)
	}
}

func TestProposalWithdrawOnlyOwn(t *testing.T) {
	s := testServer(t)
	admin, _ := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, admin)
	// Two admins: proposing needs the admin role now, and the withdraw rule
	// still distinguishes WHOSE draft it is.
	alice, _ := signedIn(t, s, "alice@example.com")
	bob, _ := signedIn(t, s, "bob@example.com")
	for _, id := range []string{alice, bob} {
		if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, ws, id); err != nil {
			t.Fatalf("insert admin: %v", err)
		}
	}
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: alice}, ws, "Alice's draft"); err != nil {
		t.Fatalf("propose: %v", err)
	}
	// Bob cannot withdraw Alice's draft.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: bob}, ws, ""); err == nil {
		t.Errorf("bob withdrew alice's proposal")
	}
	// Alice can.
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: alice}, ws, ""); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	var proposal string
	s.db.QueryRow(`SELECT rules_proposal FROM workspaces WHERE id = ?`, ws).Scan(&proposal)
	if proposal != "" {
		t.Errorf("proposal still there: %q", proposal)
	}
}

func TestDismissProposalGuards(t *testing.T) {
	s := testServer(t)
	admin, adminCookie := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, admin)
	member, memberCookie := signedIn(t, s, "b@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, member); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, "Draft"); err != nil {
		t.Fatalf("propose: %v", err)
	}

	del := func(hdr map[string]string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/api/workspaces/"+ws+"/rules-proposal", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		s.ServeHTTP(rec, req)
		return rec
	}

	// An API token — even the admin's — is turned away (sessionOnly).
	raw := newID()
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'probe', ?, 'write', ?)`, newID(), admin, tokenHash(raw), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	if rec := del(map[string]string{"Authorization": "Bearer " + raw}); rec.Code != http.StatusForbidden {
		t.Errorf("token DELETE: got %d, want 403", rec.Code)
	}
	// A plain member cannot dismiss.
	if rec := del(map[string]string{"Cookie": memberCookie}); rec.Code != http.StatusForbidden {
		t.Errorf("member DELETE: got %d, want 403", rec.Code)
	}
	var proposal string
	s.db.QueryRow(`SELECT rules_proposal FROM workspaces WHERE id = ?`, ws).Scan(&proposal)
	if proposal != "Draft" {
		t.Fatalf("a refused dismiss removed the proposal: %q", proposal)
	}
	// The admin in the browser can.
	if rec := del(map[string]string{"Cookie": adminCookie}); rec.Code != http.StatusOK {
		t.Fatalf("admin DELETE: got %d, want 200", rec.Code)
	}
	s.db.QueryRow(`SELECT rules_proposal FROM workspaces WHERE id = ?`, ws).Scan(&proposal)
	if proposal != "" {
		t.Errorf("proposal survived the dismiss: %q", proposal)
	}
}

func TestRulesAddendumHints(t *testing.T) {
	// Pure function, fixed voices: the addendum is server-authored and must
	// never carry user text beyond the admin's rules themselves. The nudges
	// (draft some, one is waiting) go to admins alone — a member's agent gets
	// the rules to follow and nothing to raise.
	if a := rulesAddendum("", "", true); !strings.Contains(a, "no rules yet") || !strings.Contains(a, "propose_workspace_rules") {
		t.Errorf("admin empty/empty addendum: %q", a)
	}
	if a := rulesAddendum("", "", false); a != "" {
		t.Errorf("member got a rules nudge: %q", a)
	}
	if a := rulesAddendum("", "pending", true); !strings.Contains(a, "already waiting") || strings.Contains(a, "pending") {
		t.Errorf("proposal-only addendum leaks or lacks hint: %q", a)
	}
	if a := rulesAddendum("", "pending", false); a != "" {
		t.Errorf("member learned about a pending proposal: %q", a)
	}
	if a := rulesAddendum("Rule.", "pending", true); !strings.Contains(a, "BEGIN WORKSPACE RULES") || !strings.Contains(a, "also waiting") {
		t.Errorf("rules+proposal addendum: %q", a)
	}
	if a := rulesAddendum("Rule.", "pending", false); !strings.Contains(a, "BEGIN WORKSPACE RULES") || strings.Contains(a, "waiting") {
		t.Errorf("member addendum must carry the rules and no proposal talk: %q", a)
	}

	// And get_workspace reports the pending flag inside the JSON.
	s := testServer(t)
	admin, _ := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, admin)
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, "Draft"); err != nil {
		t.Fatalf("propose: %v", err)
	}
	out, addendum, err := s.mcpGetWorkspace(&user{ID: admin}, ws)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(out, `"has_pending_rules_proposal":true`) {
		t.Errorf("JSON lacks pending flag: %s", out)
	}
	if strings.Contains(addendum, "Draft") {
		t.Errorf("proposal text leaked into the addendum: %q", addendum)
	}
}

func TestProposalHiddenFromMembers(t *testing.T) {
	s := testServer(t)
	admin, adminCookie := signedIn(t, s, "a@example.com")
	ws := makeWorkspace(t, s, admin)
	member, memberCookie := signedIn(t, s, "b@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, member); err != nil {
		t.Fatalf("insert member: %v", err)
	}
	if _, err := s.mcpProposeWorkspaceRules(&user{ID: admin}, ws, "Secret draft"); err != nil {
		t.Fatalf("propose: %v", err)
	}

	get := func(cookie string) string {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/workspaces", nil)
		req.Header.Set("Cookie", cookie)
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET workspaces: %d", rec.Code)
		}
		return rec.Body.String()
	}
	// The admin sees the draft; the member does not even receive it.
	if body := get(adminCookie); !strings.Contains(body, "Secret draft") {
		t.Errorf("admin list lacks the proposal: %s", body)
	}
	if body := get(memberCookie); strings.Contains(body, "Secret draft") {
		t.Errorf("member list leaks the proposal: %s", body)
	}
}
