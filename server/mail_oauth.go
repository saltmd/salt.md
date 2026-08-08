package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Sending mail through Google/Microsoft (wave 42): the same OAuth clients as
// the login, but a separate admin consent flow with a send scope (gmail.send
// or Mail.Send) plus offline_access. The refresh token is kept as a setting
// secret; sendMail prefers the connected provider and falls back to SMTP.
// No more fiddling with SMTP.

const mailOauthCookie = "salt_mail_oauth"

func mailScopes(provider string) string {
	if provider == "google" {
		return "openid email https://www.googleapis.com/auth/gmail.send"
	}
	return "openid email offline_access Mail.Send"
}

// mailProviderConfigured reports the connected provider ("" if none).
func (s *Server) mailProviderConfigured() (provider, address string) {
	p := s.setting("mail_provider", "")
	if p == "" {
		return "", ""
	}
	if s.setting("mail_oauth_refresh", "") == "" {
		return "", ""
	}
	return p, s.setting("mail_oauth_address", "")
}

// ---- Admin-Consent-Flow ----

func (s *Server) handleMailOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	pname := r.PathValue("provider")
	prov, ok := oauthProviders[pname]
	if !ok {
		httpError(w, 404, "unknown provider")
		return
	}
	clientID, clientSecret := s.oauthClient(pname)
	if clientID == "" || clientSecret == "" {
		httpErrorCode(w, 400, "mail_oauth_no_client", "Enter the client ID and secret in the Access tab first.")
		return
	}
	// The same canonicalisation hop as the login (the cookie is host-scoped).
	if base := s.setting("public_base_url", ""); base != "" {
		if u, err := url.Parse(base); err == nil && u.Host != "" && u.Host != r.Host {
			http.Redirect(w, r, strings.TrimRight(base, "/")+"/api/admin/mail-oauth/"+pname+"/start", http.StatusFound)
			return
		}
	}

	state := randB64(16)
	verifier := randB64(32)
	sum := sha256Sum(verifier)
	tx, _ := json.Marshal(oauthTx{Provider: pname, State: state, Verifier: verifier, Exp: time.Now().Add(10 * time.Minute).Unix()})
	http.SetCookie(w, &http.Cookie{
		Name:     mailOauthCookie,
		Value:    b64url(tx),
		Path:     "/api/admin/mail-oauth/",
		MaxAge:   600,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})

	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", s.baseURL(r)+"/api/admin/mail-oauth/"+pname+"/callback")
	q.Set("response_type", "code")
	q.Set("scope", mailScopes(pname))
	q.Set("state", state)
	q.Set("code_challenge", sum)
	q.Set("code_challenge_method", "S256")
	if pname == "google" {
		// Offline access + forced consent (without it Google hands back no
		// refresh token) + account picker: the admin may pick ANY mailbox here,
		// it does not have to be their own sign-in account.
		q.Set("access_type", "offline")
		q.Set("prompt", "consent select_account")
	} else {
		q.Set("prompt", "select_account")
	}
	http.Redirect(w, r, prov.authURL+"?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleMailOAuthCallback(w http.ResponseWriter, r *http.Request) {
	// The session cookie travels with top-level redirects (SameSite=Lax), so the
	// callback stays bound to the admin.
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	pname := r.PathValue("provider")
	prov, ok := oauthProviders[pname]
	if !ok {
		httpError(w, 404, "unknown provider")
		return
	}
	defer http.SetCookie(w, &http.Cookie{Name: mailOauthCookie, Path: "/api/admin/mail-oauth/", MaxAge: -1})

	// Same shape as loginErrorRedirect: a code the browser can translate, the
	// English sentence as a fallback, and provider text kept separate.
	fail := func(code, msg string, detail ...string) {
		q := url.Values{"mailOauth": {code}, "mailOauthText": {msg}}
		if len(detail) > 0 && detail[0] != "" {
			q.Set("mailOauthDetail", detail[0])
		}
		http.Redirect(w, r, "/?"+q.Encode(), http.StatusFound)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		fail("mail_oauth_cancelled", "Cancelled.", e)
		return
	}
	c, err := r.Cookie(mailOauthCookie)
	if err != nil {
		fail("mail_oauth_expired", "Expired — please connect again.")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	var tx oauthTx
	if err != nil || json.Unmarshal(raw, &tx) != nil || tx.Provider != pname ||
		tx.Exp < time.Now().Unix() || tx.State == "" || tx.State != r.URL.Query().Get("state") {
		fail("mail_oauth_bad_state", "Could not be verified — please connect again.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		fail("mail_oauth_no_code", "No authorization code.")
		return
	}

	clientID, clientSecret := s.oauthClient(pname)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", s.baseURL(r)+"/api/admin/mail-oauth/"+pname+"/callback")
	form.Set("code_verifier", tx.Verifier)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(prov.tokenURL, form)
	if err != nil {
		fail("mail_oauth_token_exchange", "Token exchange failed.")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if json.Unmarshal(body, &tok) != nil || tok.Error != "" {
		msg := tok.ErrorDesc
		if msg == "" {
			msg = "Token-Antwort unlesbar"
		}
		fail("mail_oauth_provider", "The provider refused the connection.", msg)
		return
	}
	if tok.RefreshToken == "" {
		fail("mail_oauth_no_refresh", "No refresh token received — remove the access in your account settings and connect again.")
		return
	}
	email, _, _ := parseIDToken(tok.IDToken)
	if email == "" && prov.userinfoURL != "" && tok.AccessToken != "" {
		email, _ = s.fetchUserinfo(prov.userinfoURL, tok.AccessToken, "")
	}

	s.setSetting("mail_provider", pname)
	s.setSetting("mail_oauth_refresh", tok.RefreshToken)
	s.setSetting("mail_oauth_address", strings.ToLower(strings.TrimSpace(email)))
	http.Redirect(w, r, "/?mailOauth=ok", http.StatusFound)
}

func (s *Server) handleMailOAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	s.setSetting("mail_provider", "")
	s.setSetting("mail_oauth_refresh", "")
	s.setSetting("mail_oauth_address", "")
	writeJSON(w, map[string]any{"ok": true})
}

// handleMailTest sends a test message to the calling admin via whatever is
// configured (provider or SMTP) — instant feedback for the settings dialog.
func (s *Server) handleMailTest(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	if !u.IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	if err := s.sendMail(u.Email, "salt.md test message", "Sending mail works! 🧂"); err != nil {
		httpErrorFrom(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "to": u.Email})
}

// ---- Sending through the provider APIs ----

// refreshedAccessToken swaps the stored refresh token for a fresh access
// token (mail is rare — no cache needed).
func (s *Server) refreshedAccessToken(provider string) (string, error) {
	prov := oauthProviders[provider]
	clientID, clientSecret := s.oauthClient(provider)
	refresh := s.setting("mail_oauth_refresh", "")
	if clientID == "" || refresh == "" {
		return "", coded("mail_not_connected", "No mail provider is connected.")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refresh)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	if provider == "microsoft" {
		form.Set("scope", mailScopes(provider))
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.PostForm(prov.tokenURL, form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tok); err != nil {
		return "", err
	}
	if tok.Error != "" || tok.AccessToken == "" {
		msg := tok.ErrorDesc
		if msg == "" {
			msg = tok.Error
		}
		return "", coded("mail_refresh_failed", "The connection to the mailbox has expired — connect it again.", msg)
	}
	// Microsoft rotates refresh tokens — keep the new one.
	if tok.RefreshToken != "" && tok.RefreshToken != refresh {
		s.setSetting("mail_oauth_refresh", tok.RefreshToken)
	}
	return tok.AccessToken, nil
}

// rfc2047 encodes a header value (Umlaute im Betreff!).
func rfc2047(v string) string {
	ascii := true
	for _, r := range v {
		if r > 127 {
			ascii = false
			break
		}
	}
	if ascii {
		return v
	}
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(v)) + "?="
}

func (s *Server) sendViaGoogle(to, subject, body string) error {
	access, err := s.refreshedAccessToken("google")
	if err != nil {
		return err
	}
	// Optional sender alias (has to be verified in Gmail under "Send mail as"),
	// otherwise the connected mailbox.
	from := s.setting("mail_from_override", "")
	if from == "" {
		from = s.setting("mail_oauth_address", "me")
	}
	raw := "From: " + from + "\r\nTo: " + to + "\r\nSubject: " + rfc2047(subject) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	payload, _ := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(raw))})
	req, _ := http.NewRequest("POST", "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return coded("mail_send_failed", "The provider refused to send the message.",
			fmt.Sprintf("Gmail HTTP %d: %s", resp.StatusCode, truncate(string(b), 200)))
	}
	return nil
}

func (s *Server) sendViaMicrosoft(to, subject, body string) error {
	access, err := s.refreshedAccessToken("microsoft")
	if err != nil {
		return err
	}
	message := map[string]any{
		"subject": subject,
		"body":    map[string]string{"contentType": "Text", "content": body},
		"toRecipients": []map[string]any{
			{"emailAddress": map[string]string{"address": to}},
		},
	}
	// Optional sender alias (needs send-as permission on the address).
	if from := s.setting("mail_from_override", ""); from != "" {
		message["from"] = map[string]any{"emailAddress": map[string]string{"address": from}}
	}
	payload, _ := json.Marshal(map[string]any{
		"message":         message,
		"saveToSentItems": true,
	})
	req, _ := http.NewRequest("POST", "https://graph.microsoft.com/v1.0/me/sendMail", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return coded("mail_send_failed", "The provider refused to send the message.",
			fmt.Sprintf("Microsoft HTTP %d: %s", resp.StatusCode, truncate(string(b), 200)))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
