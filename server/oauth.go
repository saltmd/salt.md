package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth login (wave 41): "sign in with Google/Microsoft" as a product
// feature. A plain OIDC authorization-code flow with PKCE, no external
// library — the admin stores client id/secret in the instance settings and the
// login page shows the buttons automatically.
//
// New accounts follow the same registration policy as password signup (invite
// = only existing accounts may sign in via OAuth, domain = email allowlist,
// open = anybody). The ID-token claims come straight from the token endpoint
// over TLS — which is why parsing them without verifying the signature
// ourselves is acceptable here.

type oauthProvider struct {
	key         string
	authURL     string
	tokenURL    string
	userinfoURL string
	extraAuth   url.Values
}

var oauthProviders = map[string]oauthProvider{
	"google": {
		key:         "google",
		authURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		tokenURL:    "https://oauth2.googleapis.com/token",
		userinfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		extraAuth:   url.Values{"access_type": {"online"}, "prompt": {"select_account"}},
	},
	"microsoft": {
		key:         "microsoft",
		authURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		tokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		userinfoURL: "https://graph.microsoft.com/oidc/userinfo",
	},
}

func (s *Server) oauthClient(p string) (id, secret string) {
	switch p {
	case "google":
		return s.setting("oauth_google_id", ""), s.setting("oauth_google_secret", "")
	case "microsoft":
		return s.setting("oauth_ms_id", ""), s.setting("oauth_ms_secret", "")
	}
	return "", ""
}

// oauthEnabled reports which providers are fully configured — the login page
// only shows buttons for these.
func (s *Server) oauthEnabled() (google, microsoft bool) {
	gid, gsec := s.oauthClient("google")
	mid, msec := s.oauthClient("microsoft")
	return gid != "" && gsec != "", mid != "" && msec != ""
}

const oauthCookie = "salt_oauth"

type oauthTx struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Verifier string `json:"v"`
	Exp      int64  `json:"e"`
	// Where to land afterwards. Rides in the state cookie rather than in the
	// query, so nothing a provider echoes back can steer it — and it is checked
	// again on the way out, because a value that survived a round trip through
	// somebody else's service is not trusted just because we sent it.
	Next string `json:"n,omitempty"`
}

// safeNext accepts only a same-origin PATH. Anything else — an absolute URL, a
// protocol-relative "//evil.example", a backslash Windows treats as a slash —
// is dropped. This value ends up in a redirect after a successful sign-in,
// which makes it exactly the shape an open redirect is built from.
func safeNext(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") {
		return ""
	}
	if strings.HasPrefix(v, "//") || strings.HasPrefix(v, "/\\") {
		return ""
	}
	if strings.ContainsAny(v, "\r\n") {
		return ""
	}
	return v
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// sha256Sum returns the base64url-encoded SHA-256 sum (PKCE challenge).
func sha256Sum(v string) string {
	sum := sha256.Sum256([]byte(v))
	return b64url(sum[:])
}

func randB64(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return b64url(b)
}

