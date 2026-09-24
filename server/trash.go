package server

import (
	"database/sql"
	"html"
	"net/http"
	"strings"
	"time"
)

// What the trash shows beside each entry, and what it shows inside one.
//
// WHO put a page in the trash is not stored on the page. The activity log
// already records it — every trashing, from the browser as trash_page and over
// MCP as set_trashed, with the account and whether a person or an agent did it
// — and nothing prunes that log. Reading it here keeps one record of the event
// instead of two that could disagree, and it answers for pages that were
// already in the trash before this existed.
//
// The log names the page that was acted on, not every page in its subtree, so
// the answer is only found on the page somebody actually threw away — which is
// the entry the sidebar shows. Two things keep it from naming the wrong one:
//
//   - Only trashings count. set_trashed is also the tool that restores, and a
//     restore is logged under the same name; its entry carries the tool's
//     answer, and only a trashing answers "Moved page …" (mcpSetTrashed).
//   - It is matched by TIME, not by recency. The trashing that put a page where
//     it is now happened at its trashed_at, give or take the moment between
//     writing the log line and the row. An older trashing of the same page —
//     before somebody restored it and it went again with a parent — is not
//     that event, however recent it is.
//
// A wrong name is worse than none, so when nothing lies within trashWindow the
// entry names nobody. The window is generous for the ordinary gap of
// microseconds because requests queue for the one database connection, and a
// long import can hold it for a while.
const trashWindow = time.Minute

// attachTrashers fills TrashedBy and TrashedByAgent on the pages of a list that
// are in the trash. A page whose trashing the log cannot place keeps both
// empty, and the sidebar then says only when.
func (s *Server) attachTrashers(list []pageMeta) {
	at := map[string]int{}
	var ids []any
	for i, m := range list {
		if m.TrashedAt != "" {
			at[m.ID] = i
			ids = append(ids, m.ID)
		}
	}
	best := map[string]time.Duration{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		rows, err := s.db.Query(`SELECT page_id, actor_type, actor_name, created_at FROM audit_log
			WHERE page_id IN (`+placeholders(len(chunk))+`)
			AND (action = 'trash_page' OR (action = 'set_trashed' AND detail LIKE 'Moved page %'))`, chunk...)
		if err != nil {
			return
		}
		for rows.Next() {
			var pid, actorType, actorName, created string
			if rows.Scan(&pid, &actorType, &actorName, &created) != nil {
				continue
			}
			i := at[pid]
			logged, err1 := time.Parse(time.RFC3339Nano, created)
			trashed, err2 := time.Parse(time.RFC3339Nano, list[i].TrashedAt)
			if err1 != nil || err2 != nil {
				continue
			}
			d := logged.Sub(trashed).Abs()
			if prev, seen := best[pid]; d > trashWindow || (seen && d >= prev) {
				continue
			}
			best[pid] = d
			list[i].TrashedBy = actorName
			list[i].TrashedByAgent = actorType == "agent"
		}
		rows.Close()
	}
}

// handlePreviewPage answers with the page as a plain document, for looking into
// it without opening it. The trash needs exactly that: a page in the trash has
// no live document to join, and restoring something only to see what it is was
// the one way to find out until now.
//
// Read-only in every sense the browser offers. The markup comes from the same
// renderer as the HTML export, with no script in it, and the response forbids
// scripts anyway — a sandboxed document that keeps its origin, so pictures
// from /files/ still arrive with the session, while nothing in it can run.
// Whoever may read the page may preview it, in the trash or not, as reading it
// is allowed in both.
func (s *Server) handlePreviewPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canReadReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	p, err := s.getPage(id)
	if err == sql.ErrNoRows {
		httpError(w, 404, "page not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox allow-same-origin allow-popups allow-popups-to-escape-sandbox; "+
		"default-src 'none'; img-src * data: blob:; media-src 'self'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(s.previewHTML(p)))
}

// previewHTML is the export's markup and stylesheet without the paginating
// script. That is also why it is not pageHTML: that one parks the document in a
// <template> for its script to lay out on sheets, and a template is the one
// element a browser never draws — without the script there is nothing to see.
// White and centred here, rather than the grey desk the sheets lie on; links
// open beside the preview instead of inside it.
func (s *Server) previewHTML(p *page) string {
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	esc := html.EscapeString
	icon := ""
	if h := s.iconHTML(p.Icon); h != "" {
		icon = h + " "
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><base target="_blank"><title>`)
	b.WriteString(esc(title))
	b.WriteString(`</title><style>` + htmlDocStyle + previewStyle + `</style></head><body class="doc opt-icon opt-links">`)
	b.WriteString("<h1>" + icon + esc(title) + "</h1>")
	if d := strings.TrimSpace(p.Description); d != "" {
		b.WriteString(`<p class="doc-desc">` + esc(d) + "</p>")
	}
	b.WriteString(blocksToHTML(p.Content))
	b.WriteString("</body></html>")
	return b.String()
}

const previewStyle = `body{background:#fff;max-width:760px;margin:0 auto;padding:28px 32px 48px}`
