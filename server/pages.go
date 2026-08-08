package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type pageMeta struct {
	ID          string          `json:"id"`
	ParentID    *string         `json:"parentId"`
	Title       string          `json:"title"`
	Icon        string          `json:"icon"`
	Cover       string          `json:"cover"`
	Position    float64         `json:"position"`
	UpdatedAt   string          `json:"updatedAt"`
	Trashed     bool            `json:"trashed"`
	Type        string          `json:"type"`
	Props       json.RawMessage `json:"props"`
	WorkspaceID string          `json:"workspaceId"`
	OwnerID     string          `json:"ownerId"`
	Visibility  string          `json:"visibility"`
	IsTemplate  bool            `json:"isTemplate"`
	Tags        []string        `json:"tags"`
	Description string          `json:"description"`
	Snippet     string          `json:"snippet"`
	Thumb       string          `json:"thumb"`
}

type page struct {
	pageMeta
	Content   json.RawMessage `json:"content"`
	CreatedAt string          `json:"createdAt"`
}

func scanMeta(sc interface{ Scan(...any) error }) (pageMeta, error) {
	var m pageMeta
	var trashedAt sql.NullString
	var props, tags string
	var isTemplate int
	err := sc.Scan(&m.ID, &m.ParentID, &m.Title, &m.Icon, &m.Cover, &m.Position, &m.UpdatedAt, &trashedAt, &m.Type, &props, &m.WorkspaceID, &m.OwnerID, &m.Visibility, &isTemplate, &tags, &m.Description, &m.Snippet, &m.Thumb)
	m.Trashed = trashedAt.Valid
	m.Props = json.RawMessage(props)
	m.IsTemplate = isTemplate != 0
	m.Tags = []string{}
	if tags != "" {
		json.Unmarshal([]byte(tags), &m.Tags)
	}
	return m, err
}

const pageMetaCols = `id, parent_id, title, icon, cover, position, updated_at, trashed_at, type, props, workspace_id, owner_id, visibility, is_template, tags, description, snippet, thumb`

func (s *Server) handleListPages(w http.ResponseWriter, r *http.Request) {
	// Scope to the user's workspaces (further narrowed by a workspace-scoped
	// token), then drop private subtrees they can't see.
	ws := s.scopeWorkspacesFor(requestUser(r), s.visibleWorkspaces(requestUser(r).ID))
	if len(ws) == 0 {
		writeJSON(w, []pageMeta{})
		return
	}
	args := make([]any, len(ws))
	for i, v := range ws {
		args[i] = v
	}
	// Exclude database rows (children of a collection): they can number in the
	// tens of thousands and belong in the paginated collection view, not the
	// sidebar tree. Trashed rows are still returned so the trash works.
	//
	// EXCEPT rows that carry live sub-pages (W124): without their row those
	// sub-pages have no parent in the list, and the sidebar showed them flat
	// under Documents, stripped of their context. Rows with children are the
	// rare case, so the tens-of-thousands argument above keeps holding.
	//
	// And EXCEPT a database nested inside another one. It is not a row — the
	// count argument never applied to it — but it was dropped by the same rule,
	// so the sidebar only ever saw it through the rows endpoint and drew it as
	// a row: no ⋯ menu, and therefore no way to move it back out again.
	rows, err := s.db.Query(`SELECT `+pageMetaCols+` FROM pages p
		WHERE workspace_id IN (`+placeholders(len(ws))+`)
		AND (parent_id IS NULL OR trashed_at IS NOT NULL
		     OR p.type = 'collection'
		     OR (SELECT type FROM pages parent WHERE parent.id = p.parent_id) != 'collection'
		     OR EXISTS (SELECT 1 FROM pages c WHERE c.parent_id = p.id AND c.trashed_at IS NULL))
		ORDER BY position, created_at`, args...)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []pageMeta{}
	for rows.Next() {
		m, err := scanMeta(rows)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		list = append(list, m)
	}
	writeJSON(w, s.filterReadable(requestUser(r).ID, list))
}

