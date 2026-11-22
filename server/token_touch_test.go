package server

import (
	"testing"
	"time"
)

func TestShouldTouchToken(t *testing.T) {
	ref := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	recent := ref.Add(-59 * time.Second).Format(time.RFC3339Nano)
	expired := ref.Add(-60 * time.Second).Format(time.RFC3339Nano)
	cases := []struct {
		name         string
		at, was, now string
		want         bool
	}{
		{"first use", "", "", "203.0.113.9", true},
		{"same address within window", recent, "203.0.113.9", "203.0.113.9", false},
		{"same address past window", expired, "203.0.113.9", "203.0.113.9", true},
		{"new address within window", recent, "203.0.113.9", "198.51.100.7", true},
		{"unparseable timestamp", "yesterday", "203.0.113.9", "203.0.113.9", true},
	}
	for _, c := range cases {
		if got := shouldTouchToken(c.at, c.was, c.now, ref); got != c.want {
			t.Errorf("%s: shouldTouchToken(%q, %q, %q) = %v, want %v", c.name, c.at, c.was, c.now, got, c.want)
		}
	}
}

func TestAPITokenTouchDebounceAndIPChange(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "owner@example.test")
	token := "a-key-for-the-touch-test"
	const tokID = "tok_touch"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent', ?, 'write', ?)`, tokID, uid, tokenHash(token), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}

	if rec := call(s, "GET", "/api/pages", "203.0.113.9", bearer, ""); rec.Code != 200 {
		t.Fatalf("first bearer call: %d %s", rec.Code, rec.Body.String())
	}
	firstAt, firstIP := apiTokenTouch(t, s, tokID)
	if firstIP != "203.0.113.9" || firstAt == "" {
		t.Fatalf("first touch recorded %q/%q, want address and timestamp", firstAt, firstIP)
	}

	// Same address within 60 seconds must not rewrite the row.
	time.Sleep(5 * time.Millisecond)
	if rec := call(s, "GET", "/api/pages", "203.0.113.9", bearer, ""); rec.Code != 200 {
		t.Fatalf("second bearer call: %d %s", rec.Code, rec.Body.String())
	}
	secondAt, secondIP := apiTokenTouch(t, s, tokID)
	if secondAt != firstAt || secondIP != firstIP {
		t.Fatalf("same address within 60s rewrote row: %q/%q then %q/%q", firstAt, firstIP, secondAt, secondIP)
	}

	// A different address updates immediately so token misuse stays visible.
	if rec := call(s, "GET", "/api/pages", "198.51.100.7", bearer, ""); rec.Code != 200 {
		t.Fatalf("third bearer call: %d %s", rec.Code, rec.Body.String())
	}
	thirdAt, thirdIP := apiTokenTouch(t, s, tokID)
	if thirdIP != "198.51.100.7" {
		t.Fatalf("new address did not update last_used_ip: %q", thirdIP)
	}

	// Backdate last_used_at past 60s and verify the next call from the same IP writes.
	staleAt := time.Now().UTC().Add(-65 * time.Second).Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, staleAt, tokID); err != nil {
		t.Fatalf("backdate token: %v", err)
	}
	if rec := call(s, "GET", "/api/pages", "198.51.100.7", bearer, ""); rec.Code != 200 {
		t.Fatalf("fourth bearer call: %d %s", rec.Code, rec.Body.String())
	}
	fourthAt, fourthIP := apiTokenTouch(t, s, tokID)
	if fourthIP != "198.51.100.7" || fourthAt == staleAt || fourthAt == thirdAt {
		t.Fatalf("expired window from same IP did not refresh last_used_at: %q (stale %q)", fourthAt, staleAt)
	}
}

func TestOAuthGrantTouchDebounceAndIPChange(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "owner@example.test")
	const grantID = "grant_touch"
	token := "an-oauth-access-token-for-the-test"
	if _, err := s.db.Exec(`INSERT INTO oauth_grants (id, user_id, client_id, refresh_hash, scope, created_at)
		VALUES (?, ?, 'client_touch', ?, 'write', ?)`, grantID, uid, tokenHash("refresh-touch"), now()); err != nil {
		t.Fatalf("insert grant: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO oauth_access (token_hash, grant_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash(token), grantID, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert access token: %v", err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}

	if rec := call(s, "GET", "/api/pages", "203.0.113.11", bearer, ""); rec.Code != 200 {
		t.Fatalf("first oauth call: %d %s", rec.Code, rec.Body.String())
	}
	firstAt, firstIP := oauthGrantTouch(t, s, grantID)
	if firstIP != "203.0.113.11" || firstAt == "" {
		t.Fatalf("first use recorded %q/%q, want address and timestamp", firstAt, firstIP)
	}

	time.Sleep(5 * time.Millisecond)
	if rec := call(s, "GET", "/api/pages", "203.0.113.11", bearer, ""); rec.Code != 200 {
		t.Fatalf("second oauth call: %d %s", rec.Code, rec.Body.String())
	}
	secondAt, secondIP := oauthGrantTouch(t, s, grantID)
	if secondAt != firstAt || secondIP != firstIP {
		t.Fatalf("same address within 60s rewrote grant: %q/%q then %q/%q", firstAt, firstIP, secondAt, secondIP)
	}

	if rec := call(s, "GET", "/api/pages", "198.51.100.23", bearer, ""); rec.Code != 200 {
		t.Fatalf("third oauth call: %d %s", rec.Code, rec.Body.String())
	}
	thirdAt, thirdIP := oauthGrantTouch(t, s, grantID)
	if thirdIP != "198.51.100.23" {
		t.Fatalf("new address did not update grant last_used_ip: %q", thirdIP)
	}

	staleAt := time.Now().UTC().Add(-65 * time.Second).Format(time.RFC3339Nano)
	if _, err := s.db.Exec(`UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, staleAt, grantID); err != nil {
		t.Fatalf("backdate grant: %v", err)
	}
	if rec := call(s, "GET", "/api/pages", "198.51.100.23", bearer, ""); rec.Code != 200 {
		t.Fatalf("fourth oauth call: %d %s", rec.Code, rec.Body.String())
	}
	fourthAt, fourthIP := oauthGrantTouch(t, s, grantID)
	if fourthIP != "198.51.100.23" || fourthAt == staleAt || fourthAt == thirdAt {
		t.Fatalf("expired window from same IP did not refresh grant last_used_at: %q (stale %q)", fourthAt, staleAt)
	}
}

func apiTokenTouch(t *testing.T, s *Server, id string) (at, ip string) {
	t.Helper()
	if err := s.db.QueryRow(`SELECT COALESCE(last_used_at, ''), COALESCE(last_used_ip, '') FROM api_tokens WHERE id = ?`, id).Scan(&at, &ip); err != nil {
		t.Fatalf("reading token: %v", err)
	}
	return at, ip
}

func oauthGrantTouch(t *testing.T, s *Server, id string) (at, ip string) {
	t.Helper()
	if err := s.db.QueryRow(`SELECT COALESCE(last_used_at, ''), COALESCE(last_used_ip, '') FROM oauth_grants WHERE id = ?`, id).Scan(&at, &ip); err != nil {
		t.Fatalf("reading grant: %v", err)
	}
	return at, ip
}
