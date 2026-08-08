package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Seven tools were folded into three: export_markdown, get_schema,
// add_select_option, move_page, set_favorite, create_from_template and
// set_tag_color are gone as entries. The capability has to survive the entry.
//
// That is the whole risk of this consolidation. Nothing errors when a
// capability quietly disappears — the tool is simply not offered any more, and
// the agent that needed it works around the gap instead of reporting it. So the
// test is not "does the catalogue have 42 entries" but "can each of the seven
// things still be done".

func callTool(t *testing.T, s *Server, u *user, name, args string) (string, error) {
	t.Helper()
	return s.mcpCall(u, name, json.RawMessage(args), "")
}

func TestFoldedCapabilitiesSurvived(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "folded@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)

	parent := s.makePage(t, ws, uid, "", "Parent", `{}`)
	child := s.makePage(t, ws, uid, parent, "Child", `{}`)
	loose := s.makePage(t, ws, uid, "", "Loose", `{}`)

	// export_markdown → get_page(include_children)
	out, err := callTool(t, s, u, "get_page", `{"page_id":"`+parent+`","include_children":true}`)
	if err != nil {
		t.Fatalf("get_page with children: %v", err)
	}
	if !strings.Contains(out, "Child") {
		t.Error("include_children did not bring the sub-page — export_markdown is gone for nothing")
	}
	// and without it, the sub-page must NOT come along
	out, err = callTool(t, s, u, "get_page", `{"page_id":"`+parent+`"}`)
	if err != nil {
		t.Fatalf("get_page: %v", err)
	}
	if strings.Contains(out, "Child") {
		t.Error("a plain get_page must not include the sub-tree")
	}

	// move_page → update_page(parent_id)
	if _, err := callTool(t, s, u, "update_page", `{"page_id":"`+loose+`","parent_id":"`+parent+`"}`); err != nil {
		t.Fatalf("move via update_page: %v", err)
	}
	var got string
	s.db.QueryRow(`SELECT COALESCE(parent_id,'') FROM pages WHERE id = ?`, loose).Scan(&got)
	if got != parent {
		t.Errorf("the page did not move: parent is %q", got)
	}

	// set_favorite → update_page(favorite)
	if _, err := callTool(t, s, u, "update_page", `{"page_id":"`+loose+`","favorite":true}`); err != nil {
		t.Fatalf("favorite via update_page: %v", err)
	}
	var favs int
	s.db.QueryRow(`SELECT COUNT(*) FROM favorites WHERE page_id = ? AND user_id = ?`, loose, uid).Scan(&favs)
	if favs != 1 {
		t.Errorf("favorite not stored (%d rows)", favs)
	}

	// A move and a rename in ONE call must both take effect — that is the whole
	// point of folding them together.
	if _, err := callTool(t, s, u, "update_page",
		`{"page_id":"`+child+`","parent_id":"","title":"Renamed and moved"}`); err != nil {
		t.Fatalf("move + rename: %v", err)
	}
	var title, par string
	s.db.QueryRow(`SELECT title, COALESCE(parent_id,'') FROM pages WHERE id = ?`, child).Scan(&title, &par)
	if title != "Renamed and moved" || par != "" {
		t.Errorf("only half applied: title=%q parent=%q", title, par)
	}

	// get_schema → get_collection, which returns schema AND views
	db := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"status","name":"Status","type":"select","options":[{"id":"open","name":"Open"}]}]`)
	out, err = callTool(t, s, u, "get_collection", `{"page_id":"`+db+`"}`)
	if err != nil {
		t.Fatalf("get_collection: %v", err)
	}
	if !strings.Contains(out, "status") {
		t.Error("get_collection does not carry the schema — get_schema is gone for nothing")
	}

	// add_select_option → update_schema, which can set the option list
	if _, err := callTool(t, s, u, "update_schema",
		`{"page_id":"`+db+`","properties":[{"id":"status","name":"Status","type":"select","options":[{"id":"open","name":"Open"},{"id":"done","name":"Done"}]}]}`); err != nil {
		t.Fatalf("add an option via update_schema: %v", err)
	}
	schema, _, err := s.loadCollection(db)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	opts, _ := schema[0]["options"].([]any)
	if len(opts) != 2 {
		t.Errorf("the added option is missing: %#v", schema[0]["options"])
	}
}

// create_from_template → create_page(template_id)
func TestCreateFromTemplateFoldedIntoCreatePage(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "tmpl@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)

	tmpl := s.makePage(t, ws, uid, "", "Weekly report", `{}`)
	if _, err := s.db.Exec(`UPDATE pages SET is_template = 1 WHERE id = ?`, tmpl); err != nil {
		t.Fatalf("mark template: %v", err)
	}
	out, err := callTool(t, s, u, "create_page", `{"template_id":"`+tmpl+`","title":"Week 32"}`)
	if err != nil {
		t.Fatalf("create from template: %v", err)
	}
	if !strings.Contains(out, "Week 32") && !strings.Contains(strings.ToLower(out), "created") {
		t.Errorf("unexpected answer: %s", out)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE title = 'Week 32'`).Scan(&n)
	if n != 1 {
		t.Errorf("the page was not created from the template (%d found)", n)
	}
}

// The catalogue must not name a tool it no longer has. An agent told to call
// something that is not there only finds out by failing — the same defect as
// get_graph promising to find orphans.
func TestCatalogueNamesNoVanishedTool(t *testing.T) {
	gone := []string{
		// steps 1 and 2
		"export_markdown", "get_schema", "add_select_option", "move_page",
		"set_favorite", "create_from_template", "set_tag_color",
		"list_pages", "list_templates", "list_tags", "list_workspaces",
		"list_files", "list_users", "list_cover_presets",
		// steps 3 to 10
		"append_markdown", "prepend_markdown", "replace_content",
		"get_page_history", "get_revision", "restore_revision",
		"get_comments", "add_comment", "resolve_comment",
		"share_page", "unshare_page", "trash_page", "restore_page",
		"get_backlinks", "get_graph", "batch_set_properties",
		"create_view", "update_view", "create_workspace",
	}
	b, err := json.Marshal(mcpTools)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, name := range gone {
		if strings.Contains(string(b), name) {
			t.Errorf("the catalogue still mentions %q", name)
		}
	}
	// And calling one must fail cleanly rather than half-work.
	s := testServer(t)
	uid, _ := signedIn(t, s, "gone@example.test")
	for _, name := range gone {
		if _, err := callTool(t, s, &user{ID: uid}, name, `{}`); err == nil {
			t.Errorf("%q still answers — it should be gone", name)
		}
	}
}
