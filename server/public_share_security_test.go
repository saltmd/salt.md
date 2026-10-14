package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Public collection shares: what an anonymous reader may and may not reach.
// Every test below maps to a review finding: private rows, filtered-out rows,
// unshared relation titles, hidden columns, password guessing, fail-open
// views and cache headers.

func TestPublicSharesScopeMatrix(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "sharer2@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	other, _ := signedIn(t, s, "other2@example.test")
	db := s.makeCollection(t, ws, uid, "Shared",
		`[{"id":"status","name":"Status","type":"text"},{"id":"secret","name":"Secret","type":"text"},{"id":"rel","name":"Rel","type":"relation"}]`)
	// Fix the relation target now that the collection exists.
	if _, err := s.db.Exec(`UPDATE collections SET schema = ? WHERE page_id = ?`,
		`[{"id":"status","name":"Status","type":"text"},{"id":"secret","name":"Secret","type":"text"},{"id":"rel","name":"Rel","type":"relation","relationCollection":"`+db+`"}]`, db); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE collections SET views = ? WHERE page_id = ?`,
		`[{"id":"v1","name":"Done","type":"table","filters":[{"property":"status","op":"is","value":"done"}],"hidden":["secret"]}]`, db); err != nil {
		t.Fatal(err)
	}
	done := s.makeRow(t, ws, uid, db, "Done row", `{"status":"done","secret":"shh"}`)
	todo := s.makeRow(t, ws, uid, db, "Todo row", `{"status":"todo"}`)
	priv := s.makeRow(t, ws, other, db, "Private row", `{"status":"done"}`)
	if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, priv); err != nil {
		t.Fatal(err)
	}
	outside := s.makePage(t, ws, uid, "", "Outside", "{}")

	call := func(method, path, body, pw string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if ck := cookie; method != "GET" || !strings.HasPrefix(path, "/api/public/") {
			r.Header.Set("Cookie", ck)
		}
		if pw != "" {
			r.Header.Set("X-Share-Password", pw)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	rec := call("POST", "/api/pages/"+db+"/share", `{"allowedViews":["v1"],"allowDetail":true}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mint share: %d %s", rec.Code, rec.Body.String())
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &minted); err != nil || minted.Token == "" {
		t.Fatalf("mint gave no token: %s", rec.Body.String())
	}
	pub := "/api/public/c/" + minted.Token

	get := func(path string) (int, map[string]any) {
		t.Helper()
		rec := call("GET", path, "", "")
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// The list: only the done row. No private row, no filtered-out row.
	code, body := get(pub + "/rows?viewId=v1")
	if code != http.StatusOK {
		t.Fatalf("rows: %d", code)
	}
	rows, _ := body["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Done row" {
		t.Fatalf("rows leak: %+v", body["rows"])
	}
	// Hidden values are pruned server-side, not just hidden in the browser.
	got := rows[0].(map[string]any)["props"].(map[string]any)
	if _, ok := got["secret"]; ok {
		t.Errorf("hidden prop leaks in row props: %+v", got)
	}
	// The schema no longer carries a column hidden in every shared view.
	code, body = get(pub)
	if code != http.StatusOK {
		t.Fatalf("collection: %d", code)
	}
	for _, p := range body["schema"].([]any) {
		if p.(map[string]any)["id"] == "secret" {
			t.Error("fully-hidden column leaks in the schema")
		}
	}

	// Detail: the done row opens, pruned…
	code, body = get(pub + "/row/" + done + "?viewId=v1")
	if code != http.StatusOK {
		t.Fatalf("detail: %d", code)
	}
	if _, ok := body["props"].(map[string]any)["secret"]; ok {
		t.Error("hidden prop leaks in row detail")
	}
	// …the filtered-out row does not…
	if code, _ := get(pub + "/row/" + todo + "?viewId=v1"); code != http.StatusNotFound {
		t.Errorf("filtered-out row detail = %d, want 404", code)
	}
	// …and neither does the private one.
	if code, _ := get(pub + "/row/" + priv + "?viewId=v1"); code != http.StatusNotFound {
		t.Errorf("private row detail = %d, want 404", code)
	}

	// Relation titles: same-collection rows resolve, nothing else does.
	relRow := s.makeRow(t, ws, uid, db, "Linked", `{"status":"done","rel":["`+priv+`","`+outside+`","`+done+`"]}`)
	_ = relRow
	code, body = get(pub + "/rows?viewId=v1")
	if code != http.StatusOK {
		t.Fatalf("rows: %d", code)
	}
	related, _ := body["related"].(map[string]any)
	if _, ok := related[done]; !ok {
		t.Error("same-collection row title missing")
	}
	if _, ok := related[priv]; ok {
		t.Error("private row title leaks through a relation")
	}
	if _, ok := related[outside]; ok {
		t.Error("outside-page title leaks through a relation")
	}

	// Cache headers on the JSON.
	rec2 := call("GET", pub+"/rows?viewId=v1", "", "")
	if cc := rec2.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Errorf("Cache-Control = %q, want private, no-store", cc)
	}
}

func TestPublicViewsFailClosed(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "failclosed@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Shared", `[{"id":"status","name":"Status","type":"text"}]`)
	if _, err := s.db.Exec(`UPDATE collections SET views = ? WHERE page_id = ?`,
		`[{"id":"v1","name":"One","type":"table"},{"id":"v2","name":"Two","type":"table"}]`, db); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}
	// An empty view list snapshots the first view instead of publishing all,
	// including later ones.
	rec := call("POST", "/api/pages/"+db+"/share", `{"allowedViews":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint share: %d", rec.Code)
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &minted); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/public/c/"+minted.Token, nil)
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, r)
	var body struct {
		Views []map[string]any `json:"views"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Views) != 1 || body.Views[0]["id"] != "v1" {
		t.Fatalf("empty view list publishes %+v, want only the first view", body.Views)
	}
}

func TestPublicPasswordRateLimited(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ratelimit@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Shared", `[{"id":"status","name":"Status","type":"text"}]`)
	r := httptest.NewRequest("POST", "/api/pages/"+db+"/share", strings.NewReader(`{"password":"right"}`))
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint share: %d", rec.Code)
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &minted); err != nil {
		t.Fatal(err)
	}
	limited := false
	for i := 0; i < 30; i++ {
		q := httptest.NewRequest("GET", "/api/public/c/"+minted.Token, nil)
		q.Header.Set("X-Share-Password", "wrong")
		q2 := httptest.NewRecorder()
		s.ServeHTTP(q2, q)
		if q2.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Error("30 wrong passwords never rate-limited: argon2 stays exposed")
	}
}
