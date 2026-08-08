package server

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Audit trail + MCP rate limiting + idempotency (audit questions Q14/16/17).

// ---- audit log ----

// audit records a mutation. actorType is "human" or "agent" (MCP).
func (s *Server) audit(actorType, actorID, actorName, action, pageID, workspaceID, detail string) {
	s.db.Exec(`INSERT INTO audit_log (created_at, actor_type, actor_id, actor_name, action, page_id, workspace_id, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		now(), actorType, actorID, actorName, action, nullIfEmpty(pageID), nullIfEmpty(workspaceID), detail)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) pageWorkspace(pageID string) string {
	var ws string
	s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pageID).Scan(&ws)
	return ws
}

type auditEntry struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"createdAt"`
	ActorType string `json:"actorType"`
	ActorName string `json:"actorName"`
	Action    string `json:"action"`
	PageID    string `json:"pageId"`
	Detail    string `json:"detail"`
}

// handleAudit returns recent audit entries for the caller's workspaces.
// Keyset pagination: ?before=<id> returns older entries, ?limit caps the page
// (default 50, max 200) — so the whole history is reachable, not just the tail.
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	ws := s.scopeWorkspacesFor(requestUser(r), s.visibleWorkspaces(requestUser(r).ID))
	// Events that concern the whole instance (an account deactivated, a workspace
	// handed over or deleted) hang off no workspace, or off one that no longer
	// exists. The workspace filter therefore made them vanish for EVERYBODY —
	// precisely the events a log is kept for. The owner sees them; for everybody
	// else the workspace still decides. The instance admin too: they are the one
	// who deactivates accounts and changes assignments. If they could never find
	// their own action again, the log was worthless to them. The NULL rows name
	// only account and workspace names — both of which they administer anyway —
	// and no page titles.
	me := requestUser(r)
	instanceScope := (s.isOwner(me.ID) || me.IsAdmin) && me.TokenWorkspaces == nil
	if len(ws) == 0 && !instanceScope {
		writeJSON(w, []auditEntry{})
		return
	}
	args := make([]any, 0, len(ws))
	for _, v := range ws {
		args = append(args, v)
	}
	wsCond := "workspace_id IN (" + placeholders(len(ws)) + ")"
	if len(ws) == 0 {
		wsCond = "0"
	}
	if instanceScope {
		// IS NULL, not = '': s.audit writes a missing workspace as NULL (see
		// nullIfEmpty), and NULL compares equal to nothing.
		//
		// ONLY the NULL rows. An earlier addition also let through entries for
		// workspaces that no longer exist — meant as a way to keep events like
		// "workspace deleted" visible, but in fact the complete title list of every
		// deleted workspace: for create_page, `detail` holds the page title, and the
		// permission check below no longer bites because the page is gone. The events
		// themselves are now written without a workspace reference, which is what
		// puts them here.
		wsCond = "(" + wsCond + " OR workspace_id IS NULL)"
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	cursor := int64(0)
	if v, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && v > 0 {
		cursor = v
	}

	// The workspace filter alone is not enough: `detail` holds page titles (on
	// creation, for one), and paging through the log let anybody read the title
	// list of EVERY page ever created — including the private subtrees of other
	// people, which /api/pages and /api/search correctly hide.
	//
	// The filtering happens afterwards, so more has to be FETCHED until the page
	// is full: the interface recognises the end of the history by getting back
	// fewer than `limit` entries. If the list were simply truncated, everything
	// older than the first filtered-out entry would be out of reach.
	uid := requestUser(r).ID
	list := []auditEntry{}
	for round := 0; len(list) < limit && round < 20; round++ {
		qArgs := append([]any{}, args...)
		beforeSQL := ""
		if cursor > 0 {
			beforeSQL = " AND id < ?"
			qArgs = append(qArgs, cursor)
		}
		rows, err := s.db.Query(`SELECT id, created_at, actor_type, actor_name, action, COALESCE(page_id,''), detail
			FROM audit_log WHERE `+wsCond+beforeSQL+`
			ORDER BY id DESC LIMIT `+strconv.Itoa(limit), qArgs...)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		batch := []auditEntry{}
		for rows.Next() {
			var e auditEntry
			if rows.Scan(&e.ID, &e.CreatedAt, &e.ActorType, &e.ActorName, &e.Action, &e.PageID, &e.Detail) == nil {
				batch = append(batch, e)
			}
		}
		rows.Close() // drain first, then check row by row (one DB connection)
		if len(batch) == 0 {
			break // history exhausted
		}
		cursor = batch[len(batch)-1].ID
		for _, e := range batch {
			if len(list) == limit {
				break
			}
			// If the entry points at a page that NO LONGER exists, it stays: otherwise
			// exactly the events a log is there for would disappear — permanent deletion,
			// and, with the automatic emptying of the trash, gradually and on their own.
			if e.PageID != "" && s.pageExists(e.PageID) && !s.canRead(uid, e.PageID) {
				continue
			}
			list = append(list, e)
		}
		if len(batch) < limit {
			break // last page of the history
		}
	}
	writeJSON(w, list)
}

// ---- MCP rate limiting (token bucket per token) ----

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(perMinute float64, burst float64) *rateLimiter {
	return &rateLimiter{buckets: map[string]*bucket{}, rate: perMinute / 60.0, burst: burst}
}

// allow reports whether a request for key may proceed, consuming one token.
func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b := rl.buckets[key]
	nowT := time.Now()
	if b == nil {
		b = &bucket{tokens: rl.burst, last: nowT}
		rl.buckets[key] = b
	}
	b.tokens += nowT.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = nowT
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// exhausted answers "has this key already used up its budget" WITHOUT taking a
// token. It exists so a limiter can be fed by failures alone: the honest caller
// never fails, so it never consumes, and asking whether to reject must not
// charge it either.
func (rl *rateLimiter) exhausted(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b := rl.buckets[key]
	if b == nil {
		return false
	}
	return b.tokens+time.Since(b.last).Seconds()*rl.rate < 1
}

// ---- idempotency ----

// idempotentResult returns a cached result for key if present.
func (s *Server) idempotentResult(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	var res string
	if s.db.QueryRow(`SELECT result FROM idempotency WHERE key = ?`, key).Scan(&res) == nil {
		return res, true
	}
	return "", false
}

func (s *Server) storeIdempotent(key, result string) {
	if key == "" {
		return
	}
	s.db.Exec(`INSERT INTO idempotency (key, result, created_at) VALUES (?, ?, ?) ON CONFLICT(key) DO NOTHING`, key, result, now())
}
