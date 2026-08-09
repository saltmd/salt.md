package server

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// The condition that makes this feature honest: taking back an agent's change
// must restore what the agent overwrote and must NOT touch a value somebody has
// edited since. Wrong in the permissive direction, it silently eats a person's
// work — which is the exact fear the feature exists to answer.
func TestRevertLeavesLaterEditsAlone(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ada@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"status","name":"Status","type":"text"},
		{"id":"owner","name":"Owner","type":"text"}]`)
	row := s.makeRow(t, ws, uid, tasks, "A task", `{"status":"open","owner":"ada"}`)

	u := &user{ID: uid, Name: "Ada"}

	// The agent changes both properties.
	if _, err := s.mcpSetProperties(row, json.RawMessage(`{"status":"done","owner":"bot"}`), u); err != nil {
		t.Fatal(err)
	}
	var entry int64
	if err := s.db.QueryRow(
		`SELECT id FROM audit_log WHERE action = 'set_properties' AND changes != '' ORDER BY id DESC LIMIT 1`).
		Scan(&entry); err != nil {
		t.Fatal("no audit entry carrying a diff was written:", err)
	}

	// A person then edits ONE of them by hand, after the agent.
	if _, err := s.mcpSetProperties(row, json.RawMessage(`{"owner":"jeremia"}`), nil); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/audit/"+strconv.FormatInt(entry, 10)+"/revert", nil)
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("revert: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ Reverted, Skipped []string }
	json.Unmarshal(rec.Body.Bytes(), &out)

	if strings.Join(out.Reverted, ",") != "status" {
		t.Errorf("reverted = %v, want [status]: the untouched property goes back", out.Reverted)
	}
	if strings.Join(out.Skipped, ",") != "owner" {
		t.Errorf("skipped = %v, want [owner]: a value edited since must be left alone", out.Skipped)
	}

	var props string
	s.db.QueryRow(`SELECT props FROM pages WHERE id = ?`, row).Scan(&props)
	var got map[string]any
	json.Unmarshal([]byte(props), &got)
	if got["status"] != "open" {
		t.Errorf("status = %v, want open — the agent's change was not taken back", got["status"])
	}
	if got["owner"] != "jeremia" {
		t.Errorf("owner = %v, want jeremia — the human edit did not survive the revert", got["owner"])
	}
}
