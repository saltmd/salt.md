package server

import (
	"fmt"
	"net/http"
	"strings"
)

// The raw trail: dated notes beside the edited version of a page.
//
// Every write in salt.md is an act of authorship — title, place, icon, tags.
// That is right for a document and it is the hurdle a note taken in the middle
// of a problem does not clear. So the good write-ups happen AFTERWARDS, and by
// then the author knows how it ended: the abandoned approaches, the dead ends
// and the uncertainty are missing from them. Not out of dishonesty — they are
// missing because the ending is known.
//
// A note costs one call and carries no structure: note("rights check per page
// → thousands of queries, no good"). Its anchor is a page, and that is all.
//
// FOUR RULES, each load-bearing:
//
// APPEND-ONLY. A note can never be edited and never be deleted singly. The
// evidence does not die at deletion, it dies at editing: whoever may touch the
// 14:02 line at 16:00 already knows how it turned out, and then it is the
// coherent version again with timestamps in front. A note that was wrong is
// corrected by a NEW one ("14:02 was nonsense, see 14:19"). A correction that
// is itself dated is worth more than a silent fix.
//
// THE SAME PERMISSION AS THE PAGE, not one bit narrower. Anything else is a
// second permission model for one feature — and worse: a trail that looks
// different per reader is worthless as evidence. Whoever may read the result
// may see how it came about.
//
// NOTHING EXPIRES BY ITSELF. But a PERSON may discard a page's whole trail,
// deliberately and as a whole. Not formalism: a system that tidies up quietly
// devalues every trail, because you can never tell whether something is
// missing. A person doing it leaves a decision behind, and that gets logged.
//
// PEOPLE TOO, not just agents. The hurdle is literally the same one — it was
// described about a person. And a trail only machines keep is not "what
// happened" but "what the machine did": a worse record that leaves out the most
// interesting line, the one where a human drops the approach.
//
// working_on and note stay SEPARATE. They look alike and are opposites:
// presence is about now and has a lifetime; a note is a dated fact that never
// changes again. Merged, a fact would acquire a "done", which is nonsense. The
// bridge sits in presence.go instead: checking out leaves the last presence
// note behind as a trail entry, because those are already exactly this — short,
// written in the moment, honest — and were being thrown away.

const maxNoteLen = 2000

type pageNote struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`          // the account — verified
	Agent     string `json:"agent,omitempty"` // a claim, like presence; empty for a person
	Label     string `json:"label,omitempty"` // what an agent called itself
	CreatedAt string `json:"createdAt"`
}

// addNote appends one entry. The only way in — HTTP, MCP and the working_on
// bridge all land here, so the cap and the write cannot drift apart.
func (s *Server) addNote(u *user, pageID, body, agent, label string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", coded("note_empty", "a note needs text")
	}
	if r := []rune(body); len(r) > maxNoteLen {
		body = strings.TrimSpace(string(r[:maxNoteLen]))
	}
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO page_notes (id, page_id, author_id, author_name, agent, label, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, pageID, u.ID, u.Name, agent, strings.TrimSpace(label), body, now()); err != nil {
		return "", err
	}
	s.notesChanged(pageID)
	return id, nil
}

// notesChanged names the page and nothing else — same rule as presence: the
// event bus reaches every browser on the instance, so the text itself would be
// a leak. The browser refetches through a route that checks the permission.
func (s *Server) notesChanged(pageID string) {
	ws := s.pageWorkspace(pageID)
	if ws == "" {
		return
	}
	s.events.broadcastTo(fmt.Sprintf(`{"type":"notes","id":%q}`, pageID),
		func(uid string) bool { return s.isMember(uid, ws) })
}

func (s *Server) pageNotes(pageID string) ([]pageNote, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(author_name, ''), agent, label, body, created_at
		FROM page_notes WHERE page_id = ? ORDER BY created_at, rowid`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []pageNote{}
	for rows.Next() {
		var n pageNote
		if rows.Scan(&n.ID, &n.Author, &n.Agent, &n.Label, &n.Body, &n.CreatedAt) == nil {
			list = append(list, n)
		}
	}
	return list, nil
}

func (s *Server) handleListNotes(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canReadReq(r, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	list, err := s.pageNotes(pageID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, list)
}

// handleAddNote is the person's way in — same hurdle, same trail.
func (s *Server) handleAddNote(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	id, err := s.addNote(requestUser(r), pageID, body.Body, "", "")
	if err != nil {
		httpErrorFrom(w, 400, err)
		return
	}
	writeJSON(w, map[string]string{"id": id})
}

// handleClearNotes discards a page's whole trail. sessionOnly on the route: a
// person decides this, never a key handed to an agent — and least of all the
// agent whose trail it is. It is logged, so the gap in the record is itself a
// recorded decision rather than a silence.
func (s *Server) handleClearNotes(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	u := requestUser(r)
	if !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	res, err := s.db.Exec(`DELETE FROM page_notes WHERE page_id = ?`, pageID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	n, _ := res.RowsAffected()
	s.audit("user", u.ID, u.Name, "notes_cleared", pageID, s.pageWorkspace(pageID), fmt.Sprintf("%d", n))
	s.notesChanged(pageID)
	writeJSON(w, map[string]any{"ok": true, "removed": n})
}

// mcpNote is the whole tool. One argument that matters and no place to choose —
// the moment a note needs a parent page picked, the mode is dead.
func (s *Server) mcpNote(u *user, pageID, text, agent, label string) (string, error) {
	if pageID == "" {
		return "", fmt.Errorf("page_id is required")
	}
	if !s.canWrite(u.ID, pageID) {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	// The label falls back to what the agent called itself, so a client that
	// passes only agent: "claude" still reads as something rather than as an
	// empty column.
	if strings.TrimSpace(label) == "" {
		label = strings.TrimSpace(agent)
	}
	if _, err := s.addNote(u, pageID, text, normalizeAgent(agent), label); err != nil {
		return "", err
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM page_notes WHERE page_id = ?`, pageID).Scan(&n)
	return fmt.Sprintf("Noted, %d on that page now. Nobody can edit or remove a single one, including you — correct a wrong note by adding another.", n), nil
}
