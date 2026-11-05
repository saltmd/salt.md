package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type commentError struct {
	status  int
	code    string
	message string
}

func (e *commentError) Error() string { return e.message }
func commentHTTPError(w http.ResponseWriter, err error) {
	var ce *commentError
	if errors.As(err, &ce) {
		httpErrorCode(w, ce.status, ce.code, ce.message)
		return
	}
	httpErrorCode(w, 500, "comment_save_failed", "Could not save the comment")
}

type commentInput struct {
	Anchor    json.RawMessage `json:"anchor"`
	Body      string          `json:"body"`
	BlockID   string          `json:"blockId"`
	ReplyToID string          `json:"replyToId"`
	RequestID string          `json:"requestId"`
	Agent     string          `json:"agent"`
	Label     string          `json:"label"`
}

// Check outside and inside the transaction so simultaneous retries share one
// comment, including its attribution, and one notification per recipient.
func existingComment(q interface{ QueryRow(string, ...any) *sql.Row }, author, page string, in commentInput) (string, error) {
	if in.RequestID == "" {
		return "", nil
	}
	var id, p, body, block, target, agent, label, anchor string
	var deleted sql.NullString
	err := q.QueryRow(`SELECT id,page_id,body,block_id,reply_to_id,agent,agent_label,anchor,deleted_at FROM comments WHERE author_id=? AND request_id=?`, author, in.RequestID).Scan(&id, &p, &body, &block, &target, &agent, &label, &anchor, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if deleted.Valid || p != page || body != in.Body || block != in.BlockID || target != in.ReplyToID || agent != in.Agent || label != in.Label || anchor != string(in.Anchor) {
		return "", &commentError{409, "comment_request_conflict", "This submission id already belongs to another comment"}
	}
	return id, nil
}

func (s *Server) createThreadComment(u *user, pageID string, in commentInput) (string, error) {
	if !s.canWrite(u.ID, pageID) || !s.credentialMayEnter(u, s.pageWorkspace(pageID)) {
		return "", &commentError{403, "comment_forbidden", "You cannot write comments on this page."}
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || len([]rune(in.Body)) > maxCommentLen || len(in.RequestID) > 100 {
		return "", &commentError{400, "comment_invalid_input", "Invalid comment body or request id"}
	}
	var gen *int64
	if len(in.Anchor) > 0 && string(in.Anchor) != "null" {
		var a struct {
			Gen   *int64          `json:"gen"`
			Quote string          `json:"quote"`
			From  json.RawMessage `json:"from"`
			To    json.RawMessage `json:"to"`
		}
		if len(in.Anchor) > 32000 || json.Unmarshal(in.Anchor, &a) != nil || a.Gen == nil || *a.Gen < 0 || *a.Gen > 1<<53-1 || strings.TrimSpace(a.Quote) == "" || len([]rune(a.Quote)) > 2000 || !validCommentPosition(a.From) || !validCommentPosition(a.To) || in.ReplyToID != "" {
			return "", &commentError{400, "comment_anchor_invalid", "Invalid comment anchor"}
		}
		gen = a.Gen
	} else {
		in.Anchor = nil
	}
	if id, err := existingComment(s.db, u.ID, pageID, in); id != "" || err != nil {
		return id, err
	}
	threadID := ""
	recipients := map[string]bool{}
	if in.ReplyToID != "" {
		if in.BlockID != "" {
			return "", &commentError{400, "comment_reply_block", "Replies use the original comment's block."}
		}
		var targetPage, targetAuthor string
		var deleted sql.NullString
		err := s.db.QueryRow(`SELECT page_id,thread_id,author_id,deleted_at FROM comments WHERE id=?`, in.ReplyToID).Scan(&targetPage, &threadID, &targetAuthor, &deleted)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && (targetPage != pageID || deleted.Valid)) {
			return "", &commentError{404, "comment_not_found", "Comment not found"}
		}
		if err != nil {
			return "", err
		}
		if threadID == "" {
			threadID = in.ReplyToID
		}
		var rootAuthor string
		var resolved, rootDeleted sql.NullString
		err = s.db.QueryRow(`SELECT author_id,resolved_at,deleted_at FROM comments WHERE id=? AND page_id=? AND thread_id=''`, threadID, pageID).Scan(&rootAuthor, &resolved, &rootDeleted)
		if errors.Is(err, sql.ErrNoRows) {
			return "", &commentError{404, "comment_not_found", "Comment not found"}
		}
		if err != nil {
			return "", err
		}
		if resolved.Valid {
			return "", &commentError{409, "comment_thread_resolved", "Reopen this thread before replying. Your draft is preserved."}
		}
		recipients[targetAuthor] = true
		if !rootDeleted.Valid {
			recipients[rootAuthor] = true
		}
	}
	delete(recipients, u.ID)
	delete(recipients, "")
	for id := range recipients {
		var enabled int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id=? AND disabled=0`, id).Scan(&enabled); err != nil {
			return "", err
		}
		if enabled == 0 || !s.canRead(id, pageID) {
			delete(recipients, id)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if id, err := existingComment(tx, u.ID, pageID, in); id != "" || err != nil {
		return id, err
	}
	// Validate the target again under the write transaction, after recipient
	// permission checks have released the server's sole database connection.
	if in.ReplyToID != "" {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM comments target JOIN comments root ON root.id=? WHERE target.id=? AND target.page_id=? AND target.deleted_at IS NULL AND root.resolved_at IS NULL`, threadID, in.ReplyToID, pageID).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", &commentError{409, "comment_thread_changed", "The thread changed. Reopen it before replying."}
		}
	}
	id, created := newID(), nowFixed()
	result, err := tx.Exec(`INSERT INTO comments(id,page_id,block_id,author_id,author_name,body,created_at,thread_id,reply_to_id,request_id,agent,agent_label,anchor)
 SELECT ?,id,?,?,?,?,?,?,?,?,?,?,? FROM pages WHERE id=? AND trashed_at IS NULL AND (? IS NULL OR doc_gen=?)`, id, in.BlockID, u.ID, u.Name, in.Body, created, threadID, in.ReplyToID, in.RequestID, in.Agent, in.Label, string(in.Anchor), pageID, gen, gen)
	if err != nil {
		return "", err
	}
	if n, err := result.RowsAffected(); err != nil {
		return "", err
	} else if n == 0 {
		if gen != nil {
			return "", &commentError{409, "comment_anchor_stale", "The page changed. Select the text again."}
		}
		return "", &commentError{409, "comment_page_changed", "The page changed. Reload it before commenting."}
	}
	for recipient := range recipients {
		if _, err := tx.Exec(`INSERT INTO notifications(kind,recipient_id,page_id,comment_id,created_at) VALUES('comment_reply',?,?,?,?)`, recipient, pageID, id, created); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.commentsChanged(pageID)
	for recipient := range recipients {
		s.notificationsChanged(recipient)
	}
	return id, nil
}

