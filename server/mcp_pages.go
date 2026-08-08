package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Agent parity, part 1: pages and content.
//
// The guiding rule for the whole MCP build-out: whatever a human can do in the
// interface, an agent has to be able to do over MCP. Everything here mirrors an
// existing REST endpoint and uses the same checks — MCP is not a back door (see
// the central permission check in mcpCall).

// --- Setting page metadata in full -----------------------------------------

// mcpUpdatePageMeta sets title, icon, cover, description, tags and visibility
// — the same fields as PATCH /api/pages/{id}. Before this an agent could only
// change title and icon, and was shut out of tags, cover and visibility even
// though the interface offers them.
func (s *Server) mcpUpdatePageMeta(pageID, title, icon, cover, description, visibility string, tags *[]string) (string, error) {
	sets := []string{"updated_at = ?"}
	sqlArgs := []any{now()}
	changed := []string{}

	add := func(col, val, label string) {
		sets = append(sets, col+" = ?")
		sqlArgs = append(sqlArgs, val)
		changed = append(changed, label)
	}
	if title != "" {
		add("title", title, "title")
	}
	if icon != "" {
		add("icon", icon, "icon")
	}
	if cover != "" {
		// The same check the REST handler makes (see validCover in pages.go).
		// It was missing here, and the gap was not cosmetic: anything that does
		// not start with "gradient:" is rendered as url(<value>), so a cover set
		// over MCP could point at a foreign host and every viewer of that page
		// would fetch from it — a beacon, planted by an agent, in a document
		// other people open.
		if !validCover(cover) {
			return "", fmt.Errorf("invalid cover — use %q or an uploaded %q path; an external URL is refused because every viewer of the page would fetch from that host",
				"gradient:linear-gradient(...)", "/files/...")
		}
		add("cover", cover, "cover")
	}
	if description != "" {
		add("description", description, "description")
	}
	if visibility != "" {
		if visibility != "workspace" && visibility != "private" {
			return "", fmt.Errorf("visibility must be %q or %q", "workspace", "private")
		}
		add("visibility", visibility, "visibility")
	}
	if tags != nil {
		// The same normalisation as the REST layer: drop '#', spaces become
		// '-', duplicates out. Otherwise the agent would create tag variants
		// the interface would never produce.
		sets = append(sets, "tags = ?")
		sqlArgs = append(sqlArgs, string(normalizeTags(*tags)))
		changed = append(changed, "tags")
	}
	if len(changed) == 0 {
		return "", fmt.Errorf("nothing to update: pass at least one of title, icon, cover, description, visibility, tags")
	}

	sqlArgs = append(sqlArgs, pageID)
	res, err := s.db.Exec(`UPDATE pages SET `+strings.Join(sets, ", ")+` WHERE id = ? AND trashed_at IS NULL`, sqlArgs...)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	s.reindexPage(pageID)
	s.pagesChanged()
	return fmt.Sprintf("Updated %s on page %s", strings.Join(changed, ", "), pageID), nil
}

// --- Inhalt ersetzen --------------------------------------------------------

// mcpReplaceContent replaces the ENTIRE page content with the Markdown given.
//
// Until now an agent could only append — fixing a typo or rewriting a paragraph
// was impossible. An honest limitation, the same one append_markdown has: the
// write path goes through SQL plus a reset of the Yjs document, not through the
// CRDT. Whoever has the page open in the editor at that exact moment loses
// their unsaved changes. That is why it is in the tool description too, so an
// agent can weigh it up.
func (s *Server) mcpReplaceContent(u *user, pageID, md string) (string, error) {
	content, err := mdToBlocksJSON(md)
	if err != nil {
		return "", err
	}
	// Save the OLD state first, then overwrite. Without this a change made by an
	// agent would be beyond recovery — and get_page_history would stay empty even
	// though the agent just replaced half the page.
	s.snapshotRevision(pageID, u.ID, u.Name)
	res, err := s.db.Exec(`UPDATE pages SET content = ?, updated_at = ? WHERE id = ? AND trashed_at IS NULL`,
		content, now(), pageID)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	s.resetYjsDoc(pageID)
	s.reindexPage(pageID)
	s.pagesChanged()
	s.fireWebhook("page.updated", pageID)
	return fmt.Sprintf("Replaced content of page %s", pageID), nil
}

