package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The MCP surface has to offer every property type the interface can render.
// It did not: `backrelation` shipped in 1.6.6, the browser could build the
// column, and update_schema rejected it for two releases. The consequence was
// not an error somebody would notice — it was that the one database the feature
// was written for never got the column, while the task saying so read "done".
//
// The type list also existed four times by hand. Keeping the spelled-out list
// level with the map is what these two tests are for.

func TestEveryValidPropTypeIsNamed(t *testing.T) {
	named := map[string]bool{}
	for _, n := range propTypeNames {
		if named[n] {
			t.Errorf("%q is listed twice in propTypeNames", n)
		}
		named[n] = true
		if !validPropTypes[n] {
			t.Errorf("propTypeNames offers %q, which normalizeSchema would reject", n)
		}
	}
	for typ := range validPropTypes {
		if !named[typ] {
			t.Errorf("%q is accepted but never named — an agent cannot know it exists", typ)
		}
	}
}

// The error an agent reads must name the type it is missing.
func TestUnknownTypeErrorNamesTheRealChoices(t *testing.T) {
	_, err := normalizeSchema([]map[string]any{{"name": "X", "type": "nonsense"}})
	if err == nil {
		t.Fatal("an invented type must be refused")
	}
	for _, want := range []string{"backrelation", "rollup", "relation"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should offer %q: %s", want, err)
		}
	}
}

