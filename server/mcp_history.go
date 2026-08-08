package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Agent parity, part 3: history, comments, graph.
//
// This is the part Notion structurally cannot offer: Salt keeps revisions and
// an audit trail that tells human and agent apart. An agent that can follow
// and undo its own changes is a different colleague from one that writes
// blind.

// mcpPageHistory lists the revisions of a page. On top of author and time, the
// audit trail says whether a HUMAN or an AGENT caused the change — exactly the
// distinction you need in order to trust automated edits.
func (s *Server) mcpPageHistory(pageID string, limit int) (string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, created_at, author_id, author_name, title, LENGTH(content)
		FROM page_revisions WHERE page_id = ? ORDER BY created_at DESC LIMIT ?`, pageID, limit)
	if err != nil {
		return "", err
	}
	type rev struct {
		ID        string `json:"id"`
		CreatedAt string `json:"created_at"`
		Author    string `json:"author"`
		Title     string `json:"title"`
		Size      int    `json:"content_bytes"`
		By        string `json:"by"` // "human" | "agent" | "unknown"
	}
	var out []rev
	type raw struct {
		r        rev
		authorID string
	}
	var list []raw
	for rows.Next() {
		var r rev
		var authorID string
		if err := rows.Scan(&r.ID, &r.CreatedAt, &authorID, &r.Author, &r.Title, &r.Size); err != nil {
			rows.Close()
			return "", err
		}
		list = append(list, raw{r, authorID})
	}
	rows.Close() // drain first — a single DB connection

	for _, it := range list {
		r := it.r
		// The audit trail knows whether the write came in through MCP. Mind the
		// order in time: a revision saves the state BEFORE the change and carries
		// that change's timestamp; the audit entry follows shortly after. So we look
		// for the NEXT entry by the same author, not the previous one — otherwise
		// everything would stay "unknown".
		var actorType string
		err := s.db.QueryRow(`SELECT actor_type FROM audit_log
			WHERE page_id = ? AND actor_id = ? AND created_at >= ?
			ORDER BY created_at ASC LIMIT 1`, pageID, it.authorID, r.CreatedAt).Scan(&actorType)
		if err != nil || actorType == "" {
			r.By = "unknown" // from before the audit trail, or not attributable
		} else {
			r.By = actorType
		}
		out = append(out, r)
	}
	if out == nil {
		out = []rev{}
	}
	b, err := json.Marshal(map[string]any{"page_id": pageID, "revisions": out})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpGetRevision returns an older state as Markdown — "read yesterday's
// version, compare the changes" — without altering anything.
func (s *Server) mcpGetRevision(pageID, revID string) (string, error) {
	var title, content, createdAt, author string
	err := s.db.QueryRow(`SELECT title, content, created_at, author_name FROM page_revisions
		WHERE id = ? AND page_id = ?`, revID, pageID).Scan(&title, &content, &createdAt, &author)
	if err != nil {
		return "", fmt.Errorf("revision %q not found on page %s", revID, pageID)
	}
	md := "# " + title + "\n\n" + blocksToMarkdown([]byte(content))
	b, err := json.Marshal(map[string]any{
		"page_id": pageID, "revision_id": revID, "created_at": createdAt,
		"author": author, "title": title, "markdown": md,
	})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpRestoreRevision puts a page back to an older state. The CURRENT state is
// saved as a new revision first, so that the restore itself stays reversible
// too.
func (s *Server) mcpRestoreRevision(u *user, pageID, revID string) (string, error) {
	var title, content string
	if err := s.db.QueryRow(`SELECT title, content FROM page_revisions WHERE id = ? AND page_id = ?`,
		revID, pageID).Scan(&title, &content); err != nil {
		return "", fmt.Errorf("revision %q not found on page %s", revID, pageID)
	}
	var curTitle, curContent string
	if err := s.db.QueryRow(`SELECT title, content FROM pages WHERE id = ?`, pageID).Scan(&curTitle, &curContent); err != nil {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	if _, err := s.db.Exec(`INSERT INTO page_revisions (id, page_id, created_at, author_id, author_name, title, content)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, newID(), pageID, now(), u.ID, u.Name, curTitle, curContent); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`UPDATE pages SET title = ?, content = ?, updated_at = ? WHERE id = ?`,
		title, content, now(), pageID); err != nil {
		return "", err
	}
	s.resetYjsDoc(pageID)
	s.reindexPage(pageID)
	s.pagesChanged()
	return fmt.Sprintf("Restored page %s to revision %s (the previous state was saved as a new revision first, so this is reversible)", pageID, revID), nil
}

// --- Kommentare ------------------------------------------------------------