// mcpPrependMarkdown puts Markdown BEFORE the existing content. Notion can do
// that ("insert at start"), Salt could not until now.
func (s *Server) mcpPrependMarkdown(u *user, pageID, md string) (string, error) {
	blocks := mdToBlocks(md)
	if len(blocks) == 0 {
		return "", fmt.Errorf("markdown is empty")
	}
	s.snapshotRevision(pageID, u.ID, u.Name) // save the old state first, see mcpReplaceContent
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var content string
	if err := tx.QueryRow(`SELECT content FROM pages WHERE id = ? AND trashed_at IS NULL`, pageID).Scan(&content); err != nil {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	var existing []json.RawMessage
	if err := json.Unmarshal([]byte(content), &existing); err != nil {
		existing = []json.RawMessage{}
	}
	head := make([]json.RawMessage, 0, len(blocks)+len(existing))
	for _, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			return "", err
		}
		head = append(head, raw)
	}
	merged, err := json.Marshal(append(head, existing...))
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE pages SET content = ?, updated_at = ? WHERE id = ?`, string(merged), now(), pageID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.resetYjsDoc(pageID)
	s.reindexPage(pageID)
	s.pagesChanged()
	return fmt.Sprintf("Prepended %d block(s) to page %s", len(blocks), pageID), nil
}

// --- Lesen: Backlinks, Tags, Export ----------------------------------------

// mcpBacklinks: which pages point here. The slip-box side of things that
// Obsidian users expect — and that Notion's MCP does not have at all.
func (s *Server) mcpBacklinks(userID, pageID string) (string, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.title, p.icon FROM links l
		JOIN pages p ON p.id = l.source_id
		WHERE l.target_id = ? AND p.trashed_at IS NULL
		ORDER BY p.updated_at DESC`, pageID)
	if err != nil {
		return "", err
	}
	type ref struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Icon  string `json:"icon"`
	}
	var cand []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.ID, &r.Title, &r.Icon); err != nil {
			rows.Close()
			return "", err
		}
		cand = append(cand, r)
	}
	rows.Close() // drain first, then check — a single DB connection
	out := []ref{}
	for _, r := range cand {
		if s.canRead(userID, r.ID) { // private subtrees stay invisible
			out = append(out, r)
		}
	}
	b, err := json.Marshal(map[string]any{"page_id": pageID, "backlinks": out})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpListTags returns every tag of the workspace with its frequency — so an
// agent reuses the tags that exist instead of inventing new ones on every
// write.
func (s *Server) mcpListTags(u *user, wsID string) (string, error) {
	// By default ALL reachable workspaces, not just the first. This tool used
	// to hang off the default workspace: as soon as content lived elsewhere an
	// agent was blind, even though its token had access.
	ws, err := s.mcpWorkspaceScope(u, wsID)
	if err != nil {
		return "", err
	}
	wargs := make([]any, len(ws))
	for i, v := range ws {
		wargs[i] = v
	}
	rows, err := s.db.Query(`SELECT id, tags FROM pages WHERE workspace_id IN (`+placeholders(len(ws))+`) AND trashed_at IS NULL AND tags IS NOT NULL AND tags != ''`, wargs...)
	if err != nil {
		return "", err
	}
	type row struct{ id, tags string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.tags); err != nil {
			rows.Close()
			return "", err
		}
		all = append(all, r)
	}
	rows.Close()

	counts := map[string]int{}
	for _, r := range all {
		if !s.canRead(u.ID, r.id) {
			continue
		}
		var list []string
		if json.Unmarshal([]byte(r.tags), &list) != nil {
			continue
		}
		for _, t := range list {
			counts[t]++
		}
	}
	type tagCount struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tagCount{}
	for t, n := range counts {
		out = append(out, tagCount{t, n})
	}
	// Most frequent first; alphabetical on a tie, so the output is stable.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].Count > out[j-1].Count ||
			(out[j].Count == out[j-1].Count && out[j].Tag < out[j-1].Tag)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	b, err := json.Marshal(map[string]any{"tags": out})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpExportMarkdown returns a page as Markdown, with its subtree on request.
