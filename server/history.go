package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Page version history and comments (W13).
//
// The CRDT update log is compacted away, so version history is a SEPARATE,
// materialized snapshot store: whenever a page's content is written (editor
// materialize, API/MCP), we record a throttled snapshot so a user can browse
// and restore prior versions. Comments live in their own table, page- or
// block-scoped, resolvable, and reachable by agents via MCP.

const revisionThrottle = 2 * time.Minute // at most one snapshot per page per window
const revisionKeep = 50                  // keep the newest N per page

// snapshotRevision records the page's current title+content as a revision,
// unless the newest revision is younger than revisionThrottle. Best-effort:
// history is a convenience, never blocks the write path.
func (s *Server) snapshotRevision(pageID, authorID, authorName string) {
	var last string
	if s.db.QueryRow(`SELECT created_at FROM page_revisions WHERE page_id = ? ORDER BY created_at DESC LIMIT 1`, pageID).Scan(&last) == nil {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(t) < revisionThrottle {
			return
		}
	}
	var title, content string
	if s.db.QueryRow(`SELECT title, content FROM pages WHERE id = ?`, pageID).Scan(&title, &content) != nil {
		return
	}
	if strings.TrimSpace(content) == "" || content == "[]" {
		return // don't snapshot an empty doc
	}
	s.db.Exec(`INSERT INTO page_revisions (id, page_id, created_at, author_id, author_name, title, content) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		newID(), pageID, now(), authorID, authorName, title, content)
	// Prune to the newest revisionKeep for this page.
	s.db.Exec(`DELETE FROM page_revisions WHERE page_id = ? AND id NOT IN (
		SELECT id FROM page_revisions WHERE page_id = ? ORDER BY created_at DESC LIMIT ?)`, pageID, pageID, revisionKeep)
}

type revisionJSON struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	AuthorName string `json:"authorName"`
	Title      string `json:"title"`
}

func (s *Server) handleListRevisions(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canReadReq(r, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	rows, err := s.db.Query(`SELECT id, created_at, author_name, title FROM page_revisions WHERE page_id = ? ORDER BY created_at DESC`, pageID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []revisionJSON{}
	for rows.Next() {
		var x revisionJSON
		rows.Scan(&x.ID, &x.CreatedAt, &x.AuthorName, &x.Title)
		list = append(list, x)
	}
	writeJSON(w, list)
}

// handleGetRevision returns a single revision's full content.
func (s *Server) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canReadReq(r, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	var title, content, createdAt, author string
	err := s.db.QueryRow(`SELECT title, content, created_at, author_name FROM page_revisions WHERE id = ? AND page_id = ?`, r.PathValue("revId"), pageID).Scan(&title, &content, &createdAt, &author)
	if err == sql.ErrNoRows {
		httpError(w, 404, "revision not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"title": title, "content": json.RawMessage(content), "createdAt": createdAt, "authorName": author,
	})
}

// handleRestoreRevision snapshots the current state (so the restore is itself
// undoable) then overwrites the page's content with the chosen revision and
// resets the live CRDT doc so open editors reload.
func (s *Server) handleRestoreRevision(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	u := requestUser(r)
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	var content string
	err := s.db.QueryRow(`SELECT content FROM page_revisions WHERE id = ? AND page_id = ?`, r.PathValue("revId"), pageID).Scan(&content)
	if err == sql.ErrNoRows {
		httpError(w, 404, "revision not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// Capture the pre-restore state first so a restore can be undone.
	s.snapshotRevision(pageID, u.ID, u.Name)
	if _, err := s.db.Exec(`UPDATE pages SET content = ?, updated_at = ?, `+bumpDocGen+` WHERE id = ?`, content, now(), pageID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.reindexPage(pageID)
	s.resetYjsDoc(pageID)
	s.pagesChanged()
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- Comments ----

type commentJSON struct {
	Anchor     json.RawMessage `json:"anchor,omitempty"`
	ThreadID   string          `json:"threadId"`
	ReplyToID  string          `json:"replyToId"`
	DeletedAt  *string         `json:"deletedAt"`
	ID         string          `json:"id"`
	BlockID    string          `json:"blockId"`
	AuthorID   string          `json:"authorId"`
	AuthorName string          `json:"authorName"`
	Body       string          `json:"body"`
	CreatedAt  string          `json:"createdAt"`
	ResolvedAt *string         `json:"resolvedAt"`
	// The author's colour/picture, so the same person looks the same
	// everywhere — the comment column used to roll a colour out of the name
	// that had nothing to do with the real user colour.
	AuthorColor  string `json:"authorColor"`
	AuthorAvatar string `json:"authorAvatar"`
	// The agent that wrote it, when one did. A claim, shown beside the account
	// that vouches for it, exactly as on the note trail.
	Agent      string `json:"agent,omitempty"`
	AgentLabel string `json:"agentLabel,omitempty"`
}

func (s *Server) pageComments(pageID string) ([]commentJSON, error) {
	// LEFT JOIN: the author may be deleted — the comment stays.
	rows, err := s.db.Query(`SELECT c.id, c.block_id, c.author_id, c.author_name, c.body, c.created_at, c.resolved_at,c.anchor,c.thread_id,c.reply_to_id,c.deleted_at,
		COALESCE(u.color, ''), COALESCE(u.avatar, ''), c.agent, c.agent_label
		FROM comments c LEFT JOIN users u ON u.id = c.author_id
		WHERE c.page_id = ? ORDER BY c.created_at`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []commentJSON{}
	for rows.Next() {
		var c commentJSON
		var resolved sql.NullString
		var anchor string
		if err := rows.Scan(&c.ID, &c.BlockID, &c.AuthorID, &c.AuthorName, &c.Body, &c.CreatedAt, &resolved, &anchor, &c.ThreadID, &c.ReplyToID, &c.DeletedAt, &c.AuthorColor, &c.AuthorAvatar, &c.Agent, &c.AgentLabel); err != nil {
			return nil, err
		}
		if anchor != "" {
			c.Anchor = json.RawMessage(anchor)
		}
		if resolved.Valid {
			c.ResolvedAt = &resolved.String
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canReadReq(r, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	list, err := s.pageComments(pageID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, list)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	u := requestUser(r)
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	var body commentInput
	if err := decodeJSON(w, r, &body); err != nil {
		httpErrorCode(w, 400, "comment_invalid_json", "Invalid comment request")
		return
	}
	if u.TokenKind != "" {
		body.Agent, body.Label = commentAgent(body.Agent, body.Label)
	} else {
		body.Agent, body.Label = "", ""
	}
	id, err := s.createThreadComment(u, pageID, body)
	if err != nil {
		commentHTTPError(w, err)
		return
	}

	writeJSON(w, map[string]string{"id": id})
}

// commentQuote is the passage a comment was written on. A page comment has none.
func commentQuote(anchor json.RawMessage) string {
	var a struct {
		Quote string `json:"quote"`
	}
	if len(anchor) > 0 {
		json.Unmarshal(anchor, &a)
	}
	return a.Quote
}

// Yjs relative positions name a shared type and, optionally, an item in it.
// Validate their shape here; only the collaborative editor can resolve them.
func validCommentPosition(raw json.RawMessage) bool {
	type itemID struct {
		Client *uint64 `json:"client"`
		Clock  *uint64 `json:"clock"`
	}
	var p struct {
		Type  *itemID `json:"type"`
		TName *string `json:"tname"`
		Item  *itemID `json:"item"`
		Assoc int64   `json:"assoc"`
	}
	validID := func(id *itemID) bool {
		return id != nil && id.Client != nil && id.Clock != nil && *id.Client <= 1<<53-1 && *id.Clock <= 1<<53-1
	}
	return json.Unmarshal(raw, &p) == nil && ((p.Type != nil && p.TName == nil && validID(p.Type)) || (p.Type == nil && p.TName != nil && *p.TName != "")) && (p.Item == nil || validID(p.Item))
}

// commentAgent turns what a caller says about itself into the stored pair,
// the way a note does: a known agent key or "generic", and a label that falls
// back to whatever it called itself.
func commentAgent(agent, label string) (string, string) {
	label = strings.TrimSpace(label)
	if label == "" {
		label = strings.TrimSpace(agent)
	}
	if r := []rune(label); len(r) > 60 {
		label = string(r[:60])
	}
	return normalizeAgent(agent), label
}

// handleResolveComment toggles a comment's resolved state.
func (s *Server) handleResolveComment(w http.ResponseWriter, r *http.Request) {
	commentID := r.PathValue("id")
	var pageID string
	if s.db.QueryRow(`SELECT page_id FROM comments WHERE id = ?`, commentID).Scan(&pageID) != nil {
		httpError(w, 404, "comment not found")
		return
	}
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	var body struct {
		Resolved bool `json:"resolved"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpErrorCode(w, 400, "comment_invalid_json", "Invalid comment request")
		return
	}
	if err := s.setThreadResolved(commentID, body.Resolved); err != nil {
		commentHTTPError(w, err)
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	commentID := r.PathValue("id")
	var pageID, authorID string
	if s.db.QueryRow(`SELECT page_id, author_id FROM comments WHERE id = ?`, commentID).Scan(&pageID, &authorID) != nil {
		httpError(w, 404, "comment not found")
		return
	}
	if !s.canReadReq(r, pageID) {
		httpErrorCode(w, 404, "comment_not_found", "Comment not found")
		return
	}
	// Author or a workspace admin may delete.
	u := requestUser(r)
	var ws string
	s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pageID).Scan(&ws)
	// And the credential has to be allowed into that workspace: the request
	// names a comment, not a page, so no page check covered it, and a token
	// deleted comments wherever its person had written one (delete_comment over
	// MCP had the same hole).
	if !s.credentialMayEnter(u, ws) {
		httpError(w, 404, "comment not found")
		return
	}
	if authorID != u.ID && !s.isWorkspaceAdmin(u.ID, ws) {
		httpError(w, 403, "forbidden")
		return
	}
	if err := s.deleteThreadComment(commentID); err != nil {
		commentHTTPError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleCommentCounts returns the number of OPEN comments per page of a
// workspace, in one go.
//
// Why its own endpoint and not a column in pageMetaCols: the page list is the
// hottest path in the whole application and is loaded on every navigation — a
// JOIN over comments would hang off it permanently, even though the number
// interests only two views (board and page header). Resolved ones do not
// count: a ticked-off thread is no longer an open task, and a counter that
// never goes down gets ignored.
func (s *Server) handleCommentCounts(w http.ResponseWriter, r *http.Request) {
	userID := requestUser(r).ID
	ws := r.URL.Query().Get("workspaceId")
	if ws == "" || !s.isMember(userID, ws) || !s.tokenReachesWorkspace(r, ws) {
		httpError(w, 404, "workspace not found")
		return
	}
	rows, err := s.db.Query(`SELECT c.page_id, COUNT(*) FROM comments c
		JOIN pages p ON p.id = c.page_id
		WHERE p.workspace_id = ? AND p.trashed_at IS NULL AND c.resolved_at IS NULL AND c.thread_id=''
 AND (c.deleted_at IS NULL OR EXISTS(SELECT 1 FROM comments r WHERE r.thread_id=c.id AND r.deleted_at IS NULL))
		GROUP BY c.page_id`, ws)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	type row struct {
		id string
		n  int
	}
	var list []row
	for rows.Next() {
		var it row
		if err := rows.Scan(&it.id, &it.n); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		list = append(list, it)
	}
	rows.Close() // drain first — a single DB connection

	out := map[string]int{}
	for _, it := range list {
		// Other people's private pages must not give themselves away through a counter.
		if s.canRead(userID, it.id) {
			out[it.id] = it.n
		}
	}
	writeJSON(w, out)
}
