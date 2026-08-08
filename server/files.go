package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The file index: files as objects you can list, filter and hand to an agent,
// instead of strings buried in a page's block JSON.
//
// Why an index and not a scan: a file's only trace used to be a `/files/…`
// reference inside a page's content JSON, so "all files in this workspace"
// meant loading and parsing every page — and with db.SetMaxOpenConns(1) a
// long scan blocks the whole server. It also could not answer the question
// that matters for housekeeping: which files exist that NO page mentions any
// more (deleted block, file still on disk, still in every backup).
//
// The index is derived, never authoritative: the page's block and the byte on
// disk are the truth. filesVersion below forces a rebuild when the extraction
// rules change, exactly like ftsVersion for the search index.
//
// 2: the MCP upload never wrote to the index (only the HTTP one did), so an
// instance could hold hundreds of files that list_files did not know about.
// The write is fixed; this rebuild is what repairs the instances that already
// ran with it. Being derived is exactly what makes that repair safe — the
// blocks and the files directory still hold the truth, so the index can simply
// be thrown away and built again.
const filesVersion = "2"

// fileRef is one `/files/…` reference found in a page's content.
type fileRef struct {
	name        string // stored name on disk, e.g. "ab12….pdf"
	displayName string // what a person calls it
}

// scanBlocksForFiles walks a page's block JSON and collects every file
// reference. It looks at `props.url` and `props.name` rather than at block
// types, because the types differ by origin: BlockNote writes file/image/
// video/audio blocks, the MCP upload writes {"type":"file"|"image"}, and a
// cover is a bare string field. Types will keep being added; a url that
// points into /files/ is the stable signal.
func scanBlocksForFiles(content string) []fileRef {
	var blocks []any
	if err := json.Unmarshal([]byte(content), &blocks); err != nil {
		return nil
	}
	var out []fileRef
	seen := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			if props, ok := t["props"].(map[string]any); ok {
				if url, ok := props["url"].(string); ok && strings.HasPrefix(url, "/files/") {
					name := strings.TrimPrefix(url, "/files/")
					// A stored name is one path segment; anything else is not
					// ours to index.
					if name != "" && !strings.ContainsAny(name, "/?#") && !seen[name] {
						seen[name] = true
						disp, _ := props["name"].(string)
						out = append(out, fileRef{name: name, displayName: disp})
					}
				}
			}
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(blocks)
	return out
}

// recordFile writes one row of the index. Called on upload, and by the
// rebuild for everything that was uploaded before the index existed.
func (s *Server) recordFile(name, pageID, displayName string) {
	var ws any
	if pageID != "" {
		var w string
		if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pageID).Scan(&w); err == nil {
			ws = w
		}
	}
	var page any
	if pageID != "" {
		page = pageID
	}
	var size int64
	if fi, err := os.Stat(filepath.Join(s.dataDir, "files", name)); err == nil {
		size = fi.Size()
	}
	s.db.Exec(`INSERT INTO files (file_name, page_id, workspace_id, display_name, ext, size, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_name) DO UPDATE SET
			page_id = COALESCE(excluded.page_id, files.page_id),
			workspace_id = COALESCE(excluded.workspace_id, files.workspace_id),
			display_name = CASE WHEN excluded.display_name != '' THEN excluded.display_name ELSE files.display_name END,
			size = excluded.size`,
		name, page, ws, displayName, strings.ToLower(filepath.Ext(name)), size, now())
}

// migrateFileIndex builds the index from what is already there: every page's
// blocks give the carrier page and the human name, the files directory gives
// size and the leftovers nobody references any more.
//
// Runs synchronously at startup like the search-index rebuild. Same rule as
// there: collect ids first, THEN work — a query issued inside an open cursor
// blocks the single connection.
func (s *Server) migrateFileIndex() error {
	if s.setting("files_version", "0") == filesVersion {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM files`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id, content FROM pages WHERE content LIKE '%/files/%'`)
	if err != nil {
		return err
	}
	type pageBlocks struct{ id, content string }
	var pages []pageBlocks
	for rows.Next() {
		var p pageBlocks
		if rows.Scan(&p.id, &p.content) == nil {
			pages = append(pages, p)
		}
	}
	rows.Close()

	referenced := map[string]bool{}
	for _, p := range pages {
		for _, ref := range scanBlocksForFiles(p.content) {
			referenced[ref.name] = true
			s.recordFile(ref.name, p.id, ref.displayName)
		}
	}

	// Files on disk that no page mentions: index them too, without a page.
	// They are exactly the ones that were invisible before — and the reason
	// this index can answer "what can be cleaned up".
	orphans := 0
	if entries, err := os.ReadDir(filepath.Join(s.dataDir, "files")); err == nil {
		for _, e := range entries {
			if e.IsDir() || referenced[e.Name()] {
				continue
			}
			s.recordFile(e.Name(), "", "")
			orphans++
		}
	}
	s.setSetting("files_version", filesVersion)
	log.Printf("file index: built (version %s, %d files on %d pages, %d unreferenced)",
		filesVersion, len(referenced), len(pages), orphans)
	return nil
}

// fileJSON is one entry of the file list.
type fileJSON struct {
	Name        string `json:"name"` // stored name (the /files/ path segment)
	DisplayName string `json:"displayName"`
	Ext         string `json:"ext"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"createdAt"`
	PageID      string `json:"pageId"`
	PageTitle   string `json:"pageTitle"`
	WorkspaceID string `json:"workspaceId"`
}

