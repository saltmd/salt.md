package server

import (
	"encoding/json"
	"testing"
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
	_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, []rowFilter{f}, "", 100, 0)
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
