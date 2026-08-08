package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Steps 3 to 10 of the tool consolidation: pairs and triples that were separate
// catalogue entries for the same object, bundled by OBJECT rather than by verb.
//
// The rule that stops this going too far: a tool must still be able to say what
// it does in one sentence. One salt(action, …) would save the most entries and
// help the least, because the schema would stop describing anything.
//
// The second rule: destroying stays standalone. delete_comment and delete_view
// are not folded into their action enums — deleting should be a deliberate
// choice of tool, not a value an agent can land on by mistake.

// --- write_content ----------------------------------------------------------

// mcpWriteContent replaces append_markdown, prepend_markdown and
// replace_content. The default is append, because it is the only one of the
// three that cannot destroy anything: a missing mode should do the harmless
// thing.
func (s *Server) mcpWriteContent(u *user, pageID, markdown, mode string) (string, error) {
	switch mode {
	case "", "append":
		if err := s.appendMarkdownToPage(pageID, markdown); err != nil {
			return "", err
		}
		return fmt.Sprintf("Appended content to page %s", pageID), nil
	case "prepend":
		return s.mcpPrependMarkdown(u, pageID, markdown)
	case "replace":
		return s.mcpReplaceContent(u, pageID, markdown)
	}
	return "", fmt.Errorf("unknown mode %q — use append (the default), prepend or replace", mode)
}

// --- revisions --------------------------------------------------------------

// mcpRevisions replaces get_page_history, get_revision and restore_revision.
// Restoring is in here rather than standalone because it does not destroy
// anything: it saves the current state as a new revision first, so the restore
// is itself reversible.
func (s *Server) mcpRevisions(u *user, pageID, action, revisionID string, limit int) (string, error) {
	switch action {
	case "", "list":
		out, err := s.mcpPageHistory(pageID, limit)
		if err != nil {
			return "", err
		}
		return wrapUntrusted(out), nil
	case "get":
		if revisionID == "" {
			return "", fmt.Errorf("revision_id is required for action=get — call action=list for the ids")
		}
		out, err := s.mcpGetRevision(pageID, revisionID)
		if err != nil {
			return "", err
		}
		return wrapUntrusted(out), nil
	case "restore":
		if revisionID == "" {
			return "", fmt.Errorf("revision_id is required for action=restore — call action=list for the ids")
		}
		return s.mcpRestoreRevision(u, pageID, revisionID)
	}
	return "", fmt.Errorf("unknown action %q — use list (the default), get or restore", action)
}

// --- comments ---------------------------------------------------------------

