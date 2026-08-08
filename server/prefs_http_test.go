package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// The guards on PUT /api/me/prefs, exercised through the real router.
//
// normalizePrefs is unit-tested next door, but that only says the VALUES come
// out clean. It says nothing about who is allowed to write them, and that is
// the part with teeth: the endpoint must refuse an anonymous caller, refuse an
// API token, and write to the caller's own row and nobody else's.
//
// Those three were checked by hand once and reported as working. Checked by
// hand once is exactly the kind of thing that quietly stops being true, so they
// live here now.

func testServer(t *testing.T) *Server {
	t.Helper()
	// A tiny embedded "build" — the server reads favicon.svg at startup for the
	// MCP icon and is happy with anything.
	dist := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	s, err := New(t.TempDir(), dist)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// signedIn creates an account and returns a cookie header for it.
func signedIn(t *testing.T, s *Server, email string) (userID, cookie string) {
	t.Helper()
	body := strings.NewReader(`{"name":"Test","email":"` + email + `","password":"passwort12345"}`)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/setup", body)
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		// Not the first account: create it directly and make a session.
		id := newID()
		if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
			VALUES (?, ?, 'Test', '#2f7d4f', ?, 0, ?)`, id, email, hashPassword("passwort12345"), now()); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		tok, err := s.createSession(id)
		if err != nil {
			t.Fatalf("createSession: %v", err)
		}
		return id, sessionCookie + "=" + tok
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			var uid string
			s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&uid)
			return uid, sessionCookie + "=" + c.Value
		}
	}
	t.Fatal("setup returned no session cookie")
	return "", ""
}

func putPrefs(t *testing.T, s *Server, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/me/prefs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	s.ServeHTTP(rec, req)
	return rec
}

func TestPutPrefsRequiresSignIn(t *testing.T) {
	s := testServer(t)
	signedIn(t, s, "a@example.com")

	rec := putPrefs(t, s, `{"clock":"12"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous PUT: got %d, want 401 — settings are not public", rec.Code)
	}
}

func TestPutPrefsRefusesAPIToken(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "a@example.com")

	// A write-scoped token: the strongest one there is, and it still must not
	// reach account configuration. A token is a key to CONTENT.
	raw := newID()
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'probe', ?, 'write', ?)`, newID(), uid, tokenHash(raw), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	rec := putPrefs(t, s, `{"clock":"12"}`, map[string]string{"Authorization": "Bearer " + raw})
	if rec.Code != http.StatusForbidden {
		t.Errorf("token PUT: got %d, want 403 — an API token may not configure the account", rec.Code)
	}
	if p := s.loadPrefs(uid); p.Clock != "" {
		t.Errorf("the refused call wrote anyway: clock = %q", p.Clock)
	}

	// The same call over the session cookie is allowed — otherwise the test
	// above would pass for the wrong reason (a broken route, say).
	rec = putPrefs(t, s, `{"clock":"12"}`, map[string]string{"Cookie": cookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("session PUT: got %d, want 200", rec.Code)
	}
	if p := s.loadPrefs(uid); p.Clock != "12" {
		t.Errorf("session PUT did not store: clock = %q", p.Clock)
	}
}

func TestPutPrefsWritesOnlyTheCallersOwnRow(t *testing.T) {
	s := testServer(t)
	mine, cookie := signedIn(t, s, "a@example.com")
	theirs, _ := signedIn(t, s, "b@example.com")

	rec := putPrefs(t, s, `{"language":"de","timeZone":"Europe/Vienna"}`, map[string]string{"Cookie": cookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if p := s.loadPrefs(mine); p.TimeZone != "Europe/Vienna" {
		t.Errorf("own row not written: %+v", p)
	}
	// The account is the URL, so there is no id to tamper with — this asserts
	// that stays true if somebody later adds one.
	if p := s.loadPrefs(theirs); p != (userPrefs{}) {
		t.Errorf("somebody else's row was touched: %+v", p)
	}
}

func TestPutPrefsAnswersWithTheCleanedSet(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "a@example.com")

	// One unusable field among four good ones. The bad one has to come back as
	// automatic WITHOUT taking the others with it, and the answer has to say so
	// — the dialog shows what was stored, not what was asked for.
	rec := putPrefs(t, s,
		`{"language":"de","region":"de-AT","timeZone":"Mars/Olympus Mons","clock":"24","weekStart":"mon"}`,
		map[string]string{"Cookie": cookie})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	var got userPrefs
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := userPrefs{Language: "de", Region: "de-AT", TimeZone: "", Clock: "24", WeekStart: "mon"}
	if got != want {
		t.Errorf("answer\n got  %+v\n want %+v", got, want)
	}
	if stored := s.loadPrefs(uid); stored != want {
		t.Errorf("stored\n got  %+v\n want %+v", stored, want)
	}
}

// /api/me carries the preferences, and must not carry anybody else's.
func TestMeCarriesOwnPrefsOnly(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "a@example.com")
	other, _ := signedIn(t, s, "b@example.com")
	if err := s.savePrefs(other, userPrefs{TimeZone: "Pacific/Auckland"}); err != nil {
		t.Fatalf("savePrefs: %v", err)
	}
	putPrefs(t, s, `{"timeZone":"Europe/Vienna"}`, map[string]string{"Cookie": cookie})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.Header.Set("Cookie", cookie)
	s.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Europe/Vienna") {
		t.Errorf("/api/me does not carry the caller's own zone:\n%s", body)
	}
	if strings.Contains(body, "Pacific/Auckland") {
		t.Errorf("/api/me leaked somebody else's zone:\n%s", body)
	}
}
