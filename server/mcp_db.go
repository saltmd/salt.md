package server

import (
	"strings"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Database-oriented MCP tools (Welle 9): they let an agent actually operate a
// salt.md database — inspect its schema, query rows, write typed properties,
// create a database, move pages, and resolve people. All go through the same
// workspace/ACL guards as the REST UI (checked by the caller in mcpCall).

// mcpGetSchema returns a collection's property schema so an agent knows the
// property ids, types and select-option ids before writing values.
func (s *Server) mcpGetSchema(pageID string) (string, error) {
	var schema string
	err := s.db.QueryRow(`SELECT schema FROM collections WHERE page_id = ?`, pageID).Scan(&schema)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("page %q is not a database", pageID)
	}
	if err != nil {
		return "", err
	}
	// schema is already a JSON array of property definitions.
	return schema, nil
}

// mcpQueryRows filters/sorts/paginates a database's rows server-side and returns
// compact JSON including computed rollup/formula values.
func (s *Server) mcpQueryRows(u *user, pageID string, filters []rowFilter, sort string, limit, offset int) (string, error) {
	var isCollection int
	if s.db.QueryRow(`SELECT COUNT(*) FROM collections WHERE page_id = ?`, pageID).Scan(&isCollection); isCollection == 0 {
		return "", fmt.Errorf("page %q is not a database", pageID)
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	// Filter values may arrive as an option NAME as well (see set_properties).
	filters = s.resolveFilterValues(pageID, filters)
	rows, total, err := s.collectionRowsQuery(u, pageID, filters, sort, limit, offset)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(map[string]any{"rows": rows, "total": total, "offset": offset, "limit": limit})
	if err != nil {
		return "", err
	}
	// Row content is user data — mark it untrusted like get_page/search do.
	return wrapUntrusted(string(out)), nil
}

// mcpSetProperties field-level-merges the given property values into a row's
// props, mirroring the REST propsPatch semantics (null clears a key).
func (s *Server) mcpSetProperties(pageID string, properties json.RawMessage, actor *user) (string, error) {
	if len(properties) == 0 {
		return "", fmt.Errorf("properties is required")
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(properties, &patch); err != nil {
		return "", fmt.Errorf("properties must be a JSON object")
	}
	// Map select values that arrive as a NAME onto the id, and wrap a single
	// value written to a list-shaped property into a list.
	// MUST come before tx.Begin(): the pool holds exactly ONE connection, so a
	// query inside the open transaction would block forever.
	s.normalizePropValues(pageID, patch)

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var current string
	var trashed sql.NullString
	if err := tx.QueryRow(`SELECT props, trashed_at FROM pages WHERE id = ?`, pageID).Scan(&current, &trashed); err != nil {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	if trashed.Valid {
		return "", fmt.Errorf("page %q is in the trash", pageID)
	}
	merged := map[string]json.RawMessage{}
	json.Unmarshal([]byte(current), &merged)
	// The before/after of every property this call touches. Recorded so the
	// change can be taken back later — a log that only says "2 properties" tells
	// you that something happened and leaves you no way to undo it.
	diff := map[string]propChange{}
	changed := make([]string, 0, len(patch))
	for k, v := range patch {
		before := "null"
		if prev, ok := merged[k]; ok {
			before = string(prev)
		}
		diff[k] = propChange{From: json.RawMessage(before), To: json.RawMessage(v)}
		if string(v) == "null" {
			delete(merged, k)
		} else {
			merged[k] = v
		}
		changed = append(changed, k)
	}
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE pages SET props = ?, updated_at = ? WHERE id = ?`, string(mergedJSON), now(), pageID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.reindexPage(pageID)
	s.pagesChanged()
	// An open board must show the move without a reload — that is the whole
	// point of an agent working while somebody watches.
	s.rowChanged(pageID)

	// Its own audit entry, carrying the diff — the generic one in the MCP
	// dispatch has no way to know what changed. set_properties is excluded
	// there, the same way working_on and note are.
	if actor != nil {
		title := s.pageTitle(pageID)
		if blob, err := json.Marshal(diff); err == nil {
			s.auditChanges("agent", actor.ID, actor.Name+" (MCP)", "set_properties", pageID,
				s.pageWorkspace(pageID),
				fmt.Sprintf("%s — %s", title, strings.Join(changed, ", ")), string(blob))
		}
	}
	return fmt.Sprintf("Set %d propert%s on row %s", len(changed), plural(len(changed), "y", "ies"), pageID), nil
}

// mcpCreateDatabase creates a collection page with the given (or default) schema.
func (s *Server) mcpCreateDatabase(u *user, title, parentID, wsID string, schema json.RawMessage) (string, error) {
	userID := u.ID
	if title == "" {
		return "", fmt.Errorf("title is required")
	}
	schemaStr := defaultSchema
	if len(schema) > 0 {
		if !json.Valid(schema) {
			return "", fmt.Errorf("schema is not valid JSON")
		}
		var defs []map[string]any
		if err := json.Unmarshal(schema, &defs); err != nil {
			return "", fmt.Errorf("schema must be an array of property definitions, e.g. [{\"name\":\"Status\",\"type\":\"select\",\"options\":[\"To do\",\"Done\"]}]")
		}
		// The same normalisation as update_schema: options may arrive as plain
		// strings. Stored unchecked, they froze the interface the moment the
		// database was opened.
		defs, err := normalizeSchema(defs)
		if err != nil {
			return "", err
		}
		b, err := json.Marshal(defs)
		if err != nil {
			return "", err
		}
		schemaStr = string(b)
	}

	var parent *string
	var workspaceID string
	if parentID != "" {
		var pws string
		var trashed sql.NullString
		if err := s.db.QueryRow(`SELECT workspace_id, trashed_at FROM pages WHERE id = ?`, parentID).Scan(&pws, &trashed); err != nil || trashed.Valid {
			return "", fmt.Errorf("parent page %q not found", parentID)
		}
		parent = &parentID
		workspaceID = pws
	} else {
		var err error
		// With no parent page the caller decides the workspace. Before this,
		// everything landed silently in the default workspace — an agent created
		// page and database in the wrong place and could not even see that.
		workspaceID, err = s.mcpCreateWorkspaceTarget(u, wsID)
		if err != nil {
			return "", err
		}
	}

	id := newID()
	ts := now()
	var pos float64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id IS ?`, parent).Scan(&pos); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, type, props, workspace_id, owner_id, visibility) VALUES (?, ?, ?, '', '[]', ?, ?, ?, 'collection', '{}', ?, ?, 'workspace')`,
		id, parent, title, pos, ts, ts, workspaceID, userID); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)`, id, schemaStr, defaultViews); err != nil {
		return "", err
	}
	s.reindexPage(id)
	s.pagesChanged()
	return fmt.Sprintf("Created database %q with id %s", title, id), nil
}

