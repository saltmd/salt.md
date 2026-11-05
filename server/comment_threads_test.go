package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

type inboxResult struct {
	Items   []replyNotification `json:"items"`
	Unread  int                 `json:"unreadCount"`
	Through int64               `json:"through"`
	Next    int64               `json:"nextCursor"`
}

func threadRequest(s *Server, cookie, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}
func readInbox(t *testing.T, s *Server, cookie, query string) inboxResult {
	t.Helper()
	rec := threadRequest(s, cookie, "GET", "/api/notifications"+query, "")
	if rec.Code != 200 {
		t.Fatalf("inbox: %d %s", rec.Code, rec.Body.String())
	}
	var result inboxResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func addThreadComment(t *testing.T, s *Server, u *user, page, body, target, key string) string {
	t.Helper()
	id, err := s.createThreadComment(u, page, commentInput{Body: body, ReplyToID: target, RequestID: key})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestReplyRecipientsReadStatePermissionsAndDeletion(t *testing.T) {
	s := testServer(t)
	a, ca := signedIn(t, s, "alice@example.test")
	b, cb := signedIn(t, s, "bob@example.test")
	c, cc := signedIn(t, s, "carol@example.test")
	ws := s.firstWorkspaceOf(t, a)
	for _, id := range []string{b, c} {
		if _, err := s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, id); err != nil {
			t.Fatal(err)
		}
	}
	ua, ub, uc := &user{ID: a, Name: "Alice"}, &user{ID: b, Name: "Bob"}, &user{ID: c, Name: "Carol"}
	page := s.makePage(t, ws, a, "", "Launch", `{}`)
	root := addThreadComment(t, s, ua, page, "Feedback", "", "root")
	r1 := addThreadComment(t, s, ub, page, "First reply", root, "r1")
	r2 := addThreadComment(t, s, uc, page, "Reply to Bob", r1, "r2")
	if got := readInbox(t, s, ca, ""); got.Unread != 2 {
		t.Fatalf("root author: %+v", got)
	}
	if got := readInbox(t, s, cb, ""); got.Unread != 1 {
		t.Fatalf("reply target: %+v", got)
	}
	if got := readInbox(t, s, cc, ""); got.Unread != 0 {
		t.Fatal("self notification")
	}
	if again := addThreadComment(t, s, ub, page, "First reply", root, "r1"); again != r1 {
		t.Fatal("retry duplicated comment")
	}
	if _, err := s.createThreadComment(ub, page, commentInput{Body: "different", ReplyToID: root, RequestID: "r1"}); err == nil {
		t.Fatal("reused key changed content")
	}
	mine := readInbox(t, s, ca, "")
	id := mine.Items[0].ID
	if rec := threadRequest(s, cb, "POST", "/api/notifications/read", `{"id":`+jsonNumber(id)+`,"read":true}`); rec.Code != 404 {
		t.Fatal("marked another recipient's notification", rec.Code)
	}
	if rec := threadRequest(s, ca, "POST", "/api/notifications/read", `{"through":`+jsonNumber(mine.Through)+`,"read":true}`); rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	addThreadComment(t, s, ub, page, "New after watermark", root, "r3")
	if got := readInbox(t, s, ca, ""); got.Unread != 1 {
		t.Fatal("new reply was marked read", got.Unread)
	}
	if err := s.setThreadResolved(r1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.createThreadComment(uc, page, commentInput{Body: "late", ReplyToID: root, RequestID: "late"}); err == nil {
		t.Fatal("replied to resolved thread")
	}
	if err := s.setThreadResolved(root, false); err != nil {
		t.Fatal(err)
	}
	other := s.makePage(t, ws, a, "", "Other", `{}`)
	if _, err := s.createThreadComment(ub, other, commentInput{Body: "wrong page", ReplyToID: root, RequestID: "cross"}); err == nil {
		t.Fatal("cross-page target accepted")
	}
	if err := s.deleteThreadComment(r1); err != nil {
		t.Fatal(err)
	}
	comments, err := s.pageComments(page)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range comments {
		if v.ID == r2 {
			found = true
		}
		if v.ID == r1 && (v.DeletedAt == nil || v.Body != "") {
			t.Fatal("text was not erased")
		}
	}
	if !found {
		t.Fatal("deletion erased someone else's reply")
	}
	if got := readInbox(t, s, ca, ""); len(got.Items) != 2 {
		t.Fatal("deleted reply left a notification", len(got.Items))
	}
	if _, err := s.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=? AND user_id=?`, ws, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE pages SET visibility='private',owner_id=? WHERE id=?`, b, page); err != nil {
		t.Fatal(err)
	}
	if got := readInbox(t, s, ca, ""); got.Unread != 0 || len(got.Items) != 0 {
		t.Fatal("private page leaked", got)
	}
	if rec := threadRequest(s, ca, "POST", "/api/notifications/read", `{"id":`+jsonNumber(id)+`,"read":false}`); rec.Code != 404 {
		t.Fatal("hidden notification was addressable")
	}
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }
func TestConcurrentReplyRetriesAndAtomicNotifications(t *testing.T) {
	s := testServer(t)
	a, ca := signedIn(t, s, "author@example.test")
	b, _ := signedIn(t, s, "reply@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, b)
	page := s.makePage(t, ws, a, "", "Concurrent", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "Root", "", "")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.createThreadComment(&user{ID: b}, page, commentInput{Body: "One reply", ReplyToID: root, RequestID: "same-request"})
			ids <- id
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate replies")
		}
	}
	if got := readInbox(t, s, ca, ""); got.Unread != 1 || len(got.Items) != 1 {
		t.Fatal("duplicate notifications", got)
	}
	// A notification failure must roll the reply back as well.
	s.db.Exec(`CREATE TRIGGER fail_notification BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT,'test failure'); END`)
	if _, err := s.createThreadComment(&user{ID: b}, page, commentInput{Body: "Must roll back", ReplyToID: root, RequestID: "rollback"}); err == nil {
		t.Fatal("expected transaction failure")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM comments WHERE request_id='rollback'`).Scan(&n)
	if n != 0 {
		t.Fatal("partial transaction saved reply")
	}
}
func TestInboxPaginationAndUnreadTotal(t *testing.T) {
	s := testServer(t)
	a, ca := signedIn(t, s, "paged@example.test")
	b, _ := signedIn(t, s, "writer@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, b)
	page := s.makePage(t, ws, a, "", "Paged", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "Root", "", "")
	for i := 0; i < 55; i++ {
		addThreadComment(t, s, &user{ID: b}, page, "Reply", root, "")
	}
	first := readInbox(t, s, ca, "")
	if len(first.Items) != 50 || first.Unread != 55 || first.Next == 0 {
		t.Fatalf("first page: %+v", first)
	}
	next := readInbox(t, s, ca, "?before="+jsonNumber(first.Next))
	if len(next.Items) != 5 || next.Unread != 55 {
		t.Fatalf("second page: %+v", next)
	}
}

func TestReplyInboxHonorsTokenScopeAndAgentWrites(t *testing.T) {
	f := newScopeFixture(t)
	b, _ := signedIn(t, f.s, "agent-writer@example.test")
	f.s.addMember(t, f.other, b, "member")
	page := f.s.makePage(t, f.other, f.owner, "", "Outside token scope", `{}`)
	root := addThreadComment(t, f.s, &user{ID: f.owner}, page, "Root", "", "")
	agent := &user{ID: b, Name: "Agent writer", TokenKind: tokenKindAPI, TokenScope: "write"}
	payload := `{"page_id":"` + page + `","action":"add","body":"Agent reply","reply_to_id":"` + root + `","request_id":"agent-retry"}`
	if _, err := callTool(t, f.s, agent, "comments", payload); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(t, f.s, agent, "comments", payload); err != nil {
		t.Fatal(err)
	}
	rec := f.bearer(f.teamTok, "GET", "/api/notifications", "")
	var hidden inboxResult
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &hidden) != nil || hidden.Unread != 0 || len(hidden.Items) != 0 {
		t.Fatalf("scoped inbox leaked: %s", rec.Body.String())
	}
	visible := readInbox(t, f.s, f.cookie, "")
	if visible.Unread != 1 {
		t.Fatal("agent replies did not use shared notifications", visible)
	}
	id := visible.Items[0].ID
	if rec := f.bearer(f.teamTok, "POST", "/api/notifications/read", `{"id":`+jsonNumber(id)+`,"read":true}`); rec.Code != 404 {
		t.Fatal("token marked an out-of-scope notification", rec.Code)
	}
	scoped := f.s.events.subscribeAs(&user{ID: f.owner, TokenKind: tokenKindAPI, TokenWorkspaces: []string{f.team}})
	defer f.s.events.unsubscribe(scoped)
	addThreadComment(t, f.s, &user{ID: b}, page, "Private activity", root, "")
	for {
		select {
		case event := <-scoped:
			if strings.Contains(event, page) || strings.Contains(event, "Private activity") {
				t.Fatal("event leaked page content", event)
			}
		default:
			return
		}
	}
}

func TestReplyErrorsCarryCodesAndMCPPreservesAttribution(t *testing.T) {
	s := testServer(t)
	a, ca := signedIn(t, s, "coded-errors@example.test")
	b, _ := signedIn(t, s, "attributed-reply@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.addMember(t, ws, b, "member")
	page := s.makePage(t, ws, a, "", "Discussion", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "The original passage is discussed here.", "", "")
	if err := s.setThreadResolved(root, true); err != nil {
		t.Fatal(err)
	}
	body := `{"body":"Keep this draft","replyToId":"` + root + `"}`
	for _, tc := range []struct {
		method, path, body, code, message string
		status                            int
	}{
		{"POST", "/api/pages/" + page + "/comments", body, "comment_thread_resolved", "Reopen this thread before replying. Your draft is preserved.", 409},
		{"POST", "/api/notifications/read", `{"read":true}`, "notification_update_invalid", "Invalid notification update", 400},
		{"POST", "/api/notifications/read", `{"id":-1,"through":999,"read":true}`, "notification_update_invalid", "Invalid notification update", 400},
		{"POST", "/api/notifications/read", `{"id":1,"through":999,"read":true}`, "notification_update_invalid", "Invalid notification update", 400},
		{"POST", "/api/notifications/read", `{"id":999999,"read":true}`, "notification_not_found", "Notification not found", 404},
		{"POST", "/api/pages/" + page + "/comments", `{`, "comment_invalid_json", "Invalid comment request", 400},
	} {
		rec := threadRequest(s, ca, tc.method, tc.path, tc.body)
		var got struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if rec.Code != tc.status || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Code != tc.code || got.Error != tc.message {
			t.Fatalf("%s: %d %s", tc.code, rec.Code, rec.Body.String())
		}
	}
	if err := s.setThreadResolved(root, false); err != nil {
		t.Fatal(err)
	}
	agent := &user{ID: b, Name: "Ada", TokenKind: tokenKindAPI, TokenScope: "write"}
	if _, err := callTool(t, s, agent, "comments", `{"page_id":"`+page+`","action":"add","body":"The response","reply_to_id":"`+root+`","agent":"codex","label":"Codex"}`); err != nil {
		t.Fatal(err)
	}
	browser := threadRequest(s, ca, "GET", "/api/pages/"+page+"/comments", "")
	var comments []commentJSON
	if browser.Code != 200 || json.Unmarshal(browser.Body.Bytes(), &comments) != nil {
		t.Fatal(browser.Body.String())
	}
	reply := comments[1]
	if reply.Agent != "codex" || reply.AgentLabel != "Codex" || reply.AuthorID != b || reply.ThreadID != root || reply.ReplyToID != root {
		t.Fatalf("Reply attribution: %+v", reply)
	}
	out, err := callTool(t, s, agent, "comments", `{"page_id":"`+page+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{`"body":"The response"`, `"replyToId":"` + root + `"`, `"agent":"codex"`} {
		if !strings.Contains(out, wanted) {
			t.Fatalf("MCP lost useful comment data %s: %s", wanted, out)
		}
	}
	for _, internal := range []string{`"anchor":`, `"gen":`, `"from":`, `"to":`} {
		if strings.Contains(out, internal) {
			t.Fatal("MCP exposed editor internals", out)
		}
	}
	if !strings.Contains(out, "BEGIN UNTRUSTED CONTENT") {
		t.Fatal("MCP lost untrusted-content framing", out)
	}
}

func TestReplyMigrationKeepsLegacyComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`INSERT INTO pages(id,title,created_at,updated_at) VALUES('legacy-page','Legacy',?,?); INSERT INTO comments(id,page_id,author_name,body,created_at) VALUES('legacy-comment','legacy-page','Original author','Original feedback',?)`, now(), now(), now()); err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	for attempt := 0; attempt < 2; attempt++ {
		db, err := openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		var body, author, thread, target, key string
		var deleted sql.NullString
		err = db.QueryRow(`SELECT body,author_name,thread_id,reply_to_id,request_id,deleted_at FROM comments WHERE id='legacy-comment'`).Scan(&body, &author, &thread, &target, &key, &deleted)
		if err != nil || body != "Original feedback" || author != "Original author" || thread != "" || target != "" || key != "" || deleted.Valid {
			t.Fatal("legacy comment changed during migration", err)
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n); err != nil || n != 0 {
			t.Fatal("migration backfilled notifications", err, n)
		}
		db.Close()
	}
}