// End to end over the tool an agent actually calls: a backrelation column can
// be created, keeps its two coordinates, and then reports the rows pointing at
// it. This is the exact call that failed against a running instance.
func TestUpdateSchemaCreatesAWorkingBackrelation(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "schema@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"},
		{"id":"status","name":"Status","type":"select","options":[{"id":"done","name":"Done"}]}]`)

	if _, err := s.mcpUpdateSchema(systems, json.RawMessage(`[
		{"name":"Tasks","type":"backrelation","backrelationCollection":"`+tasks+`","backrelationProp":"system"},
		{"name":"Done","type":"rollup","rollupRelation":"tasks","rollupTarget":"status","rollupAgg":"count",
		 "rollupWhereProp":"status","rollupWhereOp":"is","rollupWhereValue":"done"}]`), nil); err != nil {
		t.Fatalf("update_schema: %v", err)
	}

	schema, _, err := s.loadCollection(systems)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var br, ru map[string]any
	for _, p := range schema {
		switch p["type"] {
		case "backrelation":
			br = p
		case "rollup":
			ru = p
		}
	}
	if br == nil {
		t.Fatal("the backrelation column was not stored")
	}
	if br["backrelationCollection"] != tasks || br["backrelationProp"] != "system" {
		t.Errorf("the backrelation lost its coordinates: %#v", br)
	}
	// The rollup condition must survive too — without it there is no progress,
	// only a total.
	if ru == nil || ru["rollupWhereProp"] != "status" || ru["rollupWhereValue"] != "done" {
		t.Errorf("the rollup lost its condition: %#v", ru)
	}

	// And it actually answers: two tasks, one of them done.
	sysRow := s.makeRow(t, ws, uid, systems, "Salt", `{}`)
	s.makeRow(t, ws, uid, tasks, "A", `{"system":["`+sysRow+`"],"status":"done"}`)
	s.makeRow(t, ws, uid, tasks, "B", `{"system":["`+sysRow+`"],"status":"open"}`)

	rows := []map[string]any{{"id": sysRow, "props": map[string]any{}}}
	s.computeDerived(&user{ID: uid}, parseSchema(mustJSON(t, schema)), rows)
	props := rows[0]["props"].(map[string]any)
	if got := relationIDs(props["tasks"]); len(got) != 2 {
		t.Errorf("backrelation saw %d tasks, want 2", len(got))
	}
	if got := props["done"]; got != 1 {
		t.Errorf("the conditional rollup counted %v done, want 1", got)
	}
}

// A backrelation without its two coordinates is not a broken column but an
// empty one — it reads as "nothing points here", which looks exactly like the
// truth. It has to be refused at creation.
func TestBackrelationWithoutCoordinatesIsRefused(t *testing.T) {
	for _, p := range []map[string]any{
		{"name": "Tasks", "type": "backrelation"},
		{"name": "Tasks", "type": "backrelation", "backrelationCollection": "abc"},
		{"name": "Tasks", "type": "backrelation", "backrelationProp": "system"},
	} {
		if _, err := normalizeSchema([]map[string]any{p}); err == nil {
			t.Errorf("%#v should be refused", p)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// The tool's own description says "omit the id to add a new one". It did not.
// normalizeSchema invents an id for a property that has none — that is what
// makes {name:"Сезоны"} work at all — and for a name it could not spell the
// invented id was the same fallback every time, because slugID dropped every
// letter it did not know and Russian names have none it knows. So a second
// {name:"Сезоны"} invented "prop" again, found the property that already had
// that id, and REPLACED it; the answer even called it "changed prop". The
// neighbouring column disappeared and both columns' values sat in one.
func TestUpdateSchemaWithoutAnIdAddsAProperty(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "noid@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Разборы", `[{"id":"prop","name":"Первый","type":"text"}]`)

	call := `{"page_id":"` + db + `","properties":[{"name":"Сезоны","type":"text"}]}`
	for i := 1; i <= 2; i++ {
		out, err := callTool(t, s, u, "update_schema", call)
		if err != nil {
			t.Fatalf("update_schema #%d: %v", i, err)
		}
		if !strings.Contains(out, "added") {
			t.Fatalf("call #%d should ADD a property, not change one: %s", i, out)
		}
	}

	schema, _, err := s.loadCollection(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(schema) != 3 {
		t.Fatalf("want 3 properties (the one that was there plus two), got %d: %#v", len(schema), schema)
	}
	if name, _ := schema[0]["name"].(string); name != "Первый" {
		t.Errorf("the existing property was overwritten: %#v", schema[0])
	}
	seen := map[string]bool{}
	for _, p := range schema {
		id, _ := p["id"].(string)
		if id == "" {
			t.Errorf("a property without an id: %#v", p)
		}
		if seen[id] {
			t.Errorf("two properties share the id %q", id)
		}
		seen[id] = true
	}
	// Two properties of the same name still need two ids — and a Russian name
	// has to say something, not fall back to "prop-2".
	if !seen["sezony"] || !seen["sezony-2"] {
		t.Errorf("the invented ids do not spell the name out: %v", seen)
	}
}

// A select whose options arrive as plain strings — the convenient spelling the
// tool documents — got the same fallback ids: "prop", "prop-2", which are also
// property ids in the same schema. An agent reading the schema back cannot tell
// one from the other, and the person reading it learns nothing.
func TestSelectOptionIdsSayTheOptionName(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "optid@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Рилсы", `[]`)

	if _, err := callTool(t, s, u, "update_schema", `{"page_id":"`+db+`","properties":[{"name":"Сезон","type":"select","options":["Лиза","Маша","Мемное"]}]}`); err != nil {
		t.Fatalf("update_schema: %v", err)
	}
	schema, _, err := s.loadCollection(db)
	if err != nil || len(schema) != 1 {
		t.Fatalf("load: %v %#v", err, schema)
	}
	opts, _ := schema[0]["options"].([]any)
	got := map[string]string{}
	for _, o := range opts {
		om, _ := o.(map[string]any)
		id, _ := om["id"].(string)
		name, _ := om["name"].(string)
		got[id] = name
	}
	if got["liza"] != "Лиза" || got["masha"] != "Маша" || got["memnoe"] != "Мемное" {
		t.Errorf("option ids do not say the names: %#v", got)
	}
}

// The fallback is still there for a name with nothing in it, and it still
// counts up — that is what keeps two properties of the same name apart.
func TestSlugIDFallsBackAndCountsUp(t *testing.T) {
	if got := slugID("Сезоны", map[string]bool{}); got != "sezony" {
		t.Errorf("Сезоны → %q, want sezony", got)
	}
	if got := slugID("  ...  ", map[string]bool{}); got != "prop" {
		t.Errorf("a name with no letters → %q, want prop", got)
	}
	if got := slugID("Сезоны", map[string]bool{"sezony": true}); got != "sezony-2" {
		t.Errorf("a taken id → %q, want sezony-2", got)
	}
	if got := slugID("Due date", map[string]bool{}); got != "due-date" {
		t.Errorf("Due date → %q, want due-date", got)
	}
}
