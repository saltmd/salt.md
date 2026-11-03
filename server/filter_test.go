package server

import (
	"encoding/json"
	"testing"
	"time"
)

// What a filter means, checked against real rows in a real database.
//
// Three of these exist because of one report from real use: a date filter that
// "did not work at all". The server was fine — but a condition with no value yet
// compared against the empty string, so the instant somebody added "Date is …"
// the table went blank, before they had typed anything. That reads as broken,
// and it was.
//
// The other two are new powers rather than repairs: a set of values in ONE
// condition, and a range. Both were previously expressible only as several
// conditions sitting next to each other, which is not something anybody finds.

// filterFixture builds a collection with four rows: two classes, four dates.
func filterFixture(t *testing.T) (*Server, string) {
	t.Helper()
	s := testServer(t)
	_, _ = signedIn(t, s, "filter@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}

	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Belege', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	schema := `[{"id":"klasse","name":"Klasse","type":"select"},{"id":"datum","name":"Datum","type":"date"}]`
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col, schema); err != nil {
		t.Fatal(err)
	}
	rows := []struct{ klasse, datum string }{
		{"a", "2026-01-10"},
		{"b", "2026-03-15"},
		{"c", "2026-05-20"},
		{"a", "2026-08-01"},
	}
	for i, r := range rows {
		props, _ := json.Marshal(map[string]any{"klasse": r.klasse, "datum": r.datum})
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', ?, ?, ?, ?)`,
			newID(), col, ws, "Beleg", string(props), i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	return s, col
}

func count(t *testing.T, s *Server, col string, f rowFilter) int {
	t.Helper()
	return countGroups(t, s, col, [][]rowFilter{{f}})
}

func countGroups(t *testing.T, s *Server, col string, groups [][]rowFilter) int {
	t.Helper()
	_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, nil, groups, "", 100, 0, time.Time{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return total
}

// The report: adding a date filter emptied the table before anything was typed.
func TestFilterWithoutAValueDoesNotFilter(t *testing.T) {
	s, col := filterFixture(t)
	for _, op := range []string{"is", "is_not", "contains", "gt", "lt"} {
		if n := count(t, s, col, rowFilter{Prop: "datum", Op: op}); n != 4 {
			t.Errorf("%q with no value returned %d rows, want all 4 — an unfinished condition must not filter", op, n)
		}
	}
	// between needs BOTH ends before it means anything.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "between", Value: "2026-01-01"}); n != 4 {
		t.Errorf("between with only a lower bound returned %d rows, want all 4", n)
	}
	// The deliberate version of the question still works.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "is_empty"}); n != 0 {
		t.Errorf("is_empty returned %d, want 0 — every row has a date", n)
	}
}

// Several values in one condition, which is what "not A and not H" should be.
func TestFilterMatchesAnyOfSeveralValues(t *testing.T) {
	s, col := filterFixture(t)

	if n := count(t, s, col, rowFilter{Prop: "klasse", Op: "is", Values: []string{"a", "b"}}); n != 3 {
		t.Errorf("is any of [a b] returned %d, want 3", n)
	}
	if n := count(t, s, col, rowFilter{Prop: "klasse", Op: "is_not", Values: []string{"a", "b"}}); n != 1 {
		t.Errorf("is none of [a b] returned %d, want 1", n)
	}
	// A set of one behaves exactly like the single value it replaces, or the
	// two spellings would drift apart.
	single := count(t, s, col, rowFilter{Prop: "klasse", Op: "is", Value: "a"})
	asSet := count(t, s, col, rowFilter{Prop: "klasse", Op: "is", Values: []string{"a"}})
	if single != asSet || single != 2 {
		t.Errorf("one value: %d, same value as a set: %d, want 2 for both", single, asSet)
	}
}

// A range, inclusive at both ends — a range named by two dates contains them.
func TestFilterBetweenIsInclusive(t *testing.T) {
	s, col := filterFixture(t)

	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "between", Value: "2026-03-01", Value2: "2026-05-31"}); n != 2 {
		t.Errorf("between March and May returned %d, want 2", n)
	}
	// Both bounds land exactly on a row's date: both rows are in.
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "between", Value: "2026-03-15", Value2: "2026-05-20"}); n != 2 {
		t.Errorf("bounds ON the rows returned %d, want 2 — between is inclusive", n)
	}
	if n := count(t, s, col, rowFilter{Prop: "datum", Op: "between", Value: "2026-06-01", Value2: "2026-07-01"}); n != 0 {
		t.Errorf("an empty range returned %d, want 0", n)
	}
}

