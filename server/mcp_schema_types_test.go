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
