package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The five rules from the top of oauth_provider.go, each with a test that fails
// without it. Built wrong, an authorization server is WORSE than none — it
// looks like security. So these are not "does the happy path work" tests; each
// one is an attack that has to bounce.

func oauthClientFixture(t *testing.T, s *Server, redirect string) string {
	t.Helper()
	body := `{"client_name":"Claude","redirect_uris":["` + redirect + `"]}`
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ClientID == "" {
		t.Fatal("no client_id")
	}
	return out.ClientID
}

func pkce(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// approve walks the part a human does in the browser and returns the code.
func approve(t *testing.T, s *Server, cookie, clientID, redirect, challenge, scope string, ws []string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"clientId": clientID, "redirectUri": redirect, "codeChallenge": challenge,
		"codeChallengeMethod": "S256", "scope": scope, "workspaces": ws,
	})
	r := httptest.NewRequest("POST", "/api/oauth/approve", strings.NewReader(string(payload)))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Code string `json:"code"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Code
}

func tokenCall(s *Server, form url.Values) (int, map[string]any) {
	r := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The whole point, end to end: a human consents in a browser, the client
// exchanges a code, and the resulting token reaches the API — with nothing ever
// travelling in a URL.
func TestSigningInGivesAWorkingShortLivedToken(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "oauth@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/api/mcp/auth_callback"
	client := oauthClientFixture(t, s, redirect)

	verifier := "a-verifier-long-enough-to-be-real-0123456789"
	code := approve(t, s, cookie, client, redirect, pkce(verifier), "write", []string{ws})

	status, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, out)
	}
	access, _ := out["access_token"].(string)
	refresh, _ := out["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("missing tokens: %v", out)
	}
	if out["expires_in"] == nil {
		t.Error("no expiry — the token would be as permanent as the thing it replaces")
	}

	// It works as a credential.
	r := httptest.NewRequest("GET", "/api/pages", nil)
	r.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("the access token was refused by the API: %d", rec.Code)
	}

	// And the connection SURVIVES: refreshing keeps it alive without anybody
	// signing in again. A design that made you re-authorize hourly on a phone
	// would simply not be used.
	status, out = tokenCall(s, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {client},
	})
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %v", status, out)
	}
	if out["access_token"] == access {
		t.Error("refreshing handed back the same access token — nothing rotated")
	}
	// The old refresh token is dead: rotation, or the long-lived secret is back.
	if status, _ := tokenCall(s, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {client},
	}); status == http.StatusOK {
		t.Error("the previous refresh token still works — it never rotates out")
	}
}

// Rule 1: PKCE, S256, mandatory.
func TestPKCEIsRequiredAndMustBeS256(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "pkce@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)

	// The authorize endpoint refuses to even start without a challenge.
	for _, q := range []string{
		"response_type=code&client_id=" + client + "&redirect_uri=" + url.QueryEscape(redirect),
		"response_type=code&client_id=" + client + "&redirect_uri=" + url.QueryEscape(redirect) + "&code_challenge=x&code_challenge_method=plain",
	} {
		r := httptest.NewRequest("GET", "/oauth/authorize?"+q, nil)
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "error=invalid_request") {
			t.Errorf("authorize accepted %q — it went to %q", q, loc)
		}
	}
	// And a wrong verifier never becomes a token.
	code := approve(t, s, cookie, client, redirect, pkce("the-real-verifier"), "read", []string{ws})
	if status, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"a-different-verifier"},
	}); status == http.StatusOK {
		t.Errorf("a wrong code_verifier was accepted: %v", out)
	}
}

// Rule 2: exact redirect_uri. A prefix match is the classic way to have codes
// delivered to somebody else's server.
func TestRedirectURIIsComparedExactly(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "redir@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)

	for _, evil := range []string{
		"https://claude.ai/cb/../evil",
		"https://claude.ai/cb.evil.example",
		"https://claude.ai/cb?x=1",
		"https://evil.example/cb",
		"https://claude.ai/cb/",
	} {
		r := httptest.NewRequest("GET", "/oauth/authorize?response_type=code&client_id="+client+
			"&redirect_uri="+url.QueryEscape(evil)+"&code_challenge=abc&code_challenge_method=S256", nil)
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		// Refused WITHOUT redirecting: bouncing an error to an unverified target
		// would make this endpoint an open relay.
		if rec.Code == http.StatusFound {
			t.Errorf("%q was redirected to instead of refused", evil)
		}
	}
	// The exchange checks it a second time, against the one used to authorize.
	code := approve(t, s, cookie, client, redirect, pkce("v"), "read", []string{ws})
	if status, _ := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {"https://evil.example/cb"}, "code_verifier": {"v"},
	}); status == http.StatusOK {
		t.Error("the token endpoint accepted a redirect_uri that was never authorized")
	}
}

// Rule 3: a code is single use.
func TestACodeCannotBeUsedTwice(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "replay@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "read", []string{ws})

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"}}
	if status, out := tokenCall(s, form); status != http.StatusOK {
		t.Fatalf("first exchange failed: %d %v", status, out)
	}
	if status, out := tokenCall(s, form); status == http.StatusOK {
		t.Errorf("the same code was exchanged twice: %v", out)
	}
}

// Rule 5: a token can never approve a grant. That is a key minting a better
// key, and it is the one escalation this whole design would otherwise open.
func TestATokenCannotApproveAGrant(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "escalate@example.test")
	secret := "an-api-token"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent', ?, 'write', ?)`, newID(), uid, tokenHash(secret), now()); err != nil {
		t.Fatalf("insert: %v", err)
	}
	client := oauthClientFixture(t, s, "https://claude.ai/cb")
	payload, _ := json.Marshal(map[string]any{
		"clientId": client, "redirectUri": "https://claude.ai/cb",
		"codeChallenge": pkce("v"), "codeChallengeMethod": "S256", "scope": "write",
	})
	r := httptest.NewRequest("POST", "/api/oauth/approve", strings.NewReader(string(payload)))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatal("an API token approved an OAuth grant — a key just minted itself a better key")
	}
}

