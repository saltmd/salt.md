package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Calendar subscription (Welle 22): a stable per-user token grants a read-only
// iCalendar feed of every date-typed property across the collections the user
// can read. Calendar apps (Apple/Google/Outlook) poll GET /ics/{token}.ics; no
// login, the token IS the credential (like a share link).
//
// W120: the feed takes a SCOPE — everything (as before), one workspace, or one
// collection. One token still, because the token is the identity: narrowing is
// a view on what that person may read, never a way to see more. Every scope
// therefore runs through the same two checks (visible workspaces, then no
// forbidden private ancestor); an id the subscriber cannot read yields an empty
// calendar rather than an error, so a collection that gets moved or made
// private does not leave a broken subscription in somebody's calendar app.
//
// Why in the URL and not one token per scope: a calendar app remembers a URL
// and nothing else. Rotating the token then has to invalidate every feed at
// once — which is exactly what people expect from "revoke my calendar links".

func (s *Server) icsToken(userID string) string {
	tok := s.setting("ics_token_"+userID, "")
	if tok == "" {
		b := make([]byte, 18)
		rand.Read(b)
		tok = hex.EncodeToString(b)
		s.setSetting("ics_token_"+userID, tok)
	}
	return tok
}

// handleICSInfo returns the caller's subscription URL (rotating on request).
func (s *Server) handleICSInfo(w http.ResponseWriter, r *http.Request) {
	uid := requestUser(r).ID
	if r.URL.Query().Get("rotate") == "1" {
		s.setSetting("ics_token_"+uid, "")
	}
	tok := s.icsToken(uid)
	// External calendar apps subscribe to this URL — use the public base
	// (Domain/Tunnel) so the feed also works outside the LAN.
	base := s.publicShareBase(r)
	host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
	feed := func(query string) map[string]string {
		return map[string]string{
			"url":    base + "/ics/" + tok + ".ics" + query,
			"webcal": "webcal://" + host + "/ics/" + tok + ".ics" + query,
		}
	}

	// The scopes on offer: the whole account, each workspace, and each
	// collection that actually HAS a date property — offering a calendar for a
	// collection without dates would hand out a permanently empty feed.
	type scope struct {
		ID    string            `json:"id"`
		Kind  string            `json:"kind"` // all | workspace | collection
		Name  string            `json:"name"`
		Links map[string]string `json:"links"`
	}
	scopes := []scope{{ID: "", Kind: "all", Name: "", Links: feed("")}}

	ws := s.visibleWorkspaces(uid)
	if len(ws) > 0 {
		wargs := make([]any, len(ws))
		for i, v := range ws {
			wargs[i] = v
		}
		wrows, err := s.db.Query(`SELECT id, name FROM workspaces WHERE id IN (`+placeholders(len(ws))+`) ORDER BY name`, wargs...)
		if err == nil {
			for wrows.Next() {
				var id, nm string
				if wrows.Scan(&id, &nm) == nil {
					scopes = append(scopes, scope{ID: id, Kind: "workspace", Name: nm, Links: feed("?workspace=" + id)})
				}
			}
			wrows.Close()
		}
		crows, err := s.db.Query(`SELECT c.page_id, c.schema, p.title, p.workspace_id FROM collections c
			JOIN pages p ON p.id = c.page_id
			WHERE p.trashed_at IS NULL AND p.workspace_id IN (`+placeholders(len(ws))+`)
			ORDER BY p.title`, wargs...)
		if err == nil {
			type cand struct{ id, schema, title, ws string }
			var cands []cand
			for crows.Next() {
				var c cand
				if crows.Scan(&c.id, &c.schema, &c.title, &c.ws) == nil {
					cands = append(cands, c)
				}
			}
			crows.Close() // drain before the per-row permission checks (single conn)
			for _, c := range cands {
				var defs []propDef
				json.Unmarshal([]byte(c.schema), &defs)
				hasDate := false
				for _, d := range defs {
					if d.Type == "date" {
						hasDate = true
						break
					}
				}
				if !hasDate || s.forbiddenPrivateAncestor(uid, c.id, c.ws) {
					continue
				}
				title := c.title
				if title == "" {
					title = "Untitled"
				}
				scopes = append(scopes, scope{ID: c.id, Kind: "collection", Name: title, Links: feed("?collection=" + c.id)})
			}
		}
	}

	writeJSON(w, map[string]any{
		// The unscoped pair stays at the top level: older clients read exactly
		// these two fields.
		"url":    base + "/ics/" + tok + ".ics",
		"webcal": "webcal://" + host + "/ics/" + tok + ".ics",
		"scopes": scopes,
	})
}

func icsEscape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n", "\r", "")
	return r.Replace(s)
}

// icsDate formats a stored date value as an all-day (VALUE=DATE) or timed field.
// Accepts "2026-07-18" and "2026-07-18T14:30".
func icsDate(prop, v string) string {
	if len(v) >= 10 && v[4] == '-' && v[7] == '-' {
		day := strings.ReplaceAll(v[:10], "-", "")
		if len(v) >= 16 && v[10] == 'T' {
			hm := strings.ReplaceAll(v[11:16], ":", "")
			return prop + ":" + day + "T" + hm + "00"
		}
		return prop + ";VALUE=DATE:" + day
	}
	return ""
}

