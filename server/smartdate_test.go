package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Relative days resolve against the day the query runs on, so a saved view
// reading "after today-1M" is still right next month.

func smartDay(t *testing.T, now time.Time, s string) string {
	t.Helper()
	day, ok := resolveSmartDate(s, now)
	if !ok {
		t.Fatalf("%q did not resolve", s)
	}
	return day
}

func TestResolveSmartDate(t *testing.T) {
	now := time.Date(2026, 10, 9, 15, 4, 0, 0, time.UTC) // a Friday
	cases := []struct{ in, want string }{
		{"today", "2026-10-09"},
		{"yesterday", "2026-10-08"},
		{"tomorrow", "2026-10-10"},
		{"today+0d", "2026-10-09"},
		{"today+7d", "2026-10-16"},
		{"today-7d", "2026-10-02"},
		{"today-30", "2026-09-09"}, // the unit defaults to days
		{"today-1w", "2026-10-02"},
		{"today+2w", "2026-10-23"},
		{"today-1m", "2026-09-09"},
		{"today+1m", "2026-11-09"},
		{"today-1y", "2025-10-09"},
		{"today+1y", "2027-10-09"},
		{"yesterday-7d", "2026-10-01"},
		// Case and spacing are forgiven: people type "Today - 1M".
		{"TODAY", "2026-10-09"},
		{"Today - 1M", "2026-09-09"},
	}
	for _, c := range cases {
		if got := smartDay(t, now, c.in); got != c.want {
			t.Errorf("%q resolved to %s, want %s", c.in, got, c.want)
		}
	}
}

func TestResolveSmartDateMonthClamp(t *testing.T) {
	cases := []struct {
		now      time.Time
		in, want string
	}{
		// March 31 minus a month is February 28, not March 3.
		{time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC), "today-1m", "2026-02-28"},
		// Leap years keep their extra day.
		{time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC), "today-1m", "2024-02-29"},
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), "today+1m", "2026-02-28"},
		{time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), "today+1m", "2026-11-30"},
		// Across a year boundary.
		{time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), "today-1m", "2025-12-15"},
		{time.Date(2025, 12, 15, 12, 0, 0, 0, time.UTC), "today+2m", "2026-02-15"},
		// A base word off the month edge normalises first: yesterday on
		// March 1 is February 28, minus a month is January 28.
		{time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC), "yesterday-1m", "2026-01-28"},
		// Tomorrow on January 31 is February 1, plus a month is March 1.
		{time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC), "tomorrow+1m", "2026-03-01"},
	}
	for _, c := range cases {
		if got := smartDay(t, c.now, c.in); got != c.want {
			t.Errorf("%q on %s resolved to %s, want %s", c.in, c.now.Format("2006-01-02"), got, c.want)
		}
	}
}

func TestResolveSmartDateRejects(t *testing.T) {
	for _, in := range []string{
		"", "someday", "2026-10-09", // a plain date is not a word
		"today-", "today-1x", "today7d", "today+1.5d",
		"today-10000d", // capped rather than overflowed
		"next friday",
	} {
		if day, ok := resolveSmartDate(in, time.Now()); ok {
			t.Errorf("%q resolved to %s, want rejection", in, day)
		}
	}
}

// A relative day in a real query: rows dated around today, filtered with
// words. The fixture dates are computed from now, so this holds whenever it
// runs, except across a midnight boundary mid-test, which is accepted.
func TestSmartDateFilterEndToEnd(t *testing.T) {
	s := testServer(t)
	_, _ = signedIn(t, s, "smartdate@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Smart', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col,
		`[{"id":"datum","name":"Datum","type":"date"}]`); err != nil {
		t.Fatal(err)
	}
	today := time.Now()
	days := map[string]int{"old": -40, "recent": -10, "now": 0, "soon": 5}
	for name, off := range days {
		day := today.AddDate(0, 0, off).Format("2006-01-02")
		props, _ := json.Marshal(map[string]any{"datum": day})
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', ?, 0, ?, ?)`,
			newID(), col, ws, name, string(props), now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	// "in the last month": recent and now, but not old or soon.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "between", Value: "today-1M", Value2: "today"}); n != 2 {
		t.Errorf("between today-1M and today returned %d, want 2", n)
	}
	// "upcoming": soon only.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "gt", Value: "today"}); n != 1 {
		t.Errorf("after today returned %d, want 1", n)
	}
	// "older than a week": old and recent.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "lt", Value: "today-1w"}); n != 2 {
		t.Errorf("before today-1w returned %d, want 2", n)
	}
	// Exact match on a word: the row dated today.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "is", Value: "today"}); n != 1 {
		t.Errorf("is today returned %d, want 1", n)
	}
}

// countWithToday is count with an explicit viewer day instead of the
// server's: the way the interface always queries (see the today param).
func countWithToday(t *testing.T, s *Server, col string, f rowFilter, today time.Time) int {
	t.Helper()
	_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, nil, [][]rowFilter{{f}}, "", 100, 0, today)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return total
}