// handleListFiles answers "every file in this workspace", optionally narrowed
// to one subtree ("everything below this deal").
//
// Permissions are the same two stages as search, and for the same reason: the
// workspace filter is not enough on its own, because a private page inside a
// workspace is still nobody else's business. The second stage (canRead per
// carrier page) is the one that is easy to forget.
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	wsID := r.URL.Query().Get("workspace")
	under := r.URL.Query().Get("under")

	visible := s.scopeWorkspacesFor(u, s.visibleWorkspaces(u.ID))
	if wsID != "" {
		if !s.isMember(u.ID, wsID) || !s.tokenReachesWorkspace(r, wsID) {
			httpError(w, 404, "workspace not found")
			return
		}
		visible = []string{wsID}
	}
	if len(visible) == 0 {
		writeJSON(w, []fileJSON{})
		return
	}

	// "under": the subtree of one page, resolved to ids first — a recursive
	// SQL walk per row would be the slow shape here.
	var subtree map[string]bool
	if under != "" {
		if !s.canReadReq(r, under) {
			httpError(w, 404, "page not found")
			return
		}
		subtree = s.subtreeIDs(under)
	}

	args := make([]any, len(visible))
	for i, v := range visible {
		args[i] = v
	}
	rows, err := s.db.Query(`SELECT f.file_name, f.display_name, f.ext, f.size, f.created_at,
			COALESCE(f.page_id, ''), COALESCE(p.title, ''), COALESCE(f.workspace_id, '')
		FROM files f LEFT JOIN pages p ON p.id = f.page_id
		WHERE f.workspace_id IN (`+placeholders(len(visible))+`)
		AND (p.id IS NULL OR p.trashed_at IS NULL)`, args...)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	var all []fileJSON
	for rows.Next() {
		var f fileJSON
		if err := rows.Scan(&f.Name, &f.DisplayName, &f.Ext, &f.Size, &f.CreatedAt,
			&f.PageID, &f.PageTitle, &f.WorkspaceID); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		all = append(all, f)
	}
	rows.Close()

	// Second stage, after the cursor is closed: canRead issues queries of its
	// own, and on one connection those must not run inside an open cursor.
	out := []fileJSON{}
	for _, f := range all {
		if subtree != nil && !subtree[f.PageID] {
			continue
		}
		if f.PageID != "" && !s.canRead(u.ID, f.PageID) {
			continue
		}
		if f.DisplayName == "" {
			f.DisplayName = f.Name
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt // newest first
		}
		return out[i].Name < out[j].Name
	})
	writeJSON(w, out)
}

// subtreeIDs collects a page and everything under it. Iterative by level, so
// one query per level instead of one per page.
func (s *Server) subtreeIDs(root string) map[string]bool {
	ids := map[string]bool{root: true}
	level := []string{root}
	for depth := 0; depth < 20 && len(level) > 0; depth++ {
		args := make([]any, len(level))
		for i, v := range level {
			args[i] = v
		}
		rows, err := s.db.Query(`SELECT id FROM pages WHERE trashed_at IS NULL
			AND parent_id IN (`+placeholders(len(level))+`)`, args...)
		if err != nil {
			return ids
		}
		var next []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil && !ids[id] {
				ids[id] = true
				next = append(next, id)
			}
		}
		rows.Close()
		level = next
	}
	return ids
}

// mcpListFiles is the agent's side of the same question — so an agent can
// tidy a customer file without walking the tree page by page.
func (s *Server) mcpListFiles(u *user, wsID, under string) (string, error) {
	if wsID == "" && under == "" {
		wsID = s.defaultWorkspaceFor(u)
	}
	if wsID != "" && (!s.isMember(u.ID, wsID) || !s.credentialMayEnter(u, wsID)) {
		return "", fmt.Errorf("workspace %q not found", wsID)
	}
	var subtree map[string]bool
	if under != "" {
		if !s.canRead(u.ID, under) {
			return "", fmt.Errorf("page %q not found", under)
		}
		subtree = s.subtreeIDs(under)
	}
	scope := []string{}
	if wsID != "" {
		scope = append(scope, wsID)
	} else {
		for _, w := range s.visibleWorkspaces(u.ID) {
			if s.credentialMayEnter(u, w) {
				scope = append(scope, w)
			}
		}
	}
	if len(scope) == 0 {
		return `{"files":[]}`, nil
	}
	args := make([]any, len(scope))
	for i, v := range scope {
		args[i] = v
	}
	rows, err := s.db.Query(`SELECT f.file_name, f.display_name, f.ext, f.size,
			COALESCE(f.page_id, ''), COALESCE(p.title, '')
		FROM files f LEFT JOIN pages p ON p.id = f.page_id
		WHERE f.workspace_id IN (`+placeholders(len(scope))+`)
		AND (p.id IS NULL OR p.trashed_at IS NULL)`, args...)
	if err != nil {
		return "", err
	}
	type entry struct {
		URL       string `json:"url"`
		Name      string `json:"name"`
		Ext       string `json:"ext"`
		Size      int64  `json:"size"`
		PageID    string `json:"page_id"`
		PageTitle string `json:"page_title"`
	}
	var all []entry
	var pageIDs []string
	for rows.Next() {
		var e entry
		var stored string
		if err := rows.Scan(&stored, &e.Name, &e.Ext, &e.Size, &e.PageID, &e.PageTitle); err != nil {
			rows.Close()
			return "", err
		}
		e.URL = "/files/" + stored
		if e.Name == "" {
			e.Name = stored
		}
		all = append(all, e)
		pageIDs = append(pageIDs, e.PageID)
	}
	rows.Close()

	out := []entry{}
	for _, e := range all {
		if subtree != nil && !subtree[e.PageID] {
			continue
		}
		if e.PageID != "" && !s.canRead(u.ID, e.PageID) {
			continue
		}
		out = append(out, e)
	}
	b, err := json.Marshal(map[string]any{"files": out, "count": len(out)})
	if err != nil {
		return "", err
	}
	return string(b), nil
}