func (s *Server) handleICSFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("token"), ".ics")
	// Reverse-lookup the owning user (small instance; linear scan of the few
	// ics_token_* settings rows).
	var userID string
	rows, err := s.db.Query(`SELECT key FROM app_settings WHERE key LIKE 'ics_token_%' AND value = ?`, token)
	if err == nil {
		if rows.Next() {
			var key string
			rows.Scan(&key)
			userID = strings.TrimPrefix(key, "ics_token_")
		}
		rows.Close()
	}
	if userID == "" {
		httpError(w, 404, "not found")
		return
	}

	ws := s.visibleWorkspaces(userID)
	// Scope. A workspace the subscriber is not a member of simply drops out of
	// `ws` below, so a guessed id cannot widen the feed.
	scopeWS := r.URL.Query().Get("workspace")
	scopeCol := r.URL.Query().Get("collection")
	if scopeWS != "" {
		only := ws[:0]
		for _, w := range ws {
			if w == scopeWS {
				only = append(only, w)
			}
		}
		ws = only
	}

	var b strings.Builder
	name := "salt.md"
	if scopeCol != "" {
		// The calendar's NAME is what the subscriber sees in their app, so it
		// says which collection or workspace this feed is — three feeds called
		// "salt.md" would be indistinguishable there.
		var title string
		s.db.QueryRow(`SELECT title FROM pages WHERE id = ?`, scopeCol).Scan(&title)
		if title != "" {
			name = "salt.md · " + title
		}
	} else if scopeWS != "" {
		var title string
		s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, scopeWS).Scan(&title)
		if title != "" {
			name = "salt.md · " + title
		}
	}
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//salt.md//Calendar//EN\r\nCALSCALE:GREGORIAN\r\nX-WR-CALNAME:" + icsEscape(name) + "\r\n")

	if len(ws) > 0 {
		wargs := make([]any, len(ws))
		for i, v := range ws {
			wargs[i] = v
		}
		q := `SELECT c.page_id, c.schema, p.title, p.workspace_id FROM collections c
			JOIN pages p ON p.id = c.page_id
			WHERE p.trashed_at IS NULL AND p.workspace_id IN (` + placeholders(len(ws)) + `)`
		if scopeCol != "" {
			q += ` AND c.page_id = ?`
			wargs = append(wargs, scopeCol)
		}
		// Every collection in scope + its date-typed props.
		crows, err := s.db.Query(q, wargs...)
		if err == nil {
			type coll struct{ id, schema, title, ws string }
			var colls []coll
			for crows.Next() {
				var c coll
				if crows.Scan(&c.id, &c.schema, &c.title, &c.ws) == nil {
					colls = append(colls, c)
				}
			}
			crows.Close() // drain before per-collection row queries (single conn)

			// Drop collections in private subtrees the subscriber can't read —
			// membership alone is not enough (same rule handleListPages applies).
			readable := colls[:0]
			for _, c := range colls {
				if !s.forbiddenPrivateAncestor(userID, c.id, c.ws) {
					readable = append(readable, c)
				}
			}
			colls = readable

			stamp := time.Now().UTC().Format("20060102T150405Z")
			for _, c := range colls {
				dateProps := map[string]string{} // id -> name
				var defs []propDef
				json.Unmarshal([]byte(c.schema), &defs)
				for _, d := range defs {
					if d.Type == "date" {
						dateProps[d.ID] = d.Name
					}
				}
				if len(dateProps) == 0 {
					continue
				}
				rrows, err := s.db.Query(`SELECT id, title, props FROM pages WHERE parent_id = ? AND trashed_at IS NULL`, c.id)
				if err != nil {
					continue
				}
				type row struct{ id, title, props string }
				var rowsData []row
				for rrows.Next() {
					var rw row
					if rrows.Scan(&rw.id, &rw.title, &rw.props) == nil {
						rowsData = append(rowsData, rw)
					}
				}
				rrows.Close()
				for _, rw := range rowsData {
					var pm map[string]any
					json.Unmarshal([]byte(rw.props), &pm)
					for pid, pname := range dateProps {
						v, _ := pm[pid].(string)
						dt := icsDate("DTSTART", v)
						if dt == "" {
							continue
						}
						title := rw.title
						if title == "" {
							title = "Untitled"
						}
						b.WriteString("BEGIN:VEVENT\r\n")
						b.WriteString("UID:" + rw.id + "-" + pid + "@salt.md\r\n")
						b.WriteString("DTSTAMP:" + stamp + "\r\n")
						b.WriteString(dt + "\r\n")
						b.WriteString("SUMMARY:" + icsEscape(title) + " (" + icsEscape(pname) + ")\r\n")
						b.WriteString("DESCRIPTION:" + icsEscape(c.title) + "\r\n")
						b.WriteString("END:VEVENT\r\n")
					}
				}
			}
		}
	}
	b.WriteString("END:VCALENDAR\r\n")

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="salt.ics"`)
	fmt.Fprint(w, b.String())
}