// mcpResolveComment ticks a comment off. An agent could already read and
// write them, but not resolve one — so it could never finish a comment
// thread.
func (s *Server) mcpResolveComment(commentID string, resolved bool) (string, error) {
	var val any
	verb := "Resolved"
	if resolved {
		val = now()
	} else {
		val = nil
		verb = "Reopened"
	}
	res, err := s.db.Exec(`UPDATE comments SET resolved_at = ? WHERE id = ?`, val, commentID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("comment %q not found", commentID)
	}
	return fmt.Sprintf("%s comment %s", verb, commentID), nil
}

// mcpDeleteComment removes a comment for good.
func (s *Server) mcpDeleteComment(commentID string) (string, error) {
	res, err := s.db.Exec(`DELETE FROM comments WHERE id = ?`, commentID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("comment %q not found", commentID)
	}
	return fmt.Sprintf("Deleted comment %s", commentID), nil
}

// commentPage works out which page a comment belongs to, so the permission
// check can bite — otherwise an agent could write into somebody else's
// workspace through a comment id.
func (s *Server) commentPage(commentID string) (string, bool) {
	var pageID string
	if err := s.db.QueryRow(`SELECT page_id FROM comments WHERE id = ?`, commentID).Scan(&pageID); err != nil {
		return "", false
	}
	return pageID, true
}

// --- Graph -----------------------------------------------------------------

// graphNode is one page in the graph. kind separates the three things that
// look alike from outside: a document, a database, and a row inside one.
type graphNode struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // page | database | row
}

// graphEdge carries WHY two pages are connected. Without that an agent cannot
// tell "someone wrote about this" from "this lives inside that", and the three
// relationships that are not Markdown links were simply absent.
type graphEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	FromTitle string `json:"from_title"`
	ToTitle   string `json:"to_title"`
	Kind      string `json:"kind"` // link | child | row | embed
}

var graphEdgeKinds = map[string]bool{"link": true, "child": true, "row": true, "embed": true}

