package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mustIP fails the test rather than silently checking a nil address, which
// blockedIP would wave through.
func mustIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("%q is not an IP — the test itself is wrong", s)
	}
	return ip
}

// A webhook URL is a text field that makes the server call an address somebody
// typed. That is the shape of an SSRF hole, and the reason it is dangerous here
// is that the caller is INSIDE the network: cloud metadata services, the
// hypervisor, the router, anything the instance can reach and the internet
// cannot.
//
// safeDial is the guard (see ingest.go). These assert the addresses it must
// refuse — at the resolved IP, not by string matching, so an attacker cannot
// walk around it with a hostname that resolves inward.
func TestWebhookRefusesInternalAddresses(t *testing.T) {
	for _, ip := range []string{
		"169.254.169.254", // cloud metadata, the classic target
		"127.0.0.1",       // the instance itself
		"10.0.0.1",        // private network
		"172.16.0.1",      // private network
		"192.168.1.1",     // the router
		"0.0.0.0",         // unspecified
		"::1",             // loopback, v6
		"fd00::1",         // unique local, v6
		"224.0.0.1",       // multicast
	} {
		if !blockedIP(mustIP(t, ip)) {
			t.Errorf("%s is not blocked — a webhook could reach it from inside the network", ip)
		}
	}
	// Public addresses have to stay reachable, or the feature does nothing.
	for _, ip := range []string{"1.1.1.1", "93.184.216.34", "2606:4700::1111"} {
		if blockedIP(mustIP(t, ip)) {
			t.Errorf("%s is blocked, but a webhook has to be able to reach the internet", ip)
		}
	}
}

// The URL check is about shape, not safety — safety happens per resolved IP at
// delivery time. It still has to refuse the obvious.
func TestWebhookURLValidation(t *testing.T) {
	bad := []string{
		"", "not a url", "ftp://example.com", "file:///etc/passwd",
		"javascript:alert(1)", "https://", "gopher://example.com",
	}
	for _, u := range bad {
		if err := validWebhookURL(u); err == nil {
			t.Errorf("%q was accepted as a webhook URL", u)
		}
	}
	for _, u := range []string{"https://example.com/hook", "http://example.com:9000/x?y=1"} {
		if err := validWebhookURL(u); err != nil {
			t.Errorf("%q should be accepted: %v", u, err)
		}
	}
}

// End to end against a real receiver: the payload arrives, it is signed with
// the secret, and it names the page WITHOUT carrying its body.
func TestWebhookDeliversASignedPayload(t *testing.T) {
	var mu sync.Mutex
	var gotBody []byte
	var gotSig string
	done := make(chan struct{}, 1)

	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = buf[:n]
		gotSig = r.Header.Get("X-Salt-Signature")
		select {
		case done <- struct{}{}:
		default:
		}
		w.WriteHeader(200)
	}))
	defer recv.Close()

	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now())
	pid := newID()
	s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, 'Secret plans', '[{"type":"paragraph"}]', 1, ?, ?, ?, ?, 'workspace')`,
		pid, now(), now(), ws, uid)

	secret := "topsecret"
	s.db.Exec(`INSERT INTO webhooks (id, url, secret, events, active, created_at)
		VALUES (?, ?, ?, 'page.updated', 1, ?)`, newID(), recv.URL, secret, now())

	// The receiver is on 127.0.0.1, which safeDial refuses on purpose, so the
	// test lends the server an ordinary transport. Everything else — signing,
	// headers, the payload — is the real path.
	s.webhookTransport = http.DefaultTransport
	s.fireWebhook("page.updated", pid)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("no delivery within three seconds")
	}

	mu.Lock()
	defer mu.Unlock()

	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, gotBody)
	}
	if body["event"] != "page.updated" {
		t.Errorf("event is %v", body["event"])
	}
	page, _ := body["page"].(map[string]any)
	if page["id"] != pid {
		t.Errorf("page id is %v, want %s", page["id"], pid)
	}
	if page["title"] != "Secret plans" {
		t.Errorf("title is %v", page["title"])
	}
	// The line that matters: a webhook names a page, it does not export it.
	if strings.Contains(string(gotBody), "paragraph") {
		t.Errorf("the payload carries page content:\n%s", gotBody)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("signature is %q, want %q — a receiver cannot tell our call from anyone else's", gotSig, want)
	}
}

// A hook that is not subscribed to the event must stay quiet, and one that is
// switched off must stay quiet whatever it is subscribed to.
func TestWebhookOnlyFiresForSubscribedEvents(t *testing.T) {
	var hits int
	var mu sync.Mutex
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer recv.Close()

	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now())
	pid := newID()
	s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, 'P', '[]', 1, ?, ?, ?, ?, 'workspace')`, pid, now(), now(), ws, uid)

	s.webhookTransport = http.DefaultTransport
	s.db.Exec(`INSERT INTO webhooks (id, url, secret, events, active, created_at)
		VALUES (?, ?, 's', 'page.created', 1, ?)`, newID(), recv.URL, now())
	s.db.Exec(`INSERT INTO webhooks (id, url, secret, events, active, created_at)
		VALUES (?, ?, 's', 'page.updated', 0, ?)`, newID(), recv.URL, now())

	s.fireWebhook("page.updated", pid)
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("%d deliveries — one hook is subscribed to another event, the other is switched off", hits)
	}
}

// An unknown event name must not become a delivery, or a typo in a call site
// turns into traffic nobody asked for.
func TestWebhookIgnoresUnknownEvents(t *testing.T) {
	s := testServer(t)
	s.db.Exec(`INSERT INTO webhooks (id, url, secret, events, active, created_at)
		VALUES (?, 'https://example.com', 's', 'page.updated', 1, ?)`, newID(), now())
	// Would panic or deliver if the guard were missing; asserting it returns.
	s.fireWebhook("page.exploded", newID())
}

// The guard has to be IN the delivery path, not merely present in the package.
//
// This is the test that would have caught a copy-paste of the plain
// http.DefaultClient into deliverWebhook: with no transport lent to the server,
// a receiver on 127.0.0.1 must NOT be reached, and the failure must be recorded
// where an admin can see it.
func TestWebhookGuardIsInTheDeliveryPath(t *testing.T) {
	var hits int
	var mu sync.Mutex
	recv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
	}))
	defer recv.Close()

	s := testServer(t)
	uid, _ := signedIn(t, s, "a@example.com")
	ws := newID()
	s.db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'W', ?)`, ws, now())
	pid := newID()
	s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, 'P', '[]', 1, ?, ?, ?, ?, 'workspace')`, pid, now(), now(), ws, uid)

	hookID := newID()
	s.db.Exec(`INSERT INTO webhooks (id, url, secret, events, active, created_at)
		VALUES (?, ?, 's', 'page.updated', 1, ?)`, hookID, recv.URL, now())

	// Deliberately NOT lending a transport: this is the production path.
	s.fireWebhook("page.updated", pid)
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	got := hits
	mu.Unlock()
	if got != 0 {
		t.Errorf("%d deliveries reached a loopback address — the SSRF guard is not in the delivery path", got)
	}

	var status string
	s.db.QueryRow(`SELECT COALESCE(last_status,'') FROM webhooks WHERE id = ?`, hookID).Scan(&status)
	if !strings.Contains(status, "failed") {
		t.Errorf("last_status is %q — a refused delivery has to be visible to an admin", status)
	}
}