// OR groups: AND inside, OR between. The fixture rows are
// (a,01-10) (b,03-15) (c,05-20) (a,08-01).
func TestFilterGroupsOrSemantics(t *testing.T) {
	s, col := filterFixture(t)

	// klasse a OR klasse b: three of the four rows.
	if n := countGroups(t, s, col, [][]rowFilter{
		{{Prop: "klasse", Op: "is", Value: "a"}},
		{{Prop: "klasse", Op: "is", Value: "b"}},
	}); n != 3 {
		t.Errorf("a OR b returned %d, want 3", n)
	}
	// AND inside one group: klasse a AND datum after June — only 08-01.
	if n := countGroups(t, s, col, [][]rowFilter{
		{{Prop: "klasse", Op: "is", Value: "a"}, {Prop: "datum", Op: "gt", Value: "2026-06-01"}},
	}); n != 1 {
		t.Errorf("a AND after-June returned %d, want 1", n)
	}
	// One group behaves exactly like the flat list it replaces.
	single := count(t, s, col, rowFilter{Prop: "klasse", Op: "is", Value: "a"})
	grouped := countGroups(t, s, col, [][]rowFilter{{{Prop: "klasse", Op: "is", Value: "a"}}})
	if single != grouped || single != 2 {
		t.Errorf("flat: %d, same as one group: %d, want 2 for both", single, grouped)
	}
	// An empty group constrains nothing: it must not narrow the answer, or
	// one unfinished group would blank the whole view.
	if n := countGroups(t, s, col, [][]rowFilter{
		{{Prop: "klasse", Op: "is", Value: "a"}},
		{},
	}); n != 2 {
		t.Errorf("a completed group beside an empty one returned %d, want 2", n)
	}
}

// A flat filter beside groups is a term of its own: "this AND (that OR
// those)", the combination both the REST handler and query_rows build. It used
// to be copied into every group, which let an unfinished group match whatever
// the flat filter matched: [klasse a] OR [unfinished] answered 2 rows, and
// adding "datum is set" answered 4.
func TestFlatFilterIsATermOfItsOwn(t *testing.T) {
	s, col := filterFixture(t)
	query := func(flat []rowFilter, groups [][]rowFilter) int {
		t.Helper()
		_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, flat, groups, "", 100, 0, time.Time{})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		return total
	}

	flat := []rowFilter{{Prop: "klasse", Op: "is_not", Value: "c"}}
	groups := [][]rowFilter{
		{{Prop: "datum", Op: "lt", Value: "2026-04-01"}},
		{{Prop: "datum", Op: "gt", Value: "2026-06-01"}},
	}
	// (a,01-10) and (b,03-15) pass the first group, (a,08-01) the second;
	// (c,05-20) fails the flat condition either way.
	if n := query(flat, groups); n != 3 {
		t.Errorf("flat AND groups returned %d, want 3", n)
	}
	if n := query(flat, nil); n != 3 {
		t.Errorf("flat alone returned %d, want 3", n)
	}

	unfinished := [][]rowFilter{{{Prop: "klasse", Op: "is", Value: "a"}}, {}}
	if n := query([]rowFilter{{Prop: "datum", Op: "is_not_empty"}}, unfinished); n != 2 {
		t.Errorf("a flat condition widened the answer to %d rows, want the 2 of klasse a", n)
	}
}

// The wire shape: one filter_group param per group, each a JSON array.
// Unparsable params and condition-less groups never reach the query.
func TestParseFilterGroups(t *testing.T) {
	groups := parseFilterGroups([]string{
		`[{"property":"klasse","op":"is","value":"a"}]`,
		`not json at all`,
		`[]`,
		`[{"op":"is","value":"a"}]`,
		`[{"property":"datum","op":"between","value":"2026-01-01","value2":"2026-12-31"}]`,
	})
	if len(groups) != 2 {
		t.Fatalf("parsed %d groups, want 2 — only the two armed ones survive", len(groups))
	}
	if groups[0][0].Prop != "klasse" || groups[0][0].Value != "a" {
		t.Errorf("first group is %#v, want klasse is a", groups[0])
	}
	if groups[1][0].Op != "between" || groups[1][0].Value2 != "2026-12-31" {
		t.Errorf("second group is %#v, want the datum range", groups[1])
	}
}

