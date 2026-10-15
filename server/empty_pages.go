package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
)

// Only blank paragraphs are disposable. An empty table, embed, file, drawing,
// mention or other structured block is intentional content, even without text.
func blankPageContent(raw []byte) bool {
	var blocks []struct {
		Type     string          `json:"type"`
		Content  json.RawMessage `json:"content"`
		Children json.RawMessage `json:"children"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type != "paragraph" {
			return false
		}
		if len(block.Children) > 0 && !blankPageContent(block.Children) {
			return false
		}
		var inline []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if len(block.Content) > 0 && json.Unmarshal(block.Content, &inline) != nil {
			return false
		}
		for _, item := range inline {
			if item.Type != "text" || strings.TrimSpace(item.Text) != "" {
				return false
			}
		}
	}
	return true
}

type emptyPageBaseline struct {
	ParentID    *string         `json:"parentId"`
	WorkspaceID string          `json:"workspaceId"`
	Visibility  string          `json:"visibility"`
	CreatedAt   string          `json:"createdAt"`
	Title       string          `json:"title"`
	Props       json.RawMessage `json:"props"`
}

// The client remembers only pages it just created and has never edited. The
// server independently checks the creation identity, actual content and links.
// This endpoint never sweeps pages by title and never permanently deletes one.
func (s *Server) handleDiscardEmptyPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canWriteReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	var baseline emptyPageBaseline
	if decodeJSON(w, r, &baseline) != nil || baseline.CreatedAt == "" ||
		(baseline.Title != "" && baseline.Title != "Untitled") {
		httpError(w, 400, "invalid empty-page baseline")
		return
	}
	// Exclude connected editors, including another browser owned by this user.
	// Holding the hub lock also excludes a new join until the check commits.
	s.collab.mu.Lock()
	if room := s.collab.rooms[id]; room != nil {
		room.mu.Lock()
		busy := len(room.conns) != 0
		room.mu.Unlock()
		if busy {
			s.collab.mu.Unlock()
			writeJSON(w, map[string]bool{"discarded": false, "busy": true})
			return
		}
	}
	discarded, err := s.discardEmptyPage(id, requestUser(r).ID, baseline)
	s.collab.mu.Unlock()
	if err != nil {
		httpError(w, 500, "could not discard empty page")
		return
	}
	if discarded {
		s.audit("human", requestUser(r).ID, requestUser(r).Name, "trash_page", id, s.pageWorkspace(id), "Discarded untouched new page")
		s.pagesChanged()
		s.rowChanged(id)
		s.fireWebhook("page.trashed", id)
	}
	writeJSON(w, map[string]bool{"discarded": discarded})
}

func (s *Server) discardEmptyPage(id, owner string, baseline emptyPageBaseline) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var title, typ, props, content, creator, created, icon, cover, tags, description string
	var template int
	var parent sql.NullString
	var workspace, visibility string
	var trashed sql.NullString
	err = tx.QueryRow(`SELECT title,type,props,content,owner_id,created_at,icon,cover,tags,description,is_template,trashed_at,parent_id,workspace_id,visibility FROM pages WHERE id=?`, id).
		Scan(&title, &typ, &props, &content, &creator, &created, &icon, &cover, &tags, &description, &template, &trashed, &parent, &workspace, &visibility)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sameParent := (!parent.Valid && baseline.ParentID == nil) || (parent.Valid && baseline.ParentID != nil && parent.String == *baseline.ParentID)
	var actualProps, initialProps any
	if !sameParent || workspace != baseline.WorkspaceID || visibility != baseline.Visibility || typ != "doc" || creator != owner || created != baseline.CreatedAt || title != baseline.Title ||
		trashed.Valid || template != 0 || icon != "" || cover != "" || description != "" ||
		(tags != "" && tags != "[]") || !blankPageContent([]byte(content)) ||
		json.Unmarshal([]byte(props), &actualProps) != nil || json.Unmarshal(baseline.Props, &initialProps) != nil ||
		!reflect.DeepEqual(actualProps, initialProps) {
		return false, nil
	}
	var related int
	err = tx.QueryRow(`SELECT
  (SELECT COUNT(*) FROM pages WHERE parent_id=?1) +
  (SELECT COUNT(*) FROM files WHERE page_id=?1) +
  (SELECT COUNT(*) FROM comments WHERE page_id=?1) +
  (SELECT COUNT(*) FROM page_notes WHERE page_id=?1) +
  (SELECT COUNT(*) FROM links WHERE target_id=?1 OR source_id=?1) +
  (SELECT COUNT(*) FROM share_links WHERE page_id=?1) +
  (SELECT COUNT(*) FROM favorites WHERE page_id=?1) +
  (SELECT COUNT(*) FROM pages WHERE id<>?1 AND (instr(props,?1)>0 OR instr(content,?1)>0))`, id).Scan(&related)
	if err != nil || related != 0 {
		return false, err
	}
	// A saved historical body is another reason to retain the page.
	rows, err := tx.Query(`SELECT title,content FROM page_revisions WHERE page_id=?`, id)
	if err != nil {
		return false, err
	}
	keep := false
	for rows.Next() {
		var revisionTitle, revisionContent string
		if err = rows.Scan(&revisionTitle, &revisionContent); err != nil {
			rows.Close()
			return false, err
		}
		if (revisionTitle != "" && revisionTitle != baseline.Title) || !blankPageContent([]byte(revisionContent)) {
			keep = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || keep {
		return false, err
	}
	ts := now()
	if _, err = tx.Exec(`UPDATE pages SET trashed_at=?,updated_at=? WHERE id=?`, ts, ts, id); err != nil {
		return false, err
	}
	if _, err = tx.Exec(`DELETE FROM pages_fts WHERE id=?`, id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
