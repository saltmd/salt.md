package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// get_graph returned Markdown links and nothing else, while its own description
// told agents to use it for finding orphans. It could not: the answer was a
// list of EDGES, so a page with no link was not in it at all — and an empty
// answer reads exactly like "there are none".
//
// Measured once against a real workspace before this: 9 edges returned, 9 real
// relationships missing, 6 of 13 pages absent entirely.

type graphResult struct {
	Edges   []graphEdge `json:"edges"`
	Orphans []graphNode `json:"orphans"`
	Nodes   []graphNode `json:"nodes"`
	Counts  struct {
		Nodes, Edges, Orphans int
	} `json:"counts"`
}

func readGraph(t *testing.T, s *Server, u *user, kinds []string, nodes bool) graphResult {
	t.Helper()
	out, err := s.mcpGraph(u, "", kinds, nodes)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	var g graphResult
	if err := json.Unmarshal([]byte(out), &g); err != nil {
		t.Fatalf("graph is not JSON: %v", err)
	}
	return g
}

func edgeKinds(g graphResult) map[string]int {
	out := map[string]int{}
	for _, e := range g.Edges {
		out[e.Kind]++
	}
	return out
}

// Everything the old graph could not see, in one workspace.
func TestGraphSeesHierarchyRowsAndEmbeds(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "graph@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)

	handbook := s.makePage(t, ws, uid, "", "Handbook", `{}`)
	chapter := s.makePage(t, ws, uid, handbook, "Chapter", `{}`) // child edge
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"t","name":"T","type":"text"}]`)
	row := s.makeRow(t, ws, uid, tasks, "A task", `{}`)   // row edge
	s.makePage(t, ws, uid, "", "Nobody links here", `{}`) // the orphan

	// A Markdown link, recorded the way the importer records one.
	if _, err := s.db.Exec(`INSERT INTO links (source_id, target_id) VALUES (?, ?)`, chapter, handbook); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	// An embedded database sits in the page body as a block.
	if _, err := s.db.Exec(`UPDATE pages SET content = ? WHERE id = ?`,
		`[{"id":"b1","type":"database","props":{"collectionId":"`+tasks+`"},"children":[]}]`, handbook); err != nil {
		t.Fatalf("embed: %v", err)
	}

	g := readGraph(t, s, u, nil, true)
	kinds := edgeKinds(g)
	for _, want := range []string{"link", "child", "row", "embed"} {
		if kinds[want] == 0 {
			t.Errorf("no %q edge — the graph still cannot see that relationship (%v)", want, kinds)
		}
	}

	// The orphan is FOUND, not merely absent. (The welcome page a fresh instance
	// creates is genuinely unconnected too, so assert membership, not the count.)
	orphanTitles := map[string]bool{}
	for _, o := range g.Orphans {
		orphanTitles[o.Title] = true
	}
	if !orphanTitles["Nobody links here"] {
		t.Errorf("the unlinked page is not reported as an orphan: %v", g.Orphans)
	}
	for _, connected := range []string{"Handbook", "Chapter", "Tasks", "A task"} {
		if orphanTitles[connected] {
			t.Errorf("%q has edges and must not be an orphan", connected)
		}
	}
	// Nodes distinguish the three things that look alike from outside.
	byKind := map[string]int{}
	for _, n := range g.Nodes {
		byKind[n.Kind]++
	}
	if byKind["database"] != 1 || byKind["row"] != 1 {
		t.Errorf("node kinds = %v, want one database and one row", byKind)
	}
	if g.Counts.Nodes != len(g.Nodes) || g.Counts.Edges != len(g.Edges) {
		t.Errorf("counts disagree with the lists: %+v", g.Counts)
	}
	_ = row
}

// The node list is opt-in: on a real instance it is thousands of entries, and
// the question people ask is answered by orphans.
func TestGraphOmitsNodesUnlessAsked(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "nodes@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)
	s.makePage(t, ws, uid, "", "Alone", `{}`)

	if g := readGraph(t, s, u, nil, false); len(g.Nodes) != 0 {
		t.Error("nodes should be absent by default")
	} else if g.Counts.Nodes == 0 {
		t.Error("the count must still be reported, or the caller cannot tell it was omitted")
	}
	if g := readGraph(t, s, u, nil, true); len(g.Nodes) == 0 {
		t.Error("include_nodes should return them")
	}
}

func TestGraphFiltersByKind(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "kinds@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)
	parent := s.makePage(t, ws, uid, "", "Parent", `{}`)
	s.makePage(t, ws, uid, parent, "Child", `{}`)

	g := readGraph(t, s, u, []string{"child"}, false)
	if k := edgeKinds(g); k["child"] == 0 || len(k) != 1 {
		t.Errorf("kinds=[child] returned %v", k)
	}
	if _, err := s.mcpGraph(u, "", []string{"nonsense"}, false); err == nil {
		t.Error("an unknown kind should be refused, not silently ignored")
	}
}

// The hierarchy edge is exactly the shape that leaks: a private sub-page under
// a shared parent would announce its own existence through its child edge.
func TestGraphHidesPagesTheCallerMayNotRead(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "owner@example.test")
	colleague, _ := signedIn(t, s, "colleague@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	// A PLAIN member, not an admin: an admin bypasses the private-ancestor rule,
	// so a test written with one would pass no matter what the code did.
	s.addMember(t, ws, colleague, "member")

	parent := s.makePage(t, ws, owner, "", "Shared", `{}`)
	secret := s.makePage(t, ws, owner, parent, "Secret", `{}`)
	if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, secret); err != nil {
		t.Fatalf("make private: %v", err)
	}
	// A sub-page of the secret must stay hidden too — the rule is about the whole
	// ancestor chain, not only the page itself.
	buried := s.makePage(t, ws, owner, secret, "Buried", `{}`)

	// The owner sees both.
	mine := readGraph(t, s, &user{ID: owner}, nil, true)
	seen := map[string]bool{}
	for _, n := range mine.Nodes {
		seen[n.ID] = true
	}
	if !seen[secret] || !seen[buried] {
		t.Fatal("the owner should see their own private pages")
	}

	// The colleague sees neither, and no edge mentions them.
	g := readGraph(t, s, &user{ID: colleague}, nil, true)
	for _, n := range g.Nodes {
		if n.ID == secret || n.ID == buried {
			t.Errorf("a colleague can see %q in the graph", n.Title)
		}
	}
	for _, e := range g.Edges {
		if strings.Contains(e.From+e.To, secret) || strings.Contains(e.From+e.To, buried) {
			t.Errorf("an edge leaks the private page: %+v", e)
		}
	}
	// The shared parent must still be there — hiding too much is also a bug.
	found := false
	for _, n := range g.Nodes {
		if n.ID == parent {
			found = true
		}
	}
	if !found {
		t.Error("the shared parent disappeared along with its private child")
	}
}

// A parent chain should be a tree; nothing in the schema enforces it. A cycle
// must end the walk, not the server.
func TestGraphSurvivesAParentCycle(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "cycle@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	a := s.makePage(t, ws, uid, "", "A", `{}`)
	b := s.makePage(t, ws, uid, a, "B", `{}`)
	if _, err := s.db.Exec(`UPDATE pages SET parent_id = ? WHERE id = ?`, b, a); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	done := make(chan struct{})
	go func() {
		readGraph(t, s, &user{ID: uid}, nil, true)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the graph hung on a parent cycle")
	}
}

func TestEmbeddedCollectionIDs(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{`[{"type":"database","props":{"collectionId":"abc"}}]`, 1},
		{`[{"props":{"collectionId":"a"}},{"props":{"collectionId":"b"}}]`, 2},
		{`[{"props":{"collectionId":""}}]`, 0},
		{`[{"type":"paragraph"}]`, 0},
		{`{"collectionId":`, 0}, // truncated, must not panic
	} {
		if got := embeddedCollectionIDs(c.in); len(got) != c.want {
			t.Errorf("%s → %v, want %d", c.in, got, c.want)
		}
	}
}
