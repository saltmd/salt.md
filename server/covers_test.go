package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The picker in Editor.tsx and the list agents get over MCP have to be the same
// list. Two sources of truth are tolerable only while something notices when
// they drift; this is that something.
func TestCoverPresetsMatchUI(t *testing.T) {
	raw, err := os.ReadFile("../web/src/components/Editor.tsx")
	if err != nil {
		t.Fatalf("read Editor.tsx: %v", err)
	}
	rx := regexp.MustCompile(`'(gradient:[^']+)'`)
	var inUI []string
	seen := map[string]bool{}
	for _, m := range rx.FindAllStringSubmatch(string(raw), -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			inUI = append(inUI, m[1])
		}
	}
	if len(inUI) == 0 {
		t.Fatal("found no gradients in Editor.tsx — the pattern this test relies on has changed")
	}
	inGo := map[string]bool{}
	for _, c := range coverPresets {
		inGo[c] = true
	}
	for _, c := range inUI {
		if !inGo[c] {
			t.Errorf("the picker offers %s but coverPresets does not — an agent cannot choose it", c)
		}
	}
	for _, c := range coverPresets {
		if !seen[c] {
			t.Errorf("coverPresets offers %s but the picker does not — agents would set a cover no human can pick", c)
		}
	}
}

// Every preset has to survive the check that guards the column, or the tool
// would be handing agents values the server then refuses.
func TestCoverPresetsAreValid(t *testing.T) {
	for _, c := range coverPresets {
		if !validCover(c) {
			t.Errorf("preset %q is refused by validCover", c)
		}
	}
}

// The gap this closes: validCover guarded the REST handler and not the MCP one,
// so an agent could set a cover pointing at a foreign host. Anything that is not
// a gradient renders as url(<value>), which means every reader of that page
// fetches from wherever the agent pointed.
func TestCoverOverMCPIsValidated(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now()); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	pid := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, 'P', '[]', 1, ?, ?, ?, ?, 'workspace')`, pid, now(), now(), ws, uid); err != nil {
		t.Fatalf("page: %v", err)
	}

	refused := []string{
		"https://tracker.example/pixel.png",
		"//tracker.example/pixel.png",
		"data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=",
		"gradient:url(https://tracker.example/x.png)",
		"/files/../../etc/passwd'); background: url('https://tracker.example/x",
	}
	for _, c := range refused {
		if _, err := s.mcpUpdatePageMeta(pid, "", "", c, "", "", nil); err == nil {
			t.Errorf("cover %q was accepted over MCP — every viewer of the page would fetch from that host", c)
		}
	}

	accepted := []string{
		"gradient:linear-gradient(120deg,#a8edea,#5b86e5)",
		"/files/abc123.jpg",
	}
	for _, c := range accepted {
		if _, err := s.mcpUpdatePageMeta(pid, "", "", c, "", "", nil); err != nil {
			t.Errorf("cover %q should be allowed: %v", c, err)
		}
	}
	var stored string
	s.db.QueryRow(`SELECT cover FROM pages WHERE id = ?`, pid).Scan(&stored)
	if stored != "/files/abc123.jpg" {
		t.Errorf("stored cover is %q, expected the last accepted one", stored)
	}
}

// create_page gaining cover, tags and description is the whole point: the
// second call to update_page is the one nobody makes. Asserted through the
// dispatcher, so the schema and the handler are exercised together.
func TestCreatePageSetsCoverTagsAndDescription(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now()); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, ws, uid); err != nil {
		t.Fatalf("member: %v", err)
	}
	u := s.userByID(uid)

	args := `{"title":"Handbook","workspace_id":"` + ws + `",` +
		`"cover":"gradient:linear-gradient(120deg,#a8edea,#5b86e5)",` +
		`"description":"How we work.","tags":["#Handbook","team handbook","HANDBOOK","internal"]}`
	out, err := s.mcpCall(u, "create_page", []byte(args), "http://localhost")
	if err != nil {
		t.Fatalf("create_page: %v", err)
	}
	id := regexp.MustCompile(`\b([0-9a-f]{32})\b`).FindString(out)
	if id == "" {
		t.Fatalf("no page id in %q", out)
	}
	var cover, desc, tags string
	if err := s.db.QueryRow(`SELECT cover, description, tags FROM pages WHERE id = ?`, id).Scan(&cover, &desc, &tags); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(cover, "gradient:") {
		t.Errorf("cover not stored: %q", cover)
	}
	if desc != "How we work." {
		t.Errorf("description not stored: %q", desc)
	}
	// Tags go through the same normalisation as everywhere else. That does NOT
	// lower-case them — the spelling somebody chose is kept, and tagSuggest.ts
	// is what stops near-duplicates in the interface. What it does do: strip a
	// leading '#', join whitespace with '-', and drop repeats case-insensitively.
	if strings.Contains(tags, "#") {
		t.Errorf("leading # not stripped: %q", tags)
	}
	if !strings.Contains(tags, "team-handbook") {
		t.Errorf("whitespace not joined with '-': %q", tags)
	}
	if strings.Contains(tags, "HANDBOOK") {
		t.Errorf("case-insensitive duplicate kept: %q", tags)
	}
	if !strings.Contains(tags, "Handbook") || !strings.Contains(tags, "internal") {
		t.Errorf("tags lost: %q", tags)
	}
}

// A bad cover must not leave a page behind that the caller thinks failed.
func TestCreatePageReportsABadCover(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now())
	s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, ws, uid)
	u := s.userByID(uid)

	args := `{"title":"X","workspace_id":"` + ws + `","cover":"https://tracker.example/p.png"}`
	out, err := s.mcpCall(u, "create_page", []byte(args), "http://localhost")
	if err == nil {
		t.Fatalf("a foreign cover URL was accepted: %s", out)
	}
	if !strings.Contains(err.Error(), "cover") {
		t.Errorf("the error should name the cover, got: %v", err)
	}
}