func TestReplyReadStateSurvivesServerRestart(t *testing.T) {
	dir := t.TempDir()
	dist := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	s, err := New(dir, dist)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if s != nil {
			s.Close()
		}
	})
	a, cookie := signedIn(t, s, "restart-recipient@example.test")
	b, _ := signedIn(t, s, "restart-writer@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.addMember(t, ws, b, "member")
	page := s.makePage(t, ws, a, "", "Durable discussion", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "Root", "", "")
	reply := addThreadComment(t, s, &user{ID: b}, page, "Persisted reply", root, "durable-key")
	before := readInbox(t, s, cookie, "")
	if before.Unread != 1 {
		t.Fatal(before)
	}
	rec := threadRequest(s, cookie, "POST", "/api/notifications/read", `{"id":`+jsonNumber(before.Items[0].ID)+`,"read":true}`)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	s.Close()
	s = nil
	s, err = New(dir, dist)
	if err != nil {
		t.Fatal(err)
	}
	after := readInbox(t, s, cookie, "")
	if after.Unread != 0 || len(after.Items) != 1 || after.Items[0].ReadAt == nil || after.Items[0].CommentID != reply {
		t.Fatal("inbox read state did not persist", after)
	}
	comments, err := s.pageComments(page)
	if err != nil || len(comments) != 2 || comments[1].ReplyToID != root {
		t.Fatal("reply thread did not persist", comments, err)
	}
}

