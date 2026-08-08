package server

import (
	"encoding/json"
	"testing"
)

// A rollup without a condition must keep counting everything — existing
// databases carry rollups defined before conditions existed, and they may not
// change meaning under an upgrade.
func TestRollupWithoutConditionCountsEverything(t *testing.T) {
	d := propDef{Type: "rollup", RollupAgg: "count"}
	for _, props := range []map[string]any{
		{"status": "erledigt"},
		{"status": "eingang"},
		{},
		nil,
	} {
		if !matchesRollupWhere(d, props) {
			t.Errorf("no condition should match %v", props)
		}
	}
}

// The number a progress bar needs: how many of the related rows are done.
func TestRollupConditionSelectsRows(t *testing.T) {
	done := propDef{RollupWhereProp: "status", RollupWhereOp: "is", RollupWhereValue: "erledigt"}
	open := propDef{RollupWhereProp: "status", RollupWhereOp: "is_not", RollupWhereValue: "erledigt"}

	rows := []map[string]any{
		{"status": "erledigt"},
		{"status": "erledigt"},
		{"status": "eingang"},
		{"status": "in-arbeit"},
		{}, // no status at all
	}
	count := func(d propDef) int {
		n := 0
		for _, r := range rows {
			if matchesRollupWhere(d, r) {
				n++
			}
		}
		return n
	}
	if got := count(done); got != 2 {
		t.Errorf("done: got %d, want 2", got)
	}
	// is_not must include the row with no status — "not done" is true of it.
	if got := count(open); got != 3 {
		t.Errorf("open: got %d, want 3", got)
	}
}

func TestRollupConditionOperators(t *testing.T) {
	cases := []struct {
		op, value string
		props     map[string]any
		want      bool
	}{
		{"is", "a", map[string]any{"p": "a"}, true},
		{"is", "a", map[string]any{"p": "b"}, false},
		{"is_not", "a", map[string]any{"p": "b"}, true},
		{"is_empty", "", map[string]any{"p": ""}, true},
		{"is_empty", "", map[string]any{"p": "  "}, true}, // whitespace is empty
		{"is_empty", "", map[string]any{"p": "x"}, false},
		{"is_not_empty", "", map[string]any{"p": "x"}, true},
		{"is_not_empty", "", map[string]any{}, false}, // missing counts as empty
		{"contains", "erle", map[string]any{"p": "erledigt"}, true},
		{"contains", "ERLE", map[string]any{"p": "erledigt"}, true}, // case-insensitive
		{"contains", "x", map[string]any{"p": "erledigt"}, false},
		// A typo in the operator must not silently match everything — that would
		// turn "done" into "all" and quietly show 100% progress.
		{"tippfehler", "a", map[string]any{"p": "b"}, false},
		{"tippfehler", "a", map[string]any{"p": "a"}, true},
		// Non-string values still compare (a number in a text condition).
		{"is", "3", map[string]any{"p": float64(3)}, true},
	}
	for _, c := range cases {
		d := propDef{RollupWhereProp: "p", RollupWhereOp: c.op, RollupWhereValue: c.value}
		if got := matchesRollupWhere(d, c.props); got != c.want {
			t.Errorf("op=%q value=%q props=%v → %v, want %v", c.op, c.value, c.props, got, c.want)
		}
	}
}

// A backrelation with an incomplete definition must return nothing rather than
// guessing at a collection — a half-configured property is a blank column, not
// a listing of somebody else's database.
func TestBackrelationNeedsBothHalves(t *testing.T) {
	s := &Server{}
	rows := []map[string]any{{"id": "abc"}}
	for _, d := range []propDef{
		{Type: "backrelation"},
		{Type: "backrelation", BackrelationCollection: "coll"},
		{Type: "backrelation", BackrelationProp: "system"},
	} {
		got := s.backrelationIDs(&user{ID: "u"}, d, rows)
		if len(got) != 1 || got[0] != nil {
			t.Errorf("incomplete definition %+v returned %v, want nothing", d, got)
		}
	}
}

// "Open" means neither done NOR discarded, and one comparison cannot say that:
// is_not "done" counts every discarded row as open — silently, and by exactly
// the amount nobody notices until the first row is discarded.
func TestRollupConditionAcceptsSeveralValues(t *testing.T) {
	rows := []map[string]any{
		{"status": "open"}, {"status": "in-progress"},
		{"status": "done"}, {"status": "done"},
		{"status": "discarded"},
	}
	count := func(d propDef) int {
		n := 0
		for _, r := range rows {
			if matchesRollupWhere(d, r) {
				n++
			}
		}
		return n
	}

	open := propDef{RollupWhereProp: "status", RollupWhereOp: "is_not",
		RollupWhereValues: []string{"done", "discarded"}}
	if got := count(open); got != 2 {
		t.Errorf("open counted %d, want 2 (open and in-progress)", got)
	}
	closed := propDef{RollupWhereProp: "status", RollupWhereOp: "is",
		RollupWhereValues: []string{"done", "discarded"}}
	if got := count(closed); got != 3 {
		t.Errorf("closed counted %d, want 3", got)
	}
	// The single-value form must behave exactly as it always did — a condition
	// written before this existed may not change its answer under an upgrade.
	oldStyle := propDef{RollupWhereProp: "status", RollupWhereOp: "is_not", RollupWhereValue: "done"}
	if got := count(oldStyle); got != 3 {
		t.Errorf("the old single-value form counted %d, want 3 (it still counts the discarded row)", got)
	}
	// A list wins over the single value when both are set, rather than the two
	// being combined into something nobody asked for.
	both := propDef{RollupWhereProp: "status", RollupWhereOp: "is",
		RollupWhereValue: "open", RollupWhereValues: []string{"done"}}
	if got := count(both); got != 2 {
		t.Errorf("with both set the list should win: counted %d, want 2", got)
	}
}

