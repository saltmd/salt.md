package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A room used to compact yjs_updates on row count alone (compactThreshold, 200).
// Two or three wide frames carry more than any 200 rows of keystrokes, so a page
// that took a large paste kept its whole history in yjs_updates, and every
// WebSocket open replayed it. This room takes three frames over 64 KiB and must
// be asked to compact right there.
func TestCollabByteCompaction(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "owner@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Wide page", `{}`)
	srv := httptest.NewServer(s)
	defer srv.Close()

	conn := dialCollab(t, srv, cookie, page)
	defer conn.CloseNow()
	if _, err := readUntilSynced(conn); err != nil {
		t.Fatalf("join: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 3 x 24 KiB is 72 KiB, well past compactBytesThreshold and far below
	// compactThreshold rows.
	wide := make([]byte, 24<<10)
	for i := 1; i <= 3; i++ {
		if err := conn.Write(ctx, websocket.MessageBinary, append([]byte{frameUpdate}, wide...)); err != nil {
			t.Fatalf("write update %d: %v", i, err)
		}
	}

	upTo := readSnapshotRequest(t, ctx, conn)
	if upTo != 3 {
		t.Fatalf("snapshotRequest = %d, want 3", upTo)
	}
	answerSnapshot(t, ctx, conn, upTo)
	waitForCompacted(t, s, page, 3)
}

// A room that is already over the bytes line when somebody joins asks right
// away, so the history shrinks on the next open instead of waiting for an edit.
// The ask only goes to a client that can write: a viewer's snapshot would be a
// full replay it has no business paying for, and it cannot vouch for the log.
func TestCollabJoinAsksForSnapshotWhenRoomAlreadyLarge(t *testing.T) {
	s := testServer(t)
	owner, ownerCookie := signedIn(t, s, "owner@example.test")
	viewer, viewerCookie := signedIn(t, s, "viewer@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	s.addMember(t, ws, viewer, "viewer")
	page := s.makePage(t, ws, owner, "", "Wide page", `{}`)
	for i := 1; i <= 3; i++ {
		if _, err := s.db.Exec(`INSERT INTO yjs_updates (page_id, seq, data) VALUES (?, ?, ?)`, page, i, make([]byte, 24<<10)); err != nil {
			t.Fatalf("seed update %d: %v", i, err)
		}
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	readOnly := dialCollab(t, srv, viewerCookie, page)
	defer readOnly.CloseNow()
	if _, err := readUntilSynced(readOnly); err != nil {
		t.Fatalf("viewer join: %v", err)
	}
	expectNoSnapshotRequest(t, readOnly, 400*time.Millisecond)

	conn := dialCollab(t, srv, ownerCookie, page)
	defer conn.CloseNow()
	if _, err := readUntilSynced(conn); err != nil {
		t.Fatalf("writer join: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	upTo := readSnapshotRequest(t, ctx, conn)
	if upTo != 3 {
		t.Fatalf("snapshotRequest = %d, want 3", upTo)
	}
	answerSnapshot(t, ctx, conn, upTo)
	waitForCompacted(t, s, page, 3)
}

// A client can accept the request and never answer it. Without a deadline the
// room would wait on it forever, so the log would keep growing and no later
// writer would ever be asked. Past compactTimeout the next writer gets a
// request of its own and the log still shrinks.
func TestCollabCompactionTimeoutAllowsNextWriter(t *testing.T) {
	old := compactTimeout
	compactTimeout = 20 * time.Millisecond
	defer func() { compactTimeout = old }()

	s := testServer(t)
	owner, ownerCookie := signedIn(t, s, "owner@example.test")
	other, otherCookie := signedIn(t, s, "second@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	s.addMember(t, ws, other, "member")
	page := s.makePage(t, ws, owner, "", "Wide page", `{}`)
	for i := 1; i <= 3; i++ {
		if _, err := s.db.Exec(`INSERT INTO yjs_updates (page_id, seq, data) VALUES (?, ?, ?)`, page, i, make([]byte, 24<<10)); err != nil {
			t.Fatalf("seed update %d: %v", i, err)
		}
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	first := dialCollab(t, srv, ownerCookie, page)
	defer first.CloseNow()
	if _, err := readUntilSynced(first); err != nil {
		t.Fatalf("first join: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Asked, and deliberately ignored while the socket stays open.
	if upTo := readSnapshotRequest(t, ctx, first); upTo != 3 {
		t.Fatalf("first snapshotRequest = %d, want 3", upTo)
	}
	time.Sleep(25 * time.Millisecond)

	second := dialCollab(t, srv, otherCookie, page)
	defer second.CloseNow()
	if _, err := readUntilSynced(second); err != nil {
		t.Fatalf("second join: %v", err)
	}
	upTo := readSnapshotRequest(t, ctx, second)
	if upTo != 3 {
		t.Fatalf("second snapshotRequest = %d, want 3", upTo)
	}
	answerSnapshot(t, ctx, second, upTo)
	waitForCompacted(t, s, page, 3)
}

// readSnapshotRequest reads frames until the room asks for a snapshot and
// returns the seq it asked for.
func readSnapshotRequest(t *testing.T, ctx context.Context, conn *websocket.Conn) int64 {
	t.Helper()
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("reading the snapshot request: %v", err)
		}
		if typ != websocket.MessageText || !strings.Contains(string(data), `"snapshotRequest"`) {
			continue
		}
		var req struct {
			SnapshotRequest int64 `json:"snapshotRequest"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			t.Fatalf("snapshot request %q: %v", data, err)
		}
		return req.SnapshotRequest
	}
}

// expectNoSnapshotRequest fails if the room asks this connection to compact
// within d. Anything else the server sends is ignored.
func expectNoSnapshotRequest(t *testing.T, conn *websocket.Conn, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if strings.Contains(string(data), `"snapshotRequest"`) {
			t.Fatalf("a connection that cannot write was asked to compact: %s", data)
		}
	}
}

func answerSnapshot(t *testing.T, ctx context.Context, conn *websocket.Conn, upTo int64) {
	t.Helper()
	snap := make([]byte, 9, 9+len("snapshot"))
	snap[0] = frameSnapshot
	binary.BigEndian.PutUint64(snap[1:9], uint64(upTo))
	snap = append(snap, []byte("snapshot")...)
	if err := conn.Write(ctx, websocket.MessageBinary, snap); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

// waitForCompacted waits for the snapshot to land: no log rows left and the
// materialized state at the seq the snapshot covered.
func waitForCompacted(t *testing.T, s *Server, page string, wantSeq int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var rows, seq int64
		s.db.QueryRow(`SELECT COUNT(*) FROM yjs_updates WHERE page_id = ?`, page).Scan(&rows)
		s.db.QueryRow(`SELECT seq FROM yjs_state WHERE page_id = ?`, page).Scan(&seq)
		if rows == 0 && seq == wantSeq {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the snapshot: yjs_updates rows = %d, yjs_state seq = %d; want 0 and %d", rows, seq, wantSeq)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
