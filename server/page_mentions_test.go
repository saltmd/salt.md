package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func mentionDocument(pageID, authorID, recipientID, occurrence string) []any {
	return []any{map[string]any{"id": "paragraph-one", "type": "paragraph", "props": map[string]any{}, "children": []any{}, "content": []any{
		map[string]any{"type": "text", "text": "Please review ", "styles": map[string]any{}},
		map[string]any{"type": "userMention", "props": map[string]any{"userId": recipientID, "label": "Test", "mentionId": occurrence, "authorId": authorID, "originPageId": pageID}},
	}}}
}

func TestPageMentionsCollaborativeSaveAndUndo(t *testing.T) {
	s := testServer(t)
	author, ac := signedIn(t, s, "pageauthor@example.test")
	recipient, rc := signedIn(t, s, "pagerecipient@example.test")
	peer, pc := signedIn(t, s, "pagepeer@example.test")
	ws := s.firstWorkspaceOf(t, author)
	for _, id := range []string{recipient, peer} {
		if _, err := s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, id); err != nil {
			t.Fatal(err)
		}
	}
	page := s.makePage(t, ws, author, "", "Document", "{}")
	content := mentionDocument(page, author, recipient, "occurrence-one")
	endpoint := "/api/pages/" + page + "?materialize=1"
	save := func(cookie string, doc any) {
		mentionRequest(t, s, cookie, "PATCH", endpoint, map[string]any{"content": doc}, 200)
	}
	// The remote collaborator may materialize a shared edit first. They must not
	// become the sender, consume the occurrence, or generate a notification.
	save(pc, content)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("peer sent another person's mention")
	}
	save(ac, content)
	save(pc, content)
	save(ac, content)
	items, unread := mentionInbox(t, s, rc)
	if len(items) != 1 || unread != 1 || items[0].MentionID != "occurrence-one" || items[0].CommentID != "" || !strings.Contains(items[0].Body, "Test") {
		t.Fatalf("wrong page notification: %+v %d", items, unread)
	}
	var storedAuthor string
	s.db.QueryRow(`SELECT author_id FROM page_mention_notifications WHERE id=?`, items[0].ID).Scan(&storedAuthor)
	if storedAuthor != author {
		t.Fatal("wrong author")
	}
	mentionRequest(t, s, pc, "POST", "/api/notifications/"+items[0].ID+"/read", nil, 404)
	mentionRequest(t, s, rc, "POST", "/api/notifications/"+items[0].ID+"/read", nil, 200)
	// Deleting then undoing the mention restores the original, already-read item.
	save(ac, []any{})
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("removed mention still visible")
	}
	save(ac, content)
	if list, n := mentionInbox(t, s, rc); len(list) != 1 || n != 0 || list[0].ID != items[0].ID {
		t.Fatal("undo notified again")
	}
	// A genuinely new occurrence may notify again.
	save(ac, mentionDocument(page, author, recipient, "occurrence-two"))
	if list, n := mentionInbox(t, s, rc); len(list) != 1 || n != 1 {
		t.Fatal("new occurrence did not notify")
	}
	// Access is rechecked at delivery and on every read.
	s.db.Exec(`UPDATE pages SET visibility='private' WHERE id=?`, page)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("private page leaked")
	}
	save(ac, mentionDocument(page, author, recipient, "private-occurrence"))
	s.db.Exec(`UPDATE pages SET visibility='workspace' WHERE id=?`, page)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("ineligible mention queued a notification")
	}
	// Copying an existing tag to another page is not a fresh notification.
	copyPage := s.makePage(t, ws, author, "", "Copy", "{}")
	mentionRequest(t, s, ac, "PATCH", "/api/pages/"+copyPage, map[string]any{"content": content}, 200)
	if list, n := mentionInbox(t, s, rc); len(list) != 0 || n != 0 {
		t.Fatal("copy sent a notification")
	}
	// Plain text and a self mention never create a notification.
	save(ac, mentionDocument(page, author, author, "self"))
	if list, n := mentionInbox(t, s, ac); len(list) != 0 || n != 0 {
		t.Fatal("self notified")
	}
}

func TestPageMentionExportsAndNestedContent(t *testing.T) {
	content := mentionDocument("page", "author", "recipient", "one")
	raw, _ := json.Marshal(content)
	if !strings.Contains(extractText(raw), "Test") {
		t.Fatal("mention missing from search")
	}
	block := content[0].(map[string]any)
	inline, _ := json.Marshal(block["content"])
	if !strings.Contains(renderInline(inline), "@Test") || !strings.Contains(inlineHTML(inline), "@Test") || !strings.Contains(plainInline(inline), "@Test") {
		t.Fatal("mention missing from export")
	}
	wrapped, _ := json.Marshal([]any{map[string]any{"id": "parent", "type": "bulletListItem", "children": content}})
	if got := extractPageMentions(wrapped); len(got) != 1 || got[0].ID != "one" {
		t.Fatalf("nested mention lost: %+v", got)
	}
	table, _ := json.Marshal([]any{map[string]any{"id": "table", "content": map[string]any{"type": "tableContent", "rows": []any{map[string]any{"cells": []any{block["content"]}}}}}})
	if got := extractPageMentions(table); len(got) != 1 || !strings.Contains(got[0].Excerpt, "Test") {
		t.Fatal("table mention lost")
	}
	malicious := json.RawMessage(`[{"type":"userMention","props":{"label":"<script>alert(1)</script>"}}]`)
	if strings.Contains(inlineHTML(malicious), "<script>") {
		t.Fatal("mention HTML not escaped")
	}
}
