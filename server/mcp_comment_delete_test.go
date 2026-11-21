package server

import "testing"

// Who may delete a comment is the same question over MCP as in the browser:
// its author, or an admin of the workspace. delete_comment asked only whether
// the agent's person may write the page, so any member who could edit it
// could have an agent delete every other member's comments.
func TestAgentsDeleteOnlyWhatTheirPersonMay(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "owner@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	member, _ := signedIn(t, s, "member@example.test")
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'member')`, ws, member); err != nil {
		t.Fatal(err)
	}
	page := s.makePage(t, ws, owner, "", "Shared notes", `{}`)
	comment := func(author string) string {
		id := newID()
		if _, err := s.db.Exec(`INSERT INTO comments (id, page_id, block_id, author_id, author_name, body, created_at)
			VALUES (?, ?, '', ?, 'Someone', 'a remark', ?)`, id, page, author, now()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	exists := func(id string) bool {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM comments WHERE id = ?`, id).Scan(&n)
		return n == 1
	}
	agentOf := func(id string) *user {
		return &user{ID: id, Name: "Test", TokenKind: tokenKindAPI, TokenScope: "write"}
	}
	del := func(u *user, id string) error {
		_, err := callTool(t, s, u, "delete_comment", `{"comment_id":"`+id+`"}`)
		return err
	}

	owners := comment(owner)
	if err := del(agentOf(member), owners); err == nil || !exists(owners) {
		t.Errorf("a member's agent deleted somebody else's comment: %v", err)
	}
	members := comment(member)
	if err := del(agentOf(member), members); err != nil || exists(members) {
		t.Errorf("a member's agent could not delete the member's own comment: %v", err)
	}
	another := comment(member)
	if err := del(agentOf(owner), another); err != nil || exists(another) {
		t.Errorf("a workspace admin's agent could not delete a member's comment: %v", err)
	}
}
