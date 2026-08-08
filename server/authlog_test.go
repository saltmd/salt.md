package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The throttle is fed by REJECTED tokens only. That is the whole design: an
// agent makes hundreds of calls a minute with a good token and must never be
// slowed by a limiter meant for guessing.
func TestAValidTokenIsNeverThrottled(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "tok@example.test")
	tok := "a-perfectly-good-token"
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent', ?, 'write', ?)`, newID(), uid, tokenHash(tok), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	// Far more calls than the bucket would ever hold.
	for i := 0; i < 200; i++ {
		r := httptest.NewRequest("GET", "/api/pages", nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		if u := s.currentUser(r); u == nil {
			t.Fatalf("a valid token was refused on call %d — the limiter is charging the honest caller", i+1)
		}
	}
}

// Guessing, on the other hand, gets cut off — and cut off BEFORE the lookup, so
// a flood costs no database work at all.
func TestGuessingTokensIsCutOff(t *testing.T) {
	s := testServer(t)
	ip := "203.0.113.9:1234"
	rejected := 0
	for i := 0; i < 100; i++ {
		r := httptest.NewRequest("GET", "/api/pages", nil)
		r.RemoteAddr = ip
		r.Header.Set("Authorization", "Bearer wrong-token")
		if s.currentUser(r) == nil {
			rejected++
		}
	}
	if rejected != 100 {
		t.Fatalf("%d of 100 wrong tokens were accepted", 100-rejected)
	}
	if !s.tokenRate.exhausted("203.0.113.9") {
		t.Error("100 wrong tokens from one address did not exhaust its budget — nothing is being throttled")
	}
	// And a DIFFERENT address is unaffected: the ban is per caller, not global.
	if s.tokenRate.exhausted("198.51.100.7") {
		t.Error("an untouched address was throttled — one attacker would lock out everybody")
	}
}

// exhausted() must not take a token itself, or merely asking would ban people.
func TestAskingDoesNotConsume(t *testing.T) {
	rl := newRateLimiter(60, 20)
	for i := 0; i < 500; i++ {
		if rl.exhausted("someone") {
			t.Fatalf("asking %d times exhausted a bucket nobody used", i+1)
		}
	}
}

// The line fail2ban reads must carry the address and must NOT carry the
// credential or the account — a ban log ends up in journald, in log shipping
// and in backups.
func TestTheAuthFailureLineCarriesTheAddressAndNothingElse(t *testing.T) {
	s := testServer(t)
	line := captureLog(t, func() {
		r := httptest.NewRequest("GET", "/api/pages", nil)
		r.RemoteAddr = "198.51.100.23:9999"
		r.Header.Set("Authorization", "Bearer super-secret-token-value")
		s.currentUser(r)
	})
	if !strings.Contains(line, "198.51.100.23") {
		t.Errorf("no address in %q — fail2ban has nothing to ban", line)
	}
	if !strings.Contains(line, "token") {
		t.Errorf("the kind of credential is missing from %q", line)
	}
	if strings.Contains(line, "super-secret-token-value") {
		t.Errorf("the token itself was written to the log: %q", line)
	}
}

func TestAWrongPasswordIsLogged(t *testing.T) {
	s := testServer(t)
	signedIn(t, s, "victim@example.test")
	line := captureLog(t, func() {
		r := httptest.NewRequest("POST", "/api/login", jsonBody(`{"email":"victim@example.test","password":"wrong"}`))
		r.RemoteAddr = "198.51.100.44:1"
		s.ServeHTTP(httptest.NewRecorder(), r)
	})
	if !strings.Contains(line, "198.51.100.44") || !strings.Contains(line, "password") {
		t.Errorf("a wrong password left no usable line: %q", line)
	}
	if strings.Contains(line, "victim@example.test") {
		t.Errorf("the email was written into the ban log: %q", line)
	}
}

var _ = http.StatusOK

// captureLog collects what the standard logger writes while fn runs.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	old := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(old); log.SetFlags(flags) }()
	fn()
	return buf.String()
}

func jsonBody(s string) io.Reader { return strings.NewReader(s) }
