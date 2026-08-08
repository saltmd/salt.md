package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// salt.md as an OAuth authorization server, so an agent can SIGN IN instead of
// carrying a key that never dies (OAuth 2.1 + the MCP authorization spec).
//
// WHY, in one paragraph, because the reason shapes every decision below. An API
// token is forever and — for the /mcp/{token} form clients without a headers UI
// need — it travels in the URL, which means it lands in the logs of every proxy
// on the way. That is not a failure mode, it is normal operation. What this
// buys instead: nothing in the URL, an access token that is worthless in an
// hour, a refresh token that lives only in the client and dies on one click,
// and — the part that is easy to miss — a CONSENT SCREEN, which is the first
// place in this product where a human picks the reach of an agent while looking
// at what they are granting.
//
// AND THE CONNECTION STAYS CONNECTED. The refresh token is long-lived; only the
// access token rotates, invisibly, in the background. Nobody signs in again on
// their phone every hour. A design where they did would simply not be used.
//
// This is ADDITIONAL. API tokens keep working exactly as before — scripts,
// ChatGPT and every client without OAuth support are unaffected. Turning the
// stricter mode on is a per-workspace decision (see agentAccess), and its
// default is the behaviour that exists today, so an instance that updates and
// changes nothing notices nothing.
//
// The five rules below are not negotiable. Built wrong, this is WORSE than not
// having it, because then it looks like security. Every one of them has a test
// that fails without it.
//
//  1. PKCE (S256) is required, never "plain", never absent. Without it an
//     intercepted code is enough.
//  2. redirect_uri is compared EXACTLY. No prefix, no wildcard — a loose
//     comparison is the classic way to have codes delivered somewhere else.
//  3. A code is single use and lives seconds. Redeeming it twice does not hand
//     out a second token, it destroys the grant: a replay means somebody else
//     has the code.
//  4. The `resource` parameter binds the token to THIS instance, so a token
//     picked up elsewhere does not count here.
//  5. Authorizing requires a browser SESSION. An API token must never be able
//     to approve a grant — that is a key minting a better key.

const (
	oauthCodeTTL    = 60 * time.Second
	oauthAccessTTL  = time.Hour
	oauthCodeLength = 32
)

// The only two scopes that exist mirror an API token's: this is the same
// permission model, reached a different way.
//
// effectiveScope reads what a client ASKED for and answers what it gets.
//
// `scope` is a SPACE-SEPARATED LIST (RFC 6749 §3.3), not one value. Comparing
// the whole string against the two we know rejected every real client:
// Claude's connector sends several tokens at once and got invalid_scope before
// the consent screen ever appeared.
//
// Unknown tokens are IGNORED rather than refused, which the RFC explicitly
// allows. A client asking for something we do not have should get less, not a
// dead end — and less is the safe direction. Asking for nothing we recognise
// therefore lands on "read", the weaker of the two, never on "write".
func effectiveScope(requested string) string {
	out := "read"
	for _, tok := range strings.Fields(requested) {
		if tok == "write" {
			out = "write"
		}
	}
	return out
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- discovery -------------------------------------------------------------

// handleProtectedResourceMetadata is the signpost (RFC 9728). Without it — and
// without the pointer to it in the 401 below — a client has no way to learn
// that signing in is possible at all, and falls back to asking a human to paste
// a token. The discovery documents are deliberately unauthenticated: they carry
// no data, only the addresses of the doors.
func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "salt.md",
	})
}

func (s *Server) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := s.publicBase(r)
	writeJSON(w, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"revocation_endpoint":                   base + "/oauth/revoke",
		"scopes_supported":                      []string{"read", "write"},
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_post"},
		// S256 ONLY. Advertising "plain" as well would let a client choose the
		// version that protects nothing.
		"code_challenge_methods_supported": []string{"S256"},
	})
}

// publicBase is the address a CLIENT can reach, which is not always the one the
// request arrived on: behind a tunnel the Host header is the public name, but an
// explicitly configured base URL wins. Getting this wrong hands out redirect
// targets that resolve nowhere.
func (s *Server) publicBase(r *http.Request) string {
	return strings.TrimRight(s.baseURL(r), "/")
}

