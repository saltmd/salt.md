package server

import (
	"bytes"
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

// Reproduces the expired-request race: writer A is asked at seq 3 and stalls
// past compactTimeout; writer B finishes compaction at seq 6; A's next update
// asks A at seq 7; then A's late answer for seq 3 arrives. It must be ignored
// without overwriting seq 6 and without clearing compacting for seq 7.
func TestCollabExpiredSnapshotCannotOverwriteNewerState(t *testing.T) {
	old := compactTimeout
	compactTimeout = 200 * time.Millisecond
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

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	connA := dialCollab(t, srv, ownerCookie, page)
	defer connA.CloseNow()
	if _, err := readUntilSynced(connA); err != nil {
		t.Fatalf("writer A join: %v", err)
	}
	if upTo := readSnapshotRequest(t, ctx, connA); upTo != 3 {
		t.Fatalf("writer A initial snapshotRequest = %d, want 3", upTo)
	}

	// B joins before the deadline and sends seq 4 and 5 while A's request is
	// still active, then waits past compactTimeout and sends seq 6.
	connB := dialCollab(t, srv, otherCookie, page)
	defer connB.CloseNow()
	if _, err := readUntilSynced(connB); err != nil {
		t.Fatalf("writer B join: %v", err)
	}
	small := []byte{frameUpdate, 1, 2, 3, 4}
	for i := 0; i < 2; i++ {
		if err := connB.Write(ctx, websocket.MessageBinary, small); err != nil {
			t.Fatalf("writer B update %d: %v", i, err)
		}
	}
	time.Sleep(230 * time.Millisecond)
	if err := connB.Write(ctx, websocket.MessageBinary, small); err != nil {
		t.Fatalf("writer B update 6: %v", err)
	}
	if upTo := readSnapshotRequest(t, ctx, connB); upTo != 6 {
		t.Fatalf("writer B snapshotRequest = %d, want 6", upTo)
	}
	answerSnapshotWithPayload(t, ctx, connB, 6, []byte("snap-6"))
	waitForCompacted(t, s, page, 6)

	// A wakes up and pushes a >64 KiB update (seq 7), drawing a new request.
	wide := append([]byte{frameUpdate}, make([]byte, 70<<10)...)
	if err := connA.Write(ctx, websocket.MessageBinary, wide); err != nil {
		t.Fatalf("writer A update 7: %v", err)
	}
	if upTo := readSnapshotRequest(t, ctx, connA); upTo != 7 {
		t.Fatalf("writer A second snapshotRequest = %d, want 7", upTo)
	}

	// A's expired answer to seq 3 arrives first. It must neither regress
	// yjs_state back to 3 nor cancel the open request for seq 7.
	answerSnapshotWithPayload(t, ctx, connA, 3, []byte("stale-snap-3"))
	time.Sleep(100 * time.Millisecond)

	var storedSeq int64
	var storedSnap []byte
	if err := s.db.QueryRow(`SELECT seq, snapshot FROM yjs_state WHERE page_id = ?`, page).Scan(&storedSeq, &storedSnap); err != nil {
		t.Fatalf("read yjs_state after stale answer: %v", err)
	}
	if storedSeq != 6 || !bytes.Equal(storedSnap, []byte("snap-6")) {
		t.Fatalf("expired answer changed yjs_state to seq=%d snap=%q, want seq=6 snap=%q", storedSeq, storedSnap, "snap-6")
	}

	// Now A answers seq 7, proving compacting remained open and seq 7 lands.
	answerSnapshotWithPayload(t, ctx, connA, 7, []byte("snap-7"))
	waitForCompacted(t, s, page, 7)
	if err := s.db.QueryRow(`SELECT snapshot FROM yjs_state WHERE page_id = ?`, page).Scan(&storedSnap); err != nil {
		t.Fatalf("read yjs_state after seq 7: %v", err)
	}
	if !bytes.Equal(storedSnap, []byte("snap-7")) {
		t.Fatalf("yjs_state snapshot = %q, want %q", storedSnap, "snap-7")
	}
}

// applySnapshot refuses any upTo at or below the seq already stored in
// yjs_state without clearing compacting for the newer in-flight request.
func TestCollabSnapshotNeverMovesStateBackwards(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "owner@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Monotonic page", `{}`)
	if _, err := s.db.Exec(`INSERT INTO yjs_state (page_id, snapshot, seq) VALUES (?, ?, ?)`, page, []byte("snap-6"), 6); err != nil {
		t.Fatalf("seed yjs_state: %v", err)
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	conn := dialCollab(t, srv, cookie, page)
	defer conn.CloseNow()
	if _, err := readUntilSynced(conn); err != nil {
		t.Fatalf("join: %v", err)
	}

	s.collab.mu.Lock()
	room := s.collab.rooms[page]
	s.collab.mu.Unlock()
	if room == nil {
		t.Fatal("expected active room")
	}
	room.mu.Lock()
	var c *collabConn
	for candidate := range room.conns {
		c = candidate
		break
	}
	room.seq = 7
	room.compacting = true
	room.compactConn = c
	room.mu.Unlock()

	for _, staleSeq := range []int64{6, 3} {
		room.mu.Lock()
		room.compactSeq = staleSeq
		room.mu.Unlock()

		frame := make([]byte, 9+len("stale"))
		frame[0] = frameSnapshot
		binary.BigEndian.PutUint64(frame[1:9], uint64(staleSeq))
		copy(frame[9:], "stale")
		s.applySnapshot(room, c, frame)

		room.mu.Lock()
		stillCompacting := room.compacting
		room.mu.Unlock()
		if !stillCompacting {
			t.Fatalf("refusing stale upTo=%d cleared compacting", staleSeq)
		}

		var gotSeq int64
		var gotSnap []byte
		if err := s.db.QueryRow(`SELECT seq, snapshot FROM yjs_state WHERE page_id = ?`, page).Scan(&gotSeq, &gotSnap); err != nil {
			t.Fatalf("read yjs_state: %v", err)
		}
		if gotSeq != 6 || !bytes.Equal(gotSnap, []byte("snap-6")) {
			t.Fatalf("stale upTo=%d overwrote yjs_state with seq=%d snap=%q", staleSeq, gotSeq, gotSnap)
		}
	}
}

// After a missed deadline, the next update from the stalled writer asks a
// different synced writer rather than piling another request onto the client
// that is already behind (and never picks a read-only viewer).
func TestCollabTimeoutAsksDifferentWriter(t *testing.T) {
	old := compactTimeout
	compactTimeout = 150 * time.Millisecond
	defer func() { compactTimeout = old }()

	s := testServer(t)
	owner, ownerCookie := signedIn(t, s, "owner@example.test")
	other, otherCookie := signedIn(t, s, "second@example.test")
	viewer, viewerCookie := signedIn(t, s, "viewer@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	s.addMember(t, ws, other, "member")
	s.addMember(t, ws, viewer, "viewer")
	page := s.makePage(t, ws, owner, "", "Wide page", `{}`)
	for i := 1; i <= 3; i++ {
		if _, err := s.db.Exec(`INSERT INTO yjs_updates (page_id, seq, data) VALUES (?, ?, ?)`, page, i, make([]byte, 24<<10)); err != nil {
			t.Fatalf("seed update %d: %v", i, err)
		}
	}
	srv := httptest.NewServer(s)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	connA := dialCollab(t, srv, ownerCookie, page)
	defer connA.CloseNow()
	if _, err := readUntilSynced(connA); err != nil {
		t.Fatalf("writer A join: %v", err)
	}
	if upTo := readSnapshotRequest(t, ctx, connA); upTo != 3 {
		t.Fatalf("writer A snapshotRequest = %d, want 3", upTo)
	}

	connB := dialCollab(t, srv, otherCookie, page)
	defer connB.CloseNow()
	if _, err := readUntilSynced(connB); err != nil {
		t.Fatalf("writer B join: %v", err)
	}
	connC := dialCollab(t, srv, viewerCookie, page)
	defer connC.CloseNow()
	if _, err := readUntilSynced(connC); err != nil {
		t.Fatalf("viewer C join: %v", err)
	}

	time.Sleep(180 * time.Millisecond)

	// A sends seq 4; the server must delegate the snapshot request to B.
	if err := connA.Write(ctx, websocket.MessageBinary, []byte{frameUpdate, 1, 2, 3, 4}); err != nil {
		t.Fatalf("writer A update 4: %v", err)
	}
	if upTo := readSnapshotRequest(t, ctx, connB); upTo != 4 {
		t.Fatalf("writer B snapshotRequest = %d, want 4", upTo)
	}
	expectNoSnapshotRequest(t, connA, 100*time.Millisecond)
	expectNoSnapshotRequest(t, connC, 100*time.Millisecond)

	answerSnapshot(t, ctx, connB, 4)
	waitForCompacted(t, s, page, 4)
}

// Compacting must reset room.pendingBytes to the uncompacted tail, so a small
// update after compaction does not keep triggering snapshot requests.
func TestCollabCompactionResetsPendingBytes(t *testing.T) {
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

	if err := conn.Write(ctx, websocket.MessageBinary, []byte{frameUpdate, 1, 2, 3, 4}); err != nil {
		t.Fatalf("small post-compaction update: %v", err)
	}
	expectNoSnapshotRequest(t, conn, 250*time.Millisecond)
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
			t.Fatalf("unexpected snapshotRequest: %s", data)
		}
	}
}

func answerSnapshot(t *testing.T, ctx context.Context, conn *websocket.Conn, upTo int64) {
	t.Helper()
	answerSnapshotWithPayload(t, ctx, conn, upTo, []byte("snapshot"))
}

func answerSnapshotWithPayload(t *testing.T, ctx context.Context, conn *websocket.Conn, upTo int64, payload []byte) {
	t.Helper()
	snap := make([]byte, 9, 9+len(payload))
	snap[0] = frameSnapshot
	binary.BigEndian.PutUint64(snap[1:9], uint64(upTo))
	snap = append(snap, payload...)
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
