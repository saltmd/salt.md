package server

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Workspace transfer: a workspace as a native ZIP — importable 1:1 again.
//
// The Markdown export is meant for taking the content with you; on re-import it
// loses databases (schema, views, row properties), hierarchy metadata and file
// attachments. This format carries a workspace between instances with little
// loss: the page tree, databases, tags with their colours, covers, icons,
// descriptions and every referenced upload.
//
// Deliberately NOT included: users and roles (instance specific), comments and
// version history (they hang off user ids), share links (secrets), the live Yjs
// state (the materialised page content is the source; on the first open the
// CRDT is seeded from it again — the same path as for new pages).
//
// ZIP layout:
//   salt-workspace.json   manifest (format version, workspace meta, counters)
//   pages.json            every page including collection schema and views
//   tags.json             tag → colour
//   files/<name>          referenced uploads

const transferFormat = 1

type transferManifest struct {
	Format      int    `json:"format"`
	SaltVersion string `json:"saltVersion"`
	ExportedAt  string `json:"exportedAt"`
	Workspace   struct {
		Name  string `json:"name"`
		Icon  string `json:"icon"`
		Image string `json:"image"`
		// The rules were missing from this format until the library needed them,
		// and they are the most valuable thing a workspace carries — the answer to
		// "how do we work here". An older archive simply has no rules field, so
		// this needs no format bump: absent reads as empty.
		Rules string `json:"rules,omitempty"`
	} `json:"workspace"`
	Pages int `json:"pages"`
	Files int `json:"files"`
}

