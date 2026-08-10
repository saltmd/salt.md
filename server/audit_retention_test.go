package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The dangerous direction is "0 means delete everything". Every instance out
// there has no setting stored, so a wrong reading of the default would empty
// the whole activity log the first time this code ran — silently, in a
// background sweep, with no way back.
func TestAuditRetentionOffByDefault(t *testing.T) {
	s := testServer(t)

	old := time.Now().UTC().AddDate(0, 0, -400).Format(time.RFC3339Nano)
	s.db.Exec(`INSERT INTO audit_log (created_at, actor_type, actor_id, actor_name, action) VALUES (?, 'human', 'u1', 'Ada', 'update_page')`, old)

	if days := s.auditRetentionDays(); days != 0 {
		t.Fatalf("default retention = %d days, want 0 (keep forever)", days)
	}
	if n := s.pruneAuditLog(); n != 0 {
		t.Errorf("pruned %d entries with no period set — an unconfigured instance must lose nothing", n)
	}

	var count int
	s.db.QueryRow(`SELECT count(*) FROM audit_log`).Scan(&count)
	if count != 1 {
		t.Errorf("log holds %d entries, want the 400-day-old one still there", count)
	}
}

// And the other direction: once a period IS set, it has to actually bite, and
// it must not take anything inside the window with it.
func TestAuditRetentionKeepsWhatIsInsideTheWindow(t *testing.T) {
	s := testServer(t)

	stamp := func(daysAgo int) string {
		return time.Now().UTC().AddDate(0, 0, -daysAgo).Format(time.RFC3339Nano)
	}
	for _, d := range []int{1, 29, 31, 400} {
		s.db.Exec(`INSERT INTO audit_log (created_at, actor_type, actor_id, actor_name, action) VALUES (?, 'agent', 'a1', 'claude', 'set_properties')`, stamp(d))
	}
	s.setSetting("audit_days", "30")

	if got := s.auditRetentionDays(); got != 30 {
		t.Fatalf("retention = %d, want 30", got)
	}
	if n := s.pruneAuditLog(); n != 2 {
		t.Errorf("pruned %d, want 2 — the 31- and 400-day-old entries", n)
	}

	var count int
	s.db.QueryRow(`SELECT count(*) FROM audit_log`).Scan(&count)
	if count != 2 {
		t.Errorf("%d entries left, want 2 — yesterday and 29 days ago stay", count)
	}
}

// The bug this covers named the wrong person, which is the worst way to be
// wrong: a property edited in the browser moved the timestamp and left the
// PREVIOUS editor's name in the "last activity" column, because that column
// reads the name from the newest log entry and the browser wrote none.
func TestBrowserPropertyEditIsRecorded(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ada@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"status","name":"Status","type":"text"}]`)
	row := s.makeRow(t, ws, uid, db, "A task", `{"status":"open"}`)

	patch := func(v string) {
		req := httptest.NewRequest("PATCH", "/api/pages/"+row,
			strings.NewReader(`{"propsPatch":{"status":`+v+`}}`))
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
		}
	}

	patch(`"done"`)
	var changes string
	if err := s.db.QueryRow(
		`SELECT changes FROM audit_log WHERE page_id = ? AND action = 'set_properties' ORDER BY id DESC LIMIT 1`,
		row).Scan(&changes); err != nil {
		t.Fatal("a property edit from the browser wrote no log entry:", err)
	}
	var got map[string]propChange
	if err := json.Unmarshal([]byte(changes), &got); err != nil {
		t.Fatal("the diff is not readable:", err)
	}
	if string(got["status"].From) != `"open"` || string(got["status"].To) != `"done"` {
		t.Errorf("diff = %s, want open → done", changes)
	}

	// Writing the same value again is not a change, and a log full of those is
	// a log nobody reads. Dragging a card back where it came from is the case.
	var before, after int
	s.db.QueryRow(`SELECT count(*) FROM audit_log`).Scan(&before)
	patch(`"done"`)
	s.db.QueryRow(`SELECT count(*) FROM audit_log`).Scan(&after)
	if after != before {
		t.Errorf("setting the same value wrote %d entries, want 0", after-before)
	}
}
