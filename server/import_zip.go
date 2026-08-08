package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
)

// ZIP import (Welle 19): imports a Markdown export — including Notion's
// "Export as Markdown" zips — as a page tree. Folders become parent pages,
// .md files become pages; Notion's trailing 32-hex-id suffixes are stripped
// from titles, and a file that pairs with a same-named folder fills that
// folder page instead of creating a duplicate sibling.

var notionSuffix = regexp.MustCompile(` [0-9a-f]{32}$`)

func cleanImportName(name string) string {
	name = strings.TrimSuffix(name, path.Ext(name))
	name = notionSuffix.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	if name == "" {
		return "Untitled"
	}
	if r := []rune(name); len(r) > maxTitleLen {
		name = string(r[:maxTitleLen])
	}
	return name
}

const (
	maxImportZip   = 100 << 20 // whole upload
	maxImportFile  = 2 << 20   // single markdown file
	maxImportPages = 2000
	maxImportFiles = 20000 // zip entry count (bounds the pairing/parse work)
	maxZipDepth    = 5     // recursion cap for nested Notion "Part-N.zip" wrappers
)

// importFile is one archive entry, decoupled from *zip.File so that entries
// pulled from nested archives can be merged with top-level ones and processed
// uniformly. Real Notion exports wrap the database CSV + row folder inside a
// "…-Part-N.zip", so the importer must see through that extra layer.
type importFile struct {
	name  string
	size  uint64
	isDir bool
	open  func() (io.ReadCloser, error)
}