type transferPage struct {
	ID          string          `json:"id"`
	ParentID    *string         `json:"parentId"`
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Icon        string          `json:"icon"`
	Cover       string          `json:"cover"`
	Description string          `json:"description"`
	Tags        json.RawMessage `json:"tags"`
	Props       json.RawMessage `json:"props"`
	Content     json.RawMessage `json:"content"`
	Position    float64         `json:"position"`
	Visibility  string          `json:"visibility"`
	IsTemplate  bool            `json:"isTemplate"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
	// Only for type == "collection":
	Schema json.RawMessage `json:"schema,omitempty"`
	Views  json.RawMessage `json:"views,omitempty"`
}

// fileRefPattern finds upload references in content, props and covers.
// Upload names are newID()+extension (see handleUpload) — no spaces.
var fileRefPattern = regexp.MustCompile(`/files/([A-Za-z0-9._%-]+)`)

func (s *Server) handleExportWorkspace(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	wsID := r.PathValue("id")
	// Membership (or a running, logged break-glass access) is mandatory. The
	// instance admin flag used to be enough here — with it an admin could download
	// ANY workspace belonging to anybody, in full, without a trace.
	if !s.isMember(u.ID, wsID) && !s.hasBreakGlass(u.ID, wsID) {
		httpError(w, 404, "workspace not found")
		return
	}
	var wsName, wsIcon, wsImage, wsRules string
	if err := s.db.QueryRow(`SELECT name, icon, image, COALESCE(rules, '') FROM workspaces WHERE id = ?`, wsID).
		Scan(&wsName, &wsIcon, &wsImage, &wsRules); err != nil {
		httpError(w, 404, "workspace not found")
		return
	}

	rows, err := s.db.Query(`
		SELECT p.id, p.parent_id, p.type, p.title, p.icon, p.cover, p.description,
		       p.tags, p.props, p.content, p.position, p.visibility, p.is_template,
		       p.created_at, p.updated_at, c.schema, c.views
		FROM pages p LEFT JOIN collections c ON c.page_id = p.id
		WHERE p.workspace_id = ? AND p.trashed_at IS NULL
		ORDER BY p.position, p.created_at`, wsID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	var scanned []transferPage
	for rows.Next() {
		var p transferPage
		var tags, props, content []byte
		var schema, views sql.Null[[]byte]
		var isTemplate int
		if err := rows.Scan(&p.ID, &p.ParentID, &p.Type, &p.Title, &p.Icon, &p.Cover,
			&p.Description, &tags, &props, &content, &p.Position, &p.Visibility,
			&isTemplate, &p.CreatedAt, &p.UpdatedAt, &schema, &views); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		p.Tags, p.Props, p.Content = tags, props, content
		p.IsTemplate = isTemplate != 0
		if schema.Valid {
			p.Schema = schema.V
		}
		if views.Valid {
			p.Views = views.V
		}
		scanned = append(scanned, p)
	}
	rows.Close() // drain first, then check per-row permissions (one DB connection)

	// Other people's private pages stay out — the export holds exactly what the
	// person exporting sees in the app as well.
	var pages []transferPage
	for _, p := range scanned {
		if s.canRead(u.ID, p.ID) {
			pages = append(pages, p)
		}
	}

	// Collect the referenced uploads: content, props, covers, workspace image.
	fileSet := map[string]bool{}
	collect := func(b []byte) {
		for _, m := range fileRefPattern.FindAllSubmatch(b, -1) {
			fileSet[string(m[1])] = true
		}
	}
	for _, p := range pages {
		collect(p.Content)
		collect(p.Props)
		collect([]byte(p.Cover))
	}
	collect([]byte(wsImage))

	tagColors := map[string]string{}
	if tr, err := s.db.Query(`SELECT tag, color FROM tag_colors WHERE workspace_id = ?`, wsID); err == nil {
		for tr.Next() {
			var tag, color string
			if tr.Scan(&tag, &color) == nil {
				tagColors[tag] = color
			}
		}
		tr.Close()
	}

	manifest := transferManifest{Format: transferFormat, SaltVersion: Version, ExportedAt: now()}
	manifest.Workspace.Name = wsName
	manifest.Workspace.Icon = wsIcon
	manifest.Workspace.Image = wsImage
	manifest.Workspace.Rules = wsRules
	manifest.Pages = len(pages)
	manifest.Files = len(fileSet)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(wsName) + ".salt.zip"}))
	zw := zip.NewWriter(w)
	defer zw.Close()

	writeJSONEntry := func(name string, v any) bool {
		f, err := zw.Create(name)
		if err != nil {
			return false
		}
		enc := json.NewEncoder(f)
		enc.SetEscapeHTML(false)
		return enc.Encode(v) == nil
	}
	if !writeJSONEntry("salt-workspace.json", manifest) ||
		!writeJSONEntry("pages.json", pages) ||
		!writeJSONEntry("tags.json", tagColors) {
		return
	}
	for name := range fileSet {
		// name comes from a regex without path separators — no traversal possible.
		src, err := os.Open(filepath.Join(s.dataDir, "files", name))
		if err != nil {
			continue // reference to a deleted file: the page still works, the file is gone
		}
		if f, err := zw.Create("files/" + name); err == nil {
			io.Copy(f, src)
		}
		src.Close()
	}

	s.audit("human", u.ID, u.Name, "export_workspace", "", wsID, wsName)
}

func (s *Server) handleImportWorkspace(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxImportZip)
	if err := r.ParseMultipartForm(maxImportZip); err != nil {
		httpError(w, 400, "upload too large or invalid")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpError(w, 400, "file field is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		httpError(w, 400, err.Error())
		return
	}
	res, err := s.importWorkspaceArchive(u, data, importOptions{})
	if err != nil {
		httpErrorFrom(w, importStatus(err), err)
		return
	}
	writeJSON(w, res)
}

// importOptions vary what an archive turns into, not how it is read.
type importOptions struct {
	// StructureOnly keeps the databases with their schemas and views and leaves
	// out rows and documents — a blueprint rather than a copy. Every reference to
	// something left behind is then dangling, so this mode also has to clean the
	// schemas and views up; see structureOnly() below.
	StructureOnly bool
	// Name overrides the name in the archive. Empty keeps the archive's.
	Name string
}

type importResult struct {
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Pages       int    `json:"pages"`
	Files       int    `json:"files"`
}

// importStatus maps the coded errors below onto HTTP. A bad archive is the
// caller's fault, a failed write is ours, and the two must not read alike.
func importStatus(err error) int {
	var ce *codedError
	if errors.As(err, &ce) {
		switch ce.code {
		case "workspaces_disabled":
			return 403
		case "bad_archive", "archive_too_new":
			return 400
		}
	}
	return 500
}

// importWorkspaceArchive turns an uploaded ZIP into a new workspace.
func (s *Server) importWorkspaceArchive(u *user, data []byte, opt importOptions) (*importResult, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, coded("bad_archive", "not a valid zip archive")
	}
	return s.importWorkspaceFS(u, zr, opt)
}

// importWorkspaceFS turns a workspace archive into a new workspace owned by u.
//
// It takes an fs.FS rather than ZIP bytes because a *zip.Reader IS one — and so
// is a directory embedded in the binary. That is what lets an uploaded archive
// and a shipped blueprint take the same path; two readers would be two answers
// to what an archive contains, and they would drift.
func (s *Server) importWorkspaceFS(u *user, fsys fs.FS, opt importOptions) (*importResult, error) {
	// The same rule as when creating a workspace.
	if !u.IsAdmin && !s.loadSettings().AllowUserWorkspaces {
		return nil, coded("workspaces_disabled", "creating workspaces is disabled on this instance — ask an admin")
	}
	readEntry := func(name string) []byte {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil
		}
		return b
	}

	var manifest transferManifest
	if b := readEntry("salt-workspace.json"); b == nil || json.Unmarshal(b, &manifest) != nil {
		return nil, coded("bad_archive", "not a salt.md workspace archive (salt-workspace.json missing)")
	}
	if manifest.Format > transferFormat {
		return nil, coded("archive_too_new",
			fmt.Sprintf("archive format %d is newer than this instance supports (%d) — update salt.md", manifest.Format, transferFormat))
	}
	var pages []transferPage
	if b := readEntry("pages.json"); b == nil || json.Unmarshal(b, &pages) != nil {
		return nil, coded("bad_archive", "pages.json missing or invalid")
	}
	tagColors := map[string]string{}
	if b := readEntry("tags.json"); b != nil {
		json.Unmarshal(b, &tagColors)
	}
	if opt.StructureOnly {
		pages = keepDatabasesOnly(pages)
	}

	// Files first: old → new names, so the id replacement in the content can do
	// both in a single pass.
	fileMap := map[string]string{}
	filesWritten := 0
	entries, _ := fs.ReadDir(fsys, "files") // absent is normal — most archives carry none
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := ""
		if i := strings.IndexByte(name, '.'); i >= 0 {
			ext = name[i:]
		}
		newName := newID() + ext
		rc, err := fsys.Open("files/" + name)
		if err != nil {
			continue
		}
		dst, err := os.Create(filepath.Join(s.dataDir, "files", newName))
		if err != nil {
			rc.Close()
			continue
		}
		if _, err := io.Copy(dst, rc); err == nil {
			fileMap[name] = newName
			filesWritten++
		}
		dst.Close()
		rc.Close()
	}

	// Hand out new page ids. The replacement runs as text over the raw JSON
	// fields: ids are 32 hex characters from newID() — practically collision free
	// as a substring, and that is exactly how mentions and relations refer to
	// pages inside the content.
	idMap := map[string]string{}
	for _, p := range pages {
		idMap[p.ID] = newID()
	}
	replacer := make([]string, 0, (len(idMap)+len(fileMap))*2)
	for old, nw := range idMap {
		replacer = append(replacer, old, nw)
	}
	for old, nw := range fileMap {
		replacer = append(replacer, "/files/"+old, "/files/"+nw)
	}
	remap := strings.NewReplacer(replacer...)

	// StructureOnly leaves every row behind, so anything still pointing at one is
	// dangling. Cleaning that up is the same job the live blueprint does, and it
	// uses the same two functions on purpose: what survives a structure copy must
	// have exactly one answer, whether the source is a workspace or a file.
	if opt.StructureOnly {
		structureOnly(pages, idMap)
	}

	wsName := strings.TrimSpace(opt.Name)
	if wsName == "" {
		wsName = strings.TrimSpace(manifest.Workspace.Name)
	}
	if wsName == "" {
		wsName = "Imported workspace"
	}
	var exists int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE name = ?`, wsName).Scan(&exists)
	if exists > 0 {
		wsName += " (Import)"
	}

	wsID := newID()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO workspaces (id, name, created_at, icon, image, rules, owner_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		wsID, wsName, now(), manifest.Workspace.Icon, remap.Replace(manifest.Workspace.Image),
		manifest.Workspace.Rules, u.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')`, wsID, u.ID); err != nil {
		return nil, err
	}
	for tag, color := range tagColors {
		// Tag colours are palette names (see handleSetTagColor), not hex values.
		if tagColorPalette[strings.ToLower(color)] {
			tx.Exec(`INSERT OR REPLACE INTO tag_colors (workspace_id, tag, color) VALUES (?, ?, ?)`, wsID, tag, strings.ToLower(color))
		}
	}

	// Insert parents before children (FK on parent_id): roots first, then level
	// by level. Pages with an unknown parent (private subtrees filtered out during
	// the export, say) land at the top level instead of disappearing.
	inserted := map[string]bool{}
	remaining := append([]transferPage(nil), pages...)
	defaultJSON := func(raw json.RawMessage, def string) string {
		if len(raw) == 0 {
			return def
		}
		return remap.Replace(string(raw))
	}
	for len(remaining) > 0 {
		progressed := false
		var next []transferPage
		for _, p := range remaining {
			parentKnown := p.ParentID == nil || inserted[*p.ParentID] || idMap[*p.ParentID] == ""
			if !parentKnown {
				next = append(next, p)
				continue
			}
			var parent any
			if p.ParentID != nil && idMap[*p.ParentID] != "" {
				parent = idMap[*p.ParentID]
			}
			typ := p.Type
			if typ == "" {
				typ = "doc"
			}
			vis := p.Visibility
			if vis != "private" {
				vis = "workspace"
			}
			created := p.CreatedAt
			if created == "" {
				created = now()
			}
			updated := p.UpdatedAt
			if updated == "" {
				updated = created
			}
			if _, err := tx.Exec(`
				INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at,
				                   type, props, cover, workspace_id, owner_id, visibility, is_template, tags, description)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				idMap[p.ID], parent, p.Title, p.Icon, defaultJSON(p.Content, "[]"), p.Position,
				created, updated, typ, defaultJSON(p.Props, "{}"), remap.Replace(p.Cover),
				wsID, u.ID, vis, boolToInt(p.IsTemplate), defaultJSON(p.Tags, "[]"), p.Description); err != nil {
				return nil, err
			}
			if typ == "collection" {
				if _, err := tx.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)`,
					idMap[p.ID], defaultJSON(p.Schema, "[]"), defaultJSON(p.Views, "[]")); err != nil {
					return nil, err
				}
			}
			inserted[p.ID] = true
			progressed = true
		}
		if !progressed {
			// A cycle in parent_id — can only come from a tampered archive; hang the rest
			// at the top instead of circling forever.
			for i := range next {
				next[i].ParentID = nil
			}
		}
		remaining = next
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Build the search index and the backlink graph for the new pages.
	for _, p := range pages {
		id := idMap[p.ID]
		s.reindexPage(id)
		s.updateLinks(id, remap.Replace(string(p.Content)), false)
	}
	s.pagesChanged()

	action := "import_workspace"
	if opt.StructureOnly {
		action = "blueprint_workspace"
	}
	s.audit("human", u.ID, u.Name, action, "", wsID,
		fmt.Sprintf("%s (%d pages, %d files)", wsName, len(pages), filesWritten))
	return &importResult{WorkspaceID: wsID, Name: wsName, Pages: len(pages), Files: filesWritten}, nil
}