// "Salt is called salt.md — that ought to show in the API."
func (s *Server) mcpExportMarkdown(userID, pageID string, recursive bool) (string, error) {
	var out strings.Builder
	var walk func(id string, depth int) error
	walk = func(id string, depth int) error {
		var title, content string
		if err := s.db.QueryRow(`SELECT title, content FROM pages WHERE id = ? AND trashed_at IS NULL`, id).Scan(&title, &content); err != nil {
			return fmt.Errorf("page %q not found", id)
		}
		if depth > 0 {
			out.WriteString("\n\n---\n\n")
		}
		out.WriteString(strings.Repeat("#", min(depth+1, 6)) + " " + title + "\n\n")
		out.WriteString(blocksToMarkdown([]byte(content)))
		if !recursive {
			return nil
		}
		kids, err := s.db.Query(`SELECT id FROM pages WHERE parent_id = ? AND trashed_at IS NULL ORDER BY position`, id)
		if err != nil {
			return err
		}
		var ids []string
		for kids.Next() {
			var k string
			if err := kids.Scan(&k); err != nil {
				kids.Close()
				return err
			}
			ids = append(ids, k)
		}
		kids.Close() // drain before the recursive call needs the connection
		for _, k := range ids {
			if !s.canRead(userID, k) {
				continue
			}
			if err := walk(k, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(pageID, 0); err != nil {
		return "", err
	}
	return out.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- Trash and favourites --------------------------------------------------

// mcpRestorePage brings a page back out of the trash. The counterpart to
// trash_page: without this tool an agent could neither judge nor undo the
// consequences of its own deletion.
func (s *Server) mcpRestorePage(pageID string) (string, error) {
	var trashed sql.NullString
	if err := s.db.QueryRow(`SELECT trashed_at FROM pages WHERE id = ?`, pageID).Scan(&trashed); err != nil {
		return "", fmt.Errorf("page %q not found", pageID)
	}
	if !trashed.Valid {
		return "", fmt.Errorf("page %q is not in the trash", pageID)
	}
	if _, err := s.db.Exec(`UPDATE pages SET trashed_at = NULL, updated_at = ? WHERE id = ?`, now(), pageID); err != nil {
		return "", err
	}
	s.reindexPage(pageID)
	s.pagesChanged()
	return fmt.Sprintf("Restored page %s from the trash", pageID), nil
}

// mcpSetFavorite marks a page as a favourite (per user) or takes the mark
// away.
func (s *Server) mcpSetFavorite(userID, pageID string, on bool) (string, error) {
	if on {
		// The same insert as the REST handler: the table has position and no
		// created_at, and the favourite lands at the end of the list.
		var pos float64
		s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM favorites WHERE user_id = ?`, userID).Scan(&pos)
		if _, err := s.db.Exec(`INSERT INTO favorites (user_id, page_id, position) VALUES (?, ?, ?)
			ON CONFLICT(user_id, page_id) DO NOTHING`, userID, pageID, pos); err != nil {
			return "", err
		}
		return fmt.Sprintf("Page %s added to favourites", pageID), nil
	}
	if _, err := s.db.Exec(`DELETE FROM favorites WHERE user_id = ? AND page_id = ?`, userID, pageID); err != nil {
		return "", err
	}
	return fmt.Sprintf("Page %s removed from favourites", pageID), nil
}

// mcpEmbedDatabase appends a database block to a document.
//
// What prompted it: an agent used to have to create an introductory document
// AND a database separately, because a database page cannot have a body of
// text. With this block both belong to ONE document — exactly like Notion's
// "inline database". Only the reference is stored; the database stays one
// object in one place and can appear in several documents.
func (s *Server) mcpEmbedDatabase(u *user, pageID, databaseID string) (string, error) {
	var typ, title string
	if err := s.db.QueryRow(`SELECT type, title FROM pages WHERE id = ? AND trashed_at IS NULL`, databaseID).Scan(&typ, &title); err != nil {
		return "", fmt.Errorf("database %q not found", databaseID)
	}
	if typ != "collection" {
		return "", fmt.Errorf("page %q is a document, not a database — embed only works with databases", title)
	}
	if !s.canRead(u.ID, databaseID) {
		return "", fmt.Errorf("database %q not found", databaseID)
	}
	block := fmt.Sprintf(`{"id":%q,"type":"database","props":{"collectionId":%q},"content":[],"children":[]}`,
		newID(), databaseID)
	s.snapshotRevision(pageID, u.ID, u.Name)
	if err := s.appendBlockJSON(pageID, block); err != nil {
		return "", err
	}
	s.resetYjsDoc(pageID)
	s.pagesChanged()
	return fmt.Sprintf("Embedded database %q into page %s. The database itself stays where it is — this is a view, not a copy.", title, pageID), nil
}

// mcpWorkspaceScope returns the workspaces a reading tool should run over:
// either the one named explicitly (if reachable) or ALL the ones the token
// reaches.
//
// Why not the default workspace: tools that silently searched only there left
// multi-workspace setups half blind — content in a second workspace was
// invisible to the agent even though its token was allowed to read it. It came
// up when a database had been moved to "Personal" and list_tags found nothing
// afterwards.
func (s *Server) mcpWorkspaceScope(u *user, wsID string) ([]string, error) {
	if wsID != "" {
		if !s.isMember(u.ID, wsID) || !s.credentialMayEnter(u, wsID) {
			return nil, fmt.Errorf("workspace %q not found", wsID)
		}
		return []string{wsID}, nil
	}
	ws := s.scopeWorkspacesFor(u, s.visibleWorkspaces(u.ID))
	if len(ws) == 0 {
		return nil, fmt.Errorf("no workspace available")
	}
	return ws, nil
}

// mcpSetTagColor sets the colour of a tag (per workspace). An agent could
// hand out tags but not colour them — the interface can. Palette and
// normalisation are deliberately the same as in the REST handler (tags.go), or
// an agent could set a colour nobody is able to render.
func (s *Server) mcpSetTagColor(u *user, wsID, tag, color string) (string, error) {
	ws, err := s.mcpCreateWorkspaceTarget(u, wsID)
	if err != nil {
		return "", err
	}
	tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(tag), "#")))
	if tag == "" {
		return "", fmt.Errorf("tag is required")
	}
	color = strings.ToLower(strings.TrimSpace(color))
	if color == "" || color == "default" {
		if _, err := s.db.Exec(`DELETE FROM tag_colors WHERE workspace_id = ? AND tag = ?`, ws, tag); err != nil {
			return "", err
		}
		return fmt.Sprintf("Reset colour of tag %q to automatic", tag), nil
	}
	if !tagColorPalette[color] {
		return "", fmt.Errorf("unknown colour %q — use gray, brown, orange, yellow, green, blue, purple, pink, red, or \"default\" to reset", color)
	}
	if _, err := s.db.Exec(`INSERT INTO tag_colors (workspace_id, tag, color) VALUES (?, ?, ?)
		ON CONFLICT(workspace_id, tag) DO UPDATE SET color = excluded.color`, ws, tag, color); err != nil {
		return "", err
	}
	return fmt.Sprintf("Tag %q is now %s in workspace %s", tag, color, ws), nil
}

// mcpCreateWorkspaceTarget decides the workspace for something NEW that has no
// parent page. Unlike mcpWorkspaceScope exactly one has to come out, and write
// permission is mandatory — read access is not enough to create.
func (s *Server) mcpCreateWorkspaceTarget(u *user, wsID string) (string, error) {
	if wsID == "" {
		wsID = s.defaultWorkspaceFor(u)
		if wsID == "" {
			return "", fmt.Errorf("no workspace available")
		}
		if !s.credentialMayEnter(u, wsID) {
			return "", fmt.Errorf("this token cannot create top-level pages in the default workspace; pass workspace_id (see list with kind=\"workspaces\") or a parent_id inside an allowed workspace")
		}
		return wsID, nil
	}
	if !s.isMember(u.ID, wsID) || !s.credentialMayEnter(u, wsID) {
		return "", fmt.Errorf("workspace %q not found", wsID)
	}
	if s.workspaceRole(u.ID, wsID) == "viewer" {
		return "", fmt.Errorf("you are a viewer in that workspace and cannot create pages there")
	}
	return wsID, nil
}
