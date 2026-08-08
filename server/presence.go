package server

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Agent presence: who says they are working on which page right now.
//
// Three decisions shape everything here, and each of them came from asking what
// an agent can actually do rather than what would be tidy.
//
// THE NAME IS A CLAIM. Nothing in a token says which agent is calling — a token
// belongs to a human. So the agent names itself, and any agent could name
// itself anything. That is fine for a presence badge and NOT fine to present as
// a fact, which is why the account travels with it: "Claude · via Jeremia". The
// second half is verified.
//
// NOTHING EXPIRES BY ITSELF. An agent has no clock and cannot wake itself up to
// say "still here". A ten-minute lease would therefore erase a three-hour job
// halfway through, and asking agents to send a heartbeat asks for something
// structurally impossible. Instead the entry stays until it is checked out; the
// interface FADES it using last_seen. Two timestamps ("here for 2h 14m, last
// seen 47 min ago") say what is known instead of claiming what is not.
//
// A SWEEP, NOT A LEASE. What has been silent for half a day is a crashed
// session, not a long job, and gets removed.

const (
	// presenceFresh is how long after its last call an agent still counts as
	// actively working. Only affects how it is DRAWN — never whether it is kept.
	presenceFresh = 10 * time.Minute
	// presenceMaxSilence is the zombie sweep: a session that has not been heard
	// from in this long crashed, and its badge would be a lie by now.
	presenceMaxSilence = 12 * time.Hour
)

// knownAgents are the ones with a logo of their own — deliberately the same
// list the "connect an agent" dialog offers, because those are the clients this
// instance actually advertises. Keeping the two in step is what makes a badge
// recognisable: the icon somebody saw while connecting is the icon they see
// working.
//
// An unknown name is NOT refused. A client that cannot announce itself simply
// would not, and then the feature is worth nothing on exactly the day a new one
// appears. Unknown becomes "generic", and what it called itself survives in the
// label.
var knownAgents = []string{"claude", "chatgpt", "codex", "cursor", "openclaw", "hermes", "gemini", "generic"}

func knownAgentList() string { return strings.Join(knownAgents, ", ") }

func normalizeAgent(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, k := range knownAgents {
		if n == k {
			return k
		}
	}
	return "generic"
}

type presenceEntry struct {
	PageID      string `json:"pageId"`
	PageTitle   string `json:"pageTitle"`
	Agent       string `json:"agent"` // a key from knownAgents; "generic" otherwise
	Label       string `json:"label"` // what it calls itself, when that is not the key
	Note        string `json:"note"`
	AccountName string `json:"accountName"`
	StartedAt   string `json:"startedAt"`
	LastSeen    string `json:"lastSeen"`
	ExpectedMin int    `json:"expectedMinutes"`
}