// mcpComments replaces get_comments, add_comment and resolve_comment.
// delete_comment stays its own tool: see the note at the top of this file.
func (s *Server) mcpComments(u *user, pageID, action, body, blockID, commentID string, resolved *bool) (string, error) {
	switch action {
	case "", "list":
		list, err := s.pageComments(pageID)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(list)
		if err != nil {
			return "", err
		}
		return wrapUntrusted(string(out)), nil
	case "add":
		if strings.TrimSpace(body) == "" {
			return "", fmt.Errorf("body is required for action=add")
		}
		id := newID()
		if _, err := s.db.Exec(`INSERT INTO comments (id, page_id, block_id, author_id, author_name, body, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, pageID, blockID, u.ID, u.Name, strings.TrimSpace(body), now()); err != nil {
			return "", err
		}
		return "Added comment " + id, nil
	case "resolve", "reopen":
		if commentID == "" {
			return "", fmt.Errorf("comment_id is required for action=%s", action)
		}
		// The permission check cannot ride on page_id here: the caller names a
		// comment. Without resolving it to its page, a guessed comment id would
		// write into somebody else's workspace.
		pid, ok := s.commentPage(commentID)
		if !ok || !s.canWrite(u.ID, pid) {
			return "", fmt.Errorf("comment %q not found", commentID)
		}
		want := action == "resolve"
		if resolved != nil {
			want = *resolved
		}
		return s.mcpResolveComment(commentID, want)
	}
	return "", fmt.Errorf("unknown action %q — use list (the default), add, resolve or reopen; deleting has its own tool", action)
}

// --- sharing ----------------------------------------------------------------

// mcpSetSharing replaces share_page and unshare_page. One boolean rather than
// two tools: "is this page public" is one piece of state.
func (s *Server) mcpSetSharing(base requestBase, pageID string, public *bool, expiresInDays int, password string) (string, error) {
	if public == nil {
		return "", fmt.Errorf("public is required (true to create a public link, false to revoke it)")
	}
	if *public {
		return s.mcpSharePage(base, pageID, expiresInDays, password)
	}
	return s.mcpUnsharePage(pageID)
}

// --- trash ------------------------------------------------------------------

// mcpSetTrashed replaces trash_page and restore_page. Both directions are
// reversible — the trash keeps the page — so this is state, not destruction,
// and belongs in one tool.
func (s *Server) mcpSetTrashed(pageID string, trashed *bool) (string, error) {
	if trashed == nil {
		return "", fmt.Errorf("trashed is required (true to move to the trash, false to restore)")
	}
	if !*trashed {
		return s.mcpRestorePage(pageID)
	}
	ids, err := subtreeIDs(s.db, pageID)
	if err != nil || len(ids) == 0 {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	idArgs := make([]any, len(ids))
	for i, v := range ids {
		idArgs[i] = v
	}
	ts := now()
	if _, err := s.db.Exec(`UPDATE pages SET trashed_at = ?, updated_at = ? WHERE id IN (`+placeholders(len(ids))+`) AND trashed_at IS NULL`,
		append([]any{ts, ts}, idArgs...)...); err != nil {
		return "", err
	}
	s.db.Exec(`DELETE FROM pages_fts WHERE id IN (`+placeholders(len(ids))+`)`, idArgs...)
	for _, pid := range ids {
		s.collab.reset(pid)
	}
	s.pagesChanged()
	return fmt.Sprintf("Moved page %s (and %d sub-pages) to trash", pageID, len(ids)-1), nil
}

// --- links ------------------------------------------------------------------

// mcpGetLinks replaces get_backlinks and get_graph. Backlinks are the graph
// narrowed to one page, so they are one tool with an optional page_id rather
// than two tools that answer the same question at different zoom levels.
func (s *Server) mcpGetLinks(u *user, pageID, wsID string, kinds []string, includeNodes bool) (string, error) {
	if pageID != "" {
		out, err := s.mcpBacklinks(u.ID, pageID)
		if err != nil {
			return "", err
		}
		return wrapUntrusted(out), nil
	}
	out, err := s.mcpGraph(u, wsID, kinds, includeNodes)
	if err != nil {
		return "", err
	}
	return wrapUntrusted(out), nil
}

// --- workspace --------------------------------------------------------------

// mcpWorkspace creates a workspace, or renames one when given an id. Renaming
// was simply missing: an agent could make a workspace and then never correct
// the name it had chosen.
//
// The guards are the ones the interface applies, deliberately repeated rather
// than referenced: workspace admins only, a name that is not blank, and an icon
// of at most a couple of characters — the field is for an emoji, not for text
// somebody smuggles into the sidebar.
func (s *Server) mcpWorkspace(u *user, wsID, name, icon, from string) (string, error) {
	if wsID == "" {
		if narrowedToWorkspaces(u) {
			return "", fmt.Errorf("this connection is limited to particular workspaces, so it cannot create new ones — it would not be able to open them")
		}
		if icon != "" {
			return "", fmt.Errorf("pass workspace_id to set an icon — a new workspace is created with a name only")
		}
		// from = "make one like that one". The source workspace IS the blueprint;
		// there is no stored template to drift away from it.
		if from != "" {
			return s.blueprintWorkspace(u, name, from)
		}
		return s.mcpCreateWorkspace(u.ID, name)
	}
	if !s.isMember(u.ID, wsID) || !s.credentialMayEnter(u, wsID) {
		return "", fmt.Errorf("workspace %q not found", wsID)
	}
	if !s.isWorkspaceAdmin(u.ID, wsID) {
		return "", fmt.Errorf("only a workspace admin can change %q", wsID)
	}
	var sets []string
	var args []any
	if name != "" {
		n := strings.TrimSpace(name)
		if n == "" {
			return "", fmt.Errorf("name cannot be blank")
		}
		if len([]rune(n)) > 80 {
			return "", fmt.Errorf("name is too long (80 characters at most)")
		}
		sets = append(sets, "name = ?")
		args = append(args, n)
	}
	if icon != "" {
		ic := strings.TrimSpace(icon)
		if r := []rune(ic); len(r) > 8 { // an emoji or two, not arbitrary text
			ic = string(r[:8])
		}
		sets = append(sets, "icon = ?")
		args = append(args, ic)
	}
	if len(sets) == 0 {
		return "", fmt.Errorf("nothing to change: pass name or icon")
	}
	args = append(args, wsID)
	if _, err := s.db.Exec(`UPDATE workspaces SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...); err != nil {
		return "", err
	}
	s.pagesChanged()
	return fmt.Sprintf("Updated workspace %s", wsID), nil
}
