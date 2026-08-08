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

// Signing in to the desktop app through the real browser.
//
// The whole design rests on one claim: the code that travels back over
// salt:// is worthless to anybody who did not start the flow. A custom
// protocol is not a private channel — any program on the machine may register
// for it — so that claim is what stands between "signed in" and "somebody
// else's program is signed in as you".
//
// These tests attack that claim from every side the flow allows.

func desktopChallengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// approveDesktop runs the browser half and returns the code the server handed back.
func approveDesktop(t *testing.T, s *Server, cookie, challenge string) string {
	t.Helper()
	r := httptest.NewRequest("POST", "/desktop/approve",
		strings.NewReader("challenge="+url.QueryEscape(challenge)))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	// The code travels in the salt:// link on the page.
	body := rec.Body.String()
	i := strings.Index(body, "salt://auth?code=")
	if i < 0 {
		t.Fatalf("no hand-back link in the page: %s", body)
	}
	rest := body[i+len("salt://auth?code="):]
	end := strings.IndexAny(rest, `"'`)
	if end < 0 {
		t.Fatal("malformed hand-back link")
	}
	code, _ := url.QueryUnescape(rest[:end])
	return code
}

func exchangeDesktop(t *testing.T, s *Server, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"code": code, "verifier": verifier})
	r := httptest.NewRequest("POST", "/api/desktop/exchange", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

func TestTheDesktopCodeIsUselessWithoutTheVerifier(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "desk@example.test")

	verifier := "the-app-keeps-this-and-never-sends-it"
	code := approveDesktop(t, s, cookie, desktopChallengeFor(verifier))

	// Somebody else's program registered for salt:// and grabbed the code.
	// It has no verifier, so it guesses.
	for _, guess := range []string{"", "wrong", verifier + "x", "the-app-keeps-this-and-never-sends-i"} {
		if rec := exchangeDesktop(t, s, code, guess); rec.Code == http.StatusOK {
			t.Fatalf("a wrong verifier (%q) redeemed the code — the protocol is the only barrier left", guess)
		}
	}
}

func TestADesktopCodeWorksOnceAndOnlyOnce(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "once@example.test")
	verifier := "single-use-please"
	code := approveDesktop(t, s, cookie, desktopChallengeFor(verifier))

	rec := exchangeDesktop(t, s, code, verifier)
	if rec.Code != http.StatusOK {
		t.Fatalf("the honest exchange failed: %d %s", rec.Code, rec.Body.String())
	}
	// It has to hand back a session, or the app is signed in nowhere.
	if !strings.Contains(rec.Header().Get("Set-Cookie"), sessionCookie) {
		t.Fatalf("no session cookie in the reply: %v", rec.Header().Values("Set-Cookie"))
	}

	if again := exchangeDesktop(t, s, code, verifier); again.Code == http.StatusOK {
		t.Fatal("the same code was redeemed twice")
	}
}

// A failed attempt must still burn the code. Otherwise a program holding a
// stolen code can sit and guess verifiers at leisure.
func TestAFailedExchangeStillBurnsTheCode(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "burn@example.test")
	verifier := "correct-horse"
	code := approveDesktop(t, s, cookie, desktopChallengeFor(verifier))

	if rec := exchangeDesktop(t, s, code, "wrong"); rec.Code == http.StatusOK {
		t.Fatal("a wrong verifier succeeded")
	}
	if rec := exchangeDesktop(t, s, code, verifier); rec.Code == http.StatusOK {
		t.Fatal("the code survived a failed attempt — a thief could keep guessing")
	}
}

// The approval page is the gate against login-CSRF: without it any page you
// open could send your browser through this flow and mint a session for a
// program waiting on salt://.
func TestMintingACodeNeedsAPersonAndAPost(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "csrf@example.test")
	challenge := desktopChallengeFor("v")

	// A GET must not mint anything — that is what an <img> tag can do.
	r := httptest.NewRequest("GET", "/desktop/approve?challenge="+challenge, nil)
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if strings.Contains(rec.Body.String(), "salt://auth?code=") {
		t.Fatal("a GET minted a sign-in code")
	}

	// And an anonymous POST must not either.
	r2 := httptest.NewRequest("POST", "/desktop/approve",
		strings.NewReader("challenge="+challenge))
	r2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, r2)
	if strings.Contains(rec2.Body.String(), "salt://auth?code=") {
		t.Fatal("an anonymous POST minted a sign-in code")
	}
}

// Somebody arriving without a session is sent to sign in first — the ordinary
// case, and it must not look like an error.
func TestAnUnauthenticatedStartGoesToTheLoginPage(t *testing.T) {
	s := testServer(t)
	challenge := desktopChallengeFor("v")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/desktop/login?challenge="+challenge, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("expected a redirect to sign in, got %d", rec.Code)
	}
	// The return address rides along URL-encoded, so it is decoded before
	// looking — the first version of this assertion searched the raw string and
	// failed on a redirect that was perfectly correct.
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("unparseable redirect: %q", loc)
	}
	next := u.Query().Get("next")
	if !strings.HasPrefix(next, "/desktop/login") || !strings.Contains(next, challenge) {
		t.Fatalf("the redirect forgets where to come back to: %q", loc)
	}
}

// A malformed challenge is refused before anything is stored — the table must
// not become a place to write arbitrary strings.
func TestAMalformedChallengeIsRefused(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "shape@example.test")

	for _, bad := range []string{"", "short", strings.Repeat("a", 100), "has spaces in it", "has/slash+plus=="} {
		r := httptest.NewRequest("GET", "/desktop/login?challenge="+url.QueryEscape(bad), nil)
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if strings.Contains(rec.Body.String(), "/desktop/approve") {
			t.Errorf("challenge %q reached the approval form", bad)
		}
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM desktop_auth`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d rows were written for malformed challenges", n)
	}
}