func (s *Server) getPage(id string) (*page, error) {
	row := s.db.QueryRow(`SELECT `+pageMetaCols+`, content, created_at FROM pages WHERE id = ?`, id)
	var p page
	var trashedAt sql.NullString
	var content, props, tags string
	var isTemplate int
	err := row.Scan(&p.ID, &p.ParentID, &p.Title, &p.Icon, &p.Cover, &p.Position, &p.UpdatedAt, &trashedAt, &p.Type, &props, &p.WorkspaceID, &p.OwnerID, &p.Visibility, &isTemplate, &tags, &p.Description, &p.Snippet, &p.Thumb, &content, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.IsTemplate = isTemplate != 0
	p.Trashed = trashedAt.Valid
	p.Props = json.RawMessage(props)
	p.Content = json.RawMessage(content)
	p.Tags = []string{}
	if tags != "" {
		json.Unmarshal([]byte(tags), &p.Tags)
	}
	return &p, nil
}

func (s *Server) handleGetPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Return 404 (not 403) for pages the user can't see, so a non-member can't
	// even distinguish "exists elsewhere" from "doesn't exist" (anti-IDOR).
	if !s.canReadReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	p, err := s.getPage(id)
	if err == sql.ErrNoRows {
		httpError(w, 404, "page not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// A database row opened as a page must show the same numbers its card shows.
	s.fillDerivedForPage(requestUser(r), p)
	writeJSON(w, p)
}

func (s *Server) handleCreatePage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentID    *string         `json:"parentId"`
		Title       string          `json:"title"`
		Type        string          `json:"type"`
		Props       json.RawMessage `json:"props"`
		WorkspaceID string          `json:"workspaceId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if len([]rune(body.Title)) > maxTitleLen {
		httpError(w, 400, "title is too long")
		return
	}
	if body.Type != "collection" {
		body.Type = "doc"
	}
	props := "{}"
	if len(body.Props) > 0 && json.Valid(body.Props) {
		props = string(body.Props)
	}
	userID := requestUser(r).ID

	// A child inherits its parent's workspace; a root page uses the requested
	// (or the user's default) workspace. Membership is enforced either way.
	var workspaceID string
	if body.ParentID != nil {
		var pws string
		var trashed sql.NullString
		if err := s.db.QueryRow(`SELECT workspace_id, trashed_at FROM pages WHERE id = ?`, *body.ParentID).Scan(&pws, &trashed); err != nil || trashed.Valid {
			httpError(w, 400, "parent page not found")
			return
		}
		if !s.canWrite(userID, *body.ParentID) {
			httpError(w, 403, "forbidden")
			return
		}
		workspaceID = pws
	} else {
		workspaceID = body.WorkspaceID
		if workspaceID == "" {
			workspaceID = s.userDefaultWorkspace(userID)
		}
		if !s.isMember(userID, workspaceID) {
			httpError(w, 403, "not a member of that workspace")
			return
		}
	}
	id := newID()
	ts := now()
	var pos float64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id IS ?`, body.ParentID).Scan(&pos); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	_, err := s.db.Exec(
		`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, type, props, workspace_id, owner_id, visibility) VALUES (?, ?, ?, '', '[]', ?, ?, ?, ?, ?, ?, ?, 'workspace')`,
		id, body.ParentID, body.Title, pos, ts, ts, body.Type, props, workspaceID, userID,
	)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if body.Type == "collection" {
		if err := s.createDefaultCollection(id); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	}
	if err := s.reindexPage(id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	p, err := s.getPage(id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.audit("human", userID, requestUser(r).Name, "create_page", id, workspaceID, body.Title)
	s.pagesChanged()
	s.rowChanged(id)
	s.fireWebhook("page.created", id)
	writeJSON(w, p)
}

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// isWithinSubtree reports whether candidate is rootID itself or one of its
// descendants. UNION (not UNION ALL) guarantees termination even if the tree
// ever ends up with a cycle.
func isWithinSubtree(q querier, rootID, candidate string) (bool, error) {
	var found int
	err := q.QueryRow(`
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM pages WHERE id = ?
			UNION
			SELECT p.id FROM pages p JOIN sub ON p.parent_id = sub.id
		) SELECT COUNT(*) FROM sub WHERE id = ?`, rootID, candidate).Scan(&found)
	return found > 0, err
}

func subtreeIDs(q querier, rootID string) ([]string, error) {
	rows, err := q.Query(`
		WITH RECURSIVE sub(id) AS (
			SELECT id FROM pages WHERE id = ?
			UNION
			SELECT p.id FROM pages p JOIN sub ON p.parent_id = sub.id
		) SELECT id FROM sub`, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// validCover restricts covers to an empty value, a same-origin upload path, or
// a plain linear/radial gradient. This blocks a stored external url() that
// would otherwise fire a request (tracking beacon) for every viewer.
func validCover(c string) bool {
	if c == "" {
		return true
	}
	if strings.HasPrefix(c, "/files/") {
		return !strings.ContainsAny(c, "()'\"") // a plain upload path, no CSS funcs
	}
	if strings.HasPrefix(c, "gradient:") {
		body := strings.ToLower(c[len("gradient:"):])
		// Only gradient functions, no url()/expression()/imports.
		if strings.Contains(body, "url(") || strings.Contains(body, "image(") ||
			strings.Contains(body, "@") || strings.Contains(body, ";") {
			return false
		}
		return strings.Contains(body, "gradient(")
	}
	return false
}

func (s *Server) handleUpdatePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canWriteReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	var body struct {
		Title       *string         `json:"title"`
		Icon        *string         `json:"icon"`
		Cover       *string         `json:"cover"`
		Content     json.RawMessage `json:"content"`
		Props       json.RawMessage `json:"props"`
		PropsPatch  json.RawMessage `json:"propsPatch"`
		ParentID    json.RawMessage `json:"parentId"`
		Position    *float64        `json:"position"`
		Visibility  *string         `json:"visibility"`
		IsTemplate  *bool           `json:"isTemplate"`
		Tags        *[]string       `json:"tags"`
		Description *string         `json:"description"`
		WorkspaceID *string         `json:"workspaceId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}

	// Moving between workspaces: its own path, because the WHOLE subtree has to
	// come along and the parent link has to go (the parent stays in the old
	// workspace). A bare UPDATE workspace_id would separate database rows from
	// their database.
	if body.WorkspaceID != nil && *body.WorkspaceID != "" {
		n, err := s.moveSubtreeToWorkspace(requestUser(r).ID, id, *body.WorkspaceID,
			func(ws string) bool { return s.tokenReachesWorkspace(r, ws) })
		if err != nil {
			httpError(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "moved": n, "workspaceId": *body.WorkspaceID})
		return
	}

	// A mutation to a trashed page is almost always a stale write racing a
	// delete; reject it so a concurrent edit can't resurrect on restore.
	// (Position-only writes are allowed so trash housekeeping still works.)
	if body.Title != nil || body.Icon != nil || body.Cover != nil ||
		len(body.Content) > 0 || len(body.Props) > 0 || len(body.PropsPatch) > 0 {
		var trashed sql.NullString
		if err := s.db.QueryRow(`SELECT trashed_at FROM pages WHERE id = ?`, id).Scan(&trashed); err == sql.ErrNoRows {
			httpError(w, 404, "page not found")
			return
		} else if err == nil && trashed.Valid {
			httpError(w, 409, "page is in the trash")
			return
		}
	}

	sets := []string{"updated_at = ?"}
	args := []any{now()}
	metaChanged := false

	if body.IsTemplate != nil {
		v := 0
		if *body.IsTemplate {
			v = 1
		}
		sets = append(sets, "is_template = ?")
		args = append(args, v)
		metaChanged = true
	}

	if body.Title != nil {
		if len([]rune(*body.Title)) > maxTitleLen {
			httpError(w, 400, "title is too long")
			return
		}
		sets = append(sets, "title = ?")
		args = append(args, *body.Title)
		metaChanged = true
	}
	if body.Icon != nil {
		sets = append(sets, "icon = ?")
		args = append(args, *body.Icon)
		metaChanged = true
	}
	if body.Cover != nil {
		if !validCover(*body.Cover) {
			httpError(w, 400, "invalid cover")
			return
		}
		sets = append(sets, "cover = ?")
		args = append(args, *body.Cover)
		metaChanged = true
	}
	if body.Visibility != nil {
		v := *body.Visibility
		if v != "workspace" && v != "private" {
			httpError(w, 400, "visibility must be 'workspace' or 'private'")
			return
		}
		sets = append(sets, "visibility = ?")
		args = append(args, v)
		metaChanged = true
	}
	if body.Tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, string(normalizeTags(*body.Tags)))
		metaChanged = true
	}
	if body.Description != nil {
		d := *body.Description
		if len([]rune(d)) > 2000 {
			d = string([]rune(d)[:2000])
		}
		sets = append(sets, "description = ?")
		args = append(args, d)
		metaChanged = true
	}
	if len(body.Content) > 0 {
		if !json.Valid(body.Content) {
			httpError(w, 400, "content is not valid JSON")
			return
		}
		sets = append(sets, "content = ?")
		args = append(args, string(body.Content))
	}
	if len(body.Props) > 0 {
		if !json.Valid(body.Props) {
			httpError(w, 400, "props is not valid JSON")
			return
		}
		sets = append(sets, "props = ?")
		args = append(args, string(body.Props))
		metaChanged = true
	}

	// A new parent needs write permission on that parent AND the same workspace.
	// All that used to be checked was that it exists — so you could hang a page of
	// your own under somebody else's: in the target workspace it turned up in
	// database views, the Markdown export and the calendar feed, because those
	// list children without a permission check of their own. Inside one workspace
	// it was the way to hang a private page under a collection and show it to
	// every member.
	//
	// Before tx.Begin(): the connection limit is one, so a query inside an open
	// transaction would block itself.
	if len(body.ParentID) > 0 {
		if t := bytes.TrimSpace(body.ParentID); string(t) != "null" {
			var newParent string
			if json.Unmarshal(t, &newParent) == nil && newParent != "" {
				if !s.canWriteReq(r, newParent) {
					httpError(w, 400, "parent page not found")
					return
				}
				if s.pageWorkspace(newParent) != s.pageWorkspace(id) {
					httpError(w, 400, "a page can only be re-parented within its own workspace")
					return
				}
			}
		}
	}

	// Cycle check and update must be atomic: with concurrent moves a
	// check-then-act on separate statements can persist A<->B cycles.
	// The transaction holds the single DB connection until commit.
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	// Field-level props merge: only the changed keys are sent, so two devices
	// editing different properties of the same row don't clobber each other.
	// A key set to null deletes it.
	if len(body.PropsPatch) > 0 {
		var patch map[string]json.RawMessage
		if err := json.Unmarshal(body.PropsPatch, &patch); err != nil {
			httpError(w, 400, "propsPatch must be a JSON object")
			return
		}
		var current string
		if err := tx.QueryRow(`SELECT props FROM pages WHERE id = ?`, id).Scan(&current); err != nil {
			httpError(w, 404, "page not found")
			return
		}
		merged := map[string]json.RawMessage{}
		json.Unmarshal([]byte(current), &merged)
		for k, v := range patch {
			if string(v) == "null" {
				delete(merged, k)
			} else {
				merged[k] = v
			}
		}
		mergedJSON, err := json.Marshal(merged)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		sets = append(sets, "props = ?")
		args = append(args, string(mergedJSON))
		metaChanged = true
	}

	if len(body.ParentID) > 0 {
		trimmed := bytes.TrimSpace(body.ParentID)
		if string(trimmed) == "null" {
			sets = append(sets, "parent_id = NULL")
		} else {
			var parentID string
			if err := json.Unmarshal(trimmed, &parentID); err != nil {
				httpError(w, 400, "parentId must be a string or null")
				return
			}
			if parentID == id {
				httpError(w, 400, "a page cannot be its own parent")
				return
			}
			within, err := isWithinSubtree(tx, id, parentID)
			if err != nil {
				httpError(w, 500, err.Error())
				return
			}
			if within {
				httpError(w, 400, "cannot move a page into its own subtree")
				return
			}
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ? AND trashed_at IS NULL`, parentID).Scan(&exists); err != nil || exists == 0 {
				httpError(w, 400, "parent page not found")
				return
			}
			sets = append(sets, "parent_id = ?")
			args = append(args, parentID)
		}
		metaChanged = true
	}
	if body.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *body.Position)
		metaChanged = true
	}

	args = append(args, id)
	res, err := tx.Exec("UPDATE pages SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpError(w, 404, "page not found")
		return
	}
	if err := tx.Commit(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if body.Title != nil || len(body.Content) > 0 || len(body.Props) > 0 {
		if err := s.reindexPage(id); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	}
	// Content writes from outside the editor (API/MCP) invalidate the live
	// CRDT doc; the editor's own materialization passes ?materialize=1.
	if len(body.Content) > 0 && r.URL.Query().Get("materialize") != "1" {
		s.resetYjsDoc(id)
	}
	// Snapshot a version-history entry on any content write (throttled inside).
	if len(body.Content) > 0 {
		u := requestUser(r)
		s.snapshotRevision(id, u.ID, u.Name)
	}
	if metaChanged {
		s.pagesChanged()
		// If this page is a database row, the boards showing it have to move the
		// card themselves — a person editing a property should not have to tell
		// the other browsers to reload.
		s.rowChanged(id)
	}
	// One event for a save, whether the body changed, the metadata did, or both
	// — a receiver wants "this page changed", not our internal split.
	if len(body.Content) > 0 || metaChanged {
		s.fireWebhook("page.updated", id)
	}
	p, err := s.getPage(id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, p)
}