// The consent screen decides the reach, and the server does not take its word
// for the part it can check: a workspace the person is not in cannot be granted
// however the browser was tampered with.
func TestConsentCannotGrantAWorkspaceYouAreNotIn(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "member@example.test")
	otherUID, _ := signedIn(t, s, "stranger@example.test")
	foreign := makeWorkspace(t, s, otherUID)

	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)

	// Naming somebody else's workspace filters down to nothing, and nothing is
	// refused outright rather than turned into a grant that reaches everywhere.
	// That distinction is the whole point: an empty list MEANS "all", so
	// silently accepting an empty pick would turn a tampered request into the
	// widest grant there is — the exact opposite of what was asked for.
	payload, _ := json.Marshal(map[string]any{
		"clientId": client, "redirectUri": redirect, "codeChallenge": pkce("v"),
		"codeChallengeMethod": "S256", "scope": "write", "workspaces": []string{foreign},
	})
	r := httptest.NewRequest("POST", "/api/oauth/approve", strings.NewReader(string(payload)))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatal("consent was granted for a workspace the person is not a member of")
	}
}

// Discovery has to work unauthenticated, and the 401 has to point at it —
// without the pointer a client never learns signing in is possible and falls
// back to asking for a permanent token.
func TestTheUnauthorizedAnswerAdvertisesTheWayIn(t *testing.T) {
	s := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", "/mcp", strings.NewReader("{}")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/mcp without a credential answered %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("the 401 does not point at the metadata: %q", rec.Header().Get("WWW-Authenticate"))
	}
	for _, path := range []string{
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-authorization-server",
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s answered %d — a client cannot discover the flow", path, rec.Code)
		}
	}
	// S256 only: advertising "plain" as well would let a client pick the version
	// that protects nothing.
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	if strings.Contains(rec.Body.String(), "plain") {
		t.Error("the metadata advertises PKCE method \"plain\"")
	}
}

// Registration must not accept redirect targets that are dangerous by shape —
// a bad one would sit there waiting until somebody used it.
func TestRegistrationRefusesDangerousRedirects(t *testing.T) {
	for _, bad := range []string{
		"http://evil.example/cb", // plain http to a remote host
		"https://claude.ai/cb#x", // a fragment is dropped on redirect
		"not-a-uri",
	} {
		if err := validRedirectURI(bad); err == nil {
			t.Errorf("%q was accepted as a redirect_uri", bad)
		}
	}
	for _, ok := range []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://localhost:6274/callback", // native apps receive codes here
		"http://127.0.0.1:1410/cb",
		"myapp://auth",
	} {
		if err := validRedirectURI(ok); err != nil {
			t.Errorf("%q was refused: %v", ok, err)
		}
	}
}

// The bug a real client found in minutes: `scope` is a SPACE-SEPARATED LIST
// (RFC 6749 §3.3), and comparing the whole string against the two we know
// rejected every connector that asks for more than one thing. Claude's sent
// several tokens and got invalid_scope before the consent screen ever appeared.
func TestScopeIsAListAndUnknownEntriesAreIgnored(t *testing.T) {
	cases := map[string]string{
		"":                     "read",
		"read":                 "read",
		"write":                "write",
		"read write":           "write", // the case that was broken
		"write read":           "write",
		"mcp:read mcp:write":   "read",  // nothing we know → the weaker one
		"offline_access write": "write", // unknown alongside known
		"claudeai":             "read",
		"  read   write  ":     "write", // ragged spacing is still a list
	}
	for in, want := range cases {
		if got := effectiveScope(in); got != want {
			t.Errorf("scope %q → %q, want %q", in, got, want)
		}
	}
}

