package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Steps 3 to 10 of the consolidation. Same rule as before: the test is not
// "how many entries are there" but "can each folded capability still be done".
//
// The genuinely new risk in these steps is the permission gate. revisions and
// comments carry READ and WRITE actions in one tool, so a gate that judges by
// tool name alone is wrong in one direction or the other: too strict and a
// viewer cannot read a history they are allowed to see, too loose and a
// read-only token can restore a page.

func TestWriteContentDoesAllThreeModes(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "write@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Notes", `{}`)

	body := func() string {
		out, err := callTool(t, s, u, "get_page", `{"page_id":"`+page+`"}`)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		return out
	}

	// The default is append: a missing mode must do the harmless thing.
	if _, err := callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"first"}`); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"zero","mode":"prepend"}`); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	got := body()
	if !strings.Contains(got, "first") || !strings.Contains(got, "zero") {
		t.Fatalf("append or prepend lost its text: %s", got)
	}
	if strings.Index(got, "zero") > strings.Index(got, "first") {
		t.Error("prepend did not put the text first")
	}

	if _, err := callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"only this","mode":"replace"}`); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := body(); strings.Contains(got, "first") {
		t.Error("replace did not replace")
	}
	if _, err := callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"x","mode":"nonsense"}`); err == nil {
		t.Error("an unknown mode should be refused, not silently appended")
	}
}

