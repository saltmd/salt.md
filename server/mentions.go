package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"unicode/utf16"
)

// Offsets use UTF-16 code units, matching textarea selectionStart/selectionEnd.
// Labels are snapshots; the selected account ID is the recipient's identity.
type commentMention struct {
	UserID string `json:"userId"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Label  string `json:"label"`
}
type mentionPerson struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

func (s *Server) mentionable(userID, pageID string) bool {
	u := s.userByID(userID)
	return u != nil && !u.Disabled && s.isMember(userID, s.pageWorkspace(pageID)) && s.canRead(userID, pageID)
}

func (s *Server) handleMentionPeople(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	rows, err := s.db.Query(`SELECT u.id, u.name, u.avatar FROM users u JOIN workspace_members m ON m.user_id=u.id WHERE m.workspace_id=? AND u.disabled=0 ORDER BY u.name, u.id`, s.pageWorkspace(pageID))
	if err != nil {
		httpError(w, 500, "could not load people")
		return
	}
	people := []mentionPerson{}
	for rows.Next() {
		var p mentionPerson
		if err := rows.Scan(&p.ID, &p.Name, &p.Avatar); err != nil {
			rows.Close()
			httpError(w, 500, "could not load people")
			return
		}
		people = append(people, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		httpError(w, 500, "could not load people")
		return
	}
	visible := []mentionPerson{}
	for _, p := range people {
		if s.canRead(p.ID, pageID) {
			visible = append(visible, p)
		}
	}
	writeJSON(w, visible)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	u := requestUser(r)
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	var body struct {
		Body      string           `json:"body"`
		BlockID   string           `json:"blockId"`
		Mentions  []commentMention `json:"mentions"`
		RequestID string           `json:"requestId"`
	}
	if decodeJSON(w, r, &body) != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Body) == "" || len([]rune(body.Body)) > maxCommentLen || len(body.Mentions) > 50 || len(body.RequestID) > 100 {
		httpError(w, 400, "invalid comment")
		return
	}
	// A retry after a lost response returns the original comment, even if the
	// selected person was renamed since it was sent. Reusing a key for different
	// content is a conflict, never a silent loss of the new text.
	if body.Mentions == nil {
		body.Mentions = []commentMention{}
	}
	sort.Slice(body.Mentions, func(i, j int) bool { return body.Mentions[i].Start < body.Mentions[j].Start })
	raw, _ := json.Marshal(body.Mentions)
	findExisting := func() (string, bool) {
		if body.RequestID == "" {
			return "", false
		}
		var id, p, text, block, mentions string
		err := s.db.QueryRow(`SELECT id,page_id,body,block_id,mentions FROM comments WHERE author_id=? AND request_id=?`, u.ID, body.RequestID).Scan(&id, &p, &text, &block, &mentions)
		if err != nil {
			return "", false
		}
		if p != pageID || text != body.Body || block != body.BlockID || mentions != string(raw) {
			httpError(w, 409, "request ID already used")
			return "", true
		}
		writeJSON(w, map[string]string{"id": id})
		return id, true
	}
	if _, found := findExisting(); found {
		return
	}
	units := utf16.Encode([]rune(body.Body))
	previous := 0
	recipients := map[string]bool{}
	for _, m := range body.Mentions {
		if m.Start < previous || m.End <= m.Start || m.End > len(units) || m.Label == "" || string(utf16.Decode(units[m.Start:m.End])) != "@"+m.Label || !s.mentionable(m.UserID, pageID) {
			httpError(w, 400, "a mentioned person is unavailable or the mention is invalid")
			return
		}
		target := s.userByID(m.UserID)
		if target == nil || target.Name != m.Label {
			httpError(w, 400, "a mentioned person changed; select them again")
			return
		}
		previous = m.End
		if m.UserID != u.ID {
			recipients[m.UserID] = true
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, "could not post comment")
		return
	}
	defer tx.Rollback()
	id := newID()
	timestamp := now()
	var requestID any
	if body.RequestID != "" {
		requestID = body.RequestID
	}
	_, err = tx.Exec(`INSERT INTO comments(id,page_id,block_id,author_id,author_name,body,created_at,mentions,request_id) VALUES(?,?,?,?,?,?,?,?,?)`, id, pageID, body.BlockID, u.ID, u.Name, body.Body, timestamp, string(raw), requestID)
	if err != nil {
		tx.Rollback()
		if _, found := findExisting(); found {
			return
		}
		httpError(w, 500, "could not post comment")
		return
	}
	for recipient := range recipients {
		if _, err = tx.Exec(`INSERT INTO mention_notifications(id,user_id,comment_id,created_at) VALUES(?,?,?,?)`, newID(), recipient, id, timestamp); err != nil {
			httpError(w, 500, "could not post comment")
			return
		}
	}
	if tx.Commit() != nil {
		httpError(w, 500, "could not post comment")
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

type mentionNotification struct {
	ID         string `json:"id"`
	PageID     string `json:"pageId"`
	PageTitle  string `json:"pageTitle"`
	CommentID  string `json:"commentId"`
	MentionID  string `json:"mentionId,omitempty"`
	content    string
	AuthorName string `json:"authorName"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
	Read       bool   `json:"read"`
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT n.id,c.page_id,p.title,c.id,c.author_name,c.body,n.created_at,n.read_at,'' AS mention_id,'' AS content
 FROM mention_notifications n JOIN comments c ON c.id=n.comment_id JOIN pages p ON p.id=c.page_id WHERE n.user_id=? AND p.trashed_at IS NULL
 UNION ALL
 SELECT n.id,n.page_id,p.title,'',n.author_name,'',n.created_at,n.read_at,n.mention_id,p.content
 FROM page_mention_notifications n JOIN pages p ON p.id=n.page_id WHERE n.user_id=? AND p.trashed_at IS NULL
 ORDER BY 7 DESC,1 DESC`, requestUser(r).ID, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, "could not load notifications")
		return
	}
	list := []mentionNotification{}
	for rows.Next() {
		var n mentionNotification
		var read sql.NullString
		if rows.Scan(&n.ID, &n.PageID, &n.PageTitle, &n.CommentID, &n.AuthorName, &n.Body, &n.CreatedAt, &read, &n.MentionID, &n.content) != nil {
			rows.Close()
			httpError(w, 500, "could not load notifications")
			return
		}
		n.Read = read.Valid
		list = append(list, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		httpError(w, 500, "could not load notifications")
		return
	}
	visible := []mentionNotification{}
	unread := 0
	for _, n := range list {
		// Recheck access on every read: revocation, moving a page and privacy
		// changes must hide both the excerpt and its unread badge immediately.
		if !s.canReadReq(r, n.PageID) {
			continue
		}
		if n.MentionID != "" {
			found := false
			for _, mention := range extractPageMentions([]byte(n.content)) {
				if mention.ID == n.MentionID && mention.UserID == requestUser(r).ID {
					n.Body = mention.Excerpt
					found = true
					break
				}
			}
			if !found {
				continue
			} // removal, API overwrite or a restored revision
		}
		if !n.Read {
			unread++
		}
		n.Body = string([]rune(n.Body)[:min(240, len([]rune(n.Body)))])
		visible = append(visible, n)
	}
	writeJSON(w, map[string]any{"items": visible, "unread": unread})
}
func (s *Server) handleReadNotification(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.Begin()
	if err != nil {
		httpError(w, 500, "could not mark notification read")
		return
	}
	defer tx.Rollback()
	var total int64
	for _, table := range []string{"mention_notifications", "page_mention_notifications"} {
		result, err := tx.Exec(`UPDATE `+table+` SET read_at=COALESCE(read_at,?) WHERE id=? AND user_id=?`, now(), r.PathValue("id"), requestUser(r).ID)
		if err != nil {
			httpError(w, 500, "could not mark notification read")
			return
		}
		n, _ := result.RowsAffected()
		total += n
	}
	if total == 0 {
		httpError(w, 404, "notification not found")
		return
	}
	if tx.Commit() != nil {
		httpError(w, 500, "could not mark notification read")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
