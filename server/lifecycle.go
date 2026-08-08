package server

import (
	"database/sql"
	"net/http"
	"strings"
)

// Page lifecycle: deep duplication and Markdown import (Welle 16).

type dupRow struct {
	id, parentID, title, icon, cover, content, typ, props, visibility string
	position                                                          float64
	hasParent                                                         bool
}

// duplicatePage deep-copies a page and its whole subtree (new ids, preserved
// structure, copied collection schema/views) as a sibling of the original.
// fromTemplate keeps the original title (instantiating a template) instead of
// prefixing "Copy of". asTemplate marks the COPY as the template: saving a
// template is a snapshot, so the original stays a normal page and later edits
// to it never change the template. Without asTemplate the copy is never a
// template. Returns the new root id.
func (s *Server) duplicatePage(rootID, userID string, fromTemplate, asTemplate bool) (string, error) {
	ids, err := subtreeIDs(s.db, rootID)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", sql.ErrNoRows
	}
	// Copy only what the caller is allowed to see. subtreeIDs collects the
	// whole subtree unfiltered, and only the root used to be authorised — so
	// somebody else's private subpage was copied along. And because the copy
	// sets owner_id to whoever copied while visibility stays 'private', it then
	// belonged to them: duplicating turned "no access" into "mine". Filtered
	// branches drop out together with their descendants; their parent reference
	// would otherwise point at nothing.
	readable := make([]string, 0, len(ids))
	for _, id := range ids {
		if s.canRead(userID, id) {
			readable = append(readable, id)
		}
	}
	ids = readable
	if len(ids) == 0 {
		return "", sql.ErrNoRows
	}
	// Load every page in the subtree into memory (drain before further queries;
	// single DB connection).
	//
	// The insert later follows the order of `ids` — which comes from the
	// recursive query, so parents before children. The order of THIS query is
	// no good for that: `WHERE id IN (…)` runs over the index and comes back
	// sorted by id. If a child's id is smaller than its parent's, it got
	// inserted first and pointed at a parent that did not exist yet — the copy
	// then broke off with a foreign key error. Because ids are random, that
	// happened roughly every second time.
	rowsByID := map[string]*dupRow{}
	q := `SELECT id, parent_id, title, icon, cover, content, type, props, visibility, position, workspace_id
	      FROM pages WHERE id IN (` + placeholders(len(ids)) + `)`
	args := make([]any, len(ids))
	for i, v := range ids {
		args[i] = v
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return "", err
	}
	// The workspace of the ROOT is what counts. This used to be won by whichever
	// row was scanned last — with a tree bent across workspace boundaries the
	// whole copy landed in the wrong workspace.
	wsByID := map[string]string{}
	for rows.Next() {
		var d dupRow
		var parent sql.NullString
		var rowWS string
		if err := rows.Scan(&d.id, &parent, &d.title, &d.icon, &d.cover, &d.content, &d.typ, &d.props, &d.visibility, &d.position, &rowWS); err != nil {
			rows.Close()
			return "", err
		}
		d.parentID = parent.String
		d.hasParent = parent.Valid
		rowsByID[d.id] = &d
		wsByID[d.id] = rowWS
	}
	rows.Close()
	workspaceID := wsByID[rootID]

	// Parents before children: the order from subtreeIDs, narrowed to what was
	// actually loaded.
	order := make([]string, 0, len(rowsByID))
	for _, id := range ids {
		if _, ok := rowsByID[id]; ok {
			order = append(order, id)
		}
	}

	// Which of the copied pages are collections (to copy their schema/views).
	collectionSchemas := map[string][2]string{}
	crows, err := s.db.Query(`SELECT page_id, schema, views FROM collections WHERE page_id IN (`+placeholders(len(ids))+`)`, args...)
	if err == nil {
		for crows.Next() {
			var pid, schema, views string
			if crows.Scan(&pid, &schema, &views) == nil {
				collectionSchemas[pid] = [2]string{schema, views}
			}
		}
		crows.Close()
	}

	// Map old id → new id.
	newID2 := map[string]string{}
	for _, oldID := range order {
		newID2[oldID] = newID()
	}
	root := rowsByID[rootID]
	ts := now()
	// Root copy is a sibling of the original; place it right after.
	var rootParent any
	if root.hasParent {
		rootParent = root.parentID
	}
	var rootPos float64
	s.db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM pages WHERE parent_id IS ?`, rootParent).Scan(&rootPos)

	for _, oldID := range order {
		d := rowsByID[oldID]
		nid := newID2[oldID]
		var parent any
		pos := d.position
		title := d.title
		isTemplate := 0
		if oldID == rootID {
			parent = rootParent
			pos = rootPos
			// A snapshot keeps its name — "Copy of" is for duplicates that live
			// beside their original in the same list.
			if !fromTemplate && !asTemplate {
				title = "Copy of " + d.title
			}
			if asTemplate {
				isTemplate = 1 // only the root carries the flag; the subtree is its body
			}
		} else if np, ok := newID2[d.parentID]; ok {
			parent = np
		} else if d.hasParent {
			// The parent was not copied along — either because it was filtered
			// out (unreadable) or because the tree is bent across workspace
			// boundaries. This used to hang the copy on the ORIGINAL parent:
			// with an unreadable private parent it landed in somebody else's
			// subtree, with the copier as its owner. Branches like that belong
			// skipped.
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, cover, content, position, created_at, updated_at, type, props, workspace_id, owner_id, visibility, is_template)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nid, parent, title, d.icon, d.cover, d.content, pos, ts, ts, d.typ, d.props, workspaceID, userID, d.visibility, isTemplate); err != nil {
			return "", err
		}
		if sv, ok := collectionSchemas[oldID]; ok {
			s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)`, nid, sv[0], sv[1])
		}
		s.reindexPage(nid)
	}
	s.pagesChanged()
	return newID2[rootID], nil
}

