package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Public forms. A form-share is a share_links row with mode='form' on a
// collection page; it lets anyone with the link submit ONE new row without an
// account. It is deliberately separate from the read-only page share
// (mode='' / 'read'): a form token grants write-a-row, never read-the-page.

// formSchemaProp is the subset of a collection property a public form needs to
// render and validate a field. Options are used to reject unknown select ids.
type formSchemaProp struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Options []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"options"`
}

func (p formSchemaProp) hasOption(id string) bool {
	for _, o := range p.Options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// formFillable reports whether a property type can be submitted via a form.
// Computed/linked types (relation/rollup/formula) are never accepted.
func formFillable(t string) bool {
	switch t {
	case "text", "number", "select", "multiselect", "date", "checkbox", "person":
		return true
	}
	return false
}

func parseFormSchema(schemaJSON string) []formSchemaProp {
	var defs []formSchemaProp
	json.Unmarshal([]byte(schemaJSON), &defs)
	return defs
}

// formView is the slice of a ViewDef a public form reads.
type formView struct {
	Type       string   `json:"type"`
	FormTitle  string   `json:"formTitle"`
	FormDesc   string   `json:"formDesc"`
	FormSubmit string   `json:"formSubmit"`
	Hidden     []string `json:"hidden"`
}

// handleShareForm (POST /api/collections/{id}/form-share) mints a public form
// link for a collection. Owner/writer only.
func (s *Server) handleShareForm(w http.ResponseWriter, r *http.Request) {
	colID := r.PathValue("id")
	if !s.canWriteReq(r, colID) {
		httpError(w, 403, "forbidden")
		return
	}
	var typ string
	if s.db.QueryRow(`SELECT type FROM pages WHERE id = ?`, colID).Scan(&typ) != nil || typ != "collection" {
		httpError(w, 404, "not a collection")
		return
	}
	b := make([]byte, 18)
	rand.Read(b)
	token := hex.EncodeToString(b)
	// One live form-share per collection.
	s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode = 'form'`, colID)
	if _, err := s.db.Exec(`INSERT INTO share_links (token_hash, page_id, created_at, mode) VALUES (?, ?, ?, 'form')`, tokenHash(token), colID, now()); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// Absolute URL on the configured external domain (tunnel/HTTPS/base URL) so
	// the link works for people outside the LAN, not just the internal IP.
	writeJSON(w, map[string]string{"token": token, "url": s.publicShareBase(r) + "/form/" + token})
}

// handleUnshareForm (DELETE /api/collections/{id}/form-share) revokes it.
func (s *Server) handleUnshareForm(w http.ResponseWriter, r *http.Request) {
	colID := r.PathValue("id")
	if !s.canWriteReq(r, colID) {
		httpError(w, 403, "forbidden")
		return
	}
	s.db.Exec(`DELETE FROM share_links WHERE page_id = ? AND mode = 'form'`, colID)
	writeJSON(w, map[string]bool{"ok": true})
}