// collectZipFiles flattens a zip into importFile entries, recursively expanding
// nested ".zip" entries (Notion's Part-N.zip wrappers) so their contents import
// as if they sat at the top level. Bounded by depth and a total entry count so
// a maliciously nested archive can't exhaust memory.
func collectZipFiles(zr *zip.Reader, depth int, out *[]importFile, count *int) {
	for _, f := range zr.File {
		if *count >= maxImportFiles {
			return
		}
		if depth < maxZipDepth && strings.EqualFold(path.Ext(f.Name), ".zip") {
			if f.UncompressedSize64 > maxImportZip {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			nb, err := io.ReadAll(io.LimitReader(rc, maxImportZip))
			rc.Close()
			if err != nil {
				continue
			}
			nzr, err := zip.NewReader(bytes.NewReader(nb), int64(len(nb)))
			if err != nil {
				continue
			}
			collectZipFiles(nzr, depth+1, out, count)
			continue
		}
		f := f // capture for the closure
		*count++
		*out = append(*out, importFile{
			name:  f.Name,
			size:  f.UncompressedSize64,
			isDir: f.FileInfo().IsDir(),
			open:  func() (io.ReadCloser, error) { return f.Open() },
		})
	}
}

func (s *Server) handleImportZip(w http.ResponseWriter, r *http.Request) {
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
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		httpError(w, 400, "not a valid zip archive")
		return
	}

	// Root parent: optional form value, else top level of the default workspace.
	var rootParent *string
	workspaceID := s.defaultWorkspaceFor(u)
	if pid := r.FormValue("parentId"); pid != "" {
		if !s.canWriteReq(r, pid) {
			httpError(w, 403, "forbidden")
			return
		}
		var pws string
		if s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ?`, pid).Scan(&pws) == nil {
			rootParent = &pid
			workspaceID = pws
		}
	}
	if workspaceID == "" {
		httpError(w, 400, "no workspace")
		return
	}

	// Flatten the archive, expanding Notion's nested "…-Part-N.zip" wrappers so
	// the CSV + row folder inside them import as if they were at the top level.
	var entries []importFile
	count := 0
	collectZipFiles(zr, 0, &entries, &count)

	created, skipped := s.importZipFiles(entries, rootParent, workspaceID, u.ID)

	s.audit("human", u.ID, u.Name, "import_zip", "", workspaceID, "")
	s.pagesChanged()
	writeJSON(w, map[string]int{"created": created, "skipped": skipped})
}

// importZipFiles turns a flattened set of archive entries into pages. Folders
// become parent pages, .md files become pages, and Notion database CSVs become
// real collections (with the paired row-body .md files filling each row). It is
// split out of handleImportZip so the same logic can be unit-tested directly.
func (s *Server) importZipFiles(entries []importFile, rootParent *string, workspaceID, userID string) (created, skipped int) {
	// Map of RAW dir path ("A <id>/B <id>") → created page id. Sorted names give
	// parents-before-children for free (a/ sorts before a/b.md). Keying by the
	// raw (id-bearing) path — not the cleaned title — keeps same-titled Notion
	// siblings distinct so one never overwrites the other.
	dirPage := map[string]string{}

	createPage := func(parentKey, title, content string) (string, bool) {
		if created >= maxImportPages {
			return "", false
		}
		var parent any
		if parentKey == "" {
			if rootParent != nil {
				parent = *rootParent
			}
		} else {
			parent = dirPage[parentKey]
		}
		id := newID()
		ts := now()
		var pos float64
		s.db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM pages WHERE parent_id IS ?`, parent).Scan(&pos)
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
			VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, 'workspace')`,
			id, parent, title, content, pos, ts, ts, workspaceID, userID); err != nil {
			return "", false
		}
		s.reindexPage(id)
		created++
		return id, true
	}

	// ensureDir creates the page chain for a RAW dir path and returns its key.
	// The page title is the cleaned (id-stripped) last segment.
	var ensureDir func(parts []string) string
	ensureDir = func(parts []string) string {
		if len(parts) == 0 {
			return ""
		}
		key := strings.Join(parts, "/")
		if _, ok := dirPage[key]; ok {
			return key
		}
		parentKey := ensureDir(parts[:len(parts)-1])
		id, ok := createPage(parentKey, cleanImportName(parts[len(parts)-1]), "[]")
		if !ok {
			return parentKey // cap hit; attach following files higher up
		}
		dirPage[key] = id
		return key
	}

	names := make([]string, 0, len(entries))
	byName := map[string]importFile{}
	dirSet := map[string]bool{} // every ancestor dir of every entry (raw paths)
	for _, f := range entries {
		names = append(names, f.name)
		byName[f.name] = f
		for d := path.Dir(f.name); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			dirSet[d] = true
		}
	}
	sort.Strings(names)

	// Notion database exports: "<Name> <id>.csv" carries the table, the paired
	// "<Name> <id>/" folder holds each row's body. Turn each CSV into a real
	// collection and consume the row .md files so they don't re-import as pages.
	consumed := map[string]bool{}
	for _, name := range names {
		if created >= maxImportPages {
			break
		}
		if !strings.EqualFold(path.Ext(name), ".csv") {
			continue
		}
		// Notion writes both "<db> <id>.csv" (the view, which pairs with the row
		// folder) and an identical "<db> <id>_all.csv". Skip the _all twin when
		// its plain sibling exists so the database isn't imported twice; the
		// plain CSV is the one joined to the row bodies.
		if base := strings.TrimSuffix(name, path.Ext(name)); strings.HasSuffix(base, "_all") {
			if _, ok := byName[strings.TrimSuffix(base, "_all")+".csv"]; ok {
				continue
			}
		}
		f := byName[name]
		if f.size > 16<<20 { // CSVs can exceed a single-md cap
			skipped++
			continue
		}
		rc, err := f.open()
		if err != nil {
			skipped++
			continue
		}
		csvData, _ := io.ReadAll(io.LimitReader(rc, 16<<20))
		rc.Close()

		// The collection's parent is the folder the CSV lives in (if any).
		var parts []string
		if dir := path.Dir(name); dir != "." && dir != "/" {
			for _, p := range strings.Split(dir, "/") {
				if p != "" {
					parts = append(parts, p)
				}
			}
		}
		parentID := ""
		if len(parts) > 0 {
			if key := ensureDir(parts); key != "" {
				parentID = dirPage[key]
			}
		} else if rootParent != nil {
			parentID = *rootParent
		}

		// Row bodies live in the paired folder, keyed by title. Notion usually
		// names it "<Name> <id>/", but a top-level database export drops the id
		// and uses just "<Name>/" — so match whichever folder the archive really
		// contains, then pair each direct-child .md as a row body.
		rawBase := strings.TrimSuffix(name, path.Ext(name))
		folderDir := rawBase
		if !dirSet[rawBase] {
			if noID := notionSuffix.ReplaceAllString(rawBase, ""); noID != rawBase && dirSet[noID] {
				folderDir = noID
			}
		}
		bodies := map[string]string{}
		for _, mdn := range names {
			if path.Dir(mdn) != folderDir || !strings.EqualFold(path.Ext(mdn), ".md") {
				continue
			}
			consumed[mdn] = true // don't also import this row as a loose page
			mf := byName[mdn]
			if mf.size > maxImportFile {
				continue
			}
			mrc, e := mf.open()
			if e != nil {
				continue
			}
			mb, _ := io.ReadAll(io.LimitReader(mrc, maxImportFile))
			mrc.Close()
			// Keep the raw markdown: importNotionCSV strips Notion's repeated
			// "# title + Property: value" preamble (now shown by the row's
			// property panel) before converting the remaining content to blocks.
			bodies[normTitle(cleanImportName(path.Base(mdn)))] = string(mb)
		}

		if _, nrows, e := s.importNotionCSV(csvData, cleanImportName(path.Base(name)), parentID, workspaceID, userID, bodies); e == nil {
			created += 1 + nrows
		} else {
			skipped++
		}
	}

	for _, name := range names {
		if created >= maxImportPages {
			break // page cap reached — stop before parsing the rest (DoS guard)
		}
		if consumed[name] {
			continue // already imported as a database row body
		}
		f := byName[name]
		if f.isDir || strings.HasPrefix(path.Base(name), ".") {
			continue
		}
		if strings.EqualFold(path.Ext(name), ".csv") {
			continue // database CSVs are turned into collections above, not "skipped"
		}
		if !strings.EqualFold(path.Ext(name), ".md") {
			skipped++ // genuinely unhandled assets (images, etc.) are not imported
			continue
		}
		if f.size > maxImportFile {
			skipped++
			continue
		}
		rc, err := f.open()
		if err != nil {
			skipped++
			continue
		}
		md, err := io.ReadAll(io.LimitReader(rc, maxImportFile))
		rc.Close()
		if err != nil {
			skipped++
			continue
		}
		content, err := mdToBlocksJSON(string(md))
		if err != nil {
			skipped++
			continue
		}

		// Raw dir chain for this file (keys stay id-bearing to avoid collisions).
		dir := path.Dir(name)
		var parts []string
		if dir != "." && dir != "/" {
			for _, p := range strings.Split(dir, "/") {
				if p != "" {
					parts = append(parts, p)
				}
			}
		}

		// Notion pairs "X <id>.md" with a folder "X <id>/": if any entry lives
		// under that folder (O(1) set lookup), fill the folder page instead of
		// creating a duplicate sibling.
		rawNoExt := strings.TrimSuffix(name, path.Ext(name))
		if dirSet[rawNoExt] {
			key := ensureDir(append(append([]string{}, parts...), path.Base(rawNoExt)))
			if id, ok := dirPage[key]; ok {
				s.db.Exec(`UPDATE pages SET content = ?, updated_at = ? WHERE id = ?`, content, now(), id)
				s.reindexPage(id)
				continue
			}
		}

		parentKey := ensureDir(parts)
		createPage(parentKey, cleanImportName(path.Base(name)), content)
	}

	return created, skipped
}
