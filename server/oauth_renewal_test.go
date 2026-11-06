package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// A connection as a client holds it right after a person approved it.
type oauthConnection struct {
	client, access, refresh string
	cookie                  string
}

func connect(t *testing.T, s *Server, email string) oauthConnection {
	t.Helper()
	uid, cookie := signedIn(t, s, email)
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/api/mcp/auth_callback"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "write", []string{ws})
	status, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, out)
	}
	access, _ := out["access_token"].(string)
	refresh, _ := out["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("no tokens: %v", out)
	}
	return oauthConnection{client: client, access: access, refresh: refresh, cookie: cookie}
}

func renew(s *Server, client, refresh string) (int, map[string]any) {
	return tokenCall(s, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {client},
	})
}

// renewed insists on an answer and returns the pair in it.
func renewed(t *testing.T, s *Server, client, refresh string) (access, next string) {
	t.Helper()
	status, out := renew(s, client, refresh)
	if status != http.StatusOK {
		t.Fatalf("renewal refused: %d %v", status, out)
	}
	access, _ = out["access_token"].(string)
	next, _ = out["refresh_token"].(string)
	if access == "" || next == "" {
		t.Fatalf("renewal without tokens: %v", out)
	}
	return access, next
}

// endTheOverlap does what five minutes do.
func endTheOverlap(s *Server) {
	s.renewMu.Lock()
	defer s.renewMu.Unlock()
	for hash, rn := range s.renewals {
		rn.until = time.Now().Add(-time.Second)
		s.renewals[hash] = rn
	}
}

// The defect, as it happened on production. A hosted client renews from several
// machines at once, and the loser of that race used to get invalid_grant, which
// Claude reads as a revoked connection. A connector with a key in its address
// went on working in every chat, so nobody noticed, and the scheduled runs, which
// only get connected connectors, started without it every morning.
func TestRenewalsAtTheSameMomentKeepTheConnection(t *testing.T) {
	s := testServer(t)
	c := connect(t, s, "race@example.test")

	const machines = 8
	type answer struct {
		status          int
		access, refresh string
	}
	answers := make([]answer, machines)
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			status, out := renew(s, c.client, c.refresh)
			access, _ := out["access_token"].(string)
			refresh, _ := out["refresh_token"].(string)
			answers[i] = answer{status, access, refresh}
		}(i)
	}
	wg.Wait()

	for i, a := range answers {
		if a.status != http.StatusOK {
			t.Fatalf("machine %d was refused (%d): this is what ends the connection", i, a.status)
		}
		// All the same, because which answer gets stored last is not up to us.
		if a.refresh != answers[0].refresh {
			t.Fatalf("the machines got different refresh tokens: only one can stay valid, and the client may store the other")
		}
		if s.userForAccessToken(a.access, "203.0.113.7") == nil {
			t.Errorf("the access token handed to machine %d does not work", i)
		}
	}
	// And the connection goes on from there, an hour later, on what was stored.
	endTheOverlap(s)
	renewed(t, s, c.client, answers[0].refresh)

	var grants int
	s.db.QueryRow(`SELECT count(*) FROM oauth_grants`).Scan(&grants)
	if grants != 1 {
		t.Errorf("%d connections after one race, want the one", grants)
	}
}

// A copy of the pair can be older than one renewal. It gets the newest token of
// the line, not a token that has been replaced as well.
func TestAStaleCopyFollowsTheLineToTheNewestToken(t *testing.T) {
	s := testServer(t)
	c := connect(t, s, "line@example.test")

	_, second := renewed(t, s, c.client, c.refresh)
	_, third := renewed(t, s, c.client, second)

	if _, got := renewed(t, s, c.client, c.refresh); got != third {
		t.Error("a stale copy was handed a token that has been replaced already")
	}
	if _, got := renewed(t, s, c.client, third); got == third {
		t.Error("the newest token did not rotate")
	}
}

// Rotation still means something: the overlap ends, and then the old token is
// worth nothing while the connection carries on.
func TestTheOverlapEnds(t *testing.T) {
	s := testServer(t)
	c := connect(t, s, "overlap@example.test")

	_, next := renewed(t, s, c.client, c.refresh)
	endTheOverlap(s)

	status, out := renew(s, c.client, c.refresh)
	if status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Errorf("the old token after the overlap: %d %v, want invalid_grant", status, out)
	}
	renewed(t, s, c.client, next)

	s.sweepOAuth()
	s.renewMu.Lock()
	defer s.renewMu.Unlock()
	for _, rn := range s.renewals {
		if time.Now().After(rn.until) {
			t.Error("the sweep left a token in memory after its overlap ended")
		}
	}
}

