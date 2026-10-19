package server

import (
	"strings"
	"testing"
)

// idFromToolAnswer pulls the id out of a tool answer like
// `Created database "X" with id abc123`.
func idFromToolAnswer(t *testing.T, out string) string {
	t.Helper()
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("empty answer")
	}
	return fields[len(fields)-1]
}

func viewsOf(t *testing.T, s *Server, pageID string) []map[string]any {
	t.Helper()
	_, views, err := s.loadCollection(pageID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return views
}

// A database made with a schema of one's own kept the DEFAULT views, which
// group the board by "status" — a property that exists in the default schema
// and nowhere else. get_collection then answered views[0] = {type:"board",
// groupBy:"status"} for a database with no such property: the board grouped by
// nothing, and the database was broken from the moment it was created. Nothing
// in the answer said so.
func TestCreateDatabaseMatchesItsViewsToItsOwnSchema(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "createdb@example.test")
	u := &user{ID: uid, Name: "Test"}

	// A schema with something to group by: the board points at it.
	out, err := callTool(t, s, u, "create_database", `{"title":"Сезоны","schema":[
		{"name":"Название","type":"text"},
		{"name":"Год","type":"select","options":["2025","2026"]}]}`)
	if err != nil {
		t.Fatalf("create_database: %v", err)
	}
	id := idFromToolAnswer(t, out)
	schema, _, err := s.loadCollection(id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ids := map[string]string{}
	for _, p := range schema {
		pid, _ := p["id"].(string)
		typ, _ := p["type"].(string)
		ids[pid] = typ
	}
	board := map[string]any(nil)
	for _, v := range viewsOf(t, s, id) {
		if typ, _ := v["type"].(string); typ == "board" {
			board = v
		}
	}
	if board == nil {
		t.Fatal("the default views lost their board")
	}
	g, _ := board["groupBy"].(string)
	if g == "" || ids[g] == "" {
		t.Errorf("the board groups by %q, which is not a property of this database: %#v", g, ids)
	}
	if ids[g] != "select" && ids[g] != "multiselect" && ids[g] != "relation" {
		t.Errorf("the board groups by %q, a %s property", g, ids[g])
	}

	// A schema with nothing to group by: the table stays, the board goes.
	out, err = callTool(t, s, u, "create_database", `{"title":"Заметки","schema":[{"name":"Текст","type":"text"}]}`)
	if err != nil {
		t.Fatalf("create_database: %v", err)
	}
	id = idFromToolAnswer(t, out)
	types := []string{}
	for _, v := range viewsOf(t, s, id) {
		typ, _ := v["type"].(string)
		types = append(types, typ)
	}
	for _, typ := range types {
		if typ == "board" {
			t.Errorf("a board that has nothing to group by should not be there: %v", types)
		}
	}
	if len(types) == 0 {
		t.Errorf("the table should have stayed: %v", types)
	}
}

// A database made without a schema takes the defaults, which are the one case
// the default views are written for — groupBy "status" is a property there.
func TestCreateDatabaseDefaultsStillHaveABoard(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "createdbdefault@example.test")
	u := &user{ID: uid, Name: "Test"}
	out, err := callTool(t, s, u, "create_database", `{"title":"Anything"}`)
	if err != nil {
		t.Fatalf("create_database: %v", err)
	}
	id := idFromToolAnswer(t, out)
	for _, v := range viewsOf(t, s, id) {
		if typ, _ := v["type"].(string); typ == "board" {
			if g, _ := v["groupBy"].(string); g == "status" {
				return
			}
		}
	}
	t.Error("the default board is gone from a database made from the default schema")
}