// mcpWorkingOn is the check-in and the check-out.
func (s *Server) mcpWorkingOn(u *user, pageID, agent, label, note string, expected int, done bool) (string, error) {
	if pageID == "" {
		return "", fmt.Errorf("page_id is required")
	}
	if !s.canRead(u.ID, pageID) {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	key := normalizeAgent(agent)
	if done {
		// The bridge to the raw trail (notelog.go). The note carried while
		// working is already what a trail entry is — short, written in the
		// moment, before the ending was known — and it used to be thrown away
		// at check-out. Read it BEFORE the delete, and take the note passed on
		// this call if there is one: "done, and here is how it went" is the
		// most useful last line there is.
		last := strings.TrimSpace(note)
		if last == "" {
			s.db.QueryRow(`SELECT note FROM agent_presence WHERE page_id = ? AND account_id = ? AND agent = ?`,
				pageID, u.ID, key).Scan(&last)
			last = strings.TrimSpace(last)
		}
		res, err := s.db.Exec(`DELETE FROM agent_presence WHERE page_id = ? AND account_id = ? AND agent = ?`,
			pageID, u.ID, key)
		if err != nil {
			return "", err
		}
		s.presenceChanged(pageID)
		if n, _ := res.RowsAffected(); n == 0 {
			return "Nothing to check out of — you were not marked as working on that page.", nil
		}
		kept := ""
		if last != "" {
			if _, err := s.addNote(u, pageID, last, key, strings.TrimSpace(label)); err == nil {
				kept = " Your last note stays on the page as a trail entry."
			}
		}
		s.audit("agent", u.ID, u.Name, "working_on_end", pageID, s.pageWorkspace(pageID), label)
		return fmt.Sprintf("Checked out of page %s.%s", pageID, kept), nil
	}
	if strings.TrimSpace(label) == "" {
		label = strings.TrimSpace(agent)
	}
	if expected < 0 {
		expected = 0
	}
	ts := now()
	// started_at survives a repeated check-in: saying "still on it, now with a
	// different note" must not reset how long it has been going.
	if _, err := s.db.Exec(`
		INSERT INTO agent_presence (page_id, account_id, agent, label, note, started_at, last_seen, expected_minutes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(page_id, account_id, agent) DO UPDATE SET
			label = excluded.label,
			note = excluded.note,
			last_seen = excluded.last_seen,
			expected_minutes = excluded.expected_minutes`,
		pageID, u.ID, key, label, strings.TrimSpace(note), ts, ts, expected); err != nil {
		return "", err
	}
	s.audit("agent", u.ID, u.Name, "working_on", pageID, s.pageWorkspace(pageID), note)
	s.presenceChanged(pageID)
	return fmt.Sprintf("Checked in on page %s. You stay listed until you check out (done: true) — nothing expires on you mid-task.", pageID), nil
}

// touchPresence refreshes last_seen for whatever this account has checked in on
// this page. Called after a successful MCP write, so an agent that works inside
// salt.md stays fresh without spending a call on saying so.
func (s *Server) touchPresence(userID, pageID string) {
	if userID == "" || pageID == "" {
		return
	}
	res, err := s.db.Exec(`UPDATE agent_presence SET last_seen = ? WHERE page_id = ? AND account_id = ?`,
		now(), pageID, userID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		s.presenceChanged(pageID)
	}
}

// sweepPresence drops sessions that crashed. Cheap enough to run on read.
func (s *Server) sweepPresence() {
	cutoff := time.Now().UTC().Add(-presenceMaxSilence).Format(time.RFC3339)
	s.db.Exec(`DELETE FROM agent_presence WHERE last_seen < ?`, cutoff)
}

// presenceChanged tells the workspace that the list moved. Content-free on
// purpose — see events.go: the browser refetches through a route that checks
// permissions per page.
func (s *Server) presenceChanged(pageID string) {
	ws := s.pageWorkspace(pageID)
	if ws == "" {
		return
	}
	s.events.broadcastTo(`{"type":"presence"}`, func(uid string) bool { return s.isMember(uid, ws) })
}

// handlePresence lists what the caller may see. The permission check is per
// page and it is the whole reason this is a route rather than a payload on the
// event: the event bus reaches every browser on the instance.
func (s *Server) handlePresence(w http.ResponseWriter, r *http.Request) {
	s.sweepPresence()
	u := requestUser(r)
	rows, err := s.db.Query(`
		SELECT p.page_id, COALESCE(pg.title, ''), p.agent, p.label, p.note,
		       COALESCE(us.name, ''), p.started_at, p.last_seen, p.expected_minutes
		FROM agent_presence p
		JOIN pages pg ON pg.id = p.page_id
		LEFT JOIN users us ON us.id = p.account_id
		WHERE pg.trashed_at IS NULL
		ORDER BY p.started_at`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	var all []presenceEntry
	for rows.Next() {
		var e presenceEntry
		if rows.Scan(&e.PageID, &e.PageTitle, &e.Agent, &e.Label, &e.Note,
			&e.AccountName, &e.StartedAt, &e.LastSeen, &e.ExpectedMin) == nil {
			all = append(all, e)
		}
	}
	rows.Close() // drain before the per-row permission checks — single connection

	out := []presenceEntry{}
	for _, e := range all {
		if s.canRead(u.ID, e.PageID) && s.tokenReachesWorkspace(r, s.pageWorkspace(e.PageID)) {
			out = append(out, e)
		}
	}
	writeJSON(w, map[string]any{"working": out, "freshSeconds": int(presenceFresh.Seconds())})
}