// A client asking for a scope we do not have must get LESS, never more, and
// never an error page: a dead end here means the connector cannot be set up at
// all, which is what happened.
func TestAnUnknownScopeStillReachesTheConsentScreen(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "scope@example.test")
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)

	r := httptest.NewRequest("GET", "/oauth/authorize?response_type=code&client_id="+client+
		"&redirect_uri="+url.QueryEscape(redirect)+
		"&code_challenge=abc&code_challenge_method=S256&scope="+url.QueryEscape("claudeai offline_access"), nil)
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)

	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "error=") {
		t.Fatalf("an unknown scope was refused instead of narrowed: %s", loc)
	}
	if !strings.HasPrefix(loc, "/oauth/consent?") {
		t.Fatalf("did not reach the consent screen: %s", loc)
	}
	// And the screen is handed the NARROWED scope, so it shows what will really
	// be granted rather than what was asked for.
	q, _ := url.ParseQuery(strings.TrimPrefix(loc, "/oauth/consent?"))
	if q.Get("scope") != "read" {
		t.Errorf("the consent screen was handed scope %q", q.Get("scope"))
	}
}

// A list of workspace ids is a photograph of one moment. He put his finger on
// it: allow everything today, and a workspace created tomorrow — by a colleague
// or by the agent itself — is simply not in the picture, so the connection
// quietly stops covering the thing somebody just made.
//
// "All" therefore has to be the ABSENCE of a list, which is exactly what an
// unrestricted API token already is. One meaning, not two.
func TestAllWorkspacesCoversOnesMadeLater(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "all@example.test")
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)

	// Consent to everything, while only one workspace exists.
	payload, _ := json.Marshal(map[string]any{
		"clientId": client, "redirectUri": redirect, "codeChallenge": pkce("v"),
		"codeChallengeMethod": "S256", "scope": "write", "allWorkspaces": true,
	})
	r := httptest.NewRequest("POST", "/api/oauth/approve", strings.NewReader(string(payload)))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	var appr struct {
		Code string `json:"code"`
	}
	json.Unmarshal(rec.Body.Bytes(), &appr)

	status, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {appr.Code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	if status != http.StatusOK {
		t.Fatalf("token: %d %v", status, out)
	}
	access, _ := out["access_token"].(string)

	// NOW a new workspace appears — after the consent was given.
	fresh := makeWorkspace(t, s, uid)

	u := s.userForAccessToken(access, "1.2.3.4")
	if u == nil {
		t.Fatal("no user for the access token")
	}
	if u.TokenWorkspaces != nil {
		t.Fatalf("an all-workspaces grant stored a list: %v", u.TokenWorkspaces)
	}
	if !u.tokenCanReach(fresh) {
		t.Error("a workspace created after the consent is out of reach — the grant froze at the moment it was given")
	}
}

// The other half: picking particular workspaces means picking them, and a new
// one does NOT quietly join.
func TestPickedWorkspacesDoNotGrowOnTheirOwn(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "picked@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "write", []string{ws})

	_, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	access, _ := out["access_token"].(string)
	fresh := makeWorkspace(t, s, uid)

	u := s.userForAccessToken(access, "1.2.3.4")
	if u.tokenCanReach(fresh) {
		t.Error("a workspace nobody consented to became reachable")
	}
	if !u.tokenCanReach(ws) {
		t.Error("the workspace that WAS picked is not reachable")
	}
}

// And the trap in between: a narrowed credential must not be able to create a
// workspace it could then never open. Adding it to the list automatically would
// be the obvious fix and the wrong one — a credential that widens its own reach
// is not a boundary.
func TestANarrowedCredentialCannotCreateAWorkspace(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "narrow@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	secret := "a-scoped-token"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, workspace_scope, created_at)
		VALUES (?, ?, 'agent', ?, 'write', ?, ?)`, newID(), uid, tokenHash(secret), ws, now()); err != nil {
		t.Fatalf("insert: %v", err)
	}
	r := httptest.NewRequest("POST", "/api/workspaces", strings.NewReader(`{"name":"Sneaky"}`))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatal("a workspace-scoped credential created a workspace it cannot open")
	}

	// An UNRESTRICTED token still may — this must not become a blanket ban.
	open := "an-unscoped-token"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent2', ?, 'write', ?)`, newID(), uid, tokenHash(open), now()); err != nil {
		t.Fatalf("insert: %v", err)
	}
	r = httptest.NewRequest("POST", "/api/workspaces", strings.NewReader(`{"name":"Fine"}`))
	r.Header.Set("Authorization", "Bearer "+open)
	r.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("an unrestricted token was refused too: %d %s", rec.Code, rec.Body.String())
	}
}

