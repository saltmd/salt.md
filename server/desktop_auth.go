package server

import (
	"crypto/sha256"
	"encoding/base64"
	"html"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Signing in to the desktop app through the REAL browser.
//
// The app could show the sign-in page in its own window, and the first version
// did. It works, and it is what several well-known desktop apps do. It is still
// the worse answer, for a reason that has nothing to do with convenience:
//
//   **In your own browser you can see the address bar.** You can check that the
//   password is going to login.microsoftonline.com and not to a window an
//   application drew. That is exactly why Google refuses embedded sign-in
//   flows, and working around that refusal by trimming the user agent evades
//   the rule rather than honouring it.
//
// It also reuses the browser session you already have, and passkeys and
// hardware keys work there reliably.
//
// So: the app sends you to your browser, you sign in normally — password,
// Microsoft, Google, all unchanged — and the browser hands control back.
//
// THE HAND-BACK IS THE WHOLE PROBLEM. A custom protocol (salt://) is not a
// private channel: any program on the machine may register for it, and the one
// that answers is not necessarily ours. So the code that travels over it is
// useless on its own.
//
//	app                              browser                         server
//	 │ verifier = random             │                                │
//	 │ challenge = sha256(verifier)  │                                │
//	 ├── opens /desktop/login?challenge ──────────────────────────────▶ remembers it
//	 │                               │ sign in as usual               │
//	 │                               │ "allow the desktop app?" ──────▶ mints a code
//	 │◀──── salt://auth?code ────────┤                                │
//	 ├── POST /api/desktop/exchange {code, verifier} ─────────────────▶ sha256(verifier) == challenge?
//	 │◀──────────────── a session cookie ──────────────────────────────┤ single use, then gone
//
// Whoever intercepts the code does not have the verifier, which never leaves
// the app. This is PKCE, and it is the same shape salt.md already uses for
// agents signing in over MCP.
//
// The confirmation step is not ceremony. Without it, ANY page you open could
// send your browser to /desktop/login and silently mint a session for a program
// waiting on salt:// — the classic login-CSRF, with a desktop app as the prize.

const (
	desktopCodeTTL   = 5 * time.Minute
	desktopScheme    = "salt"
	desktopChallenge = 43 // base64url of a 32-byte digest, unpadded
)

// handleDesktopLogin is where the app sends the browser. Unauthenticated: the
// person may well have to sign in first, and that is the normal case.
func (s *Server) handleDesktopLogin(w http.ResponseWriter, r *http.Request) {
	challenge := r.URL.Query().Get("challenge")
	if !validChallenge(challenge) {
		desktopPage(w, http.StatusBadRequest, "That sign-in request is malformed.",
			"Start it again from the salt.md app.", "")
		return
	}
	s.sweepDesktopPending()

	u := s.currentUser(r)
	if u == nil || u.TokenKind != "" {
		// Not signed in yet — through the normal front door, and back here
		// afterwards. currentUser is used rather than the auth middleware
		// because this route is deliberately outside it.
		http.Redirect(w, r, "/?next="+url.QueryEscape("/desktop/login?challenge="+challenge), http.StatusFound)
		return
	}

	// Signed in. Ask, in a page of its own — see the comment at the top for why
	// this step exists at all.
	desktopApprovalPage(w, challenge, u.Name, u.Email)
}

// handleDesktopApprove mints the one-time code and sends the browser back to
// the app. POST only: a GET would be followable from an image tag.
func (s *Server) handleDesktopApprove(w http.ResponseWriter, r *http.Request) {
	challenge := r.FormValue("challenge")
	if !validChallenge(challenge) {
		desktopPage(w, http.StatusBadRequest, "That sign-in request is malformed.", "", "")
		return
	}
	u := s.currentUser(r)
	if u == nil || u.TokenKind != "" {
		desktopPage(w, http.StatusUnauthorized, "You are not signed in any more.",
			"Start again from the app.", "")
		return
	}
	// randomToken lives in oauth_provider.go — the same generator the agent
	// sign-in uses, so there is one place where the entropy of a credential in
	// this product is decided.
	code := randomToken(32)
	if code == "" {
		desktopPage(w, http.StatusInternalServerError, "Could not create the sign-in.", "", "")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO desktop_auth (challenge, code_hash, user_id, created_at)
		VALUES (?, ?, ?, ?)`, challenge, tokenHash(code), u.ID, now()); err != nil {
		desktopPage(w, http.StatusInternalServerError, "Could not create the sign-in.", "", "")
		return
	}
	s.audit("user", u.ID, u.Name, "desktop_signin", "", "", "")

	// Back to the app. Shown as a link as well as followed, because a browser
	// that has never seen this scheme may refuse to redirect to it silently.
	target := desktopScheme + "://auth?code=" + url.QueryEscape(code)
	desktopPage(w, http.StatusOK, "Signed in.",
		"You can close this tab and go back to the salt.md app.", target)
}

// handleDesktopExchange turns the code plus the verifier into a session.
func (s *Server) handleDesktopExchange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code     string `json:"code"`
		Verifier string `json:"verifier"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpErrorCode(w, 400, "invalid_json", "invalid JSON")
		return
	}
	s.sweepDesktopPending()

	// The code is looked up by its hash, like every other credential here: a
	// dump of this table must not be a set of usable sign-ins.
	var challenge, userID, createdAt string
	err := s.db.QueryRow(`SELECT challenge, user_id, created_at FROM desktop_auth WHERE code_hash = ?`,
		tokenHash(body.Code)).Scan(&challenge, &userID, &createdAt)
	if err != nil {
		httpErrorCode(w, 400, "bad_code", "that sign-in code is not valid")
		return
	}
	// Single use, whatever happens next.
	s.db.Exec(`DELETE FROM desktop_auth WHERE code_hash = ?`, tokenHash(body.Code))

	if t, e := time.Parse(time.RFC3339Nano, createdAt); e != nil || time.Since(t) > desktopCodeTTL {
		httpErrorCode(w, 400, "expired", "that sign-in code has expired")
		return
	}
	// THE check: only the app that started this knows the verifier.
	if challengeOf(body.Verifier) != challenge {
		httpErrorCode(w, 400, "bad_verifier", "that sign-in code was not issued to this app")
		return
	}
	u := s.userByID(userID)
	if u == nil || u.Disabled {
		httpErrorCode(w, 403, "account_unavailable", "that account can no longer sign in")
		return
	}
	token, err := s.createSession(userID)
	if err != nil {
		httpErrorCode(w, 500, "session_failed", "could not create a session")
		return
	}
	setSessionCookie(w, r, token, s.sessionDays()*24*3600)
	writeJSON(w, map[string]any{"ok": true, "name": u.Name, "email": u.Email})
}

// ---- helpers ---------------------------------------------------------------

func validChallenge(c string) bool {
	if len(c) != desktopChallenge {
		return false
	}
	for _, r := range c {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// sweepDesktopPending removes what nobody came back for. A code that outlives
// its window is a credential lying around for no reason.
func (s *Server) sweepDesktopPending() {
	cutoff := time.Now().UTC().Add(-desktopCodeTTL).Format(time.RFC3339Nano)
	s.db.Exec(`DELETE FROM desktop_auth WHERE created_at < ?`, cutoff)
}

// ---- the two pages ---------------------------------------------------------
//
// Served as plain HTML rather than through the app: this runs in a browser that
// may not be signed in, mid-flow, and the single-page app has no business here.

// The real mark rather than the salt-shaker emoji. The emoji is somebody
// else's drawing, it renders differently on every platform, and it is the first
// thing a person sees at the end of a sign-in — the one moment where looking
// like the product matters.
const desktopMark = `<svg class="mark" viewBox="96 100 204 204" fill="currentColor" xmlns="http://www.w3.org/2000/svg" aria-hidden="true"><path d="M201.1 113.4c-6.7-.2-13 .2-17.2 1-3.7.8-10.1 3-14.2 4.9-4.3 2-9.8 5.6-12.6 8.2s-6.3 7.3-7.8 10.4c-2.4 4.8-2.8 6.8-2.8 14.6 0 5.9-.5 9.9-1.4 11.5-.8 1.4-1.4 5-1.4 8 0 3.7.7 6.9 2.2 9.8l2.2 4.2-3 3.8c-1.6 2-3.4 5.3-4 7.2s-4.1 20.1-7.6 40.5c-3.6 20.3-6.4 38.1-6.3 39.5.2 1.7 1 2.6 2.4 2.8 1.1.2 2.5-.3 3.1-1s4-18.6 7.7-39.8 7.2-39.7 7.7-41.2c.6-1.4 2-3.5 3.2-4.6l2.2-2.1 5.2 3.3c2.9 1.8 8.2 4.3 11.7 5.4 3.6 1.2 9.8 2.7 13.8 3.3 4.2.7 12 .9 18.3.6 6.6-.3 13.9-1.4 18.3-2.6 4.1-1.2 10-3.5 13.2-5.1s6.1-3.4 6.5-4 1.2-.7 2.4-.1c.9.5 2.5 2.5 3.4 4.3 1 1.8 4.4 18.6 7.6 37.3 3.3 18.7 6.2 36.2 6.6 39 .4 2.7 1.2 5.6 1.7 6.2.6.7 1.8 1.3 2.8 1.3s2.2-.6 2.8-1.3c.7-.8-.8-11.9-5.3-37.7-3.5-20.1-6.9-38.8-7.5-41.6-.7-3.3-2.3-6.6-4.5-9.2l-3.5-4 2.5-4.4c1.8-3.1 2.5-5.8 2.5-9.6 0-2.9-.6-6.8-1.4-8.6-.9-2.1-1.4-6.6-1.3-11.8.1-6.6-.3-9.3-2.1-13.3-1.3-2.8-4.2-6.9-6.5-9.3s-6.2-5.6-8.7-7.2-8.1-4.1-12.4-5.5c-6-2-10.5-2.8-18.5-3.1m-3.6 6.8c2.2-.1 7.5.5 11.8 1.3 4.3.7 10.6 2.7 14.1 4.3s8.2 4.7 10.5 6.8c2.4 2.1 5.2 5.6 6.4 7.8q2.1 4.05 2.1 9.6c0 3.7-.7 6.9-2.2 9.7-1.2 2.2-3.8 5.5-5.9 7.2-2.1 1.8-6.2 4.3-9.1 5.7-3 1.4-8.4 3.2-12 4-3.7.7-10.5 1.4-15.2 1.4s-11.6-.7-15.3-1.5c-3.8-.8-9.2-2.5-12.1-3.8-2.8-1.3-6.9-3.9-9-5.7-2-1.8-4.6-4.9-5.6-7-1-2-2.1-5.3-2.5-7.3-.4-2.2-.2-5.6.5-8.3.8-2.7 3-6.5 5.6-9.3 2.3-2.6 6.9-6.1 10.1-7.8s7.6-3.6 9.7-4.3 6.2-1.6 9-2 6.9-.8 9.1-.8M151.4 167c.1 0 2.3 2 4.8 4.4s7.5 5.8 11 7.5c3.4 1.7 9.5 3.8 13.3 4.6 3.9.8 11.8 1.5 17.6 1.5 7.2 0 13-.6 18.1-1.9 4.2-1.1 10.3-3.3 13.6-5 3.4-1.7 7.9-4.8 10.1-6.9 2.3-2.1 4.3-3.7 4.6-3.4s.5 2.3.5 4.5-.9 5.4-2 7.2-3.5 4.4-5.3 5.8c-1.7 1.3-5.4 3.5-8.2 4.9-2.7 1.4-8.1 3.3-11.8 4.2-3.8 1-10.8 2-15.5 2.3-5.8.3-11.8 0-18.2-1.1-5.9-1-12.2-2.9-16.5-5-3.8-1.8-8.5-4.6-10.4-6.2-1.9-1.5-4.3-4.8-5.3-7.2-1.3-2.8-1.7-5.3-1.3-7.2.4-1.7.8-3 .9-3"/><path d="M190.5 132.2c-1.1-.1-2.6.3-3.3 1-.6.6-1.2 1.8-1.2 2.5s-1.1 1.3-2.4 1.3-3.1.3-4 .6-1.6 1.6-1.6 2.9.7 2.6 1.6 2.9 2.5.6 3.6.6c1.6 0 1.9.5 1.4 2.2-.3 1.3-.6 2.6-.6 3s-1.7.8-3.7 1c-3.3.3-3.8.6-3.8 2.8 0 2.1.5 2.6 3.3 2.8 2.6.3 3.2.7 2.8 2-.3.9-.2 2.3.3 3 .4.8 1.9 1.2 3.2 1 1.5-.2 2.5-1.2 2.9-2.8.4-1.8 1.4-2.6 3.2-2.8 1.4-.2 2.9-1 3.2-1.8s.3-2.1 0-2.9c-.4-.8-1.5-1.5-2.6-1.5-1.6 0-1.9-.6-1.6-3.2.2-2.5.9-3.4 2.8-3.8 1.6-.4 2.6-1.4 2.8-2.8.2-1.5-.3-2.3-2.1-2.7-1.6-.4-2.3-1.3-2.3-2.8.1-1.5-.6-2.3-1.9-2.5m19 0c-1.2-.2-2.4 0-2.8.4s-.7 1.6-.7 2.6c0 1.2-.7 1.8-2.4 1.8-1.3 0-2.9.7-3.7 1.6-.9 1.1-1 2-.2 3.2.5 1 1.9 1.8 3 2 1.5.2 1.9.9 1.5 3.2-.3 2.3-.9 3-2.7 3-1.2 0-2.8.7-3.5 1.5s-1 2.2-.6 3c.3.8 1.4 1.5 2.5 1.5 1.4 0 2.1.8 2.3 2.7.2 2 .9 2.9 2.6 3.1 1.8.3 2.5-.3 3.3-2.7.9-2.6 1.6-3.1 4.4-3.1 2.1 0 3.5-.6 3.9-1.6.4-.9.1-2.3-.5-3-.6-.8-2.2-1.4-3.5-1.4-1.4 0-2.4-.6-2.4-1.4s.3-2.1.6-3 1.6-1.6 2.8-1.6 3.1-.6 4.2-1.4c1.5-1.2 1.7-1.8.8-3.3-.6-1-2.3-1.9-3.7-2.1-1.8-.2-2.7-1-2.9-2.5-.2-1.3-1.1-2.3-2.3-2.5m-38 86.8c-.7 0-1.7.6-2.3 1.2-.5.7-1.9 9.2-3.1 18.8-1.1 9.6-2.9 24.7-4 33.5-1.4 11.2-1.6 16.4-.9 17.2.6.7 1.9 1.3 2.9 1.3s2.1-.5 2.5-1.1c.3-.6 1.5-8.1 2.5-16.7 1-8.7 2.8-23.7 4-33.5 1.7-14.1 1.8-18.1.9-19.2-.7-.8-1.8-1.5-2.5-1.5m-14.4 6.1c-1.3-.7-2.3-.7-3.5.2s-2.5 7.1-5.1 23.7c-1.9 12.4-3.9 25.2-4.5 28.4-.7 4.2-.6 6.3.1 7.3.6.7 1.7 1.3 2.4 1.3s1.8-.7 2.5-1.5c.6-.7 3.2-14.1 5.6-29.7 2.4-15.5 4.4-28.4 4.4-28.5s-.8-.7-1.9-1.2"/></svg>`

const desktopStyle = `body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;
background:#fff;color:#37352f;font:15px/1.55 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
.c{width:400px;max-width:calc(100vw - 48px);text-align:center}
.mark{width:54px;height:54px;margin:0 auto 16px;display:block;color:#37352f}
h1{font-size:21px;margin:0 0 8px}
p{margin:0 0 18px;color:#787774;font-size:14px}
.who{display:inline-block;margin-bottom:18px;padding:7px 13px;border:1px solid #e9e7e4;border-radius:8px;font-size:13.5px}
button,a.b{display:block;width:100%;padding:11px 13px;font:inherit;font-size:14px;font-weight:600;
color:#fff;background:#2f7d4f;border:0;border-radius:8px;cursor:pointer;text-decoration:none;box-sizing:border-box}
a.s{display:inline-block;margin-top:14px;color:#787774;font-size:13px}
@media(prefers-color-scheme:dark){body{background:#191919;color:#d4d4d4}.who{border-color:#2f2f2f}.mark{color:#d4d4d4}}`

func desktopApprovalPage(w http.ResponseWriter, challenge, name, email string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>salt.md</title><style>%s</style></head><body><div class="c">
%s
<h1>Sign in to the desktop app?</h1>
<p>The salt.md app on this computer is asking for a session.</p>
<div class="who">%s%s</div>
<form method="POST" action="/desktop/approve">
<input type="hidden" name="challenge" value="%s">
<button type="submit">Allow</button>
</form>
<a class="s" href="/">Not now</a>
</div></body></html>`, desktopStyle, desktopMark, html.EscapeString(name), escapeOptional(" · ", email), html.EscapeString(challenge))
}

// desktopPage is every other outcome: an error, or the hand-back.
func desktopPage(w http.ResponseWriter, status int, title, detail, jump string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	body := ""
	if jump != "" {
		// Both: the meta refresh does it, the link is there when the browser
		// will not follow an unknown scheme without a click.
		body = fmt.Sprintf(`<meta http-equiv="refresh" content="0;url=%s">`, html.EscapeString(jump))
	}
	link := ""
	if jump != "" {
		link = fmt.Sprintf(`<a class="b" href="%s">Open salt.md</a>`, html.EscapeString(jump))
	}
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">%s
<title>salt.md</title><style>%s</style></head><body><div class="c">
%s<h1>%s</h1><p>%s</p>%s</div></body></html>`,
		body, desktopStyle, desktopMark, html.EscapeString(title), html.EscapeString(detail), link)
}

func escapeOptional(sep, s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return sep + html.EscapeString(s)
}
