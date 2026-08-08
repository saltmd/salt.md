package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// A new workspace starts empty, and everything that makes one usable is
// invisible: the rules, the option ids, the backrelation, the rollups, the view
// filters. Rebuilt by hand it comes out almost-but-not-quite the same.
//
// The dangerous part of copying it is the id remapping. A relation that still
// points at the ORIGINAL database keeps working — it just reads rows out of
// somebody else's workspace, and that looks correct until the numbers turn out
// to belong to another project.

func blueprintSource(t *testing.T, s *Server, ws, uid string) (systems, tasks string) {
	t.Helper()
	systems = s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	tasks = s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"status","name":"Status","type":"select","options":[
			{"id":"open","name":"Open","color":"#337ea9"},{"id":"done","name":"Done"}]},
		{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"}]`)
	// The derived trio, exactly as the real workspace carries it.
	if _, err := s.mcpUpdateSchema(systems, json.RawMessage(`[
		{"name":"Tasks","type":"backrelation","backrelationCollection":"`+tasks+`","backrelationProp":"system"},
		{"name":"Open","type":"rollup","rollupRelation":"tasks","rollupTarget":"status","rollupAgg":"count",
		 "rollupWhereProp":"status","rollupWhereOp":"is_not","rollupWhereValue":"done"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}
	// Two views: one filtered on a select, one on the relation.
	if _, err := s.mcpCreateView(tasks, viewSpec{Name: "Open", Type: "board", GroupBy: ptr("status"),
		Filters: &[]map[string]any{{"property": "status", "op": "is_not", "value": "done"}}}); err != nil {
		t.Fatalf("view: %v", err)
	}
	if _, err := s.mcpCreateView(tasks, viewSpec{Name: "One system", Type: "table",
		Filters: &[]map[string]any{{"property": "system", "op": "is", "value": "some-row-id"}}}); err != nil {
		t.Fatalf("view: %v", err)
	}
	s.db.Exec(`UPDATE workspaces SET rules = ? WHERE id = ?`, "Always check in before you start.", ws)
	return systems, tasks
}

func TestBlueprintCopiesStructureAndRemapsIDs(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "bp@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)
	systems, tasks := blueprintSource(t, s, ws, uid)

	// A row, to prove rows do NOT come along.
	s.makeRow(t, ws, uid, tasks, "Do not copy me", `{}`)

	out, err := s.blueprintWorkspace(u, "Entwicklung 2", ws)
	if err != nil {
		t.Fatalf("blueprint: %v", err)
	}
	newWS := ""
	if i := strings.Index(out, "with id "); i >= 0 {
		newWS = strings.Fields(out[i+len("with id "):])[0]
	}
	if newWS == "" {
		t.Fatalf("no workspace id in %q", out)
	}

	// Two databases, no rows.
	var dbs, pages int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ? AND type = 'collection'`, newWS).Scan(&dbs)
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ?`, newWS).Scan(&pages)
	if dbs != 2 {
		t.Errorf("copied %d databases, want 2", dbs)
	}
	if pages != 2 {
		t.Errorf("the new workspace holds %d pages — rows or documents came along", pages)
	}

	// The rules travelled.
	var rules string
	s.db.QueryRow(`SELECT COALESCE(rules,'') FROM workspaces WHERE id = ?`, newWS).Scan(&rules)
	if rules != "Always check in before you start." {
		t.Errorf("rules are %q — the most valuable part of a blueprint did not come along", rules)
	}

	// Find the copies.
	var newSystems, newTasks string
	s.db.QueryRow(`SELECT id FROM pages WHERE workspace_id = ? AND title = 'Systems'`, newWS).Scan(&newSystems)
	s.db.QueryRow(`SELECT id FROM pages WHERE workspace_id = ? AND title = 'Tasks'`, newWS).Scan(&newTasks)
	if newSystems == "" || newTasks == "" {
		t.Fatal("a database is missing from the copy")
	}
	if newSystems == systems || newTasks == tasks {
		t.Fatal("the copy reuses the source ids")
	}

	// THE test: every reference points inside the new workspace.
	schema, views, err := s.loadCollection(newTasks)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, p := range schema {
		if got, _ := p["relationCollection"].(string); got != "" {
			if got == systems {
				t.Error("the relation still points at the ORIGINAL database — it would read another workspace's rows")
			}
			if got != newSystems {
				t.Errorf("relation points at %q, want the copied database", got)
			}
		}
		// Option ids and their colours have to survive, or every stored value in a
		// future row is a dead entry.
		if p["id"] == "status" {
			opts, _ := p["options"].([]any)
			if len(opts) != 2 {
				t.Errorf("the status options did not come along: %#v", p["options"])
			}
		}
	}
	sysSchema, _, _ := s.loadCollection(newSystems)
	for _, p := range sysSchema {
		if got, _ := p["backrelationCollection"].(string); got != "" && got != newTasks {
			t.Errorf("the backrelation points at %q, want the copied Tasks", got)
		}
	}

	// The select filter survives; the relation filter does not, because its value
	// is a row id and no rows were copied.
	byName := map[string][]any{}
	for _, v := range views {
		name, _ := v["name"].(string)
		f, _ := v["filters"].([]any)
		byName[name] = f
	}
	if len(byName["Open"]) != 1 {
		t.Errorf("the select filter was dropped: %#v", byName["Open"])
	}
	if len(byName["One system"]) != 0 {
		t.Errorf("a relation filter survived and now matches nothing: %#v", byName["One system"])
	}
}

// A reference pointing OUTSIDE the workspace being copied must not travel — it
// would reach across into data the new workspace has no business reading.
func TestBlueprintDropsReferencesItCannotRemap(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "bp2@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)

	foreign := s.makeCollection(t, ws, uid, "Foreign", `[{"id":"x","name":"X","type":"text"}]`)
	// A second workspace holding the database we copy, pointing at the first.
	other := newID()
	s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id) VALUES (?, 'Other', ?, ?)`, other, now(), uid)
	s.addMember(t, other, uid, "admin")
	s.makeCollection(t, other, uid, "Tasks",
		`[{"id":"ref","name":"Ref","type":"relation","relationCollection":"`+foreign+`"}]`)

	out, err := s.blueprintWorkspace(u, "Copy", other)
	if err != nil {
		t.Fatalf("blueprint: %v", err)
	}
	newWS := strings.Fields(out[strings.Index(out, "with id ")+len("with id "):])[0]
	var copied string
	s.db.QueryRow(`SELECT id FROM pages WHERE workspace_id = ? AND title = 'Tasks'`, newWS).Scan(&copied)
	schema, _, _ := s.loadCollection(copied)
	for _, p := range schema {
		if got, _ := p["relationCollection"].(string); got == foreign {
			t.Error("a relation to a database outside the copy survived — the new workspace can read another one's rows")
		}
	}
}

// An empty workspace is not a blueprint, and saying so beats creating an empty
// copy that looks like it worked.
func TestBlueprintRefusesAWorkspaceWithoutDatabases(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "bp3@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	ws := s.firstWorkspaceOf(t, uid)

	if _, err := s.blueprintWorkspace(u, "Copy", ws); err == nil {
		t.Error("copying a workspace with no databases should say so")
	}
	// And a workspace the caller is not a member of must not be readable at all.
	if _, err := s.blueprintWorkspace(&user{ID: "stranger"}, "Copy", ws); err == nil {
		t.Error("a stranger used somebody else's workspace as a blueprint")
	}
}