// Reported from a real connection: one workspace was granted, and the agent's
// very first call answered `workspace "…" not found` — naming an id it had
// never been given. Every "no workspace named, use the default" path asked the
// ACCOUNT for its default and then checked the credential against it, which for
// a narrowed credential fails by construction.
func TestTheDefaultWorkspaceIsOneTheCallerCanReach(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "default@example.test")
	u := &user{ID: uid}

	// Two workspaces. The account's default is whichever sorts first, so grant
	// the OTHER one — that is the situation that broke.
	a := s.firstWorkspaceOf(t, uid)
	b := makeWorkspace(t, s, uid)
	acct := s.userDefaultWorkspace(uid)
	granted := a
	if acct == a {
		granted = b
	}
	if s.userDefaultWorkspace(uid) == granted {
		t.Skip("the account default happens to be the granted one — nothing to prove")
	}
	_ = u

	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "write", []string{granted})
	_, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	access, _ := out["access_token"].(string)
	agent := s.userForAccessToken(access, "1.2.3.4")
	if agent == nil {
		t.Fatal("no user for the access token")
	}

	if got := s.defaultWorkspaceFor(agent); got != granted {
		t.Errorf("the default for this connection is %q, but only %q was granted", got, granted)
	}
	// And the tool that reads it now works on the first call, with no arguments.
	if _, _, err := s.mcpGetWorkspace(agent, ""); err != nil {
		t.Errorf("get_workspace with no argument failed for a narrowed connection: %v", err)
	}
	// Naming one outside the grant still fails — but says WHY, so an agent does
	// not go hunting for a typo.
	other := a
	if granted == a {
		other = b
	}
	_, _, err := s.mcpGetWorkspace(agent, other)
	if err == nil {
		t.Fatal("a workspace outside the grant was readable")
	}
	if !strings.Contains(err.Error(), "outside what this connection was granted") {
		t.Errorf("the refusal reads like a missing workspace: %v", err)
	}
}

// He spotted it from the outside: the agent could NAME every workspace on the
// account and only failed when it tried to open one. "Privat", "Sales", a
// customer's name — a list like that is information in itself, and an agent
// deliberately given ONE workspace should not come away with a directory of the
// rest. Every other enumeration in the MCP surface already scoped itself; this
// one flagged instead of filtering.
func TestANarrowedConnectionCannotEnumerateTheOtherWorkspaces(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "leak@example.test")
	granted := s.firstWorkspaceOf(t, uid)

	secret := makeNamedWorkspace(t, s, uid, "Privat")
	client2 := makeNamedWorkspace(t, s, uid, "Northwind")

	redirect := "https://claude.ai/cb"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "write", []string{granted})
	_, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	agent := s.userForAccessToken(out["access_token"].(string), "1.2.3.4")

	listed, err := s.mcpListWorkspaces(agent)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, leaked := range []string{"Privat", "Northwind", secret, client2} {
		if strings.Contains(listed, leaked) {
			t.Errorf("a workspace outside the grant leaked into the list: %q\n%s", leaked, listed)
		}
	}
	if !strings.Contains(listed, granted) {
		t.Error("the granted workspace is missing from its own list")
	}
	// The useful half survives: it learns THAT there is more, without learning
	// what. A count answers the question; a list answers one nobody asked.
	if !strings.Contains(listed, "not_granted") {
		t.Error("no hint that anything was withheld — the agent cannot know to ask")
	}

	// An UNRESTRICTED connection still sees everything: this must not turn into
	// a blanket ban on knowing your own workspaces.
	full := &user{ID: uid, Name: "Jeremia"}
	all, err := s.mcpListWorkspaces(full)
	if err != nil {
		t.Fatalf("list (unrestricted): %v", err)
	}
	for _, want := range []string{"Privat", "Northwind"} {
		if !strings.Contains(all, want) {
			t.Errorf("an unrestricted connection lost sight of %q", want)
		}
	}
	if strings.Contains(all, "not_granted") {
		t.Error("an unrestricted connection was told something was withheld")
	}
}

// makeNamedWorkspace is makeWorkspace with a name worth recognising in a leak.
func makeNamedWorkspace(t *testing.T, s *Server, uid, name string) string {
	t.Helper()
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id) VALUES (?, ?, ?, ?)`,
		id, name, now(), uid); err != nil {
		t.Fatalf("workspace %s: %v", name, err)
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, id, uid); err != nil {
		t.Fatalf("member: %v", err)
	}
	return id
}
