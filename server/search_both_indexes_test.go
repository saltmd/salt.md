package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The passages answer a search first, and the whole-page index knows what no
// passage holds: the text of an attached PDF, a row's property values, two words
// in different passages, a word a hard cut split in two. It used to be asked only
// when the passages found nothing at all, so a match on any other page hid all of
// these (#127).

type searchCase struct {
	t      *testing.T
	s      *Server
	uid    string
	ws     string
	cookie string
}

func newSearchCase(t *testing.T) *searchCase {
	t.Helper()
	s := testServer(t)
	uid, cookie := signedIn(t, s, "both-indexes@example.test")
	return &searchCase{t: t, s: s, uid: uid, ws: s.firstWorkspaceOf(t, uid), cookie: cookie}
}

func paragraph(text string) map[string]any {
	return map[string]any{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": text}}}
}

// page stores a page owned by the signed-in account and indexes it.
func (c *searchCase) page(title string, blocks ...map[string]any) string {
	c.t.Helper()
	raw, err := json.Marshal(blocks)
	if err != nil {
		c.t.Fatal(err)
	}
	id := newID()
	if _, err := c.s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility, type)
		VALUES (?, ?, ?, 0, ?, ?, ?, ?, 'workspace', 'doc')`,
		id, title, string(raw), now(), now(), c.ws, c.uid); err != nil {
		c.t.Fatalf("insert: %v", err)
	}
	if err := c.s.reindexPage(id); err != nil {
		c.t.Fatalf("index: %v", err)
	}
	return id
}

// hits searches the way the browser does.
func (c *searchCase) hits(q string) []searchResult {
	c.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(q), nil)
	req.Header.Set("Cookie", c.cookie)
	c.s.ServeHTTP(rec, req)
	var out []searchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		c.t.Fatalf("%q: %d %s", q, rec.Code, rec.Body.String())
	}
	return out
}

// wantFound checks both routes, the browser's and the agent's.
func (c *searchCase) wantFound(q string, ids ...string) {
	c.t.Helper()
	browser := map[string]int{}
	for _, h := range c.hits(q) {
		browser[h.ID]++
	}
	agent, err := c.s.mcpSearch(&user{ID: c.uid}, q)
	if err != nil {
		c.t.Fatalf("mcp %q: %v", q, err)
	}
	for _, id := range ids {
		if browser[id] != 1 {
			c.t.Errorf("%q: the browser's search lists page %s %d times, want once", q, id, browser[id])
		}
		if n := strings.Count(agent, id); n != 1 {
			c.t.Errorf("%q: the agent's search lists page %s %d times, want once", q, id, n)
		}
	}
}

// The case from #127. A hard cut falls between 数 and 据, so neither passage of
// the long page holds the word.
func TestSearchFindsAWordCutBetweenTwoPassages(t *testing.T) {
	c := newSearchCase(t)
	long := c.page("Long paragraph", paragraph("a"+strings.Repeat("文", 598)+"数据库迁移方案"+strings.Repeat("尾", 620)))
	short := c.page("Ordinary paragraph", paragraph("今天讨论数据库迁移方案。"))

	var inPassage int
	if err := c.s.db.QueryRow(`SELECT count(*) FROM chunks_fts JOIN page_chunks p ON p.id = chunks_fts.chunk_id
		WHERE chunks_fts MATCH ? AND p.page_id = ?`, ftsMatch("数据库"), long).Scan(&inPassage); err != nil {
		t.Fatal(err)
	}
	if inPassage != 0 {
		t.Fatalf("a passage of the long page holds the word, so this no longer tests the cut")
	}

	c.wantFound("数据库", long, short)

	hits := c.hits("数据库")
	if len(hits) != 2 || hits[0].ID != short {
		t.Fatalf("got %+v, want the passage hit first and the long page after it", hits)
	}
	if !strings.Contains(hits[1].Snippet, "\x01") || strings.Contains(hits[1].Snippet, cjkGap) {
		t.Errorf("the long page's excerpt does not show the match cleanly: %q", hits[1].Snippet)
	}
}

// The same hole in every other language, and the larger part of it.
func TestSearchFindsWhatOnlyTheWholePageIndexHolds(t *testing.T) {
	filler := strings.Repeat("plain words about something else entirely ", 20)
	cases := []struct {
		name  string
		query string
		make  func(c *searchCase) string
	}{
		{"text of an attached PDF", "invoice", func(c *searchCase) string {
			id := c.page("Carrier", paragraph("See the attachment."))
			c.s.indexFileText("scan.pdf", id, "The invoice number is on the second sheet.")
			return id
		}},
		{"a property value of a row", "invoice", func(c *searchCase) string {
			id := c.page("Row", paragraph("A row with a value."))
			if _, err := c.s.db.Exec(`UPDATE pages SET props = ? WHERE id = ?`, `{"kind":"invoice"}`, id); err != nil {
				c.t.Fatal(err)
			}
			if err := c.s.reindexPage(id); err != nil {
				c.t.Fatal(err)
			}
			return id
		}},
		{"two words in different passages", "invoice reminder", func(c *searchCase) string {
			return c.page("Two sections", paragraph("invoice "+filler), paragraph("reminder "+filler))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newSearchCase(t)
			id := tc.make(c)
			c.wantFound(tc.query, id)
			other := c.page("Another page", paragraph("The invoice and the reminder went out."))
			c.wantFound(tc.query, id, other)
			if hits := c.hits(tc.query); len(hits) != 2 || hits[0].ID != other {
				t.Errorf("got %+v, want the passage hit first", hits)
			}
		})
	}
}

// A page both indexes know is one result, and it keeps what the passage knows.
func TestSearchListsAPageFoundByBothIndexesOnce(t *testing.T) {
	c := newSearchCase(t)
	id := c.page("Contract",
		map[string]any{"type": "heading", "props": map[string]any{"level": 1},
			"content": []map[string]any{{"type": "text", "text": "Termination"}}},
		paragraph("The invoice goes out in March."))
	c.s.indexFileText("scan.pdf", id, "Another invoice, in the attachment.")

	hits := c.hits("invoice")
	if len(hits) != 1 {
		t.Fatalf("got %d results, want the page once: %+v", len(hits), hits)
	}
	if hits[0].Heading != "Termination" {
		t.Errorf("heading %q, want the passage's", hits[0].Heading)
	}
}

// Filling the list reads the whole-page index, so its results pass the same two
// stages: the workspace, and then canRead for somebody else's private page.
func TestSearchKeepsWholePageHitsBehindThePermissions(t *testing.T) {
	c := newSearchCase(t)
	visible := c.page("Shared", paragraph("The invoice is shared."))

	private := c.page("Carrier", paragraph("See the attachment."))
	if _, err := c.s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, private); err != nil {
		t.Fatal(err)
	}
	c.s.indexFileText("private.pdf", private, "A private invoice.")

	closed := newID()
	if _, err := c.s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id) VALUES (?, 'Closed', ?, ?)`, closed, now(), c.uid); err != nil {
		t.Fatal(err)
	}
	if _, err := c.s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, closed, c.uid); err != nil {
		t.Fatal(err)
	}
	elsewhere := c.s.makePage(t, closed, c.uid, "", "Carrier", `{}`)
	c.s.indexFileText("elsewhere.pdf", elsewhere, "An invoice in a closed workspace.")

	// The owner finds all three, so the index does hold them.
	c.wantFound("invoice", visible, private, elsewhere)

	// A plain member: workspace admins read private pages.
	member, cookie := signedIn(t, c.s, "member@example.test")
	if _, err := c.s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, c.ws, member); err != nil {
		t.Fatal(err)
	}
	m := &searchCase{t: t, s: c.s, uid: member, ws: c.ws, cookie: cookie}
	if hits := m.hits("invoice"); len(hits) != 1 || hits[0].ID != visible {
		t.Errorf("got %+v, want only the shared page", hits)
	}
	agent, err := c.s.mcpSearch(&user{ID: member}, "invoice")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(agent, private) || strings.Contains(agent, elsewhere) {
		t.Errorf("an agent saw a page its person may not read: %s", agent)
	}
}

// The list stays at twenty, passage hits first, and whole-page hits fill what is
// left of it.
func TestSearchFillsTheListUpToTwenty(t *testing.T) {
	c := newSearchCase(t)
	passages := map[string]bool{}
	for i := 0; i < 15; i++ {
		passages[c.page(fmt.Sprintf("Body %d", i), paragraph("The invoice is here."))] = true
	}
	for i := 0; i < 10; i++ {
		id := c.page(fmt.Sprintf("Carrier %d", i), paragraph("See the attachment."))
		c.s.indexFileText(fmt.Sprintf("scan-%d.pdf", i), id, "The invoice is in here.")
	}
	hits := c.hits("invoice")
	if len(hits) != 20 {
		t.Fatalf("got %d results, want twenty", len(hits))
	}
	seen := map[string]bool{}
	for i, h := range hits {
		if seen[h.ID] {
			t.Errorf("page %s is listed twice", h.ID)
		}
		seen[h.ID] = true
		if passages[h.ID] != (i < 15) {
			t.Errorf("result %d is in the wrong place: %+v", i, h)
		}
	}
}