func TestReplyAnchorValidationRetryAndThreadResolution(t *testing.T) {
	s := testServer(t)
	a, _ := signedIn(t, s, "anchor-author@example.test")
	b, _ := signedIn(t, s, "anchor-replier@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.addMember(t, ws, b, "member")
	page := s.makePage(t, ws, a, "", "Anchored discussion", `{}`)
	ua, ub := &user{ID: a}, &user{ID: b}
	in := commentInput{Body: "Anchored root", Anchor: json.RawMessage(testCommentAnchor), RequestID: "anchor-request"}
	root, err := s.createThreadComment(ua, page, in)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := s.createThreadComment(ua, page, in); err != nil || id != root {
		t.Fatal("retry lost anchor identity", id, err)
	}
	in.Anchor = json.RawMessage(strings.Replace(testCommentAnchor, "selected text", "different passage", 1))
	if _, err := s.createThreadComment(ua, page, in); err == nil {
		t.Fatal("retry key accepted another passage")
	}
	reply := addThreadComment(t, s, ub, page, "Reply", root, "reply")
	if _, err := s.createThreadComment(ub, page, commentInput{Body: "Wrong anchor", ReplyToID: root, Anchor: json.RawMessage(testCommentAnchor)}); err == nil {
		t.Fatal("reply accepted an independent anchor")
	}
	if err := s.setThreadResolved(reply, true); err != nil {
		t.Fatal(err)
	}
	comments, err := s.pageComments(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range comments {
		if c.ResolvedAt == nil {
			t.Fatal("thread and highlight disagree", c)
		}
	}
	if err := s.setThreadResolved(root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE pages SET doc_gen=doc_gen+1 WHERE id=?`, page); err != nil {
		t.Fatal(err)
	}
	in.RequestID = "stale-anchor"
	_, err = s.createThreadComment(ua, page, in)
	var ce *commentError
	if !errors.As(err, &ce) || ce.code != "comment_anchor_stale" {
		t.Fatal("shared creation lost stale-anchor rejection", err)
	}
	// Accepted retries remain idempotent even after a document replacement.
	in.RequestID = "anchor-request"
	in.Anchor = json.RawMessage(testCommentAnchor)
	if id, err := s.createThreadComment(ua, page, in); err != nil || id != root {
		t.Fatal("accepted retry rejected after replacement", id, err)
	}
	if err := s.deleteThreadComment(root); err != nil {
		t.Fatal(err)
	}
	comments, err = s.pageComments(page)
	if err != nil || len(comments) != 2 {
		t.Fatal(comments, err)
	}
	for _, c := range comments {
		if c.ID == root && (len(c.Anchor) != 0 || c.Body != "" || c.DeletedAt == nil) {
			t.Fatal("deleted root left passage text", c)
		}
	}
}

func TestNotificationDeletionOnlyInvalidatesRecipients(t *testing.T) {
	s := testServer(t)
	a, _ := signedIn(t, s, "recipient@example.test")
	b, _ := signedIn(t, s, "sender@example.test")
	c, _ := signedIn(t, s, "bystander@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.addMember(t, ws, b, "member")
	s.addMember(t, ws, c, "member")
	page := s.makePage(t, ws, a, "", "Deletion events", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "Root", "", "")
	for _, soft := range []bool{false, true} {
		reply := addThreadComment(t, s, &user{ID: b}, page, "Reply", root, "")
		if soft {
			addThreadComment(t, s, &user{ID: a}, page, "Keep dependent", reply, "")
		}
		streams := []chan string{s.events.subscribe(a), s.events.subscribe(b), s.events.subscribe(c)}
		if err := s.deleteThreadComment(reply); err != nil {
			t.Fatal(err)
		}
		for i, ch := range streams {
			count := 0
			for len(ch) > 0 {
				if <-ch == `{"type":"notifications"}` {
					count++
				}
			}
			want := 0
			if i == 0 {
				want = 1
			}
			if count != want {
				t.Fatalf("soft=%v account=%d got %d notifications, want %d", soft, i, count, want)
			}
			s.events.unsubscribe(ch)
		}
	}
}

func TestInboxRetentionUnreadAndPermissionAwareBatches(t *testing.T) {
	s := testServer(t)
	a, cookie := signedIn(t, s, "history@example.test")
	b, _ := signedIn(t, s, "writer@example.test")
	ws := s.firstWorkspaceOf(t, a)
	s.addMember(t, ws, b, "member")
	page := s.makePage(t, ws, a, "", "Visible", `{}`)
	hidden := s.makePage(t, ws, b, "", "Private", `{}`)
	s.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=? AND user_id=?`, ws, a)
	s.db.Exec(`UPDATE pages SET visibility='private',owner_id=? WHERE id=?`, b, hidden)
	// Distinct synthetic rows avoid spending this regression on hundreds of API writes.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`DELETE FROM notifications`); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().AddDate(0, 0, -31).Format(time.RFC3339Nano)
	insert := func(id int, kind, p string, comment any, read any) {
		t.Helper()
		if _, err := tx.Exec(`INSERT INTO notifications(id,kind,recipient_id,page_id,comment_id,created_at,read_at) VALUES(?,?,?,?,?,?,?)`, id, kind, a, p, comment, old, read); err != nil {
			t.Fatal(err)
		}
	}
	// A comment may cause one notification per recipient, so use distinct comments.
	for i := 1; i <= 170; i++ {
		id := fmt.Sprintf("history-%d", i)
		if _, err := tx.Exec(`INSERT INTO comments(id,page_id,body,created_at) VALUES(?,?,?,?)`, id, page, "History", old); err != nil {
			t.Fatal(err)
		}
		read := any(nil)
		if i <= 5 {
			read = old
		}
		if i == 6 {
			read = nowFixed()
		}
		insert(i, "comment_reply", page, id, read)
	}
	// These newer inaccessible entries span more than a single SQL batch.
	for i := 171; i <= 280; i++ {
		id := fmt.Sprintf("hidden-%d", i)
		if _, err := tx.Exec(`INSERT INTO comments(id,page_id,body,created_at) VALUES(?,?,?,?)`, id, hidden, "Private", old); err != nil {
			t.Fatal(err)
		}
		insert(i, "comment_reply", hidden, id, nil)
	}
	insert(281, "page_mention", page, nil, nil)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	first := readInbox(t, s, cookie, "")
	if len(first.Items) != 50 || first.Unread != 164 || first.Through != 170 || first.Items[0].ID != 170 || first.Next != 121 {
		t.Fatalf("first page: %+v", first)
	}
	second := readInbox(t, s, cookie, "?before="+jsonNumber(first.Next))
	if len(second.Items) != 50 || second.Items[0].ID != 120 {
		t.Fatal("keyset pagination skipped visible rows", second)
	}
	s.pruneReadNotifications()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id<=5`).Scan(&n); err != nil || n != 0 {
		t.Fatal("expired read rows survived", n, err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id=6 OR (read_at IS NULL AND created_at=?)`, old).Scan(&n); err != nil || n != 276 {
		t.Fatal("cleanup erased unread/recent history", n, err)
	}
	rec := threadRequest(s, cookie, "POST", "/api/notifications/read", `{"through":170,"read":true}`)
	if rec.Code != 200 || readInbox(t, s, cookie, "").Unread != 0 {
		t.Fatal("mark-all did not cover all pages", rec.Body.String())
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE read_at IS NULL`).Scan(&n); err != nil || n != 111 {
		t.Fatal("mark-all touched hidden/other kinds", n, err)
	}
}

func TestNotificationMigrationPreservesReadStateAndAllowsOtherKinds(t *testing.T) {
	s := testServer(t)
	a, _ := signedIn(t, s, "old-inbox@example.test")
	ws := s.firstWorkspaceOf(t, a)
	page := s.makePage(t, ws, a, "", "Migration", `{}`)
	root := addThreadComment(t, s, &user{ID: a}, page, "Original", "", "")
	if _, err := s.db.Exec(`DROP TABLE notifications;
        CREATE TABLE notifications(id INTEGER PRIMARY KEY AUTOINCREMENT,recipient_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,comment_id TEXT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,created_at TEXT NOT NULL,read_at TEXT,UNIQUE(recipient_id,comment_id));
        INSERT INTO notifications(id,recipient_id,comment_id,created_at,read_at) VALUES(42,?,?,?,?)`, a, root, nowFixed(), nowFixed()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateNotifications(s.db); err != nil {
			t.Fatal(err)
		}
		var kind, p string
		var read sql.NullString
		if err := s.db.QueryRow(`SELECT kind,page_id,read_at FROM notifications WHERE id=42`).Scan(&kind, &p, &read); err != nil || kind != "comment_reply" || p != page || !read.Valid {
			t.Fatal("migration lost data", kind, p, read, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO notifications(kind,recipient_id,page_id,created_at) VALUES('page_mention',?,?,?)`, a, page, nowFixed()); err != nil {
		t.Fatal("another kind requires a comment", err)
	}
}
