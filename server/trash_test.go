package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The trash says who threw an entry away and when, and shows what is in it
// without restoring it. Both answers are read, not stored: who from the
// activity log, what from the page itself. These tests pin the reading.

func trashReq(t *testing.T, s *Server, method, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	s.ServeHTTP(rec, req)
	return rec
}

func trashedList(t *testing.T, s *Server, cookie string) map[string]pageMeta {
	t.Helper()
	rec := trashReq(t, s, "GET", "/api/pages", cookie)
	if rec.Code != 200 {
		t.Fatalf("GET /api/pages: %d %s", rec.Code, rec.Body.String())
	}
	var list []pageMeta
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode page list: %v", err)
	}
	out := map[string]pageMeta{}
	for _, m := range list {
		out[m.ID] = m
	}
	return out
}

func TestTrashSaysWhoAndWhen(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "trash-who@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	byHand := s.makePage(t, ws, uid, "", "Thrown away by hand", `{}`)
	under := s.makePage(t, ws, uid, byHand, "Went along with it", `{}`)
	byAgent := s.makePage(t, ws, uid, "", "Thrown away by an agent", `{}`)
	alive := s.makePage(t, ws, uid, "", "Still here", `{}`)

	if rec := trashReq(t, s, "DELETE", "/api/pages/"+byHand, cookie); rec.Code != 200 {
		t.Fatalf("trash in the browser: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := callTool(t, s, &user{ID: uid, Name: "Test"}, "set_trashed", `{"page_id":"`+byAgent+`","trashed":true}`); err != nil {
		t.Fatalf("trash over MCP: %v", err)
	}

	got := trashedList(t, s, cookie)
	if m := got[byHand]; m.TrashedAt == "" || m.TrashedBy != "Test" || m.TrashedByAgent {
		t.Errorf("thrown by hand: at=%q by=%q agent=%v — want a time, Test, not an agent", m.TrashedAt, m.TrashedBy, m.TrashedByAgent)
	}
	if m := got[byAgent]; m.TrashedAt == "" || m.TrashedBy != "Test (MCP)" || !m.TrashedByAgent {
		t.Errorf("thrown by an agent: at=%q by=%q agent=%v — want a time, Test (MCP), an agent", m.TrashedAt, m.TrashedBy, m.TrashedByAgent)
	}
	// The log names the page that was acted on. A sub-page that went along is
	// in the trash, with the time, but it is not an entry of its own — nobody
	// is named for it, and the sidebar never asks.
	if m := got[under]; m.TrashedAt == "" || m.TrashedBy != "" {
		t.Errorf("sub-page that went along: at=%q by=%q — want a time and nobody", m.TrashedAt, m.TrashedBy)
	}
	if m := got[alive]; m.TrashedAt != "" || m.TrashedBy != "" {
		t.Errorf("a live page carries trash fields: at=%q by=%q", m.TrashedAt, m.TrashedBy)
	}
}

// An agent trashed the page and took it back out; then a person put it in the
// trash again. The person is named, not the agent.
func TestTrashNamesWhoeverPutItThereLast(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "trash-last@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	pg := s.makePage(t, ws, uid, "", "Back and forth", `{}`)
	agent := &user{ID: uid, Name: "Test"}

	if _, err := callTool(t, s, agent, "set_trashed", `{"page_id":"`+pg+`","trashed":true}`); err != nil {
		t.Fatalf("agent trashes: %v", err)
	}
	if _, err := callTool(t, s, agent, "set_trashed", `{"page_id":"`+pg+`","trashed":false}`); err != nil {
		t.Fatalf("agent restores: %v", err)
	}
	if rec := trashReq(t, s, "DELETE", "/api/pages/"+pg, cookie); rec.Code != 200 {
		t.Fatalf("person trashes: %d %s", rec.Code, rec.Body.String())
	}
	if m := trashedList(t, s, cookie)[pg]; m.TrashedBy != "Test" || m.TrashedByAgent {
		t.Errorf("by=%q agent=%v — the person put it there last, not the agent", m.TrashedBy, m.TrashedByAgent)
	}
}

func TestPreviewShowsATrashedPageAndRunsNothing(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "trash-preview@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	pg := s.makePage(t, ws, uid, "", "Contract notes", `{}`)
	content := `[{"type":"paragraph","content":[{"type":"text","text":"Notice period: three months","styles":{}}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"<script>alert(1)</script>","styles":{}}]}]`
	if _, err := s.db.Exec(`UPDATE pages SET content = ? WHERE id = ?`, content, pg); err != nil {
		t.Fatal(err)
	}
	if rec := trashReq(t, s, "DELETE", "/api/pages/"+pg, cookie); rec.Code != 200 {
		t.Fatalf("trash: %d", rec.Code)
	}

	rec := trashReq(t, s, "GET", "/api/pages/"+pg+"/preview", cookie)
	if rec.Code != 200 {
		t.Fatalf("preview of a trashed page: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Contract notes", "Notice period: three months"} {
		if !strings.Contains(body, want) {
			t.Errorf("preview is missing %q", want)
		}
	}
	// No script of its own, and the page's text escaped — and even if either
	// failed, the response itself forbids scripts.
	if strings.Contains(strings.ToLower(body), "<script") {
		t.Error("preview contains a <script> element")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-scripts") {
		t.Errorf("CSP %q must sandbox the document without allow-scripts", csp)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q", ct)
	}

	// Reading rules, unchanged: somebody outside the workspace gets nothing, and
	// neither does a caller without a session.
	_, stranger := signedIn(t, s, "trash-stranger@example.test")
	if rec := trashReq(t, s, "GET", "/api/pages/"+pg+"/preview", stranger); rec.Code != 404 {
		t.Errorf("stranger: %d, want 404", rec.Code)
	}
	if rec := trashReq(t, s, "GET", "/api/pages/"+pg+"/preview", ""); rec.Code == 200 {
		t.Error("anonymous caller got the preview")
	}
}

// The case that decides how the log is read. An agent trashed X on its own an
// hour ago and restored it; today a person trashed X's parent, which took X
// along, and an agent restored only the parent — so X is an entry of its own
// now. Everything logged ON X is the agent's: an old trashing and a restore.
// Neither put X where it is, and the entry must say nobody rather than blame
// the agent. Recency would name the restore; any trashing on X would name the
// hour-old one.
func TestTrashNamesNobodyItCannotPlace(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "trash-nobody@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	parent := s.makePage(t, ws, uid, "", "Parent", `{}`)
	x := s.makePage(t, ws, uid, parent, "X", `{}`)
	agent := &user{ID: uid, Name: "Test"}

	if _, err := callTool(t, s, agent, "set_trashed", `{"page_id":"`+x+`","trashed":true}`); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(t, s, agent, "set_trashed", `{"page_id":"`+x+`","trashed":false}`); err != nil {
		t.Fatal(err)
	}
	hourAgo := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE audit_log SET created_at = ? WHERE page_id = ? AND detail LIKE 'Moved page %'`, hourAgo, x); err != nil {
		t.Fatal(err)
	}
	if rec := trashReq(t, s, "DELETE", "/api/pages/"+parent, cookie); rec.Code != 200 {
		t.Fatalf("person trashes the parent: %d", rec.Code)
	}
	if _, err := callTool(t, s, agent, "set_trashed", `{"page_id":"`+parent+`","trashed":false}`); err != nil {
		t.Fatal(err)
	}

	m := trashedList(t, s, cookie)[x]
	if !m.Trashed || m.TrashedAt == "" {
		t.Fatalf("X should still be in the trash: %+v", m)
	}
	if m.TrashedBy != "" || m.TrashedByAgent {
		t.Errorf("by=%q agent=%v — nothing on X put it there; want nobody named", m.TrashedBy, m.TrashedByAgent)
	}
}
