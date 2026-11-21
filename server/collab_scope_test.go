package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A credential that may only read must not write through the editor's live
// connection either. The socket opens with a GET, so the gate in auth that
// refuses a read-only credential every POST, PUT, PATCH and DELETE never saw
// it, and the check on every incoming frame asked only whether the PERSON
// may write. A read-only key and a read-only sign-in could both change the
// text of any page their human may edit.
func TestCollabRefusesWritesFromReadOnlyCredentials(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "owner@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Plans", `{}`)

	for secret, scope := range map[string]string{"collab-read-token": "read", "collab-write-token": "write"} {
		if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, newID(), uid, scope, tokenHash(secret), scope, now()); err != nil {
			t.Fatal(err)
		}
	}
	redirect := "https://claude.ai/api/mcp/auth_callback"
	client := oauthClientFixture(t, s, redirect)
	code := approve(t, s, cookie, client, redirect, pkce("v"), "read", []string{ws})
	status, out := tokenCall(s, url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"client_id": {client}, "redirect_uri": {redirect}, "code_verifier": {"v"},
	})
	readSignIn, _ := out["access_token"].(string)
	if status != http.StatusOK || readSignIn == "" {
		t.Fatalf("read-only sign-in: %d %v", status, out)
	}

	srv := httptest.NewServer(s)
	defer srv.Close()
	send := func(bearer string, marker string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/collab/"+page, &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + bearer}},
		})
		if err != nil {
			t.Fatalf("%s: the socket did not open: %v", marker, err)
		}
		defer c.CloseNow()
		if _, err := readUntilSynced(c); err != nil {
			t.Fatal(err)
		}
		if err := c.Write(ctx, websocket.MessageBinary, append([]byte{frameUpdate}, marker...)); err != nil {
			t.Fatal(err)
		}
		// Following along is still allowed: the read-only socket stays open.
		time.Sleep(300 * time.Millisecond)
	}
	stored := func(marker string) bool {
		rows, err := s.db.Query(`SELECT data FROM yjs_updates WHERE page_id = ?`, page)
		if err != nil {
			t.Fatal(err)
		}
		var all [][]byte
		for rows.Next() {
			var data []byte
			rows.Scan(&data)
			all = append(all, data)
		}
		rows.Close()
		for _, data := range all {
			if bytes.Contains(data, []byte(marker)) {
				return true
			}
		}
		return false
	}

	send("collab-read-token", "from-read-key")
	send(readSignIn, "from-read-sign-in")
	// The control, last, so the two refusals have had all the time it takes.
	send("collab-write-token", "from-write-key")
	deadline := time.Now().Add(5 * time.Second)
	for !stored("from-write-key") {
		if time.Now().After(deadline) {
			t.Fatal("a token that may write could not write either, so this test proves nothing")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if stored("from-read-key") {
		t.Error("a read-only API token wrote to the page through the live connection")
	}
	if stored("from-read-sign-in") {
		t.Error("a read-only sign-in wrote to the page through the live connection")
	}
}