// handleReindexSiblings renumbers a parent's children to 1,2,3,… so the
// float midpoint scheme (position = (a+b)/2) can't exhaust f64 precision under
// heavy reordering. The client calls this when a computed gap gets too small.
func (s *Server) handleReindexSiblings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ParentID    *string `json:"parentId"`
		WorkspaceID string  `json:"workspaceId"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	// This endpoint had no check at all: with parentId=null it matched
	// `parent_id IS NULL` and so rewrote the order of EVERY root page on the
	// WHOLE instance — for any signed-in account, across all workspaces. Below a
	// page it needs write permission on that page; at the top level it is narrowed
	// to your own workspace.
	me := requestUser(r)
	var wsFilter string
	if body.ParentID != nil {
		if !s.canWriteReq(r, *body.ParentID) {
			httpError(w, 404, "page not found")
			return
		}
	} else {
		wsFilter = strings.TrimSpace(body.WorkspaceID)
		if wsFilter == "" {
			wsFilter = s.defaultWorkspaceFor(me)
		}
		if !s.isMember(me.ID, wsFilter) || !s.tokenReachesWorkspace(r, wsFilter) {
			httpError(w, 404, "workspace not found")
			return
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT id FROM pages WHERE parent_id IS ? AND trashed_at IS NULL
		AND (? = '' OR workspace_id = ?) ORDER BY position, created_at`, body.ParentID, wsFilter, wsFilter)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE pages SET position = ? WHERE id = ?`, float64(i+1), id); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.pagesChanged()
	writeJSON(w, map[string]int{"reindexed": len(ids)})
}

// reindexPage refreshes the full-text index for a single page.
func (s *Server) reindexPage(id string) error {
	if _, err := s.db.Exec(`DELETE FROM pages_fts WHERE id = ?`, id); err != nil {
		return err
	}
	var title, content string
	var trashedAt sql.NullString
	err := s.db.QueryRow(`SELECT title, content, trashed_at FROM pages WHERE id = ?`, id).Scan(&title, &content, &trashedAt)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	// Keep the outgoing-links index in sync with the current content.
	s.updateLinks(id, content, trashedAt.Valid)
	if trashedAt.Valid {
		// In the trash: clear the passages, or the page keeps turning up in
		// the passage-based search.
		s.reindexChunks(id, "", "", nil, true)
		return nil
	}
	// Refresh the notes-list preview (snippet + first image) alongside the index.
	sn, th := extractSnippetAndThumb([]byte(content))
	s.db.Exec(`UPDATE pages SET snippet = ?, thumb = ? WHERE id = ?`, sn, th, id)
	body := extractText([]byte(content))
	var props string
	if s.db.QueryRow(`SELECT props FROM pages WHERE id = ?`, id).Scan(&props) == nil {
		body += " " + extractPropsText([]byte(props))
	}
	// Attached file texts (e.g. extracted PDFs) are searchable under the page.
	if rows, err := s.db.Query(`SELECT text FROM file_texts WHERE page_id = ?`, id); err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				body += " " + t
			}
		}
		rows.Close()
	}
	// Strip the snippet highlight markers so page content can never inject
	// fake <mark> tags into search results.
	clean := strings.NewReplacer("\x01", "", "\x02", "")
	if _, err := s.db.Exec(`INSERT INTO pages_fts (id, title, body) VALUES (?, ?, ?)`,
		id, clean.Replace(title), clean.Replace(body)); err != nil {
		return err
	}
	// Passages in the same breath (see chunks.go). A failure here may not make the
	// page indexing fail — the full-text search is the foundation, the passages
	// are the refinement.
	var wsID string
	s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, id).Scan(&wsID)
	if err := s.reindexChunks(id, wsID, clean.Replace(title), []byte(content), false); err != nil {
		log.Printf("reindexChunks %s: %v", id, err)
	}
	return nil
}

func (s *Server) handleDeletePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canWriteReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	action := "trash_page"
	if r.URL.Query().Get("permanent") == "1" {
		action = "delete_page"
	}
	s.audit("human", requestUser(r).ID, requestUser(r).Name, action, id, s.pageWorkspace(id), "")
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	ids, err := subtreeIDs(tx, id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if len(ids) == 0 {
		httpError(w, 404, "page not found")
		return
	}
	idArgs := make([]any, len(ids))
	for i, v := range ids {
		idArgs[i] = v
	}

	if r.URL.Query().Get("permanent") == "1" {
		// Explicitly clear extracted file text for the subtree: older DBs may
		// predate the file_texts foreign key, so CASCADE can't be relied on.
		if _, err := tx.Exec(`DELETE FROM file_texts WHERE page_id IN (`+placeholders(len(ids))+`)`, idArgs...); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		// chunks_fts is virtual and knows no cascade — otherwise the passages of the
		// deleted pages would stay in the search index and return hits on text that
		// no longer exists.
		if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE chunk_id IN
			(SELECT id FROM page_chunks WHERE page_id IN (`+placeholders(len(ids))+`))`, idArgs...); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE id = ?`, id); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	} else {
		// One shared timestamp marks the batch, so restore can un-trash
		// exactly the pages that were trashed together.
		ts := now()
		args := append([]any{ts, ts}, idArgs...)
		if _, err := tx.Exec(`UPDATE pages SET trashed_at = ?, updated_at = ? WHERE id IN (`+placeholders(len(ids))+`) AND trashed_at IS NULL`, args...); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	}
	if _, err := tx.Exec(`DELETE FROM pages_fts WHERE id IN (`+placeholders(len(ids))+`)`, idArgs...); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// Kick live editors of any page in the subtree so they don't keep
	// editing a trashed/deleted page.
	for _, pid := range ids {
		s.collab.reset(pid)
	}
	s.pagesChanged()
	// And the DATABASE the page belonged to, if any. Creating and updating have
	// always named it; trashing did not — so a board on a second screen kept
	// showing the card of a row that was already gone, until somebody reloaded.
	// The whole subtree, because a database can sit inside the part being
	// thrown away.
	for _, pid := range ids {
		s.rowChanged(pid)
	}
	// One event per page in the subtree: a receiver filtering on a single page
	// would otherwise never hear that it went, because only the root was named.
	for _, pid := range ids {
		s.fireWebhook("page.trashed", pid)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRestorePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canWriteReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer tx.Rollback()

	var parentID sql.NullString
	var trashedAt sql.NullString
	err = tx.QueryRow(`SELECT parent_id, trashed_at FROM pages WHERE id = ?`, id).Scan(&parentID, &trashedAt)
	if err == sql.ErrNoRows {
		httpError(w, 404, "page not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if !trashedAt.Valid {
		httpError(w, 400, "page is not in the trash")
		return
	}

	// If the original parent is gone or still trashed, restore to top level.
	var newParent any
	if parentID.Valid {
		var alive int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ? AND trashed_at IS NULL`, parentID.String).Scan(&alive); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		if alive > 0 {
			newParent = parentID.String
		}
	}

	ids, err := subtreeIDs(tx, id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	idArgs := make([]any, len(ids))
	for i, v := range ids {
		idArgs[i] = v
	}
	// Only un-trash pages from the same trash batch: descendants that were
	// trashed separately (earlier) keep their own trashed_at and stay in the trash.
	args := append([]any{now(), trashedAt.String}, idArgs...)
	if _, err := tx.Exec(`UPDATE pages SET trashed_at = NULL, updated_at = ? WHERE trashed_at = ? AND id IN (`+placeholders(len(ids))+`)`, args...); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if _, err := tx.Exec(`UPDATE pages SET parent_id = ? WHERE id = ?`, newParent, id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	for _, pid := range ids {
		if err := s.reindexPage(pid); err != nil {
			httpError(w, 500, err.Error())
			return
		}
	}
	s.pagesChanged()
	// Same on the way back: a restored row has to reappear on an open board.
	for _, pid := range ids {
		s.rowChanged(pid)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

type searchResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Icon    string `json:"icon"`
	Snippet string `json:"snippet"`
	// Heading: the heading path of the passage that matched, for example
	// "Contract › Termination". Empty when the hit comes from the fallback or
	// sits under no heading at all.
	Heading string `json:"heading,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	results := []searchResult{}
	if q == "" {
		writeJSON(w, results)
		return
	}
	match := ftsMatch(q)

	userID := requestUser(r).ID
	ws := s.scopeWorkspacesFor(requestUser(r), s.visibleWorkspaces(userID))
	if len(ws) == 0 {
		writeJSON(w, results)
		return
	}
	wargs := make([]any, 0, len(ws)+1)
	wargs = append(wargs, match)
	for _, v := range ws {
		wargs = append(wargs, v)
	}
	// The search runs over the PASSAGES, not over whole pages (see chunks.go):
	// the excerpt then comes from the paragraph that actually matches, and carries
	// its heading path along.
	//
	// Fetching in rounds rather than 40 at once: the canRead filter below throws
	// hits away, and with a fixed LIMIT the list stayed short as soon as a
	// workspace held many private pages belonging to other people — you got four
	// results and believed there were no more.
	results = s.searchChunks(userID, match, ws, 20)
	// Fallback: if the passage search finds nothing, the old page search counts.
	// It is the foundation; a mistake in the cutting may not make content
	// unfindable.
	if len(results) == 0 {
		results = s.searchPagesFallback(userID, match, ws, 20)
	}
	writeJSON(w, results)
}

// searchChunks searches the passages and returns the BEST one per page.
//
// The de-duplication happens in Go and not in SQL: `GROUP BY page_id` would
// upset the ranking, because FTS5 only guarantees its ordering on the outer
// query. Since the rows arrive ranked anyway, the first hit per page is the
// best one.
func (s *Server) searchChunks(userID, match string, ws []string, want int) []searchResult {
	out := []searchResult{}
	seen := map[string]bool{}
	args := make([]any, 0, len(ws)+1)
	args = append(args, match)
	for _, v := range ws {
		args = append(args, v)
	}
	for offset, round := 0, 0; len(out) < want && round < 8; round++ {
		qArgs := append([]any{}, args...)
		rows, err := s.db.Query(`
			SELECT c.page_id, p.title, p.icon, c.heading,
			       snippet(chunks_fts, 3, char(1), char(2), '…', 18)
			FROM chunks_fts
			JOIN page_chunks c ON c.id = chunks_fts.chunk_id
			JOIN pages p ON p.id = c.page_id
			WHERE chunks_fts MATCH ? AND p.trashed_at IS NULL
			  AND c.workspace_id IN (`+placeholders(len(ws))+`)
			ORDER BY bm25(chunks_fts, 0.0, 5.0, 3.0, 1.0)
			LIMIT 60 OFFSET `+strconv.Itoa(offset), qArgs...)
		if err != nil {
			return out
		}
		// Drain the cursor BEFORE any canRead query — with a single DB connection, a
		// query inside an open cursor blocks the server.
		var cand []searchResult
		for rows.Next() {
			var res searchResult
			if rows.Scan(&res.ID, &res.Title, &res.Icon, &res.Heading, &res.Snippet) == nil {
				cand = append(cand, res)
			}
		}
		rows.Close()
		for _, res := range cand {
			if len(out) >= want {
				break
			}
			if seen[res.ID] {
				continue
			}
			if s.canRead(userID, res.ID) {
				seen[res.ID] = true
				out = append(out, res)
			}
		}
		if len(cand) < 60 {
			break
		}
		offset += 60
	}
	return out
}

// searchPagesFallback is the old, page-by-page search.
func (s *Server) searchPagesFallback(userID, match string, ws []string, want int) []searchResult {
	out := []searchResult{}
	args := make([]any, 0, len(ws)+1)
	args = append(args, match)
	for _, v := range ws {
		args = append(args, v)
	}
	for offset, round := 0, 0; len(out) < want && round < 8; round++ {
		qArgs := append([]any{}, args...)
		rows, err := s.db.Query(`
			SELECT p.id, p.title, p.icon, snippet(pages_fts, 2, char(1), char(2), '…', 14)
			FROM pages_fts JOIN pages p ON p.id = pages_fts.id
			WHERE pages_fts MATCH ? AND p.trashed_at IS NULL AND p.workspace_id IN (`+placeholders(len(ws))+`)
			ORDER BY bm25(pages_fts, 0.0, 5.0, 1.0)
			LIMIT 60 OFFSET `+strconv.Itoa(offset), qArgs...)
		if err != nil {
			return out
		}
		var cand []searchResult
		for rows.Next() {
			var res searchResult
			if rows.Scan(&res.ID, &res.Title, &res.Icon, &res.Snippet) == nil {
				cand = append(cand, res)
			}
		}
		rows.Close()
		for _, res := range cand {
			if len(out) >= want {
				break
			}
			if s.canRead(userID, res.ID) {
				out = append(out, res)
			}
		}
		if len(cand) < 60 {
			break
		}
		offset += 60
	}
	return out
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Admin-configurable per-file cap (default 50 MiB).
	maxUploadSize := s.maxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1<<20) // +1MiB for multipart overhead
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		// Distinguish "too big" (413) from a malformed body (400) so the client
		// can show a precise message.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) || strings.Contains(err.Error(), "too large") {
			httpError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("file too large — max %d MB", maxUploadSize>>20))
		} else {
			httpError(w, 400, "upload malformed")
		}
		return
	}
	// If attaching to a page, require write access to it.
	if pageID := r.URL.Query().Get("page"); pageID != "" && !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpError(w, 400, "missing file field")
		return
	}
	defer file.Close()

	name := newID() + sanitizeExt(header)
	path := filepath.Join(s.dataDir, "files", name)
	dst, err := os.Create(path)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// PDFs attached to a page become searchable under that page.
	if strings.HasSuffix(name, ".pdf") {
		if pageID := r.URL.Query().Get("page"); pageID != "" {
			s.indexFileText(name, pageID, extractPDFText(path))
		}
	}
	// The file index (W125). header.Filename is the only place the human name
	// survives — the stored name is an opaque id.
	s.recordFile(name, r.URL.Query().Get("page"), filepath.Base(header.Filename))
	writeJSON(w, map[string]string{"url": "/files/" + name})
}

func sanitizeExt(h *multipart.FileHeader) string {
	ext := strings.ToLower(filepath.Ext(h.Filename))
	clean := strings.Builder{}
	for _, c := range ext {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' {
			clean.WriteRune(c)
		}
	}
	e := clean.String()
	if len(e) > 12 || e == "." {
		return ""
	}
	return e
}
