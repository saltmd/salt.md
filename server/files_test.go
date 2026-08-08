package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The file index (W125): built from what pages already contain, and read back
// through the same two permission stages as search.

func TestScanBlocksForFiles(t *testing.T) {
	// Three shapes that all occur in real content: the MCP upload's block, a
	// BlockNote block with extra props, and one nested inside a column layout.
	// Plus two that must NOT be picked up: an external URL and a traversal.
	content := `[
		{"type":"file","props":{"url":"/files/aaa.pdf","name":"Angebot.pdf"}},
		{"type":"image","props":{"url":"/files/bbb.png","name":"Logo.png","previewWidth":512}},
		{"type":"columnList","children":[
			{"type":"column","children":[
				{"type":"file","props":{"url":"/files/ccc.docx","name":"Vertrag.docx"}}
			]}
		]},
		{"type":"image","props":{"url":"https://example.com/x.png","name":"extern"}},
		{"type":"file","props":{"url":"/files/../secret","name":"nope"}},
		{"type":"file","props":{"url":"/files/aaa.pdf","name":"Angebot.pdf"}}
	]`
	refs := scanBlocksForFiles(content)
	got := map[string]string{}
	for _, r := range refs {
		got[r.name] = r.displayName
	}
	if len(got) != 3 {
		t.Fatalf("want 3 refs, got %d: %+v", len(got), got)
	}
	if got["aaa.pdf"] != "Angebot.pdf" || got["bbb.png"] != "Logo.png" || got["ccc.docx"] != "Vertrag.docx" {
		t.Errorf("wrong refs: %+v", got)
	}
	// Malformed content must not panic or invent references.
	if refs := scanBlocksForFiles("not json"); len(refs) != 0 {
		t.Errorf("garbage content produced refs: %+v", refs)
	}
}