func (s *Server) handleDuplicatePage(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	u := requestUser(r)
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	newRoot, err := s.duplicatePage(pageID, u.ID, r.URL.Query().Get("fromTemplate") == "1", r.URL.Query().Get("asTemplate") == "1")
	if err == sql.ErrNoRows {
		httpError(w, 404, "page not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]string{"id": newRoot})
}

// handleImport creates a new page from Markdown (parses to BlockNote blocks).
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	var body struct {
		ParentID *string `json:"parentId"`
		Title    string  `json:"title"`
		Markdown string  `json:"markdown"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if len([]rune(body.Title)) > maxTitleLen {
		httpError(w, 400, "title is too long")
		return
	}
	// Parent (if any) must be writable; a root import lands in the user's
	// default workspace.
	var parent any
	var workspaceID string
	if body.ParentID != nil && *body.ParentID != "" {
		if !s.canWriteReq(r, *body.ParentID) {
			httpError(w, 403, "forbidden")
			return
		}
		var pws string
		var trashed sql.NullString
		if s.db.QueryRow(`SELECT workspace_id, trashed_at FROM pages WHERE id = ?`, *body.ParentID).Scan(&pws, &trashed) != nil || trashed.Valid {
			httpError(w, 404, "parent not found")
			return
		}
		parent = *body.ParentID
		workspaceID = pws
	} else {
		workspaceID = s.userDefaultWorkspace(u.ID)
		if workspaceID == "" {
			httpError(w, 400, "no workspace")
			return
		}
	}

	// If no explicit title, use the first Markdown heading.
	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = firstHeading(body.Markdown)
	}
	if title == "" {
		title = "Imported"
	}
	content, err := mdToBlocksJSON(body.Markdown)
	if err != nil {
		httpError(w, 400, "could not parse markdown")
		return
	}
	id := newID()
	ts := now()
	var pos float64
	s.db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM pages WHERE parent_id IS ?`, parent).Scan(&pos)
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, 'workspace')`,
		id, parent, title, content, pos, ts, ts, workspaceID, u.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.reindexPage(id)
	s.audit("human", u.ID, u.Name, "import_page", id, workspaceID, title)
	s.pagesChanged()
	writeJSON(w, map[string]string{"id": id})
}

// firstHeading returns the text of the first Markdown ATX heading, if any.
func firstHeading(md string) string {
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			return strings.TrimSpace(strings.TrimLeft(t, "# "))
		}
	}
	return ""
}