// loginErrorRedirect sends the browser back to the login screen with a
// human-readable message (the SPA shows it above the form).
// loginErrorRedirect sends the browser back to the login page with a machine
// readable code plus its English sentence, the same bargain the JSON API makes
// (see httpErrorCode). The browser renders the reader's own language from the
// code and falls back to the English if it does not know it.
//
// `detail` carries text the PROVIDER produced — an OAuth error description, a
// rejected signup reason. That cannot be translated by anybody, so it travels
// beside the sentence instead of being baked into it.
func loginErrorRedirect(w http.ResponseWriter, r *http.Request, code, msg string, detail ...string) {
	q := url.Values{"oauthError": {code}, "oauthErrorText": {msg}}
	if len(detail) > 0 && detail[0] != "" {
		q.Set("oauthErrorDetail", detail[0])
	}
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	pname := r.PathValue("provider")
	prov, ok := oauthProviders[pname]
	if !ok {
		httpError(w, 404, "unknown provider")
		return
	}
	clientID, clientSecret := s.oauthClient(pname)
	if clientID == "" || clientSecret == "" {
		loginErrorRedirect(w, r, "oauth_not_configured", "This sign-in method is not configured.")
		return
	}

	// The whole flow must run on ONE origin (the state cookie is host-scoped
	// and the registered redirect URI must match). If a public base URL is
	// configured and the user is browsing via LAN IP / tunnel alias, hop to
	// the canonical origin first.
	if base := s.setting("public_base_url", ""); base != "" {
		if u, err := url.Parse(base); err == nil && u.Host != "" && u.Host != r.Host {
			http.Redirect(w, r, strings.TrimRight(base, "/")+"/api/oauth/"+pname+"/start", http.StatusFound)
			return
		}
	}

	state := randB64(16)
	verifier := randB64(32)
	sum := sha256.Sum256([]byte(verifier))
	challenge := b64url(sum[:])

	tx, _ := json.Marshal(oauthTx{
		Provider: pname, State: state, Verifier: verifier,
		Exp:  time.Now().Add(10 * time.Minute).Unix(),
		Next: safeNext(r.URL.Query().Get("next")),
	})
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCookie,
		Value:    b64url(tx),
		Path:     "/api/oauth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", s.baseURL(r)+"/api/oauth/"+pname+"/callback")
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	for k, vs := range prov.extraAuth {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	http.Redirect(w, r, prov.authURL+"?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	pname := r.PathValue("provider")
	prov, ok := oauthProviders[pname]
	if !ok {
		httpError(w, 404, "unknown provider")
		return
	}

	// Clear the transaction cookie in any case.
	defer http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/api/oauth/", MaxAge: -1})

	if e := r.URL.Query().Get("error"); e != "" {
		loginErrorRedirect(w, r, "oauth_cancelled", "Sign-in was cancelled.", e)
		return
	}

	c, err := r.Cookie(oauthCookie)
	if err != nil {
		loginErrorRedirect(w, r, "oauth_expired", "Sign-in expired — please try again.")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	var tx oauthTx
	if err != nil || json.Unmarshal(raw, &tx) != nil || tx.Provider != pname ||
		tx.Exp < time.Now().Unix() || tx.State == "" || tx.State != r.URL.Query().Get("state") {
		loginErrorRedirect(w, r, "oauth_bad_state", "Sign-in could not be verified — please try again.")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		loginErrorRedirect(w, r, "oauth_no_code", "No authorization code received.")
		return
	}

	clientID, clientSecret := s.oauthClient(pname)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", s.baseURL(r)+"/api/oauth/"+pname+"/callback")
	form.Set("code_verifier", tx.Verifier)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(prov.tokenURL, form)
	if err != nil {
		loginErrorRedirect(w, r, "oauth_token_exchange", "Token exchange failed.")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.Error != "" || tok.IDToken == "" {
		msg := tok.ErrorDesc
		if msg == "" {
			msg = tok.Error
		}
		if msg == "" {
			msg = "token response unreadable"
		}
		loginErrorRedirect(w, r, "oauth_failed", "Sign-in failed.", msg)
		return
	}

	email, name, verified := parseIDToken(tok.IDToken)
	if email == "" && prov.userinfoURL != "" && tok.AccessToken != "" {
		email, name = s.fetchUserinfo(prov.userinfoURL, tok.AccessToken, name)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		loginErrorRedirect(w, r, "oauth_no_email", "The provider did not supply an email address.")
		return
	}
	if pname == "google" && !verified {
		loginErrorRedirect(w, r, "oauth_email_unverified", "This Google address is not verified.")
		return
	}

	// Sign in only over a CONFIRMED email. An address set by the account
	// itself (and therefore unconfirmed) must not establish an OAuth identity —
	// otherwise somebody could hijack a colleague's future SSO sign-in.
	var uid string
	// disabled = 0: otherwise a deactivated account would fetch itself a fresh
	// session over the SSO route, although the old one ended on deactivation.
	err = s.db.QueryRow(`SELECT id FROM users WHERE email = ? AND email_verified = 1 AND disabled = 0`, email).Scan(&uid)
	if err != nil {
		// If an UNconfirmed account holds this address, that is a squatter: do
		// not create (email is UNIQUE) and do not quietly sign in.
		var squat int
		s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&squat)
		if squat > 0 {
			loginErrorRedirect(w, r, "oauth_email_squatter", "This address belongs to an account that has not confirmed it. Please sign in with a password or contact your administrator.")
			return
		}
		uid, err = s.oauthCreateUser(email, name)
		if err != nil {
			loginErrorRedirect(w, r, "oauth_signup_blocked", "This address cannot create an account here.", err.Error())
			return
		}
	}

	sessTok, err := s.createSession(uid)
	if err != nil {
		loginErrorRedirect(w, r, "oauth_session_failed", "The session could not be created.")
		return
	}
	setSessionCookie(w, r, sessTok, s.sessionDays()*24*3600)
	// Back where the sign-in started, when it started somewhere specific — the
	// desktop app sends people through here and needs them to come back to
	// /desktop/login rather than land in the workspace, which is where the
	// round trip used to end.
	dest := safeNext(tx.Next)
	if dest == "" {
		dest = "/"
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// parseIDToken extracts the claims we need from a JWT delivered directly by
// the token endpoint (no signature check needed on this trust path).
func parseIDToken(jwt string) (email, name string, verified bool) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return "", "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", false
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     any    `json:"email_verified"` // bool bei Google, string bei manchen IdPs
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", "", false
	}
	email = claims.Email
	if email == "" && strings.Contains(claims.PreferredUsername, "@") {
		email = claims.PreferredUsername
	}
	verified = true
	switch v := claims.EmailVerified.(type) {
	case bool:
		verified = v
	case string:
		verified = v != "false"
	}
	return email, claims.Name, verified
}

func (s *Server) fetchUserinfo(uiURL, accessToken, fallbackName string) (email, name string) {
	req, _ := http.NewRequest("GET", uiURL, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fallbackName
	}
	defer resp.Body.Close()
	var ui struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ui)
	if ui.Name == "" {
		ui.Name = fallbackName
	}
	return ui.Email, ui.Name
}

// oauthCreateUser applies the instance signup policy and provisions the
// account with an unusable random password (login happens via OAuth).
func (s *Server) oauthCreateUser(email, name string) (string, error) {
	switch s.setting("signup_mode", "invite") {
	case "open":
		// erlaubt
	case "domain":
		if !s.domainAllowsSelfSignup(email) {
			return "", fmt.Errorf("this email address may not register here — ask an admin for an invitation")
		}
	default:
		return "", fmt.Errorf("no account for %s — registration here is by invitation", email)
	}
	if name = strings.TrimSpace(name); name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	randPw := make([]byte, 32)
	rand.Read(randPw)
	uid := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?, 0, ?)`,
		uid, email, name, s.nextColor(), hashPassword(hex.EncodeToString(randPw)), now()); err != nil {
		return "", fmt.Errorf("the account could not be created")
	}
	s.addOrgMember(uid, false)
	s.onboardUser(uid, name)
	return uid, nil
}
