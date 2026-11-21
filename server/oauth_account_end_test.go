package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A changed password and a deactivated account end the account's sessions
// and keys at once. A sign-in (the OAuth connection an agent holds) is the
// same kind of credential and went on working after both: the access token
// answered and the renewal handed out a new one. Somebody who had learned the
// password, connected an agent and then been locked out by a new password
// kept that agent's access, and a deactivated account's connections came
// back to life the day it was switched on again.
func TestPasswordChangeAndDeactivationEndSignIns(t *testing.T) {
	bearerWorks := func(s *Server, access string) bool {
		rec := httptest.NewRecorder()
		// Not /api/me, which answers a visitor who is not signed in as well.
		req := httptest.NewRequest("GET", "/api/workspaces", nil)
		req.Header.Set("Authorization", "Bearer "+access)
		s.ServeHTTP(rec, req)
		return rec.Code == http.StatusOK
	}
	call := func(s *Server, method, path, cookie, body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		s.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("password change", func(t *testing.T) {
		s := testServer(t)
		c := connect(t, s, "owner@example.test")
		var uid string
		s.db.QueryRow(`SELECT id FROM users WHERE email = ?`, "owner@example.test").Scan(&uid)
		if !bearerWorks(s, c.access) {
			t.Fatal("the sign-in did not work to begin with")
		}
		s.db.Exec(`INSERT INTO app_settings (key, value) VALUES (?, 'old-calendar-link')`, "ics_token_"+uid)
		if code := call(s, "PATCH", "/api/users/"+uid, c.cookie,
			`{"password":"a-new-password-42","currentPassword":"passwort12345"}`); code != http.StatusOK {
			t.Fatalf("password change: %d", code)
		}
		if bearerWorks(s, c.access) {
			t.Error("the access token still works after the password changed")
		}
		if status, out := renew(s, c.client, c.refresh); status == http.StatusOK {
			t.Errorf("the renewal still works after the password changed: %v", out)
		}
		var grants int
		s.db.QueryRow(`SELECT COUNT(*) FROM oauth_grants WHERE user_id = ?`, uid).Scan(&grants)
		if grants != 0 {
			t.Errorf("%d sign-in(s) left after the password changed", grants)
		}
		// The calendar link is a key in an address, and it ends with the others.
		if tok := s.setting("ics_token_"+uid, ""); tok == "old-calendar-link" {
			t.Error("the calendar link survived the password change")
		}
	})

	t.Run("deactivation", func(t *testing.T) {
		s := testServer(t)
		ownerID, ownerCookie := signedIn(t, s, "owner@example.test")
		ws := s.firstWorkspaceOf(t, ownerID)
		memberID, memberCookie := signedIn(t, s, "member@example.test")
		if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, memberID); err != nil {
			t.Fatal(err)
		}
		redirect := "https://claude.ai/api/mcp/auth_callback"
		client := oauthClientFixture(t, s, redirect)
		code := approve(t, s, memberCookie, client, redirect, pkce("v"), "write", []string{ws})
		status, out := tokenCall(s, url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
		})
		access, _ := out["access_token"].(string)
		refresh, _ := out["refresh_token"].(string)
		if status != http.StatusOK || access == "" || !bearerWorks(s, access) {
			t.Fatalf("the member's sign-in did not work to begin with: %d %v", status, out)
		}
		if code := call(s, "PUT", "/api/users/"+memberID+"/disabled", ownerCookie, `{"disabled":true}`); code != http.StatusOK {
			t.Fatalf("deactivate: %d", code)
		}
		if code := call(s, "PUT", "/api/users/"+memberID+"/disabled", ownerCookie, `{"disabled":false}`); code != http.StatusOK {
			t.Fatalf("reactivate: %d", code)
		}
		if bearerWorks(s, access) {
			t.Error("the access token works again after the account was switched off and on")
		}
		if status, out := renew(s, client, refresh); status == http.StatusOK {
			t.Errorf("the renewal works again after the account was switched off and on: %v", out)
		}
	})
}
