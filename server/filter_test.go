package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	return countGroups(t, s, col, [][]rowFilter{{f}})
}

func countGroups(t *testing.T, s *Server, col string, groups [][]rowFilter) int {
	t.Helper()
	_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, nil, groups, "", 100, 0)
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
		_, total, err := s.collectionRowsQuery(&user{ID: "u"}, col, flat, groups, "", 100, 0)
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

func TestTemplateFilter(t *testing.T) {
	// The one condition that is not about a column: whether the row is a
	// template. It must answer for all four spellings a view can carry, and for
	// the empty one a half-built row leaves behind.
	cases := []struct {
		op, value string
		want      int
	}{
		{"is", "true", 1},
		{"is", "false", 0},
		{"is_not", "true", 0},
		{"is_not", "false", 1},
		{"", "", 1},
	}
	for _, c := range cases {
		sql, args := (rowFilter{Prop: templateFilterProp, Op: c.op, Value: c.value}).where()
		if sql != "is_template = ?" {
			t.Errorf("%q %q: sql = %q", c.op, c.value, sql)
			continue
		}
		if len(args) != 1 || args[0] != c.want {
			t.Errorf("%q %q: args = %v, want %d", c.op, c.value, args, c.want)
		}
	}
}

func TestSystemSorts(t *testing.T) {
	s, col := filterFixture(t)
	// Two rows with known timestamps: the older one must come first ascending.
	// The fixture already holds four rows; both newcomers sort after them by
	// position only when no sort applies, so the assertions name ids, not
	// places.
	oldID, newID := newID(), newID()
	var ws string
	if err := s.db.QueryRow(`SELECT id FROM workspaces LIMIT 1`).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		id, created, updated string
	}{
		{oldID, "2020-01-01T00:00:00Z", "2020-01-02T00:00:00Z"},
		{newID, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z"},
	} {
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'row', '{}', 0, ?, ?)`, r.id, col, ws, "row", r.created, r.updated); err != nil {
			t.Fatal(err)
		}
	}
	u := &user{ID: "tester"}
	// Both newcomers carry the timestamps above; the order between the two must
	// follow the sort in all four spellings, wherever the fixture rows land.
	at := func(list []map[string]any, id string) int {
		for i, r := range list {
			if r["id"] == id {
				return i
			}
		}
		return -1
	}
	for _, tc := range []struct {
		sort         string
		oldBeforeNew bool
	}{
		{sortCreatedProp + ":asc", true},
		{sortCreatedProp + ":desc", false},
		{sortUpdatedProp + ":asc", true},
		{sortUpdatedProp + ":desc", false},
	} {
		list, _, err := s.collectionRowsQuery(u, col, nil, nil, tc.sort, 10, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.sort, err)
		}
		if len(list) != 6 {
			t.Fatalf("%s: got %d rows, want 6", tc.sort, len(list))
		}
		if (at(list, oldID) < at(list, newID)) != tc.oldBeforeNew {
			t.Errorf("%s: old/new order wrong", tc.sort)
		}
		if list[0]["createdAt"] == nil || list[0]["updatedAt"] == nil {
			t.Errorf("%s: rows carry no timestamps for the client to sort by", tc.sort)
		}
	}
}

func TestNewFromTemplateRoundtrip(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "tpl@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"a","name":"A","type":"text"}]`)
	put := func(body string) int {
		r := httptest.NewRequest("PUT", "/api/collections/"+db, strings.NewReader(body))
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}
	get := func() map[string]any {
		r := httptest.NewRequest("GET", "/api/collections/"+db, nil)
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("get: %v", err)
		}
		return out
	}
	views := `"views":[{"id":"v1","name":"Table","type":"table"}]`
	schema := `"schema":[{"id":"a","name":"A","type":"text"}]`
	// Nothing set: the key is omitted rather than sent as nothing.
	if code := put(`{` + schema + `,` + views + `}`); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if _, ok := get()["newFromTemplate"]; ok {
		t.Error("empty template choice should be omitted, not sent as nothing")
	}
	// A page that is not a template does not take: the choice clears itself.
	notTpl := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at) VALUES (?, NULL, ?, 'X', 'doc', '{}', ?, ?)`, notTpl, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if code := put(`{` + schema + `,` + views + `,"newFromTemplate":"` + notTpl + `"}`); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if _, ok := get()["newFromTemplate"]; ok {
		t.Error("a non-template must clear the choice")
	}
	// A real template round-trips.
	tmpl := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, workspace_id, title, type, props, created_at, updated_at, is_template) VALUES (?, NULL, ?, 'T', 'doc', '{}', ?, ?, 1)`, tmpl, ws, now(), now()); err != nil {
		t.Fatal(err)
	}
	if code := put(`{` + schema + `,` + views + `,"newFromTemplate":"` + tmpl + `"}`); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if got, _ := get()["newFromTemplate"].(string); got != tmpl {
		t.Errorf("roundtrip: got %q, want %q", got, tmpl)
	}
	// And a save that does not mention it — the schema dialog sending only
	// schema and views — leaves it alone.
	if code := put(`{` + schema + `,` + views + `}`); code != http.StatusOK {
		t.Fatalf("put: %d", code)
	}
	if got, _ := get()["newFromTemplate"].(string); got != tmpl {
		t.Errorf("untouched save cleared it: got %q, want %q", got, tmpl)
	}
}
