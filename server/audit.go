package server

import (
	"strings"
	"fmt"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Audit trail + MCP rate limiting + idempotency (audit questions Q14/16/17).

// ---- audit log ----

// audit records a mutation. actorType is "human" or "agent" (MCP).
func (s *Server) audit(actorType, actorID, actorName, action, pageID, workspaceID, detail string) {
	s.auditChanges(actorType, actorID, actorName, action, pageID, workspaceID, detail, "")
}

// propsDiff records what a property patch actually changes: for every key, the
// value before and the value after. Keys whose value is identical are left out,
// so dropping a board card back into the column it came from writes nothing.
//
// It lives here rather than in either caller because there are two of them —
// the browser's propsPatch and the MCP set_properties — and a log where the two
// disagree about the shape of a change is a log that cannot be replayed.
func propsDiff(before, patch map[string]json.RawMessage) map[string]propChange {
	diff := map[string]propChange{}
	for k, v := range patch {
		prev := json.RawMessage("null")
		if p, ok := before[k]; ok {
			prev = p
		}
		if jsonEqual(string(prev), string(v)) {
			continue
		}
		diff[k] = propChange{From: prev, To: v}
	}
	return diff
}

// auditChanges is audit plus the before/after of what was written. Without the
// before, a log can say that something was changed and never what it was — so
// the only way back is to remember, and nobody remembers 40 rows.
func (s *Server) auditChanges(actorType, actorID, actorName, action, pageID, workspaceID, detail, changes string) {
	s.db.Exec(`INSERT INTO audit_log (created_at, actor_type, actor_id, actor_name, action, page_id, workspace_id, detail, changes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		now(), actorType, actorID, actorName, action, nullIfEmpty(pageID), nullIfEmpty(workspaceID), detail, changes)
}

// propChange is one property's before and after, as raw JSON so a value keeps
// its type — a number stays a number, a multi-select stays a list.
type propChange struct {
	From json.RawMessage `json:"from"`
	To   json.RawMessage `json:"to"`
}

func (s *Server) pageTitle(id string) string {
	var t string
	s.db.QueryRow(`SELECT title FROM pages WHERE id = ?`, id).Scan(&t)
	if t == "" {
		return "Untitled"
	}
	return t
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
	// Whether this entry carries a before/after that can be taken back. The
	// diff itself stays on the server: the browser only needs to know whether
	// to offer the button.
	Revertible bool   `json:"revertible"`
	PageTitle  string `json:"pageTitle,omitempty"`
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
	// ?page=<id> narrows the log to one page. Without it, "what happened to this
	// row" means scrolling a whole workspace's history fifty entries at a time —
	// which is not something anybody does, so the answer was effectively not
	// available even though the data was there.
	pageFilter := r.URL.Query().Get("page")
	if pageFilter != "" && !s.canRead(requestUser(r).ID, pageFilter) {
		writeJSON(w, []auditEntry{})
		return
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
		pageSQL := ""
		if pageFilter != "" {
			pageSQL = " AND page_id = ?"
			qArgs = append(qArgs, pageFilter)
		}
		rows, err := s.db.Query(`SELECT id, created_at, actor_type, actor_name, action, COALESCE(page_id,''), detail, COALESCE(changes,''), COALESCE((SELECT title FROM pages WHERE pages.id = audit_log.page_id),'')
			FROM audit_log WHERE `+wsCond+beforeSQL+pageSQL+`
			ORDER BY id DESC LIMIT `+strconv.Itoa(limit), qArgs...)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		batch := []auditEntry{}
		for rows.Next() {
			var e auditEntry
			var changes string
			if rows.Scan(&e.ID, &e.CreatedAt, &e.ActorType, &e.ActorName, &e.Action, &e.PageID, &e.Detail, &changes, &e.PageTitle) == nil {
				e.Revertible = changes != ""
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

// handleAuditRevert takes back ONE recorded change, and only the part of it
// that is still exactly as the actor left it.
//
// That condition is the whole feature. "Undo what the agent did" is easy;
// "undo what the agent did without undoing what I did since" is the thing
// people actually need, and the only way to have it is to compare the current
// value against the recorded `to` before writing `from` back. A property
// somebody has edited since is left alone and reported as skipped, because
// quietly restoring it would be the exact failure this is meant to prevent.
func (s *Server) handleAuditRevert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpErrorCode(w, 400, "bad_id", "That is not an audit entry id.")
		return
	}
	var pageID, changes string
	err = s.db.QueryRow(`SELECT COALESCE(page_id,''), COALESCE(changes,'') FROM audit_log WHERE id = ?`, id).
		Scan(&pageID, &changes)
	if err == sql.ErrNoRows {
		httpErrorCode(w, 404, "not_found", "No such audit entry.")
		return
	} else if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if changes == "" || pageID == "" {
		httpErrorCode(w, 400, "not_revertible", "This entry records no property change that could be taken back.")
		return
	}
	u := requestUser(r)
	if !s.canWrite(u.ID, pageID) {
		httpErrorCode(w, 403, "forbidden", "You cannot write to that page.")
		return
	}

	var diff map[string]propChange
	if err := json.Unmarshal([]byte(changes), &diff); err != nil {
		httpErrorCode(w, 500, "bad_changes", "The recorded change could not be read.")
		return
	}

	var current string
	if err := s.db.QueryRow(`SELECT COALESCE(props,'{}') FROM pages WHERE id = ? AND trashed_at IS NULL`, pageID).
		Scan(&current); err == sql.ErrNoRows {
		httpErrorCode(w, 404, "page_gone", "That page is no longer there.")
		return
	} else if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	props := map[string]json.RawMessage{}
	json.Unmarshal([]byte(current), &props)

	reverted, skipped := []string{}, []string{}
	for key, ch := range diff {
		now, has := props[key]
		nowStr := "null"
		if has {
			nowStr = string(now)
		}
		// Byte comparison of the stored JSON. Both sides were written by the
		// same marshaller, so a value that has not been touched is byte-identical.
		if !jsonEqual(nowStr, string(ch.To)) {
			skipped = append(skipped, key)
			continue
		}
		if jsonEqual(string(ch.From), "null") {
			delete(props, key)
		} else {
			props[key] = ch.From
		}
		reverted = append(reverted, key)
	}

	if len(reverted) > 0 {
		blob, err := json.Marshal(props)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
		if _, err := s.db.Exec(`UPDATE pages SET props = ?, updated_at = ? WHERE id = ?`, string(blob), now(), pageID); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		s.reindexPage(pageID)
		s.pagesChanged()
		s.rowChanged(pageID)
		s.audit("human", u.ID, u.Name, "revert_change", pageID, s.pageWorkspace(pageID),
			fmt.Sprintf("%s — %s", s.pageTitle(pageID), strings.Join(reverted, ", ")))
	}
	writeJSON(w, map[string]any{"reverted": reverted, "skipped": skipped})
}

// jsonEqual compares two JSON values by meaning rather than by bytes, so
// whitespace or key order cannot make an untouched value look changed.
func jsonEqual(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return a == b
	}
	ab, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return string(ab) == string(bb)
}

// handleAuditPrune applies the retention period NOW instead of waiting for the
// nightly run. It exists because the setting is invisible otherwise: an admin
// shortens the period, nothing appears to happen, and the only way to find out
// whether it worked is to come back tomorrow.
//
// adminOnly, which already implies a signed-in browser session: an API token is
// a key to content, not a licence to destroy history.
func (s *Server) handleAuditPrune(w http.ResponseWriter, r *http.Request) {
	days := s.auditRetentionDays()
	if days <= 0 {
		httpErrorCode(w, 400, "audit_retention_off", "no retention period is set, so there is nothing to clean up")
		return
	}
	writeJSON(w, map[string]any{"removed": s.pruneAuditLog(), "days": days})
}