// mcpMovePageOrWorkspace is the move half of update_page, which absorbed the
// former move_page tool. A target workspace takes precedence: that is a move of
// the whole subtree, not a re-parenting inside the same one.
func (s *Server) mcpMovePageOrWorkspace(u *user, pageID, parentID, wsID string) (string, error) {
	if wsID != "" {
		return s.mcpMoveToWorkspace(u, pageID, wsID)
	}
	return s.mcpMovePage(u.ID, pageID, parentID)
}

// mcpMovePage reparents a page (empty parentID = top level) with a cycle guard.
// IMPORTANT: with SetMaxOpenConns(1) an open tx holds the only connection, so
// all access checks that query via s.db MUST happen BEFORE tx.Begin — calling
// s.canWrite inside the tx would deadlock the whole server.
func (s *Server) mcpMovePage(userID, pageID, parentID string) (string, error) {
	if parentID != "" {
		if parentID == pageID {
			return "", fmt.Errorf("a page cannot be its own parent")
		}
		if !s.canWrite(userID, parentID) {
			return "", fmt.Errorf("parent page %q not found", parentID)
		}
		// The same rule as in the REST handler: re-parent only inside your own
		// workspace. Missing here, the boundary would be walkable through an
		// agent — and the mixed tree that results (a page in A under a parent in
		// B) defeats visibility checks, which read the workspace off whichever
		// page they are looking at.
		if s.pageWorkspace(parentID) != s.pageWorkspace(pageID) {
			return "", fmt.Errorf("a page can only be re-parented within its own workspace")
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if parentID == "" {
		if _, err := tx.Exec(`UPDATE pages SET parent_id = NULL, updated_at = ? WHERE id = ?`, now(), pageID); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		s.pagesChanged()
		return fmt.Sprintf("Moved page %s to the top level", pageID), nil
	}
	within, err := isWithinSubtree(tx, pageID, parentID)
	if err != nil {
		return "", err
	}
	if within {
		return "", fmt.Errorf("cannot move a page into its own subtree")
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ? AND trashed_at IS NULL`, parentID).Scan(&exists); err != nil || exists == 0 {
		return "", fmt.Errorf("parent page %q not found", parentID)
	}
	if _, err := tx.Exec(`UPDATE pages SET parent_id = ?, updated_at = ? WHERE id = ?`, parentID, now(), pageID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.pagesChanged()
	return fmt.Sprintf("Moved page %s under %s", pageID, parentID), nil
}

// mcpListUsers returns people who share at least one workspace with the caller.
//
// The list follows the limit of the TOKEN, not only that of the account: a
// token narrowed to a single workspace used to hand out the names and e-mail
// addresses of every colleague anyway, because the query read the user's
// memberships directly.
func (s *Server) mcpListUsers(u *user) (string, error) {
	ws := s.scopeWorkspacesFor(u, s.visibleWorkspaces(u.ID))
	if len(ws) == 0 {
		return "[]", nil
	}
	args := make([]any, 0, len(ws))
	for _, w := range ws {
		args = append(args, w)
	}
	rows, err := s.db.Query(`
		SELECT DISTINCT u.id, u.name, u.email FROM users u
		JOIN workspace_members m ON m.user_id = u.id
		WHERE m.workspace_id IN (`+placeholders(len(ws))+`)
		ORDER BY u.name`, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	type person struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	people := []person{}
	for rows.Next() {
		var p person
		if rows.Scan(&p.ID, &p.Name, &p.Email) == nil {
			people = append(people, p)
		}
	}
	out, err := json.Marshal(people)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