// mcpGraph returns how the pages of a workspace hang together.
//
// It used to return Markdown links and nothing else, while claiming in its own
// description to find orphans. It could not: the answer was a list of EDGES, so
// a page with no link was not in it — and an agent reads that empty answer as
// "there are no orphans". Measured once against a real workspace: 9 edges
// returned, 9 real relationships missing, 6 of 13 pages absent entirely.
//
// Now every edge says what kind it is, hierarchy and database rows and embeds
// produce edges of their own, and orphans are computed here rather than being
// something the caller is invited to infer from an absence.
//
// The permission check is done IN MEMORY, deliberately. canRead costs a query
// plus an ancestor walk, and per page over a real instance that is thousands of
// queries on the single connection this server has. The rule it applies is the
// same one: readable unless some ancestor is private and owned by somebody
// else, with workspace admins exempt.
func (s *Server) mcpGraph(u *user, wsID string, kinds []string, includeNodes bool) (string, error) {
	want := map[string]bool{}
	for _, k := range kinds {
		if !graphEdgeKinds[k] {
			return "", fmt.Errorf("unknown edge kind %q — use link, child, row or embed", k)
		}
		want[k] = true
	}
	if len(want) == 0 {
		want = graphEdgeKinds
	}
	// Like list_tags: by default ALL reachable workspaces (see mcpWorkspaceScope)
	// — otherwise the graph ends at the workspace boundary.
	ws, err := s.mcpWorkspaceScope(u, wsID)
	if err != nil {
		return "", err
	}
	wargs := make([]any, len(ws))
	for i, v := range ws {
		wargs[i] = v
	}

	// Every page in scope, once. Drain the cursor before any other query: with
	// SetMaxOpenConns(1) a query inside an open cursor blocks the whole server.
	type pageRow struct {
		id, parent, title, typ, visibility, owner, ws string
	}
	pages := map[string]*pageRow{}
	var order []string
	rows, err := s.db.Query(`
		SELECT id, COALESCE(parent_id, ''), title, type, visibility, owner_id, workspace_id
		FROM pages
		WHERE workspace_id IN (`+placeholders(len(ws))+`) AND trashed_at IS NULL`, wargs...)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var p pageRow
		if err := rows.Scan(&p.id, &p.parent, &p.title, &p.typ, &p.visibility, &p.owner, &p.ws); err != nil {
			rows.Close()
			return "", err
		}
		pages[p.id] = &p
		order = append(order, p.id)
	}
	rows.Close()

	admin := map[string]bool{}
	for _, w := range ws {
		admin[w] = s.isWorkspaceAdmin(u.ID, w)
	}
	// Same rule as forbiddenPrivateAncestor, walked over the map we already have.
	readable := map[string]bool{}
	visible := func(id string) bool {
		if v, done := readable[id]; done {
			return v
		}
		p, exists := pages[id]
		if !exists {
			return false
		}
		ok := true
		if !admin[p.ws] {
			// A parent chain should be a tree, but nothing in the schema enforces
			// it. Walk it with a seen-set: a cycle must end the walk, not the
			// server. (An id repeating means we already judged that ancestor.)
			seen := map[string]bool{}
			for cur := p; cur != nil && !seen[cur.id]; cur = pages[cur.parent] {
				seen[cur.id] = true
				if cur.visibility == "private" && cur.owner != u.ID {
					ok = false
					break
				}
				if cur.parent == "" {
					break
				}
			}
		}
		readable[id] = ok
		return ok
	}

	edges := []graphEdge{}
	add := func(from, to, kind string) {
		if !want[kind] || from == to {
			return
		}
		f, okF := pages[from]
		t, okT := pages[to]
		// Both ends have to be visible — otherwise the edge gives away that a
		// private page exists.
		if !okF || !okT || !visible(from) || !visible(to) {
			return
		}
		edges = append(edges, graphEdge{From: from, To: to, FromTitle: f.title, ToTitle: t.title, Kind: kind})
	}

	// Hierarchy: a parent that is a database makes its children ROWS, anything
	// else makes them child pages. That distinction is the one the sidebar makes
	// too, and it is why "row" is not just "child".
	for _, id := range order {
		p := pages[id]
		if p.parent == "" {
			continue
		}
		kind := "child"
		if parent, ok := pages[p.parent]; ok && parent.typ == "collection" {
			kind = "row"
		}
		add(p.parent, id, kind)
	}

	// Markdown links.
	if want["link"] {
		rows, err := s.db.Query(`
			SELECT l.source_id, l.target_id
			FROM links l
			JOIN pages s ON s.id = l.source_id
			JOIN pages t ON t.id = l.target_id
			WHERE s.workspace_id IN (`+placeholders(len(ws))+`) AND s.trashed_at IS NULL AND t.trashed_at IS NULL`, wargs...)
		if err != nil {
			return "", err
		}
		var pairs [][2]string
		for rows.Next() {
			var from, to string
			if err := rows.Scan(&from, &to); err != nil {
				rows.Close()
				return "", err
			}
			pairs = append(pairs, [2]string{from, to})
		}
		rows.Close()
		for _, p := range pairs {
			add(p[0], p[1], "link")
		}
	}

	// Embedded databases live as a block in the page body, so they are found by
	// reading it. The LIKE keeps that to the few pages that carry one.
	if want["embed"] {
		rows, err := s.db.Query(`
			SELECT id, content FROM pages
			WHERE workspace_id IN (`+placeholders(len(ws))+`) AND trashed_at IS NULL
			  AND content LIKE '%"collectionId"%'`, wargs...)
		if err != nil {
			return "", err
		}
		var bodies [][2]string
		for rows.Next() {
			var id, content string
			if err := rows.Scan(&id, &content); err != nil {
				rows.Close()
				return "", err
			}
			bodies = append(bodies, [2]string{id, content})
		}
		rows.Close()
		for _, b := range bodies {
			for _, target := range embeddedCollectionIDs(b[1]) {
				add(b[0], target, "embed")
			}
		}
	}

	kindOf := func(p *pageRow) string {
		if p.typ == "collection" {
			return "database"
		}
		if parent, ok := pages[p.parent]; ok && parent.typ == "collection" {
			return "row"
		}
		return "page"
	}
	connected := map[string]bool{}
	for _, e := range edges {
		connected[e.From], connected[e.To] = true, true
	}
	nodes, orphans := []graphNode{}, []graphNode{}
	for _, id := range order {
		p := pages[id]
		if !visible(id) {
			continue
		}
		n := graphNode{ID: id, Title: p.title, Kind: kindOf(p)}
		nodes = append(nodes, n)
		if !connected[id] {
			orphans = append(orphans, n)
		}
	}

	out := map[string]any{
		"edges":   edges,
		"count":   len(edges),
		"orphans": orphans,
		"counts": map[string]int{
			"nodes": len(nodes), "edges": len(edges), "orphans": len(orphans),
		},
	}
	// The full node list is opt-in: on a real instance it is thousands of
	// entries, and the question people actually ask ("what is unconnected?") is
	// answered by orphans.
	if includeNodes {
		out["nodes"] = nodes
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// embeddedCollectionIDs pulls the collectionId out of every database block in a
// page body. Deliberately a scan rather than a full BlockNote parse: the block
// shape has changed before, and a missed embed is a missing edge, not a crash.
func embeddedCollectionIDs(content string) []string {
	const key = `"collectionId":"`
	var out []string
	for {
		i := strings.Index(content, key)
		if i < 0 {
			return out
		}
		content = content[i+len(key):]
		j := strings.IndexByte(content, '"')
		if j < 0 {
			return out
		}
		if id := content[:j]; id != "" {
			out = append(out, id)
		}
		content = content[j:]
	}
}
