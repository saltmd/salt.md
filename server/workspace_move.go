package server

import (
	"fmt"
	"strings"
)

// Moving pages between workspaces.
//
// This was missing EVERYWHERE — interface, REST and MCP. `workspaceId` was only
// read when creating a page; whoever had made one in the wrong workspace could
// never get it to where it belonged. It surfaced when an agent created a
// database in the default workspace and afterwards nobody could move it.
//
// Why this is more than an `UPDATE workspace_id`:
//   • The WHOLE subtree has to come along, or the rows of a database sit in a
//     different workspace than the database itself.
//   • The parent link has to go: the previous parent stays behind in the old
//     workspace, and a child there would be orphaned out of reach.
//   • Permissions on BOTH sides: allowed to write on the page, and more than a
//     reader in the target workspace.
// Untouched: the Yjs document, comments, versions, favourites and share links —
// they all hang off the page id, and that does not change.

// moveSubtreeToWorkspace moves pageID and its subtree into the target
// workspace. tokenOK reports whether a workspace-restricted API token is
// allowed to reach the target. The caller passes that check in, because only
// the user id arrives here while the restriction hangs off the token.
func (s *Server) moveSubtreeToWorkspace(userID, pageID, targetWS string, tokenOK func(string) bool) (int, error) {
	var curWS string
	var title string
	if err := s.db.QueryRow(`SELECT workspace_id, title FROM pages WHERE id = ? AND trashed_at IS NULL`, pageID).Scan(&curWS, &title); err != nil {
		return 0, fmt.Errorf("page %q not found", pageID)
	}
	if curWS == targetWS {
		return 0, fmt.Errorf("page %q is already in that workspace", title)
	}
	if !s.isMember(userID, targetWS) {
		return 0, fmt.Errorf("workspace %q not found", targetWS)
	}
	if !tokenOK(targetWS) {
		return 0, fmt.Errorf("workspace %q not found", targetWS)
	}
	if role := s.workspaceRole(userID, targetWS); role == "viewer" {
		return 0, fmt.Errorf("you are a viewer in the target workspace and cannot add pages there")
	}

	ids, err := subtreeIDs(s.db, pageID)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		ids = []string{pageID}
	}
	// Move nothing the mover is not allowed to see. The subtree can hold other
	// people's private pages; in the target workspace they may well be an admin,
	// and workspace admins see private pages. The move would then be a way to
	// make somebody else's notes readable. Leaving them behind is no option
	// either — those pages would hang off a parent in another workspace. So
	// stop and say why.
	for _, id := range ids {
		if !s.canRead(userID, id) {
			return 0, fmt.Errorf("this subtree contains private pages owned by someone else — they cannot be moved along")
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// The root loses its parent — that one stays in the old workspace. Without
	// this the subtree would hang off a parent nobody in the target workspace
	// can see, and would be unfindable in the sidebar.
	var pos float64
	tx.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id IS NULL AND workspace_id = ?`, targetWS).Scan(&pos)
	if _, err := tx.Exec(`UPDATE pages SET parent_id = NULL, position = ?, updated_at = ? WHERE id = ?`,
		pos, now(), pageID); err != nil {
		return 0, err
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+2)
	args = append(args, targetWS, now())
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := tx.Exec(`UPDATE pages SET workspace_id = ?, updated_at = ? WHERE id IN (`+ph+`)`, args...); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.reindexPage(id)
	}
	s.pagesChanged()
	return len(ids), nil
}

// mcpCreateWorkspace creates a workspace; the caller becomes its admin.
func (s *Server) mcpCreateWorkspace(userID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	// The same gate as in the REST handler: an agent should not walk past a
	// boundary that is set for a human in the interface.
	if u := s.userByID(userID); (u == nil || !u.IsAdmin) && !s.loadSettings().AllowUserWorkspaces {
		return "", fmt.Errorf("creating workspaces is disabled on this instance")
	}
	if len([]rune(name)) > 80 {
		return "", fmt.Errorf("name is too long")
	}
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id) VALUES (?, ?, ?, ?)`, id, name, now(), userID); err != nil {
		return "", err
	}
	// Like the REST handler: workspace_members has no created_at.
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`,
		id, userID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created workspace %q with id %s — you are its admin. Use move_page with workspace_id to move existing pages into it. "+
		"It has no rules yet: consider drafting working conventions with the user and submitting them via propose_workspace_rules — an admin applies them in the browser.", name, id), nil
}

// mcpMoveToWorkspace is the MCP facade of the move.
func (s *Server) mcpMoveToWorkspace(u *user, pageID, targetWS string) (string, error) {
	userID := u.ID
	n, err := s.moveSubtreeToWorkspace(userID, pageID, targetWS, u.tokenCanReach)
	if err != nil {
		return "", err
	}
	sub := ""
	if n > 1 {
		sub = fmt.Sprintf(" together with %d sub-page(s)", n-1)
	}
	return fmt.Sprintf("Moved page %s%s to workspace %s. It now sits at the top level there — its previous parent stayed behind.", pageID, sub, targetWS), nil
}