// handleFormShareStatus (GET /api/collections/{id}/form-share) reports whether a
// live form link exists (the raw token can't be recovered — only its hash is
// stored — so re-sharing mints a fresh one).
func (s *Server) handleFormShareStatus(w http.ResponseWriter, r *http.Request) {
	colID := r.PathValue("id")
	if !s.canReadReq(r, colID) {
		httpError(w, 404, "not found")
		return
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM share_links WHERE page_id = ? AND mode = 'form'`, colID).Scan(&n)
	writeJSON(w, map[string]bool{"shared": n > 0})
}

// resolveFormShare maps a form token to its collection id.
func (s *Server) resolveFormShare(token string) (string, bool) {
	var colID string
	if s.db.QueryRow(`SELECT page_id FROM share_links WHERE token_hash = ? AND mode = 'form'`, tokenHash(token)).Scan(&colID) != nil {
		return "", false
	}
	// A trashed collection's form stops accepting submissions.
	var trashed *string
	if s.db.QueryRow(`SELECT trashed_at FROM pages WHERE id = ?`, colID).Scan(&trashed) != nil || trashed != nil {
		return "", false
	}
	return colID, true
}

// handlePublicFormConfig (GET /api/public/form/{token}) returns everything a
// public form needs to render — WITHOUT auth and without leaking rows or the
// rest of the workspace: only the fillable, non-hidden field definitions.
func (s *Server) handlePublicFormConfig(w http.ResponseWriter, r *http.Request) {
	colID, ok := s.resolveFormShare(r.PathValue("token"))
	if !ok {
		httpError(w, 404, "not found")
		return
	}
	var title, icon string
	s.db.QueryRow(`SELECT title, COALESCE(icon, '') FROM pages WHERE id = ?`, colID).Scan(&title, &icon)
	var schemaJSON, viewsJSON string
	if s.db.QueryRow(`SELECT schema, views FROM collections WHERE page_id = ?`, colID).Scan(&schemaJSON, &viewsJSON) != nil {
		httpError(w, 404, "not a collection")
		return
	}
	var views []formView
	json.Unmarshal([]byte(viewsJSON), &views)
	var fv *formView
	for i := range views {
		if views[i].Type == "form" {
			fv = &views[i]
			break
		}
	}
	if fv == nil {
		httpError(w, 404, "no form view")
		return
	}
	hidden := map[string]bool{}
	for _, h := range fv.Hidden {
		hidden[h] = true
	}
	fields := []formSchemaProp{}
	for _, p := range parseFormSchema(schemaJSON) {
		if formFillable(p.Type) && !hidden[p.ID] {
			fields = append(fields, p)
		}
	}
	writeJSON(w, map[string]any{
		"title":      title,
		"icon":       icon,
		"formTitle":  fv.FormTitle,
		"formDesc":   fv.FormDesc,
		"formSubmit": fv.FormSubmit,
		"schema":     fields,
	})
}

// handlePublicFormSubmit (POST /api/public/form/{token}/submit) creates a row
// from an anonymous submission. Rate-limited per IP; props are validated
// against the schema (unknown/computed props and bad values are dropped).
func (s *Server) handlePublicFormSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.formRate.allow(s.clientIP(r)) {
		httpError(w, 429, "too many submissions — please try again shortly")
		return
	}
	colID, ok := s.resolveFormShare(r.PathValue("token"))
	if !ok {
		httpError(w, 404, "not found")
		return
	}
	var body struct {
		Title string         `json:"title"`
		Props map[string]any `json:"props"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	title := strings.TrimSpace(body.Title)
	if title == "" {
		httpError(w, 400, "title is required")
		return
	}
	if len([]rune(title)) > maxTitleLen {
		httpError(w, 400, "title is too long")
		return
	}
	var schemaJSON, ownerID, wsID string
	s.db.QueryRow(`SELECT COALESCE(owner_id, ''), workspace_id FROM pages WHERE id = ?`, colID).Scan(&ownerID, &wsID)
	s.db.QueryRow(`SELECT schema FROM collections WHERE page_id = ?`, colID).Scan(&schemaJSON)
	byID := map[string]formSchemaProp{}
	for _, p := range parseFormSchema(schemaJSON) {
		byID[p.ID] = p
	}
	clean := map[string]any{}
	for id, v := range body.Props {
		p, ok := byID[id]
		if !ok || !formFillable(p.Type) {
			continue
		}
		if cv, ok := coerceFormValue(p, v); ok {
			clean[id] = cv
		}
	}
	propsJSON, _ := json.Marshal(clean)

	id := newID()
	ts := now()
	var pos float64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id IS ?`, colID).Scan(&pos); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if _, err := s.db.Exec(
		`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, type, props, workspace_id, owner_id, visibility) VALUES (?, ?, ?, '', '[]', ?, ?, ?, 'doc', ?, ?, ?, 'workspace')`,
		id, colID, title, pos, ts, ts, string(propsJSON), wsID, ownerID,
	); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if err := s.reindexPage(id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.audit("form", "", "public form", "create_page", id, wsID, title)
	s.pagesChanged()
	writeJSON(w, map[string]bool{"ok": true})
}

// coerceFormValue validates and normalises one submitted value against its
// property definition. Returns (value, true) to keep it, (nil, false) to drop.
func coerceFormValue(p formSchemaProp, v any) (any, bool) {
	switch p.Type {
	case "text", "person", "date":
		str, ok := v.(string)
		if !ok {
			return nil, false
		}
		str = strings.TrimSpace(str)
		if str == "" {
			return nil, false
		}
		if len([]rune(str)) > 4000 {
			str = string([]rune(str)[:4000])
		}
		return str, true
	case "select":
		str, ok := v.(string)
		if !ok || str == "" || !p.hasOption(str) {
			return nil, false
		}
		return str, true
	case "multiselect":
		arr, ok := v.([]any)
		if !ok {
			return nil, false
		}
		out := []string{}
		for _, e := range arr {
			if es, ok := e.(string); ok && p.hasOption(es) {
				out = append(out, es)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case "number":
		f, ok := v.(float64)
		if !ok {
			return nil, false
		}
		return f, true
	case "checkbox":
		b, ok := v.(bool)
		if !ok {
			return nil, false
		}
		return b, true
	}
	return nil, false
}