func (s *Server) commentsChanged(pageID string) {
	// Page ids are sent only through credentials with current page access.
	s.events.broadcastTo(fmt.Sprintf(`{"type":"comments","id":%q}`, pageID), func(u *user) bool { return s.canRead(u.ID, pageID) && s.credentialMayEnter(u, s.pageWorkspace(pageID)) })
}
func (s *Server) notificationsChanged(recipient string) {
	s.events.broadcastTo(`{"type":"notifications"}`, func(u *user) bool { return u.ID == recipient })
}

func (s *Server) setThreadResolved(commentID string, resolved bool) error {
	var page, thread string
	if err := s.db.QueryRow(`SELECT page_id, CASE WHEN thread_id='' THEN id ELSE thread_id END FROM comments WHERE id=?`, commentID).Scan(&page, &thread); err != nil {
		return &commentError{404, "comment_not_found", "Comment not found"}
	}
	var at any
	if resolved {
		at = nowFixed()
	}
	if _, err := s.db.Exec(`UPDATE comments SET resolved_at=? WHERE id=? OR thread_id=?`, at, thread, thread); err != nil {
		return err
	}
	s.commentsChanged(page)
	return nil
}

func (s *Server) deleteThreadComment(id string) error {
	var page string
	if err := s.db.QueryRow(`SELECT page_id FROM comments WHERE id=?`, id).Scan(&page); err != nil {
		return &commentError{404, "comment_not_found", "Comment not found"}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT DISTINCT recipient_id FROM notifications WHERE comment_id=?`, id)
	if err != nil {
		return err
	}
	recipients := []string{}
	for rows.Next() {
		var recipient string
		if err := rows.Scan(&recipient); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, recipient)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var dependents int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM comments WHERE thread_id=? OR reply_to_id=?`, id, id).Scan(&dependents); err != nil {
		return err
	}
	if dependents == 0 {
		if _, err := tx.Exec(`DELETE FROM comments WHERE id=?`, id); err != nil {
			return err
		}
	} else {
		// Keep the structural placeholder, erase the text; never cascade into
		// another person's reply or a message that names this target.
		if _, err := tx.Exec(`UPDATE comments SET body='',block_id='',anchor='',deleted_at=COALESCE(deleted_at,?) WHERE id=?`, nowFixed(), id); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(`DELETE FROM notifications WHERE comment_id=?`, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.commentsChanged(page)
	for _, recipient := range recipients {
		s.notificationsChanged(recipient)
	}
	return nil
}

type replyNotification struct {
	Kind          string  `json:"kind"`
	ID            int64   `json:"id"`
	PageID        string  `json:"pageId"`
	PageTitle     string  `json:"pageTitle"`
	WorkspaceName string  `json:"workspaceName"`
	CommentID     string  `json:"commentId"`
	ThreadID      string  `json:"threadId"`
	ActorName     string  `json:"actorName"`
	ActorColor    string  `json:"actorColor"`
	ActorAvatar   string  `json:"actorAvatar"`
	Preview       string  `json:"preview"`
	CreatedAt     string  `json:"createdAt"`
	ReadAt        *string `json:"readAt"`
}

// Read notifications expire; unread notifications remain until acknowledged.
func notificationReadCutoff() string {
	return time.Now().UTC().AddDate(0, 0, -30).Format(time.RFC3339Nano)
}
func (s *Server) pruneReadNotifications() {
	s.db.Exec(`DELETE FROM notifications WHERE read_at < ?`, notificationReadCutoff())
}

const notificationFrom = ` FROM notifications n JOIN comments c ON c.id=n.comment_id JOIN pages p ON p.id=n.page_id
 WHERE n.recipient_id=? AND n.kind='comment_reply' AND c.deleted_at IS NULL AND p.trashed_at IS NULL AND (n.read_at IS NULL OR n.read_at >= ?)`

// Aggregate unread rows only: a badge refresh never loads a person's read history.
func (s *Server) notificationSummary(r *http.Request) (int, int64, error) {
	rows, err := s.db.Query(`SELECT n.page_id,COUNT(*),MAX(n.id)`+notificationFrom+` AND n.read_at IS NULL GROUP BY n.page_id`, requestUser(r).ID, notificationReadCutoff())
	if err != nil {
		return 0, 0, err
	}
	type count struct {
		page    string
		n       int
		through int64
	}
	counts := []count{}
	for rows.Next() {
		var c count
		if err := rows.Scan(&c.page, &c.n, &c.through); err != nil {
			rows.Close()
			return 0, 0, err
		}
		counts = append(counts, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, 0, err
	}
	unread, through := 0, int64(0)
	for _, c := range counts {
		if s.canReadReq(r, c.page) {
			unread += c.n
			if c.through > through {
				through = c.through
			}
		}
	}
	return unread, through, nil
}

// Keyset batches bound memory even if many intervening pages are inaccessible.
func (s *Server) visibleNotifications(r *http.Request, before int64, onlyUnread bool) ([]replyNotification, error) {
	visible := []replyNotification{}
	access := map[string]bool{}
	for {
		query := `SELECT n.id,n.kind,n.page_id,p.title,w.name,c.id,c.thread_id,c.author_name,COALESCE(u.color,''),COALESCE(u.avatar,''),substr(c.body,1,180),n.created_at,n.read_at
         FROM notifications n JOIN comments c ON c.id=n.comment_id JOIN pages p ON p.id=n.page_id JOIN workspaces w ON w.id=p.workspace_id LEFT JOIN users u ON u.id=c.author_id
         WHERE n.recipient_id=? AND n.kind='comment_reply' AND c.deleted_at IS NULL AND p.trashed_at IS NULL AND (n.read_at IS NULL OR n.read_at>=?)`
		args := []any{requestUser(r).ID, notificationReadCutoff()}
		if before > 0 {
			query += ` AND n.id<?`
			args = append(args, before)
		}
		if onlyUnread {
			query += ` AND n.read_at IS NULL`
		}
		rows, err := s.db.Query(query+` ORDER BY n.id DESC LIMIT 100`, args...)
		if err != nil {
			return nil, err
		}
		batch := []replyNotification{}
		for rows.Next() {
			var n replyNotification
			if err := rows.Scan(&n.ID, &n.Kind, &n.PageID, &n.PageTitle, &n.WorkspaceName, &n.CommentID, &n.ThreadID, &n.ActorName, &n.ActorColor, &n.ActorAvatar, &n.Preview, &n.CreatedAt, &n.ReadAt); err != nil {
				rows.Close()
				return nil, err
			}
			batch = append(batch, n)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, n := range batch {
			allowed, known := access[n.PageID]
			if !known {
				allowed = s.canReadReq(r, n.PageID)
				access[n.PageID] = allowed
			}
			if allowed {
				visible = append(visible, n)
				if len(visible) == 51 {
					return visible, nil
				}
			}
		}
		if len(batch) < 100 {
			return visible, nil
		}
		before = batch[len(batch)-1].ID
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	items, err := s.visibleNotifications(r, before, r.URL.Query().Get("unread") == "1")
	if err != nil {
		httpErrorCode(w, 500, "inbox_load_failed", "Could not load the inbox")
		return
	}
	unread, through, err := s.notificationSummary(r)
	if err != nil {
		httpErrorCode(w, 500, "inbox_load_failed", "Could not load the inbox")
		return
	}
	next := int64(0)
	if len(items) > 50 {
		items = items[:50]
		next = items[49].ID
	}
	writeJSON(w, map[string]any{"items": items, "unreadCount": unread, "through": through, "nextCursor": next})
}

func (s *Server) handleNotificationRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      int64 `json:"id"`
		Read    *bool `json:"read"`
		Through int64 `json:"through"`
	}
	if decodeJSON(w, r, &body) != nil || body.Read == nil || (body.ID < 0 || body.Through < 0 || (body.ID > 0) == (body.Through > 0)) || (body.Through > 0 && !*body.Read) {
		httpErrorCode(w, 400, "notification_update_invalid", "Invalid notification update")
		return
	}
	query := `SELECT DISTINCT n.page_id` + notificationFrom
	args := []any{requestUser(r).ID, notificationReadCutoff()}
	if body.ID > 0 {
		query += ` AND n.id=?`
		args = append(args, body.ID)
	} else {
		query += ` AND n.id<=? AND n.read_at IS NULL`
		args = append(args, body.Through)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		httpErrorCode(w, 500, "inbox_update_failed", "Could not update the inbox")
		return
	}
	pages := []string{}
	for rows.Next() {
		var page string
		if err = rows.Scan(&page); err != nil {
			break
		}
		pages = append(pages, page)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		httpErrorCode(w, 500, "inbox_update_failed", "Could not update the inbox")
		return
	}
	allowed := []string{}
	for _, page := range pages {
		if s.canReadReq(r, page) {
			allowed = append(allowed, page)
		}
	}
	if body.ID > 0 && len(allowed) == 0 {
		httpErrorCode(w, 404, "notification_not_found", "Notification not found")
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		httpErrorCode(w, 500, "inbox_update_failed", "Could not update the inbox")
		return
	}
	defer tx.Rollback()
	var at any
	if *body.Read {
		at = nowFixed()
	}
	for _, page := range allowed {
		query := `UPDATE notifications SET read_at=? WHERE recipient_id=? AND kind='comment_reply' AND page_id=?`
		args := []any{at, requestUser(r).ID, page}
		if body.ID > 0 {
			query += ` AND id=?`
			args = append(args, body.ID)
		} else {
			query += ` AND id<=? AND read_at IS NULL`
			args = append(args, body.Through)
		}
		if _, err := tx.Exec(query, args...); err != nil {
			httpErrorCode(w, 500, "inbox_update_failed", "Could not update the inbox")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpErrorCode(w, 500, "inbox_update_failed", "Could not update the inbox")
		return
	}
	s.notificationsChanged(requestUser(r).ID)
	writeJSON(w, map[string]bool{"ok": true})
}