// Relative days resolve on DATE properties only. A select option named
// "Today" has the id `today`: `status is today` must keep meaning the
// option, not the date. Likewise `note contains tomorrow` is a text search.
func TestSmartDateOnlyOnDateProperties(t *testing.T) {
	s := testServer(t)
	_, _ = signedIn(t, s, "smartdate-gate@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Gated', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col,
		`[{"id":"status","name":"Status","type":"select"},{"id":"note","name":"Note","type":"text"},{"id":"datum","name":"Datum","type":"date"}]`); err != nil {
		t.Fatal(err)
	}
	day := time.Now().Format("2006-01-02")
	rows := []map[string]any{
		{"status": "today", "note": "kickoff", "datum": day},
		{"status": "open", "note": "see you tomorrow", "datum": day},
	}
	for i, props := range rows {
		raw, _ := json.Marshal(props)
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', ?, ?, ?, ?)`,
			newID(), col, ws, "Row", string(raw), i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, s, col, rowFilter{Prop: "status", Op: "is", Value: "today"}); n != 1 {
		t.Errorf("status is today returned %d, want 1: the word must stay an option id off date properties", n)
	}
	if n := count(t, s, col, rowFilter{Prop: "note", Op: "contains", Value: "tomorrow"}); n != 1 {
		t.Errorf("note contains tomorrow returned %d, want 1: text is searched literally", n)
	}
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "is", Value: "today"}); n != 2 {
		t.Errorf("datum is today returned %d, want 2", n)
	}
}

// A half-typed word ("today-") is not a value yet: the condition waits
// instead of comparing literally and emptying the table mid-keystroke.
func TestSmartDateUnfinishedWaits(t *testing.T) {
	s, col := filterFixture(t)
	for _, f := range []rowFilter{
		{Prop: "datum", Op: "is", Value: "today-"},
		{Prop: "datum", Op: "is", Value: "tomorrow+"},
		{Prop: "datum", Op: "between", Value: "today-", Value2: "today"},
		{Prop: "datum", Op: "between", Value: "2026-01-01", Value2: "today+"},
		{Prop: "datum", Op: "contains", Value: "today-"},
	} {
		if n := count(t, s, col, f); n != 4 {
			t.Errorf("%+v returned %d rows, want all 4: an unfinished word must not filter", f, n)
		}
	}
}

// The viewer day travels with the query: with today set to tomorrow, the
// word "today" names tomorrow's date.
func TestSmartDateExplicitToday(t *testing.T) {
	s := testServer(t)
	_, _ = signedIn(t, s, "smartdate-today@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Anchored', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col,
		`[{"id":"datum","name":"Datum","type":"date"}]`); err != nil {
		t.Fatal(err)
	}
	nowDay := time.Now()
	for i, off := range []int{0, 1} {
		raw, _ := json.Marshal(map[string]any{"datum": nowDay.AddDate(0, 0, off).Format("2006-01-02")})
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', ?, ?, ?, ?)`,
			newID(), col, ws, "Row", string(raw), i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	tomorrow := time.Date(nowDay.Year(), nowDay.Month(), nowDay.Day()+1, 12, 0, 0, 0, time.UTC)
	if n := countWithToday(t, s, col, rowFilter{Prop: "datum", Op: "is", Value: "today"}, tomorrow); n != 1 {
		t.Errorf("is today with viewer day tomorrow returned %d, want 1: the word follows the viewer, not the server", n)
	}
}

func TestParseTodayParam(t *testing.T) {
	day, ok := parseTodayParam("2026-10-09")
	if !ok || day.Format("2006-01-02") != "2026-10-09" {
		t.Errorf("2026-10-09 parsed to %v, %v", day, ok)
	}
	for _, bad := range []string{"", "today", "tomorrow", "2026-1-1", "2026-13-01", "2026-02-30", "Oct 9", "2026-10-09T00:00"} {
		if _, ok := parseTodayParam(bad); ok {
			t.Errorf("%q parsed as a day, want rejection", bad)
		}
	}
}

// query_rows takes the agent's day along: with today set to tomorrow, the
// word "today" names tomorrow's rows. Without it, the server's day applies.
func TestSmartDateTodayOverMCP(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "smartdate-mcp@example.com")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Agent days', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col,
		`[{"id":"datum","name":"Datum","type":"date"}]`); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	for i, off := range []int{0, 1} {
		raw, _ := json.Marshal(map[string]any{"datum": base.AddDate(0, 0, off).Format("2006-01-02")})
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', ?, ?, ?, ?)`,
			newID(), col, ws, "Row", string(raw), i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	tomorrow := base.AddDate(0, 0, 1).Format("2006-01-02")
	out, err := callTool(t, s, u, "query_rows",
		`{"page_id":"`+col+`","filter":[{"property":"datum","op":"is","value":"today"}],"today":"`+tomorrow+`"}`)
	if err != nil {
		t.Fatalf("query_rows: %v", err)
	}
	if !strings.Contains(out, `"total":1`) {
		t.Errorf("query_rows with today=tomorrow answered %s, want total 1", out)
	}
	plain, err := callTool(t, s, u, "query_rows",
		`{"page_id":"`+col+`","filter":[{"property":"datum","op":"is","value":"today"}]}`)
	if err != nil {
		t.Fatalf("query_rows: %v", err)
	}
	if !strings.Contains(plain, `"total":1`) {
		t.Errorf("query_rows without today answered %s, want total 1 (the server day)", plain)
	}
}
