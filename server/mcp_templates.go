package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Template tools for agents (W115).
//
// The interface had templates for a long time and MCP had no idea: an agent
// asked to "set up a project like the others" rebuilt the structure from
// scratch every time, next to a prepared snapshot nobody told it about.
//
// A template is a snapshot, not a link (see duplicatePage's asTemplate): using
// one copies it and the two never affect each other again. That is the single
// fact these descriptions have to carry, because an agent that believes the
// copy stays attached will avoid editing it.

// mcpListTemplates lists the templates the caller may actually read. Same two
// stages as everywhere else: the token's workspaces first, then canRead per
// hit, which is what keeps somebody else's private template out of the list.
func (s *Server) mcpListTemplates(u *user) (string, error) {
	ws := s.scopeWorkspacesFor(u, s.visibleWorkspaces(u.ID))
	if len(ws) == 0 {
		return "[]", nil
	}
	wargs := make([]any, len(ws))
	for i, v := range ws {
		wargs[i] = v
	}
	rows, err := s.db.Query(`SELECT id, title, icon, type, description, workspace_id
		FROM pages WHERE is_template = 1 AND trashed_at IS NULL
		AND workspace_id IN (`+placeholders(len(ws))+`) ORDER BY title`, wargs...)
	if err != nil {
		return "", err
	}
	type tpl struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Icon        string `json:"icon"`
		Kind        string `json:"kind"`
		Description string `json:"description"`
		WorkspaceID string `json:"workspace_id"`
	}
	var scanned []tpl
	for rows.Next() {
		var t tpl
		if rows.Scan(&t.ID, &t.Title, &t.Icon, &t.Kind, &t.Description, &t.WorkspaceID) == nil {
			scanned = append(scanned, t)
		}
	}
	rows.Close() // drain before per-row canRead (single DB connection)
	out := []tpl{}
	for _, t := range scanned {
		if s.canRead(u.ID, t.ID) {
			out = append(out, t)
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	// Titles and descriptions are user-authored, so they travel fenced.
	return wrapUntrusted(string(b)), nil
}

// mcpCreateFromTemplate instantiates a template. Reading it is enough — the
// copy lands where the template lives and belongs to whoever asked, so a
// read-only template can still be a starting point for everyone.
func (s *Server) mcpCreateFromTemplate(u *user, templateID, title string) (string, error) {
	if templateID == "" {
		return "", fmt.Errorf("template_id is required — call list with kind=\"templates\" for the ids")
	}
	if !s.canRead(u.ID, templateID) {
		return "", fmt.Errorf("template %q not found", templateID)
	}
	var isTemplate int
	if err := s.db.QueryRow(`SELECT is_template FROM pages WHERE id = ? AND trashed_at IS NULL`, templateID).Scan(&isTemplate); err == sql.ErrNoRows {
		return "", fmt.Errorf("template %q not found", templateID)
	} else if err != nil {
		return "", err
	}
	if isTemplate == 0 {
		return "", fmt.Errorf("page %q is not a template — call list with kind=\"templates\", or duplicate_page for an ordinary copy", templateID)
	}
	nid, err := s.duplicatePage(templateID, u.ID, true, false)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("template %q not found", templateID)
	}
	if err != nil {
		return "", err
	}
	if title != "" {
		if _, err := s.db.Exec(`UPDATE pages SET title = ?, updated_at = ? WHERE id = ?`, title, now(), nid); err != nil {
			return "", err
		}
		s.reindexPage(nid)
		s.pagesChanged()
	}
	return fmt.Sprintf("Created page %s from template %s", nid, templateID), nil
}

// mcpSaveAsTemplate snapshots a page into a template, leaving the page itself
// untouched — the same asymmetry the ⋯ menu offers.
func (s *Server) mcpSaveAsTemplate(u *user, pageID string) (string, error) {
	if pageID == "" {
		return "", fmt.Errorf("page_id is required")
	}
	if !s.canWrite(u.ID, pageID) {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	nid, err := s.duplicatePage(pageID, u.ID, false, true)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved page %s as template %s — the page itself is unchanged", pageID, nid), nil
}