// "is" and "is not" on a number or a checkbox (#95). A number is stored as a
// JSON number and a ticked box as JSON true, and comparing either with the
// filter's text matched nothing, while the browser's own pass matched them.
func TestFilterIsOnNumbersAndCheckboxes(t *testing.T) {
	s := testServer(t)
	_, _ = signedIn(t, s, "typed@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Stock', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	schema := `[{"id":"qty","name":"Qty","type":"number"},{"id":"ok","name":"Done","type":"checkbox"}]`
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col, schema); err != nil {
		t.Fatal(err)
	}
	// Five rows: 5 ticked, 5.0 unticked, "5" as text and never touched, 7
	// ticked, and one with nothing at all.
	for i, props := range []string{`{"qty":5,"ok":true}`, `{"qty":5.0,"ok":false}`, `{"qty":"5"}`, `{"qty":7,"ok":true}`, `{}`} {
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, 'Item', 'row', ?, ?, ?, ?)`, newID(), col, ws, props, i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		f    rowFilter
		want int
	}{
		{rowFilter{Prop: "qty", Op: "is", Value: "5"}, 3},
		{rowFilter{Prop: "qty", Value: "5"}, 3},
		{rowFilter{Prop: "qty", Op: "is", Values: []string{"5", "7"}}, 4},
		{rowFilter{Prop: "qty", Op: "is_not", Value: "5"}, 2},
		{rowFilter{Prop: "qty", Op: "gt", Value: "4"}, 4},
		{rowFilter{Prop: "ok", Op: "is", Value: "true"}, 2},
		{rowFilter{Prop: "ok", Op: "is", Value: "false"}, 3},
		{rowFilter{Prop: "ok", Op: "is_not", Value: "true"}, 3},
		{rowFilter{Prop: "ok", Op: "is_not", Value: "false"}, 2},
		{rowFilter{Prop: "ok", Op: "is", Values: []string{"true", "false"}}, 5},
	} {
		if n := count(t, s, col, c.f); n != c.want {
			t.Errorf("%+v matched %d rows, want %d", c.f, n, c.want)
		}
	}
	// The flat list takes the same path as the groups.
	if _, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, []rowFilter{{Prop: "ok", Op: "is", Value: "true"}}, nil, "", 100, 0, time.Time{}); err != nil || total != 2 {
		t.Errorf("flat ok is true: %d rows, %v; want 2", total, err)
	}
}

func TestPersonFilterMe(t *testing.T) {
	s := testServer(t)
	_, _ = signedIn(t, s, "person_filter@example.com")
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}

	col := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at)
		VALUES (?, NULL, ?, 'Tasks', 'collection', '{}', ?, ?)`, col, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	schema := `[{"id":"assignee","name":"Assignee","type":"person"}]`
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`, col, schema); err != nil {
		t.Fatal(err)
	}

	rows := []string{
		`{"assignee":"u_ada"}`,
		`{"assignee":"Ada Lovelace"}`,
		`{"assignee":["u_ada","u_bob"]}`,
		`{"assignee":"u_bob"}`,
		`{}`,
	}
	for i, r := range rows {
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, 'Task', 'row', ?, ?, ?, ?)`,
			newID(), col, ws, r, i, now(), now()); err != nil {
			t.Fatal(err)
		}
	}

	ada := &user{ID: "u_ada", Name: "Ada Lovelace"}
	bob := &user{ID: "u_bob", Name: "Bob"}

	query := func(u *user, f rowFilter) int {
		_, total, err := s.collectionRowsQuery(u, col, []rowFilter{f}, nil, "", 100, 0, time.Time{})
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		return total
	}

	// For Ada:
	if n := query(ada, rowFilter{Prop: "assignee", Op: "is", Value: "@me"}); n != 3 {
		t.Errorf("Ada is @me: got %d rows, want 3", n)
	}
	if n := query(ada, rowFilter{Prop: "assignee", Op: "is", Values: []string{"@me"}}); n != 3 {
		t.Errorf("Ada is [@me]: got %d rows, want 3", n)
	}
	if n := query(ada, rowFilter{Prop: "assignee", Op: "is_not", Value: "@me"}); n != 2 {
		t.Errorf("Ada is_not @me: got %d rows, want 2", n)
	}
	if n := query(ada, rowFilter{Prop: "assignee", Op: "is", Values: []string{"@me", "u_bob"}}); n != 4 {
		t.Errorf("Ada is [@me, u_bob]: got %d rows, want 4", n)
	}

	// For Bob:
	if n := query(bob, rowFilter{Prop: "assignee", Op: "is", Value: "@me"}); n != 2 {
		t.Errorf("Bob is @me: got %d rows, want 2", n)
	}
	if n := query(bob, rowFilter{Prop: "assignee", Op: "is_not", Value: "@me"}); n != 3 {
		t.Errorf("Bob is_not @me: got %d rows, want 3", n)
	}

	// Unauthenticated:
	if n := query(nil, rowFilter{Prop: "assignee", Op: "is", Value: "@me"}); n != 0 {
		t.Errorf("nil user is @me: got %d rows, want 0", n)
	}
}
