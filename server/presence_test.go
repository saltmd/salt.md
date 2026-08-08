package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// An agent says which page it is working on, so a person watching sees it live.
//
// The one thing these tests exist to protect is the decision that a check-in
// does NOT expire. An agent has no clock and cannot wake itself to send a
// heartbeat, so a lease would erase a three-hour job halfway through — and the
// obvious "fix" (ask agents to check in every ten minutes) asks for something
// structurally impossible.

func presenceOf(t *testing.T, s *Server, cookie string) []presenceEntry {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/presence", nil)
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /api/presence: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Working []presenceEntry `json:"working"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("presence is not JSON: %v", err)
	}
	return out.Working
}

func TestWorkingOnCheckInAndOut(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "presence@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := s.mcpWorkingOn(u, page, "claude", "Claude Code", "tidying the file index", 180, false); err != nil {
		t.Fatalf("check in: %v", err)
	}
	list := presenceOf(t, s, cookie)
	if len(list) != 1 {
		t.Fatalf("expected one entry, got %d", len(list))
	}
	e := list[0]
	if e.Agent != "claude" || e.Label != "Claude Code" {
		t.Errorf("identity lost: %+v", e)
	}
	if e.Note != "tidying the file index" || e.ExpectedMin != 180 {
		t.Errorf("note or estimate lost: %+v", e)
	}
	// The account travels WITH the claim — the agent name is unverified, the
	// account is not, and the interface shows both.
	if e.AccountName == "" {
		t.Error("the entry does not say which account it came through")
	}

	if _, err := s.mcpWorkingOn(u, page, "claude", "", "", 0, true); err != nil {
		t.Fatalf("check out: %v", err)
	}
	if list := presenceOf(t, s, cookie); len(list) != 0 {
		t.Errorf("still listed after checking out: %+v", list)
	}
}

// The decision this feature stands on: silence does not remove anybody.
func TestPresenceSurvivesALongSilence(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "long@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := s.mcpWorkingOn(u, page, "claude", "", "writing the migration", 0, false); err != nil {
		t.Fatalf("check in: %v", err)
	}
	// Three hours of working somewhere else entirely.
	silent := time.Now().UTC().Add(-3 * time.Hour).Format(time.RFC3339)
	if _, err := s.db.Exec(`UPDATE agent_presence SET last_seen = ?, started_at = ?`, silent, silent); err != nil {
		t.Fatalf("age it: %v", err)
	}
	list := presenceOf(t, s, cookie)
	if len(list) != 1 {
		t.Fatal("three hours of silence erased a running job — that is the bug this design exists to avoid")
	}
	// It still reports WHEN it was last heard from, so the interface can fade it
	// rather than claim it is active.
	if list[0].LastSeen != silent {
		t.Errorf("last seen is %q, want the old timestamp so staleness stays visible", list[0].LastSeen)
	}

	// Half a day is a crash, not a long job.
	dead := time.Now().UTC().Add(-13 * time.Hour).Format(time.RFC3339)
	s.db.Exec(`UPDATE agent_presence SET last_seen = ?`, dead)
	if list := presenceOf(t, s, cookie); len(list) != 0 {
		t.Errorf("a session silent for 13 hours should have been swept: %+v", list)
	}
}

// Any other call on the page refreshes it, so an agent working inside salt.md
// never has to spend a call saying "still here".
func TestAnyCallIsASignOfLife(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "touch@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	s.mcpWorkingOn(u, page, "claude", "", "", 0, false)
	old := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	s.db.Exec(`UPDATE agent_presence SET last_seen = ?`, old)

	if _, err := s.mcpCall(u, "get_page", json.RawMessage(`{"page_id":"`+page+`"}`), ""); err != nil {
		t.Fatalf("get_page: %v", err)
	}
	if list := presenceOf(t, s, cookie); len(list) != 1 || list[0].LastSeen == old {
		t.Error("a call on the page did not refresh the check-in")
	}
}

// An unknown agent is accepted and shown neutrally. Refusing it would mean the
// feature is worth nothing on the day a new client appears — the client would
// simply not announce itself.
func TestUnknownAgentIsAcceptedNotRefused(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "unknown@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := s.mcpWorkingOn(u, page, "SomeBrandNewThing", "", "having a look", 0, false); err != nil {
		t.Fatalf("an unknown agent must not be refused: %v", err)
	}
	list := presenceOf(t, s, cookie)
	if len(list) != 1 {
		t.Fatal("not listed")
	}
	if list[0].Agent != "generic" {
		t.Errorf("agent key is %q, want the neutral badge", list[0].Agent)
	}
	// What it actually calls itself must survive, or the badge says nothing.
	if list[0].Label != "SomeBrandNewThing" {
		t.Errorf("label is %q, want the name it gave", list[0].Label)
	}
}

// The list is per page, and it must not leak the existence of pages the caller
// cannot read — the whole reason it is a route and not a payload on the event
// bus, which reaches every browser on the instance.
func TestPresenceHidesPagesTheCallerMayNotRead(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "owner2@example.test")
	colleague, colleagueCookie := signedIn(t, s, "colleague2@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	s.addMember(t, ws, colleague, "member")

	secret := s.makePage(t, ws, owner, "", "Secret plan", `{}`)
	s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, secret)
	shared := s.makePage(t, ws, owner, "", "Shared", `{}`)

	ownerUser := &user{ID: owner, Name: "Owner"}
	s.mcpWorkingOn(ownerUser, secret, "claude", "", "on the secret", 0, false)
	s.mcpWorkingOn(ownerUser, shared, "claude", "", "on the shared one", 0, false)

	for _, e := range presenceOf(t, s, colleagueCookie) {
		if e.PageID == secret {
			t.Errorf("a colleague sees activity on a private page: %+v", e)
		}
		if strings.Contains(e.PageTitle, "Secret") {
			t.Errorf("the title of a private page leaked: %q", e.PageTitle)
		}
	}
	// And the one they may see is there — hiding everything would also be wrong.
	found := false
	for _, e := range presenceOf(t, s, colleagueCookie) {
		if e.PageID == shared {
			found = true
		}
	}
	if !found {
		t.Error("the shared page's activity is hidden from a member who may read it")
	}
}

// Checking in on a page you cannot read must not work — it would otherwise be a
// way to confirm that a page id exists.
func TestCheckInNeedsReadAccess(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "o3@example.test")
	stranger, _ := signedIn(t, s, "s3@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	page := s.makePage(t, ws, owner, "", "Doc", `{}`)

	if _, err := s.mcpWorkingOn(&user{ID: stranger}, page, "claude", "", "", 0, false); err == nil {
		t.Error("a stranger checked in on somebody else's page")
	}
}

// A check-in must leave ONE log entry, and it must be the readable one.
//
// The generic MCP audit records every writing call under the tool's name with
// the tool's REPLY as the detail. For this tool that produced a second line
// reading "started working on: Checked out of page b534…" — the wrong verb and
// an unreadable detail, right beside the correct entry.
func TestCheckInLeavesOneReadableLogEntry(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "log@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := s.mcpCall(u, "working_on", json.RawMessage(
		`{"page_id":"`+page+`","agent":"claude","note":"tidying the file index"}`), ""); err != nil {
		t.Fatalf("check in: %v", err)
	}
	rows, err := s.db.Query(`SELECT action, detail FROM audit_log WHERE page_id = ?`, page)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	type entry struct{ action, detail string }
	var got []entry
	for rows.Next() {
		var e entry
		if rows.Scan(&e.action, &e.detail) == nil {
			got = append(got, e)
		}
	}
	rows.Close()

	if len(got) != 1 {
		t.Fatalf("expected one entry, got %d: %+v", len(got), got)
	}
	if got[0].action != "working_on" {
		t.Errorf("action is %q", got[0].action)
	}
	// The note, not the tool's reply — that is what makes the line worth reading.
	if got[0].detail != "tidying the file index" {
		t.Errorf("detail is %q, want the note", got[0].detail)
	}
}
