package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func mentionRequest(t *testing.T, s *Server, cookie, method, path string, body any, status int) []byte {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != status {
		t.Fatalf("%s %s: %d %s (want %d)", method, path, rec.Code, rec.Body.String(), status)
	}
	return rec.Body.Bytes()
}
func mentionInbox(t *testing.T, s *Server, cookie string) ([]mentionNotification, int) {
	t.Helper()
	var result struct {
		Items  []mentionNotification `json:"items"`
		Unread int                   `json:"unread"`
	}
	if err := json.Unmarshal(mentionRequest(t, s, cookie, "GET", "/api/notifications", nil, 200), &result); err != nil {
		t.Fatal(err)
	}
	return result.Items, result.Unread
}
func TestCommentMentionsDeliveryAndAccess(t *testing.T) {
	s := testServer(t)
	author, ac := signedIn(t, s, "author@example.test")
	recipient, rc := signedIn(t, s, "recipient@example.test")
	outsider, oc := signedIn(t, s, "outsider@example.test")
	ws := s.firstWorkspaceOf(t, author)
	if _, err := s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, recipient); err != nil {
		t.Fatal(err)
	}
	page := s.makePage(t, ws, author, "", "Secret project", "{}")
	path := "/api/pages/" + page + "/comments"
	body := map[string]any{"body": "👋 @Test and @Test", "requestId": "send-1", "mentions": []commentMention{{recipient, 3, 8, "Test"}, {recipient, 13, 18, "Test"}}}
	first := mentionRequest(t, s, ac, "POST", path, body, 200)
	retry := mentionRequest(t, s, ac, "POST", path, body, 200)
	if string(first) != string(retry) {
		t.Fatal("retry created a new comment")
	}
	var id struct {
		ID string `json:"id"`
	}
	json.Unmarshal(first, &id)
	items, unread := mentionInbox(t, s, rc)
	if len(items) != 1 || unread != 1 || items[0].CommentID != id.ID || items[0].PageID != page {
		t.Fatalf("wrong inbox: %+v %d", items, unread)
	}
	if list, n := mentionInbox(t, s, ac); len(list) != 0 || n != 0 {
		t.Fatal("author got a notification")
	}
	if list, n := mentionInbox(t, s, oc); len(list) != 0 || n != 0 {
		t.Fatal("outsider got a notification")
	}
	mentionRequest(t, s, oc, "POST", "/api/notifications/"+items[0].ID+"/read", nil, 404)
	mentionRequest(t, s, rc, "POST", "/api/notifications/"+items[0].ID+"/read", nil, 200)
	if list, n := mentionInbox(t, s, rc); n != 0 || !list[0].Read {
		t.Fatal("read state not persisted")
	}
	body["body"] = "changed"
	mentionRequest(t, s, ac, "POST", path, body, 409)
	// Plain @text does not notify anyone.
	mentionRequest(t, s, ac, "POST", path, map[string]any{"body": "@Test"}, 200)
	if list, _ := mentionInbox(t, s, rc); len(list) != 1 {
		t.Fatal("plain text notified")
	}
	// Private ancestor revokes both suggestions and notification contents.
	parent := s.makePage(t, ws, author, "", "Private", "{}")
	s.db.Exec(`UPDATE pages SET visibility='private' WHERE id=?`, parent)
	s.db.Exec(`UPDATE pages SET parent_id=? WHERE id=?`, parent, page)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("private content leaked")
	}
	candidates := string(mentionRequest(t, s, ac, "GET", "/api/pages/"+page+"/mention-people", nil, 200))
	if strings.Contains(candidates, recipient) || strings.Contains(candidates, outsider) {
		t.Fatal("ineligible candidate leaked")
	}
	body["body"] = "@Test"
	body["requestId"] = "send-2"
	body["mentions"] = []commentMention{{recipient, 0, 5, "Test"}}
	mentionRequest(t, s, ac, "POST", path, body, 400)
	s.db.Exec(`UPDATE pages SET parent_id=NULL WHERE id=?`, page)
	s.db.Exec(`UPDATE users SET disabled=1 WHERE id=?`, recipient)
	mentionRequest(t, s, ac, "POST", path, body, 400)
	s.db.Exec(`UPDATE users SET disabled=0 WHERE id=?`, recipient)
	// Removing membership hides historical notifications too.
	s.db.Exec(`DELETE FROM workspace_members WHERE workspace_id=? AND user_id=?`, ws, recipient)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("revoked member saw notification")
	}
	s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, recipient)
	mentionRequest(t, s, ac, "DELETE", "/api/comments/"+id.ID, nil, 200)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("deleted comment left notification")
	}
}
func TestCommentMentionsValidationAndSelf(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "self@example.test")
	page := s.makePage(t, s.firstWorkspaceOf(t, uid), uid, "", "Notes", "{}")
	path := "/api/pages/" + page + "/comments"
	for _, m := range []commentMention{{uid, -1, 5, "Test"}, {uid, 0, 100, "Test"}, {uid, 0, 4, "Test"}, {"missing", 0, 5, "Test"}, {uid, 0, 5, "Fake"}} {
		mentionRequest(t, s, cookie, "POST", path, map[string]any{"body": "@Test", "mentions": []commentMention{m}}, 400)
	}
	mentionRequest(t, s, cookie, "POST", path, map[string]any{"body": "@Test", "mentions": []commentMention{{uid, 0, 5, "Test"}, {uid, 0, 5, "Test"}}}, 400)
	mentionRequest(t, s, cookie, "POST", path, map[string]any{"body": "@Test", "mentions": []commentMention{{uid, 0, 5, "Test"}}}, 200)
	if list, n := mentionInbox(t, s, cookie); len(list) != 0 || n != 0 {
		t.Fatal("self mention notified")
	}
	comments, err := s.pageComments(page)
	if err != nil || len(comments) != 1 || len(comments[0].Mentions) != 1 {
		t.Fatalf("invalid comments: %+v %v", comments, err)
	}
}

func TestMentionMigrationPreservesOldComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "salt.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO pages(id,title,created_at,updated_at) VALUES('old-page','Old page','2020','2020')`,
		`INSERT INTO comments(id,page_id,body,created_at) VALUES('old-comment','old-page','Existing @Test text','2020')`,
		`DROP INDEX idx_comment_request`,
		`ALTER TABLE comments DROP COLUMN request_id`,
		`ALTER TABLE comments DROP COLUMN mentions`,
		`DROP TABLE mention_notifications`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	for i := 0; i < 2; i++ {
		db, err = openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		var body, mentions string
		if err = db.QueryRow(`SELECT body,mentions FROM comments WHERE id='old-comment'`).Scan(&body, &mentions); err != nil {
			t.Fatal(err)
		}
		if body != "Existing @Test text" || mentions != "[]" {
			t.Fatal("migration changed an old comment")
		}
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM mention_notifications`).Scan(&count); err != nil || count != 0 {
			t.Fatal("migration created notifications")
		}
		db.Close()
	}
}
