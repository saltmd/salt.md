package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// An agent could create a view and then never say what it shows: create_view
// took a name, a type, a grouping and a date property, and there was no
// update_view at all. Filters, sort and hidden columns — the three things that
// make two views of one database differ — were reachable only in the browser.
//
// The practical consequence was that "set up a working board" could not be
// finished by an agent. It could make the board; the filter that keeps the done
// column from swallowing it had to be clicked by a human.

func viewByID(t *testing.T, s *Server, pageID, viewID string) map[string]any {
	t.Helper()
	_, views, err := s.loadCollection(pageID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, v := range views {
		if id, _ := v["id"].(string); id == viewID {
			return v
		}
	}
	t.Fatalf("view %q not found", viewID)
	return nil
}

func tasksCollection(t *testing.T, s *Server, ws, uid string) string {
	t.Helper()
	return s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"status","name":"Status","type":"select","options":[
			{"id":"open","name":"Open"},{"id":"done","name":"Done"},{"id":"dropped","name":"Dropped"}]},
		{"id":"system","name":"System","type":"text"},
		{"id":"due","name":"Due","type":"date"}]`)
}

// The three views that were waiting on this, built end to end.
func TestCreateViewCarriesItsConfiguration(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "views@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := tasksCollection(t, s, ws, uid)

	// "Open" — the board people work in. Two filters, ANDed: neither done nor
	// dropped. One condition could not express this; a view can.
	if _, err := s.mcpCreateView(tasks, viewSpec{
		Name: "Open", Type: "board", GroupBy: ptr("status"),
		Filters: &[]map[string]any{
			{"property": "status", "op": "is_not", "value": "done"},
			{"property": "status", "op": "is_not", "value": "dropped"},
		},
		Hidden: &[]string{"due"},
	}); err != nil {
		t.Fatalf("create Open: %v", err)
	}
	v := viewByID(t, s, tasks, "open")
	filters, _ := v["filters"].([]any)
	if len(filters) != 2 {
		t.Fatalf("Open should carry 2 filters, has %d", len(filters))
	}
	if hidden, _ := v["hidden"].([]any); len(hidden) != 1 {
		t.Errorf("Open should hide 1 column, hides %d", len(hidden))
	}

	// "History" — only what is finished, newest first.
	if _, err := s.mcpCreateView(tasks, viewSpec{
		Name: "History", Type: "table",
		Filters: &[]map[string]any{{"property": "status", "op": "is", "value": "done"}},
		Sort:    ptr("due:desc"),
	}); err != nil {
		t.Fatalf("create History: %v", err)
	}
	v = viewByID(t, s, tasks, "history")
	sort, _ := v["sort"].(map[string]any)
	if sort["property"] != "due" || sort["dir"] != "desc" {
		t.Errorf("History sort is %#v, want due/desc", v["sort"])
	}
}

// MERGE, like update_schema: what is not mentioned survives. Getting this wrong
// would silently wipe a filter every time somebody renamed a view.
func TestUpdateViewMergesAndCanClear(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "merge@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := tasksCollection(t, s, ws, uid)

	if _, err := s.mcpCreateView(tasks, viewSpec{
		Name: "Board", Type: "board", GroupBy: ptr("status"),
		Filters: &[]map[string]any{{"property": "status", "op": "is_not", "value": "done"}},
		Sort:    ptr("due:asc"),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Rename only — the filter and the sort must survive untouched.
	if _, err := s.mcpUpdateView(tasks, "board", viewSpec{Name: "Working board"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	v := viewByID(t, s, tasks, "board")
	if v["name"] != "Working board" {
		t.Errorf("name is %v", v["name"])
	}
	if f, _ := v["filters"].([]any); len(f) != 1 {
		t.Errorf("renaming dropped the filter: %#v", v["filters"])
	}
	if v["sort"] == nil {
		t.Error("renaming dropped the sort")
	}

	// An EMPTY list clears — otherwise a filter could never be removed again.
	if _, err := s.mcpUpdateView(tasks, "board", viewSpec{
		Filters: &[]map[string]any{}, Sort: ptr(""),
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	v = viewByID(t, s, tasks, "board")
	if f, _ := v["filters"].([]any); len(f) != 0 {
		t.Errorf("filters not cleared: %#v", v["filters"])
	}
	if _, still := v["sort"]; still {
		t.Error("sort not cleared")
	}
	// The grouping was never mentioned in either call and must still be there.
	if v["groupBy"] != "status" {
		t.Errorf("groupBy lost: %#v", v["groupBy"])
	}
}

// A view that silently ignores a typo is worse than one that refuses it: the
// agent sees a view it believes is filtered, and reads the unfiltered rows as
// the truth.
func TestViewConfigurationIsValidated(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "bad@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := tasksCollection(t, s, ws, uid)

	for _, c := range []struct {
		name string
		spec viewSpec
		want string
	}{
		{"unknown filter property", viewSpec{Name: "A", Type: "table",
			Filters: &[]map[string]any{{"property": "nonsense", "op": "is", "value": "x"}}}, "nonsense"},
		{"unknown filter op", viewSpec{Name: "B", Type: "table",
			Filters: &[]map[string]any{{"property": "status", "op": "roughly", "value": "x"}}}, "roughly"},
		{"unknown sort property", viewSpec{Name: "C", Type: "table", Sort: ptr("nope:asc")}, "nope"},
		{"unknown hidden property", viewSpec{Name: "D", Type: "table", Hidden: &[]string{"ghost"}}, "ghost"},
		{"board without grouping", viewSpec{Name: "E", Type: "board"}, "group_by"},
		{"grouping that does not exist", viewSpec{Name: "F", Type: "board", GroupBy: ptr("ghost")}, "ghost"},
	} {
		_, err := s.mcpCreateView(tasks, c.spec)
		if err == nil {
			t.Errorf("%s: should have been refused", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error should name %q, says %q", c.name, c.want, err)
		}
	}
}

// The type is what the renderer switches on; changing it under an existing view
// leaves its other fields describing something else entirely.
func TestUpdateViewRefusesATypeChange(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "type@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := tasksCollection(t, s, ws, uid)

	if _, err := s.mcpCreateView(tasks, viewSpec{Name: "T", Type: "table"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.mcpUpdateView(tasks, "t", viewSpec{Type: "board"}); err == nil {
		t.Error("changing the type should be refused")
	}
	if _, err := s.mcpUpdateView(tasks, "does-not-exist", viewSpec{Name: "X"}); err == nil {
		t.Error("an unknown view id should be refused")
	}
}

// A tool that is not in the catalogue exists only in Go: no agent can call it.
// This is the check that would have caught the missing "backrelation" type two
// releases earlier.
func TestUpdateViewIsOfferedToAgents(t *testing.T) {
	var found map[string]any
	for _, tool := range mcpTools {
		if tool["name"] == "set_view" {
			found = tool
		}
	}
	if found == nil {
		t.Fatal("set_view is not in the tool list — an agent cannot call it")
	}
	b, _ := json.Marshal(found)
	for _, want := range []string{"filters", "sort", "hidden", "view_id"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("update_view does not offer %q", want)
		}
	}
	// It has to offer creating too — set_view without a view_id — or the two
	// halves of the merge did not both survive.
	if !strings.Contains(string(b), "type") {
		t.Error("set_view does not offer type, so it cannot create a view")
	}
}

// Every new writing tool has to be listed as mutating, or a READ-ONLY token may
// reconfigure views with it. Checked through the real call path rather than
// against a boolean, because the list is what is easy to forget.
func TestUpdateViewRefusesAReadOnlyToken(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "ro@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	tasks := tasksCollection(t, s, ws, uid)
	if _, err := s.mcpCreateView(tasks, viewSpec{Name: "T", Type: "table"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	reader := &user{ID: uid, TokenScope: "read"}
	args := json.RawMessage(`{"page_id":"` + tasks + `","view_id":"t","name":"Renamed"}`)
	_, err := s.mcpCall(reader, "set_view", args, "")
	if err == nil {
		t.Fatal("a read-only token must not be able to change a view")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("the refusal should say why: %v", err)
	}
	if v := viewByID(t, s, tasks, "t"); v["name"] == "Renamed" {
		t.Error("the view was changed despite the refusal")
	}
}

func ptr[T any](v T) *T { return &v }