// keepDatabasesOnly drops rows and documents. A blueprint carrying somebody's
// tasks is not a blueprint, and a workspace full of somebody else's notes is not
// an empty start.
//
// A database nested under a dropped document keeps its now-unknown parent, which
// the insert loop already handles by hanging it at the top level — better than
// losing it because its folder did not come along.
func keepDatabasesOnly(pages []transferPage) []transferPage {
	kept := make([]transferPage, 0, len(pages))
	for _, p := range pages {
		if p.Type == "collection" {
			kept = append(kept, p)
		}
	}
	return kept
}

// structureOnly repairs what dropping the rows broke. It works on the ORIGINAL
// ids — idMap holds exactly the pages that survived, so "not in idMap" is the
// test for a reference that has nothing left to point at.
func structureOnly(pages []transferPage, idMap map[string]string) {
	// remapSchema rewrites to the NEW id; here the text replacement further down
	// does that, so this pass hands it an identity map and uses it only to decide
	// what still exists.
	keep := make(map[string]string, len(idMap))
	for old := range idMap {
		keep[old] = old
	}
	for i := range pages {
		if pages[i].Type != "collection" {
			continue
		}
		var schema []map[string]any
		var views []map[string]any
		if len(pages[i].Schema) > 0 {
			json.Unmarshal(pages[i].Schema, &schema)
		}
		if len(pages[i].Views) > 0 {
			json.Unmarshal(pages[i].Views, &views)
		}
		remapSchema(schema, keep)
		views = blueprintViews(views, schema)
		if b, err := json.Marshal(schema); err == nil {
			pages[i].Schema = b
		}
		if b, err := json.Marshal(views); err == nil {
			pages[i].Views = b
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