// The overlap keeps a connection alive. It must not bring back one that a person
// ended, in the minutes right after a renewal or any other time.
func TestAnEndedConnectionStaysEndedInsideTheOverlap(t *testing.T) {
	end := map[string]func(s *Server, c oauthConnection, next string){
		"the list": func(s *Server, c oauthConnection, _ string) {
			var id string
			s.db.QueryRow(`SELECT id FROM oauth_grants`).Scan(&id)
			r := httptest.NewRequest("DELETE", "/api/oauth/grants/"+id, nil)
			r.Header.Set("Cookie", c.cookie)
			s.ServeHTTP(httptest.NewRecorder(), r)
		},
		"a revocation": func(s *Server, _ oauthConnection, next string) {
			r := httptest.NewRequest("POST", "/oauth/revoke", strings.NewReader(url.Values{"token": {next}}.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			s.ServeHTTP(httptest.NewRecorder(), r)
		},
	}
	for name, fn := range end {
		t.Run(name, func(t *testing.T) {
			s := testServer(t)
			c := connect(t, s, "ended@example.test")
			access, next := renewed(t, s, c.client, c.refresh)

			fn(s, c, next)

			if status, out := renew(s, c.client, c.refresh); status == http.StatusOK {
				t.Errorf("the old token brought the connection back: %v", out)
			}
			if status, out := renew(s, c.client, next); status == http.StatusOK {
				t.Errorf("the new token outlived the connection: %v", out)
			}
			for _, tok := range []string{c.access, access} {
				if s.userForAccessToken(tok, "203.0.113.7") != nil {
					t.Error("an access token outlived the connection")
				}
			}
		})
	}
}

// The overlap answers the client the grant belongs to, with its secret, the same
// as a renewal does.
func TestTheOverlapChecksTheClient(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "client@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/api/mcp/auth_callback"

	body := `{"client_name":"Claude","redirect_uris":["` + redirect + `"],"token_endpoint_auth_method":"client_secret_post"}`
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body)))
	var reg struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)
	if reg.ClientSecret == "" {
		t.Fatalf("no client secret: %s", rec.Body.String())
	}
	code := approve(t, s, cookie, reg.ClientID, redirect, pkce("v"), "write", []string{ws})
	_, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "client_id": {reg.ClientID},
		"client_secret": {reg.ClientSecret}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	refresh, _ := out["refresh_token"].(string)
	form := func(client, secret string) url.Values {
		return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh},
			"client_id": {client}, "client_secret": {secret}}
	}
	if status, out := tokenCall(s, form(reg.ClientID, reg.ClientSecret)); status != http.StatusOK {
		t.Fatalf("renewal: %d %v", status, out)
	}

	other := oauthClientFixture(t, s, redirect)
	if status, _ := tokenCall(s, form(other, "")); status == http.StatusOK {
		t.Error("another client got an answer for this connection's old token")
	}
	if status, out := tokenCall(s, form(reg.ClientID, "wrong")); status != http.StatusUnauthorized {
		t.Errorf("the wrong secret inside the overlap: %d %v, want 401", status, out)
	}
	if status, out := tokenCall(s, form(reg.ClientID, reg.ClientSecret)); status != http.StatusOK {
		t.Errorf("the client itself inside the overlap: %d %v", status, out)
	}
}

// Another machine of the client may be holding the access token a renewal
// replaces. Taking it away gives that machine a 401, and a 401 sends it off to
// renew with a token that is already old.
func TestARenewalLeavesTheAccessTokenInUseAlone(t *testing.T) {
	s := testServer(t)
	c := connect(t, s, "access@example.test")

	renewed(t, s, c.client, c.refresh)
	if s.userForAccessToken(c.access, "203.0.113.7") == nil {
		t.Fatal("a renewal took away an access token that had not run out")
	}

	// It still runs out by itself: an hour is an hour.
	s.db.Exec(`UPDATE oauth_access SET expires_at = ? WHERE token_hash = ?`,
		time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), tokenHash(c.access))
	if s.userForAccessToken(c.access, "203.0.113.7") != nil {
		t.Error("an expired access token was accepted")
	}
}

// Nothing else records why a client dropped its connection, and the first version
// of this analysis had to guess. The line must not reach fail2ban: these requests
// come from the agent platform's address, and banning it cuts every connector.
func TestARefusedRenewalIsLoggedWhereFail2BanDoesNotRead(t *testing.T) {
	s := testServer(t)
	c := connect(t, s, "logged@example.test")

	line := captureLog(t, func() {
		if status, _ := renew(s, c.client, "not-a-token-anybody-issued"); status == http.StatusOK {
			t.Fatal("an unknown token was accepted")
		}
	})
	if !strings.Contains(line, "oauth: refused renewal from ") || !strings.Contains(line, "unknown refresh token") {
		t.Errorf("a refused renewal left no usable line: %q", line)
	}
	if strings.Contains(line, "auth: rejected") {
		t.Errorf("a refused renewal would count toward a ban: %q", line)
	}
	if strings.Contains(line, "not-a-token-anybody-issued") {
		t.Errorf("the token was written into the log: %q", line)
	}

	// An overlap is the fix working, and worth seeing, without a token in it.
	_, next := renewed(t, s, c.client, c.refresh)
	line = captureLog(t, func() { renewed(t, s, c.client, c.refresh) })
	if !strings.Contains(line, "oauth: overlapping renewal answered") {
		t.Errorf("an overlap left no line: %q", line)
	}
	if strings.Contains(line, c.refresh) || strings.Contains(line, next) {
		t.Errorf("a token was written into the log: %q", line)
	}
}
