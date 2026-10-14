package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A share password guards the link without becoming it: setting, changing or
// removing it keeps the same token, and the old token-bound format from before
// keeps verifying until its password is changed.
func TestSharePasswordKeepsTheLink(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "pwshare@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Board", `[{"id":"status","name":"Status","type":"text"}]`)
	s.makeRow(t, ws, uid, db, "A row", `{}`)

	call := func(method, path, body string, pw string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Cookie", cookie)
		if pw != "" {
			r.Header.Set("X-Share-Password", pw)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}

	rec := call("POST", "/api/pages/"+db+"/share", `{"password":"first"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("mint share: %d %s", rec.Code, rec.Body.String())
	}
	var minted struct {
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &minted); err != nil || minted.Token == "" {
		t.Fatalf("mint share gave no token: %s", rec.Body.String())
	}

	pub := "/api/public/c/" + minted.Token
	if rec := call("GET", pub, "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("no password: got %d, want 403", rec.Code)
	}
	if rec := call("GET", pub, "", "wrong"); rec.Code != http.StatusForbidden {
		t.Errorf("wrong password: got %d, want 403", rec.Code)
	}
	if rec := call("GET", pub, "", "first"); rec.Code != http.StatusOK {
		t.Errorf("right password: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// Change the password in place: the token must survive.
	rec = call("PATCH", "/api/pages/"+db+"/share", `{"password":"second"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch password: %d %s", rec.Code, rec.Body.String())
	}
	var patched struct {
		HasPassword bool `json:"hasPassword"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || !patched.HasPassword {
		t.Fatalf("patch password did not report hasPassword: %s", rec.Body.String())
	}
	if rec := call("GET", pub, "", "first"); rec.Code != http.StatusForbidden {
		t.Errorf("old password after change: got %d, want 403", rec.Code)
	}
	if rec := call("GET", pub, "", "second"); rec.Code != http.StatusOK {
		t.Errorf("new password, same token: got %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	// An empty Apply keeps the password: it is never a remover.
	rec = call("PATCH", "/api/pages/"+db+"/share", `{"password":""}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty apply: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", pub, "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("open link after empty apply: got %d, want 403", rec.Code)
	}

	// Remove it explicitly: the same link opens freely.
	rec = call("PATCH", "/api/pages/"+db+"/share", `{"removePassword":true}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove password: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil || patched.HasPassword {
		t.Fatalf("remove password still reports hasPassword: %s", rec.Body.String())
	}
	if rec := call("GET", pub, "", ""); rec.Code != http.StatusOK {
		t.Errorf("open link after removal: got %d, want 200", rec.Code)
	}
}

func TestLegacyTokenBoundSharePasswordStillVerifies(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "legacypw@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Board", `[{"id":"status","name":"Status","type":"text"}]`)

	// A link minted before passwords were salted: sha256(token:password).
	const token = "abcdef0123456789abcdef0123456789abcd"
	if _, err := s.db.Exec(`INSERT INTO share_links (token_hash, page_id, created_at, password_hash) VALUES (?, ?, ?, ?)`,
		tokenHash(token), db, now(), tokenHash(token+":"+"oldsecret")); err != nil {
		t.Fatal(err)
	}
	get := func(pw string) int {
		r := httptest.NewRequest("GET", "/api/public/c/"+token, nil)
		r.Header.Set("Cookie", cookie)
		if pw != "" {
			r.Header.Set("X-Share-Password", pw)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec.Code
	}
	if get("oldsecret") != http.StatusOK {
		t.Error("legacy token-bound password no longer verifies")
	}
	if get("nope") != http.StatusForbidden {
		t.Error("legacy link accepted a wrong password")
	}
}

// A passworded document behaves like a collection link: ?pw= opens it
// directly, a wrong one renders the gate with the error, and no password
// renders a clean gate.
func TestDocSharePasswordQuery(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "docpwq@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	doc := s.makePage(t, ws, uid, "", "Secret doc", "{}")
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}
	rec := call("POST", "/api/pages/"+doc+"/share", `{"password":"docsecret"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("mint share: %d %s", rec.Code, rec.Body.String())
	}
	var minted struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &minted); err != nil {
		t.Fatal(err)
	}
	pub := func(q string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/public/"+minted.Token+q, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		return rec
	}
	if rec := pub(""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "pwform") {
		t.Errorf("no password: got %d, want clean gate", rec.Code)
	}
	if rec := pub("?pw=docsecret"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Secret doc") {
		t.Errorf("right ?pw=: got %d, want the document", rec.Code)
	}
	if rec := pub("?pw=nope"); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Wrong password") {
		t.Errorf("wrong ?pw=: got %d, want the gate with the error", rec.Code)
	}
}