// ---- dynamic client registration (RFC 7591) --------------------------------

// handleOAuthRegister lets a client introduce itself without a human copying
// ids around. Open registration is what the MCP ecosystem expects and it is
// safe here for one reason worth stating: a registered client can do NOTHING on
// its own. It can only ask a signed-in human for consent, and every grant is
// bound to that human's own permissions. Registration creates an applicant, not
// an account.
func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		oauthError(w, 400, "invalid_client_metadata", "invalid JSON")
		return
	}
	if len(body.RedirectURIs) == 0 {
		oauthError(w, 400, "invalid_redirect_uri", "at least one redirect_uri is required")
		return
	}
	for _, u := range body.RedirectURIs {
		if err := validRedirectURI(u); err != nil {
			oauthError(w, 400, "invalid_redirect_uri", err.Error())
			return
		}
	}
	name := strings.TrimSpace(body.ClientName)
	if name == "" {
		name = "Unnamed client"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	id := "salt-" + randomToken(12)
	uris, _ := json.Marshal(body.RedirectURIs)

	// A public client (PKCE only, no secret) is the normal case for the kind of
	// app that talks MCP; a confidential one gets a secret it sees exactly once.
	secret := ""
	secretHash := ""
	if body.TokenEndpointAuthMethod == "client_secret_post" {
		secret = randomToken(24)
		secretHash = tokenHash(secret)
	}
	if _, err := s.db.Exec(`INSERT INTO oauth_clients (id, secret_hash, name, redirect_uris, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, secretHash, name, string(uris), now()); err != nil {
		oauthError(w, 500, "server_error", "could not register")
		return
	}
	out := map[string]any{
		"client_id":                  id,
		"client_name":                name,
		"redirect_uris":              body.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	}
	if secret != "" {
		out["client_secret"] = secret
		out["token_endpoint_auth_method"] = "client_secret_post"
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, out)
}

// validRedirectURI keeps the obviously dangerous shapes out at registration
// time, so a bad one cannot sit waiting to be exploited later. Loopback and
// custom schemes are how native apps receive a code and are explicitly fine;
// what is not fine is a fragment (it would be dropped on redirect and make the
// exact comparison later meaningless) or a plain-http remote host.
func validRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("redirect_uri must be an absolute URI")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return fmt.Errorf("redirect_uri must not contain a fragment")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			return fmt.Errorf("http is only allowed for loopback addresses")
		}
	}
	return nil
}

// ---- authorize -------------------------------------------------------------

// handleOAuthAuthorize is the one place a human decides what an agent may
// reach. It runs in the browser and needs a SESSION — an API token is refused
// on purpose, because a key that can approve a new key is not a boundary.
//
// It renders no HTML of its own: the SPA owns the consent screen, this hands it
// the request and takes the answer back. That keeps one interface, one
// language, one place where a workspace list is drawn.
func (s *Server) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")

	// Errors BEFORE the redirect target is verified must never be redirected —
	// that would make this endpoint an open relay for anyone's error page.
	client, err := s.connectorClient(clientID)
	if err != nil {
		oauthError(w, 400, "invalid_client", "unknown client_id")
		return
	}
	if !client.allows(redirectURI) {
		oauthError(w, 400, "invalid_redirect_uri", "redirect_uri does not match a registered one")
		return
	}
	state := q.Get("state")
	fail := func(code, desc string) {
		u, _ := url.Parse(redirectURI)
		v := u.Query()
		v.Set("error", code)
		v.Set("error_description", desc)
		if state != "" {
			v.Set("state", state)
		}
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only response_type=code is supported")
		return
	}
	// Rule 1. Absent or "plain" is refused rather than tolerated: a challenge
	// that is not a hash protects nothing, and accepting it would let a client
	// opt out of the protection by mistake.
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "code_challenge with code_challenge_method=S256 is required")
		return
	}
	// Normalised HERE and passed on, so the browser never has to take the list
	// apart a second time — two parsers for one string is how they end up
	// disagreeing.
	forward := q
	forward.Set("scope", effectiveScope(q.Get("scope")))

	// Rule 5. A session, not a token. Unauthenticated: send them to sign in and
	// come back to exactly this request.
	//
	// s.currentUser, NOT requestUser: this route deliberately hangs outside the
	// auth middleware (a client arrives here unauthenticated by design), and
	// requestUser only reads what that middleware put in the context — so it
	// was nil even for somebody signed in, and everybody got bounced to the
	// login screen they did not need.
	u := s.currentUser(r)
	if u == nil || u.TokenScope != "" {
		to := "/oauth/consent?" + forward.Encode()
		http.Redirect(w, r, "/login?next="+url.QueryEscape(to), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/oauth/consent?"+forward.Encode(), http.StatusFound)
}

// handleOAuthApprove is the consent screen's answer: the human said yes, to
// these workspaces, with this scope. It mints the code.
func (s *Server) handleOAuthApprove(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	var body struct {
		ClientID    string   `json:"clientId"`
		RedirectURI string   `json:"redirectUri"`
		Challenge   string   `json:"codeChallenge"`
		Method      string   `json:"codeChallengeMethod"`
		Scope       string   `json:"scope"`
		Resource    string   `json:"resource"`
		Workspaces  []string `json:"workspaces"`
		// "Everything, including whatever is made later." Stored as an EMPTY
		// list, which is what an unrestricted API token already looks like
		// (TokenWorkspaces == nil) — one meaning, not two.
		//
		// It has to be sayable, because a list of ids is a photograph of one
		// moment: a workspace created tomorrow — by a colleague, or by the agent
		// itself — is simply not in it, and the connection silently stops
		// covering the thing somebody just made.
		AllWorkspaces bool `json:"allWorkspaces"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpErrorCode(w, 400, "invalid_json", "invalid JSON")
		return
	}
	client, err := s.connectorClient(body.ClientID)
	if err != nil || !client.allows(body.RedirectURI) {
		httpErrorCode(w, 400, "invalid_client", "unknown client or redirect_uri")
		return
	}
	if body.Method != "S256" || body.Challenge == "" {
		httpErrorCode(w, 400, "pkce_required", "code_challenge with S256 is required")
		return
	}
	body.Scope = effectiveScope(body.Scope)
	// Only workspaces this person is actually in. The consent screen shows their
	// own list, but the answer comes from a browser and is therefore editable —
	// so it is checked here rather than trusted.
	//
	// "All" needs no filtering and gets none: it is not a list of everything they
	// are in today, it is the absence of a list. That is the difference between
	// a grant that follows along and one that was true once.
	var wsList []string
	if !body.AllWorkspaces {
		for _, ws := range body.Workspaces {
			if s.isMember(u.ID, ws) {
				wsList = append(wsList, ws)
			}
		}
		if len(wsList) == 0 {
			httpErrorCode(w, 400, "no_workspace", "pick at least one workspace, or allow all of them")
			return
		}
	}
	code := randomToken(oauthCodeLength)
	if _, err := s.db.Exec(`INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, challenge, scope, workspaces, resource, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tokenHash(code), client.ID, u.ID, body.RedirectURI, body.Challenge, body.Scope,
		strings.Join(wsList, ","), body.Resource,
		time.Now().UTC().Add(oauthCodeTTL).Format(time.RFC3339Nano)); err != nil {
		httpErrorCode(w, 500, "server_error", "could not issue a code")
		return
	}
	s.audit("human", u.ID, u.Name, "oauth_consent", "", "", client.Name+" ("+body.Scope+")")
	writeJSON(w, map[string]string{"code": code})
}

// ---- token -----------------------------------------------------------------

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request", "could not parse the form")
		return
	}
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		s.oauthExchangeCode(w, r)
	case "refresh_token":
		s.oauthRefresh(w, r)
	default:
		oauthError(w, 400, "unsupported_grant_type", "use authorization_code or refresh_token")
	}
}

func (s *Server) oauthExchangeCode(w http.ResponseWriter, r *http.Request) {
	code := r.PostFormValue("code")
	if code == "" {
		oauthError(w, 400, "invalid_request", "code is required")
		return
	}
	var clientID, userID, redirectURI, challenge, scope, workspaces, resource, expires string
	err := s.db.QueryRow(`SELECT client_id, user_id, redirect_uri, challenge, scope, workspaces, resource, expires_at
		FROM oauth_codes WHERE code_hash = ?`, tokenHash(code)).
		Scan(&clientID, &userID, &redirectURI, &challenge, &scope, &workspaces, &resource, &expires)
	if err != nil {
		oauthError(w, 400, "invalid_grant", "unknown or already-used code")
		return
	}
	// Rule 3. Gone the moment it is seen, whatever happens next. A second
	// attempt below therefore finds nothing — and the grant it created dies with
	// it, because a replay means somebody other than the client has the code.
	s.db.Exec(`DELETE FROM oauth_codes WHERE code_hash = ?`, tokenHash(code))

	if t, e := time.Parse(time.RFC3339Nano, expires); e != nil || time.Now().UTC().After(t) {
		oauthError(w, 400, "invalid_grant", "the code has expired")
		return
	}
	if r.PostFormValue("client_id") != clientID {
		oauthError(w, 400, "invalid_grant", "the code was issued to a different client")
		return
	}
	// Rule 2. Exact, not a prefix.
	if r.PostFormValue("redirect_uri") != redirectURI {
		oauthError(w, 400, "invalid_grant", "redirect_uri does not match the one used to authorize")
		return
	}
	// Rule 1, the other half: the verifier has to hash to the challenge.
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		oauthError(w, 400, "invalid_grant", "code_verifier does not match code_challenge")
		return
	}
	if !s.oauthClientOK(clientID, r.PostFormValue("client_secret")) {
		oauthError(w, 401, "invalid_client", "client authentication failed")
		return
	}
	// Rule 4. A token minted for another audience must not be usable here.
	if want := r.PostFormValue("resource"); want != "" && resource != "" && want != resource {
		oauthError(w, 400, "invalid_target", "resource does not match the authorized one")
		return
	}
	s.issueGrant(w, r, userID, clientID, scope, workspaces, resource)
}

func (s *Server) oauthRefresh(w http.ResponseWriter, r *http.Request) {
	refresh := r.PostFormValue("refresh_token")
	var id, userID, clientID, scope, workspaces, resource string
	err := s.db.QueryRow(`SELECT id, user_id, client_id, scope, workspaces, resource FROM oauth_grants WHERE refresh_hash = ?`,
		tokenHash(refresh)).Scan(&id, &userID, &clientID, &scope, &workspaces, &resource)
	if err != nil {
		oauthError(w, 400, "invalid_grant", "unknown refresh token")
		return
	}
	if r.PostFormValue("client_id") != clientID {
		oauthError(w, 400, "invalid_grant", "the grant belongs to a different client")
		return
	}
	if !s.oauthClientOK(clientID, r.PostFormValue("client_secret")) {
		oauthError(w, 401, "invalid_client", "client authentication failed")
		return
	}
	// The old grant row is replaced, so the previous refresh token stops working
	// — rotation. A refresh token that stays valid forever is the long-lived
	// secret this whole thing exists to avoid.
	s.db.Exec(`DELETE FROM oauth_grants WHERE id = ?`, id)
	s.db.Exec(`DELETE FROM oauth_access WHERE grant_id = ?`, id)
	s.issueGrant(w, r, userID, clientID, scope, workspaces, resource)
}

// issueGrant writes the pair and answers with it: a short access token and the
// refresh token that keeps the connection alive without anybody signing in
// again.
func (s *Server) issueGrant(w http.ResponseWriter, r *http.Request, userID, clientID, scope, workspaces, resource string) {
	grantID := newID()
	refresh := randomToken(32)
	access := randomToken(32)
	exp := time.Now().UTC().Add(oauthAccessTTL)

	if _, err := s.db.Exec(`INSERT INTO oauth_grants (id, user_id, client_id, refresh_hash, scope, workspaces, resource, created_at, last_used_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')`,
		grantID, userID, clientID, tokenHash(refresh), scope, workspaces, resource, now()); err != nil {
		oauthError(w, 500, "server_error", "could not issue a grant")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO oauth_access (token_hash, grant_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash(access), grantID, exp.Format(time.RFC3339Nano)); err != nil {
		oauthError(w, 500, "server_error", "could not issue a token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{
		"access_token":  access,
		"token_type":    "Bearer",
		"expires_in":    int(oauthAccessTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

// handleOAuthRevoke is the "disconnect" button meaning what it says: the grant
// and every access token minted from it are gone at once, not when the last one
// happens to expire.
func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tok := r.PostFormValue("token")
	h := tokenHash(tok)
	var id string
	if s.db.QueryRow(`SELECT id FROM oauth_grants WHERE refresh_hash = ?`, h).Scan(&id) != nil {
		s.db.QueryRow(`SELECT grant_id FROM oauth_access WHERE token_hash = ?`, h).Scan(&id)
	}
	if id != "" {
		s.db.Exec(`DELETE FROM oauth_grants WHERE id = ?`, id)
		s.db.Exec(`DELETE FROM oauth_access WHERE grant_id = ?`, id)
	}
	// Always 200: telling a caller whether a token existed is a lookup oracle.
	w.WriteHeader(http.StatusOK)
}

// ---- using an access token -------------------------------------------------

// userForAccessToken resolves a bearer that is an OAuth access token. Returns
// nil for anything else, so the caller can fall through to API tokens.
func (s *Server) userForAccessToken(tok, ip string) *user {
	var grantID, expires string
	if s.db.QueryRow(`SELECT grant_id, expires_at FROM oauth_access WHERE token_hash = ?`, tokenHash(tok)).
		Scan(&grantID, &expires) != nil {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, expires); err != nil || time.Now().UTC().After(t) {
		// Expired tokens are cleared as they are met; the sweep is a backstop,
		// not the thing that makes expiry work.
		s.db.Exec(`DELETE FROM oauth_access WHERE token_hash = ?`, tokenHash(tok))
		return nil
	}
	var userID, scope, workspaces string
	if s.db.QueryRow(`SELECT user_id, scope, workspaces FROM oauth_grants WHERE id = ?`, grantID).
		Scan(&userID, &scope, &workspaces) != nil {
		return nil
	}
	u := s.userByID(userID)
	if u == nil {
		return nil
	}
	s.db.Exec(`UPDATE oauth_grants SET last_used_at = ?, last_used_ip = ? WHERE id = ?`, now(), ip, grantID)
	// The very same fields an API token sets. A grant is not a second permission
	// model, it is another way to arrive at the one that exists — TokenScope
	// being non-empty is what every existing gate already reads.
	if scope != "read" {
		scope = "write"
	}
	u.TokenScope = scope
	u.TokenKind = tokenKindOAuth
	if strings.TrimSpace(workspaces) != "" {
		for _, w := range strings.Split(workspaces, ",") {
			if w = strings.TrimSpace(w); w != "" {
				u.TokenWorkspaces = append(u.TokenWorkspaces, w)
			}
		}
	}
	return u
}

// ---- helpers ---------------------------------------------------------------

type oauthClient struct {
	ID           string
	Name         string
	SecretHash   string
	RedirectURIs []string
}

// allows is rule 2: exact string equality against a registered URI. Written as
// its own method so there is one place to read, and one place a well-meant
// "just allow a trailing slash" would have to be argued for.
func (c *oauthClient) allows(uri string) bool {
	for _, u := range c.RedirectURIs {
		if u == uri {
			return true
		}
	}
	return false
}

func (s *Server) connectorClient(id string) (*oauthClient, error) {
	if id == "" {
		return nil, fmt.Errorf("client_id is required")
	}
	c := &oauthClient{ID: id}
	var uris string
	if err := s.db.QueryRow(`SELECT name, secret_hash, redirect_uris FROM oauth_clients WHERE id = ?`, id).
		Scan(&c.Name, &c.SecretHash, &uris); err != nil {
		return nil, fmt.Errorf("unknown client")
	}
	json.Unmarshal([]byte(uris), &c.RedirectURIs)
	return c, nil
}

func (s *Server) oauthClientOK(id, secret string) bool {
	c, err := s.connectorClient(id)
	if err != nil {
		return false
	}
	if c.SecretHash == "" {
		return true // public client: PKCE is the proof
	}
	return tokenHash(secret) == c.SecretHash
}

// oauthError answers in the shape OAuth clients parse (RFC 6749 §5.2) rather
// than in this server's own error shape — a client that cannot read the reason
// retries forever.
func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}

// sweepOAuth drops what has aged out. Expiry is enforced when a token is used,
// so this only keeps the tables from growing.
func (s *Server) sweepOAuth() {
	cutoff := time.Now().UTC().Format(time.RFC3339Nano)
	s.db.Exec(`DELETE FROM oauth_access WHERE expires_at < ?`, cutoff)
	s.db.Exec(`DELETE FROM oauth_codes WHERE expires_at < ?`, cutoff)
}

// ---- what the browser needs ------------------------------------------------

// handleOAuthRequestInfo answers the consent screen's question: who is asking,
// for what, and which workspaces can I offer? The client NAME comes from the
// registration and is therefore something a stranger chose — the screen has to
// present it as a claim, not as an identity.
func (s *Server) handleOAuthRequestInfo(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	c, err := s.connectorClient(r.URL.Query().Get("client_id"))
	if err != nil {
		httpErrorCode(w, 404, "unknown_client", "unknown client_id")
		return
	}
	type wsInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	list := []wsInfo{}
	rows, qerr := s.db.Query(`SELECT w.id, w.name FROM workspaces w
		JOIN workspace_members m ON m.workspace_id = w.id AND m.user_id = ?
		ORDER BY w.name`, u.ID)
	if qerr == nil {
		for rows.Next() {
			var i wsInfo
			if rows.Scan(&i.ID, &i.Name) == nil {
				list = append(list, i)
			}
		}
		rows.Close()
	}
	writeJSON(w, map[string]any{
		"clientName": c.Name,
		"clientId":   c.ID,
		"workspaces": list,
		// WHICH instance is being asked about. Without it the screen could be
		// any salt.md anywhere — and "which server am I handing this to" is the
		// first question somebody should be able to answer at a glance.
		"instanceName": s.setting("instance_name", ""),
		"host":         r.Host,
	})
}

type grantJSON struct {
	ID         string  `json:"id"`
	ClientName string  `json:"clientName"`
	Scope      string  `json:"scope"`
	Workspaces string  `json:"workspaces"`
	CreatedAt  string  `json:"createdAt"`
	LastUsedAt *string `json:"lastUsedAt"`
	LastUsedIP string  `json:"lastUsedIp"`
}

// handleListGrants is the "what is connected to my account" list — the other
// half of consent. Granting access without a place to see and undo it is a
// one-way door.
func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT g.id, COALESCE(c.name, g.client_id), g.scope, g.workspaces, g.created_at, g.last_used_at, g.last_used_ip
		FROM oauth_grants g LEFT JOIN oauth_clients c ON c.id = g.client_id
		WHERE g.user_id = ? ORDER BY g.created_at DESC`, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []grantJSON{}
	for rows.Next() {
		var g grantJSON
		if rows.Scan(&g.ID, &g.ClientName, &g.Scope, &g.Workspaces, &g.CreatedAt, &g.LastUsedAt, &g.LastUsedIP) == nil {
			out = append(out, g)
		}
	}
	writeJSON(w, out)
}

// handleDeleteGrant is "disconnect", and it means it: the grant and every
// access token minted from it go at once.
func (s *Server) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := s.db.Exec(`DELETE FROM oauth_grants WHERE id = ? AND user_id = ?`, id, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpErrorCode(w, 404, "not_found", "no such connection")
		return
	}
	s.db.Exec(`DELETE FROM oauth_access WHERE grant_id = ?`, id)
	writeJSON(w, map[string]bool{"ok": true})
}