// A row opened as a PAGE has to show the same numbers its card shows.
// computeDerived ran in exactly one place — the query that returns a
// collection's rows — so the page route rendered an em dash where the board two
// clicks away had a number. Nothing was missing; this route never asked.
func TestDerivedValuesAppearOnThePageItself(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "page@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"},
		{"id":"status","name":"Status","type":"select","options":[{"id":"done","name":"Done"},{"id":"open","name":"Open"}]}]`)

	if _, err := s.mcpUpdateSchema(systems, json.RawMessage(`[
		{"name":"Tasks","type":"backrelation","backrelationCollection":"`+tasks+`","backrelationProp":"system"},
		{"name":"Open","type":"rollup","rollupRelation":"tasks","rollupTarget":"status","rollupAgg":"count",
		 "rollupWhereProp":"status","rollupWhereOp":"is_not","rollupWhereValue":"done"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}

	row := s.makeRow(t, ws, uid, systems, "Salt", `{}`)
	s.makeRow(t, ws, uid, tasks, "A", `{"system":["`+row+`"],"status":"open"}`)
	s.makeRow(t, ws, uid, tasks, "B", `{"system":["`+row+`"],"status":"open"}`)
	s.makeRow(t, ws, uid, tasks, "C", `{"system":["`+row+`"],"status":"done"}`)

	p, err := s.getPage(row)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Before the fix the props came back exactly as stored — empty.
	s.fillDerivedForPage(u, p)

	var props map[string]any
	if err := json.Unmarshal(p.Props, &props); err != nil {
		t.Fatalf("props: %v", err)
	}
	if got := relationIDs(props["tasks"]); len(got) != 3 {
		t.Errorf("the page shows %d tasks, want 3", len(got))
	}
	// The value travels as JSON, so it arrives as a float — compare numerically
	// rather than by Go type.
	if got, ok := toNumber(props["open"]); !ok || got != 2 {
		t.Errorf("the page shows %v open, want 2", props["open"])
	}
}

// A page that is not a database row must come back untouched — no schema to
// compute against, and no reason to pay for a query.
func TestDerivedIsANoOpForAnOrdinaryPage(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "plain@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	id := s.makePage(t, ws, uid, "", "Just a page", `{"note":"kept"}`)

	p, err := s.getPage(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	before := string(p.Props)
	s.fillDerivedForPage(&user{ID: uid}, p)
	if string(p.Props) != before {
		t.Errorf("props changed: %s → %s", before, p.Props)
	}
}

// A progress bar without a formula. A formula would have to divide, and 0 of 0
// related rows is a division by zero — which renders as "⚠ division by zero" in
// the column of every newly created row, forever.
func TestRollupPercent(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "pct@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"},
		{"id":"status","name":"Status","type":"select","options":[{"id":"done","name":"Done"}]}]`)
	if _, err := s.mcpUpdateSchema(systems, json.RawMessage(`[
		{"name":"Tasks","type":"backrelation","backrelationCollection":"`+tasks+`","backrelationProp":"system"},
		{"name":"Progress","type":"rollup","rollupRelation":"tasks","rollupTarget":"status","rollupAgg":"percent",
		 "rollupWhereProp":"status","rollupWhereOp":"is","rollupWhereValue":"done","numberDisplay":"bar"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}

	withTasks := s.makeRow(t, ws, uid, systems, "Busy", `{}`)
	s.makeRow(t, ws, uid, tasks, "A", `{"system":["`+withTasks+`"],"status":"done"}`)
	s.makeRow(t, ws, uid, tasks, "B", `{"system":["`+withTasks+`"],"status":"done"}`)
	s.makeRow(t, ws, uid, tasks, "C", `{"system":["`+withTasks+`"],"status":"open"}`)
	s.makeRow(t, ws, uid, tasks, "D", `{"system":["`+withTasks+`"],"status":"open"}`)
	// A system with NO tasks at all: the case a formula cannot survive.
	empty := s.makeRow(t, ws, uid, systems, "Fresh", `{}`)

	schema, _, err := s.loadCollection(systems)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	rows := []map[string]any{
		{"id": withTasks, "props": map[string]any{}},
		{"id": empty, "props": map[string]any{}},
	}
	s.computeDerived(u, parseSchema(mustJSON(t, schema)), rows)

	if got, _ := toNumber(rows[0]["props"].(map[string]any)["progress"]); got != 50 {
		t.Errorf("two of four done is %v, want 50", got)
	}
	got := rows[1]["props"].(map[string]any)["progress"]
	if n, ok := toNumber(got); !ok || n != 0 {
		t.Errorf("a system with no tasks should be 0, got %#v — a formula would say \"division by zero\" here", got)
	}
}