// seedFile writes a byte on disk so the index has a size to record.
func seedFile(t *testing.T, s *Server, name, body string) {
	t.Helper()
	dir := filepath.Join(s.dataDir, "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func seedPage(t *testing.T, s *Server, id, parent, ws, owner, visibility, content string) {
	t.Helper()
	var par any
	if parent != "" {
		par = parent
	}
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility, type)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, 'doc')`,
		id, par, "Page "+id, content, now(), now(), ws, owner, visibility); err != nil {
		t.Fatalf("insert page %s: %v", id, err)
	}
}

func TestFileIndexBuildsAndRespectsPermissions(t *testing.T) {
	s := testServer(t)
	admin, _ := signedIn(t, s, "admin@example.com")
	ws := makeWorkspace(t, s, admin)
	alice, aliceCookie := signedIn(t, s, "alice@example.com")
	bob, bobCookie := signedIn(t, s, "bob@example.com")
	for _, id := range []string{alice, bob} {
		if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, id); err != nil {
			t.Fatalf("insert member: %v", err)
		}
	}

	seedFile(t, s, "deal.pdf", "0123456789")
	seedFile(t, s, "sub.pdf", "xy")
	seedFile(t, s, "private.pdf", "secret")
	seedFile(t, s, "orphan.pdf", "nobody references me")

	seedPage(t, s, "deal", "", ws, alice, "workspace",
		`[{"type":"file","props":{"url":"/files/deal.pdf","name":"Angebot.pdf"}}]`)
	seedPage(t, s, "dossier", "deal", ws, alice, "workspace",
		`[{"type":"file","props":{"url":"/files/sub.pdf","name":"Freigabe.pdf"}}]`)
	seedPage(t, s, "secret", "", ws, alice, "private",
		`[{"type":"file","props":{"url":"/files/private.pdf","name":"Geheim.pdf"}}]`)

	// Force the rebuild the way a version bump would.
	s.setSetting("files_version", "0")
	if err := s.migrateFileIndex(); err != nil {
		t.Fatalf("migrateFileIndex: %v", err)
	}

	var indexed int
	s.db.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&indexed)
	if indexed != 4 {
		t.Fatalf("index holds %d files, want 4 (3 referenced + 1 orphan)", indexed)
	}
	// The human name and the size come along.
	var disp string
	var size int64
	s.db.QueryRow(`SELECT display_name, size FROM files WHERE file_name = 'deal.pdf'`).Scan(&disp, &size)
	if disp != "Angebot.pdf" || size != 10 {
		t.Errorf("deal.pdf: display=%q size=%d", disp, size)
	}
	// The unreferenced file is indexed without a page — that is the point of
	// indexing it at all.
	var orphanPage any
	s.db.QueryRow(`SELECT page_id FROM files WHERE file_name = 'orphan.pdf'`).Scan(&orphanPage)
	if orphanPage != nil {
		t.Errorf("orphan carries a page: %v", orphanPage)
	}

	list := func(cookie, query string) []fileJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/files"+query, nil)
		req.Header.Set("Cookie", cookie)
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/files%s: %d %s", query, rec.Code, rec.Body.String())
		}
		var out []fileJSON
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}
	names := func(fs []fileJSON) map[string]bool {
		m := map[string]bool{}
		for _, f := range fs {
			m[f.Name] = true
		}
		return m
	}

	// Alice owns everything and sees all three of her files (the orphan has no
	// workspace, so it is in nobody's list — it exists for housekeeping).
	got := names(list(aliceCookie, "?workspace="+ws))
	for _, want := range []string{"deal.pdf", "sub.pdf", "private.pdf"} {
		if !got[want] {
			t.Errorf("alice misses %s", want)
		}
	}
	if got["orphan.pdf"] {
		t.Errorf("an unreferenced file showed up in a workspace list")
	}

	// Bob is in the same workspace — and must NOT see the file on Alice's
	// private page. This is the second permission stage; the workspace filter
	// alone would have handed it over.
	got = names(list(bobCookie, "?workspace="+ws))
	if !got["deal.pdf"] || !got["sub.pdf"] {
		t.Errorf("bob misses shared files: %+v", got)
	}
	if got["private.pdf"] {
		t.Errorf("bob sees a file on somebody else's private page")
	}

	// Subtree filter: the deal and everything under it.
	got = names(list(aliceCookie, "?under=deal"))
	if !got["deal.pdf"] || !got["sub.pdf"] {
		t.Errorf("subtree of the deal is incomplete: %+v", got)
	}
	if got["private.pdf"] {
		t.Errorf("subtree filter leaked an unrelated page's file")
	}
	// …and the leaf alone.
	got = names(list(aliceCookie, "?under=dossier"))
	if len(got) != 1 || !got["sub.pdf"] {
		t.Errorf("leaf subtree: %+v", got)
	}

	// The carrier page travels with each entry, so the view can link to it.
	for _, f := range list(aliceCookie, "?under=deal") {
		if f.Name == "sub.pdf" && f.PageID != "dossier" {
			t.Errorf("sub.pdf points at page %q", f.PageID)
		}
	}

	// A trashed page takes its files out of the list (they are not gone, just
	// not in the working set).
	if _, err := s.db.Exec(`UPDATE pages SET trashed_at = ? WHERE id = 'dossier'`, now()); err != nil {
		t.Fatalf("trash: %v", err)
	}
	if names(list(aliceCookie, "?workspace="+ws))["sub.pdf"] {
		t.Errorf("a trashed page still contributes files")
	}
}

func TestMcpListFilesRespectsPermissions(t *testing.T) {
	s := testServer(t)
	admin, _ := signedIn(t, s, "admin@example.com")
	ws := makeWorkspace(t, s, admin)
	alice, _ := signedIn(t, s, "alice@example.com")
	bob, _ := signedIn(t, s, "bob@example.com")
	for _, id := range []string{alice, bob} {
		if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, id); err != nil {
			t.Fatalf("insert member: %v", err)
		}
	}
	seedFile(t, s, "open.pdf", "abc")
	seedFile(t, s, "closed.pdf", "abc")
	seedPage(t, s, "open", "", ws, alice, "workspace",
		`[{"type":"file","props":{"url":"/files/open.pdf","name":"Offen.pdf"}}]`)
	seedPage(t, s, "closed", "", ws, alice, "private",
		`[{"type":"file","props":{"url":"/files/closed.pdf","name":"Zu.pdf"}}]`)
	s.setSetting("files_version", "0")
	if err := s.migrateFileIndex(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	out, err := s.mcpListFiles(&user{ID: bob}, ws, "")
	if err != nil {
		t.Fatalf("mcpListFiles: %v", err)
	}
	var res struct {
		Files []struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		} `json:"files"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Count != 1 || res.Files[0].URL != "/files/open.pdf" {
		t.Fatalf("agent sees the wrong set: %s", out)
	}
	// A workspace the token cannot reach is not found, not empty.
	if _, err := s.mcpListFiles(&user{ID: bob, TokenWorkspaces: []string{"other"}}, ws, ""); err == nil {
		t.Errorf("a token outside the workspace got a listing")
	}
}
