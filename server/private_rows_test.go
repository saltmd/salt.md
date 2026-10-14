package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A row set to private in a shared collection was hidden from the row list
// and nowhere else: the Markdown export and get_page over MCP (both through
// collectionMarkdown), the calendar feed and a publicly shared collection all
// read every row. Each of those four doors gets its own test here, because each
// one was a separate place that forgot.

type privateRowsFixture struct {
	s            *Server
	admin        string // the first account, an admin in every workspace
	member       string
	memberCookie string
	col          string
}

func newPrivateRowsFixture(t *testing.T) privateRowsFixture {
	t.Helper()
	s := testServer(t)
	admin, _ := signedIn(t, s, "admin@example.test")
	member, memberCookie := signedIn(t, s, "member@example.test")
	ws := s.firstWorkspaceOf(t, admin)
	// A PLAIN member: an admin sees private rows by design, so a test run as
	// one would pass whatever the code did.
	s.addMember(t, ws, member, "member")

	col := s.makeCollection(t, ws, member, "Salaries",
		`[{"id":"amount","name":"Amount","type":"number"},{"id":"due","name":"Due","type":"date"}]`)
	s.makeRow(t, ws, member, col, "Shared row", `{"amount":10,"due":"2026-10-20"}`)
	mine := s.makeRow(t, ws, member, col, "Member secret", `{"amount":20,"due":"2026-10-21"}`)
	theirs := s.makeRow(t, ws, admin, col, "Admin secret", `{"amount":95000,"due":"2026-10-22"}`)
	for _, id := range []string{mine, theirs} {
		if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, id); err != nil {
			t.Fatalf("make private: %v", err)
		}
	}
	return privateRowsFixture{s: s, admin: admin, member: member, memberCookie: memberCookie, col: col}
}

func assertRows(t *testing.T, where, body string, see, hide []string) {
	t.Helper()
	for _, title := range see {
		if !strings.Contains(body, title) {
			t.Errorf("%s: %q is missing; hiding too much is a bug as well", where, title)
		}
	}
	for _, title := range hide {
		if strings.Contains(body, title) {
			t.Errorf("%s: %q is somebody else's private row and must not appear", where, title)
		}
	}
}

func TestPrivateRowsStayOutOfTheMarkdownTable(t *testing.T) {
	f := newPrivateRowsFixture(t)
	p, err := f.s.getPage(f.col)
	if err != nil {
		t.Fatalf("get collection: %v", err)
	}
	for _, c := range []struct {
		name, userID string
		see, hide    []string
	}{
		{"member", f.member, []string{"Shared row", "Member secret"}, []string{"Admin secret"}},
		{"anonymous", "", []string{"Shared row"}, []string{"Member secret", "Admin secret"}},
		{"workspace admin", f.admin, []string{"Shared row", "Member secret", "Admin secret"}, nil},
	} {
		md, err := f.s.collectionMarkdown(p, c.userID)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		assertRows(t, c.name, md, c.see, c.hide)
	}
}

func TestPrivateRowsStayOutOfTheExport(t *testing.T) {
	f := newPrivateRowsFixture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/export/"+f.col, nil)
	req.Header.Set("Cookie", f.memberCookie)
	f.s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("export: %d %s", rec.Code, rec.Body.String())
	}
	assertRows(t, "export", rec.Body.String(), []string{"Shared row", "Member secret"}, []string{"Admin secret"})
}

func TestPrivateRowsStayOutOfAPublicCollection(t *testing.T) {
	f := newPrivateRowsFixture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/pages/"+f.col+"/share", strings.NewReader(`{}`))
	req.Header.Set("Cookie", f.memberCookie)
	req.Header.Set("Content-Type", "application/json")
	f.s.ServeHTTP(rec, req)
	var share struct {
		Token string `json:"token"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &share) != nil || share.Token == "" {
		t.Fatalf("share: %d %s", rec.Code, rec.Body.String())
	}

	// Nobody signed in: not even the member's own private row belongs on a
	// page anybody with the link can read.
	rec = httptest.NewRecorder()
	f.s.ServeHTTP(rec, httptest.NewRequest("GET", "/public/"+share.Token, nil))
	if rec.Code != 200 {
		t.Fatalf("public view: %d", rec.Code)
	}
	assertRows(t, "public page", rec.Body.String(), []string{"Shared row"}, []string{"Member secret", "Admin secret"})
}

func TestPrivateRowsStayOutOfTheCalendarFeed(t *testing.T) {
	f := newPrivateRowsFixture(t)
	rec := httptest.NewRecorder()
	f.s.ServeHTTP(rec, httptest.NewRequest("GET", "/ics/"+f.s.icsToken(f.member)+".ics", nil))
	if rec.Code != 200 {
		t.Fatalf("feed: %d %s", rec.Code, rec.Body.String())
	}
	assertRows(t, "calendar feed", rec.Body.String(), []string{"Shared row", "Member secret"}, []string{"Admin secret"})
}

func TestPrivateRowsStayOutOfGetPageOverMCP(t *testing.T) {
	f := newPrivateRowsFixture(t)
	secret := "member-agent-token"
	if _, err := f.s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent', ?, 'read', ?)`, newID(), f.member, tokenHash(secret), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_page","arguments":{"page_id":"` + f.col + `"}}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(call))
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	f.s.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Shared row") {
		t.Fatalf("get_page: %d %s", rec.Code, rec.Body.String())
	}
	assertRows(t, "get_page", rec.Body.String(), []string{"Shared row", "Member secret"}, []string{"Admin secret"})
}

// Filter groups OR their conditions together. The private-row condition must
// stay outside that OR: another member's private row that matches a group is
// still nobody else's business.
func TestPrivateRowsStayOutOfFilterGroups(t *testing.T) {
	f := newPrivateRowsFixture(t)
	list, _, err := f.s.collectionRowsQuery(&user{ID: f.member}, f.col, nil, [][]rowFilter{
		{{Prop: "amount", Op: "gt", Value: "50000"}},
		{{Prop: "amount", Op: "lt", Value: "15"}},
	}, "", 100, 0, time.Time{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var titles []string
	for _, row := range list {
		titles = append(titles, row["title"].(string))
	}
	assertRows(t, "filter groups", strings.Join(titles, ", "), []string{"Shared row"}, []string{"Admin secret"})
}