func TestRevisionsListGetRestore(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "rev@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	// Two writes so there is something to go back to.
	callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"version one","mode":"replace"}`)
	callTool(t, s, u, "write_content", `{"page_id":"`+page+`","markdown":"version two","mode":"replace"}`)

	out, err := callTool(t, s, u, "revisions", `{"page_id":"`+page+`"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed struct {
		Revisions []struct{ ID string } `json:"revisions"`
	}
	if err := json.Unmarshal([]byte(stripMarkers(out)), &listed); err != nil {
		t.Fatalf("list is not JSON: %v (%s)", err, out)
	}
	if len(listed.Revisions) == 0 {
		t.Fatal("no revisions listed")
	}
	rev := listed.Revisions[len(listed.Revisions)-1].ID

	if _, err := callTool(t, s, u, "revisions", `{"page_id":"`+page+`","action":"get","revision_id":"`+rev+`"}`); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := callTool(t, s, u, "revisions", `{"page_id":"`+page+`","action":"restore","revision_id":"`+rev+`"}`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	// Missing ids must be named, not guessed at.
	if _, err := callTool(t, s, u, "revisions", `{"page_id":"`+page+`","action":"get"}`); err == nil {
		t.Error("get without a revision_id should be refused")
	}
}

func TestCommentsListAddResolve(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "cmt@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := callTool(t, s, u, "comments", `{"page_id":"`+page+`","action":"add","body":"look at this"}`); err != nil {
		t.Fatalf("add: %v", err)
	}
	out, err := callTool(t, s, u, "comments", `{"page_id":"`+page+`"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "look at this") {
		t.Fatalf("the comment is not in the list: %s", out)
	}
	var id string
	s.db.QueryRow(`SELECT id FROM comments WHERE page_id = ?`, page).Scan(&id)
	if _, err := callTool(t, s, u, "comments", `{"page_id":"`+page+`","action":"resolve","comment_id":"`+id+`"}`); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var resolved any
	s.db.QueryRow(`SELECT resolved_at FROM comments WHERE id = ?`, id).Scan(&resolved)
	if resolved == nil {
		t.Error("the comment was not resolved")
	}
	if _, err := callTool(t, s, u, "comments", `{"page_id":"`+page+`","action":"add"}`); err == nil {
		t.Error("adding without a body should be refused")
	}
	// Deleting is deliberately NOT an action here.
	if _, err := callTool(t, s, u, "comments", `{"page_id":"`+page+`","action":"delete","comment_id":"`+id+`"}`); err == nil {
		t.Error("delete must not be reachable as an action — it has its own tool")
	}
}

// The gate that judges by ACTION, not by tool name. This is the one that would
// silently go wrong.
func TestReadOnlyTokenMayReadHistoryButNotRestoreIt(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "scope@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)
	writer := &user{ID: uid, Name: "Test"}
	callTool(t, s, writer, "write_content", `{"page_id":"`+page+`","markdown":"one","mode":"replace"}`)

	reader := &user{ID: uid, Name: "Test", TokenScope: "read"}

	if _, err := callTool(t, s, reader, "revisions", `{"page_id":"`+page+`"}`); err != nil {
		t.Errorf("a read-only token must be able to LIST revisions: %v", err)
	}
	if _, err := callTool(t, s, reader, "revisions", `{"page_id":"`+page+`","action":"restore","revision_id":"x"}`); err == nil {
		t.Error("a read-only token must not be able to restore")
	}
	if _, err := callTool(t, s, reader, "comments", `{"page_id":"`+page+`"}`); err != nil {
		t.Errorf("a read-only token must be able to LIST comments: %v", err)
	}
	if _, err := callTool(t, s, reader, "comments", `{"page_id":"`+page+`","action":"add","body":"no"}`); err == nil {
		t.Error("a read-only token must not be able to comment")
	}
}

func TestSetTrashedBothWays(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "trash@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Doc", `{}`)

	if _, err := callTool(t, s, u, "set_trashed", `{"page_id":"`+page+`","trashed":true}`); err != nil {
		t.Fatalf("trash: %v", err)
	}
	var trashed any
	s.db.QueryRow(`SELECT trashed_at FROM pages WHERE id = ?`, page).Scan(&trashed)
	if trashed == nil {
		t.Fatal("the page was not trashed")
	}
	// Restoring has to work on a TRASHED page — the gate must not lock it away.
	if _, err := callTool(t, s, u, "set_trashed", `{"page_id":"`+page+`","trashed":false}`); err != nil {
		t.Fatalf("restore: %v", err)
	}
	s.db.QueryRow(`SELECT trashed_at FROM pages WHERE id = ?`, page).Scan(&trashed)
	if trashed != nil {
		t.Error("the page was not restored")
	}
	// A missing boolean must be refused rather than guessed.
	if _, err := callTool(t, s, u, "set_trashed", `{"page_id":"`+page+`"}`); err == nil {
		t.Error("set_trashed without the flag should be refused")
	}
}

func TestGetLinksIsBothBacklinksAndGraph(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "links@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	target := s.makePage(t, ws, uid, "", "Target", `{}`)
	source := s.makePage(t, ws, uid, "", "Source", `{}`)
	s.db.Exec(`INSERT INTO links (source_id, target_id) VALUES (?, ?)`, source, target)

	out, err := callTool(t, s, u, "get_links", `{"page_id":"`+target+`"}`)
	if err != nil {
		t.Fatalf("backlinks: %v", err)
	}
	if !strings.Contains(out, "Source") {
		t.Errorf("backlinks did not find the page pointing here: %s", out)
	}
	out, err = callTool(t, s, u, "get_links", `{}`)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	if !strings.Contains(out, "orphans") {
		t.Errorf("the graph form should carry orphans: %s", out)
	}
}

func TestSetViewCreatesAndUpdates(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "view@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)
	db := tasksCollection(t, s, ws, uid)

	if _, err := callTool(t, s, u, "set_view", `{"page_id":"`+db+`","name":"Open","type":"board","group_by":"status"}`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := callTool(t, s, u, "set_view",
		`{"page_id":"`+db+`","view_id":"open","filters":[{"property":"status","op":"is_not","value":"done"}]}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	v := viewByID(t, s, db, "open")
	if f, _ := v["filters"].([]any); len(f) != 1 {
		t.Errorf("the filter did not stick: %#v", v["filters"])
	}
	if v["name"] != "Open" {
		t.Errorf("the update wiped the name: %#v", v["name"])
	}
	if _, err := callTool(t, s, u, "set_view", `{"page_id":"`+db+`","view_id":"open","type":"table"}`); err == nil {
		t.Error("changing a view's type should be refused")
	}
}

// Creating a workspace was possible, renaming one was not — the gap this step
// closes on the way past.
func TestWorkspaceCreatesAndRenames(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "ws@example.test")
	u := &user{ID: uid, Name: "Test"}
	ws := s.firstWorkspaceOf(t, uid)

	if _, err := callTool(t, s, u, "workspace", `{"workspace_id":"`+ws+`","name":"Renamed"}`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	var name string
	s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, ws).Scan(&name)
	if name != "Renamed" {
		t.Errorf("the workspace is still called %q", name)
	}
	if _, err := callTool(t, s, u, "workspace", `{"workspace_id":"`+ws+`"}`); err == nil {
		t.Error("a call that changes nothing should say so")
	}
	// A stranger must not be able to rename it.
	other, _ := signedIn(t, s, "other@example.test")
	if _, err := callTool(t, s, &user{ID: other}, "workspace", `{"workspace_id":"`+ws+`","name":"Hijacked"}`); err == nil {
		t.Error("a non-member renamed somebody else's workspace")
	}
}

// The untrusted-content markers wrap agent-facing answers; tests that parse
// those answers have to look past them.
func stripMarkers(s string) string {
	const begin = "-----"
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	_ = begin
	return s
}
