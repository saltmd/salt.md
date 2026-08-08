package server

// i18n-ok-file: the CSV fixture is deliberately German ("Priorität") — it
// tests that a non-ASCII column header survives the Notion import.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newImportTestServer boots a Server backed by a fresh on-disk SQLite DB with
// the full schema/migrations, plus one workspace + user the import can attach to.
func newImportTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Server{db: db}
	uid, wsid, ts := newID(), newID(), now()
	if _, err := db.Exec(`INSERT INTO users (id, email, name, password_hash, created_at) VALUES (?, 'a@b.c', 'Tester', 'x', ?)`, uid, ts); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, name, created_at) VALUES (?, 'WS', ?)`, wsid, ts); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	return s, wsid, uid
}

// writeZip builds an in-memory zip from name→content (Store method, so nested
// zips stay usable). Names ending in "/" are directory entries.
func writeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func flatten(t *testing.T, data []byte) []importFile {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	var out []importFile
	count := 0
	collectZipFiles(zr, 0, &out, &count)
	return out
}

// TestImportNestedNotionZip mirrors a real Notion "Export → Markdown & CSV":
// an OUTER zip whose only entry is a "…-Part-1.zip", inside which live the
// database CSV, its identical "_all.csv" twin, and the per-row body folder.
// It asserts nested-zip expansion, _all dedup, and a well-formed collection.
func TestImportNestedNotionZip(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	const rowID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	bom := "\uFEFF"
	csvContent := []byte(bom + "Aufgabe,Status,Priorität,Notizen\n" +
		"Row One,To Do,Hoch,Erste Notiz\n" +
		"Row Two,In Arbeit,Mittel,Zweite Notiz\n" +
		"Row Three,Erledigt,Niedrig,Dritte Notiz\n")
	base := "Acme Campaigns " + id
	inner := writeZip(t, map[string][]byte{
		base + ".csv":                      csvContent,
		base + "_all.csv":                  csvContent, // identical twin — must be skipped
		base + "/Row One " + rowID + ".md": []byte("# Row One\n\nBody of row one."),
	})
	outer := writeZip(t, map[string][]byte{
		"ExportBlock-" + id + "-Part-1.zip": inner,
	})

	// 1) Flattening sees through the nested Part-1.zip.
	entries := flatten(t, outer)
	var csvCount, mdCount, zipCount int
	for _, e := range entries {
		switch filepath.Ext(e.name) {
		case ".csv":
			csvCount++
		case ".md":
			mdCount++
		case ".zip":
			zipCount++
		}
	}
	if zipCount != 0 {
		t.Fatalf("nested zip not expanded: %d .zip entries remain", zipCount)
	}
	if csvCount != 2 || mdCount != 1 {
		t.Fatalf("flatten got csv=%d md=%d, want csv=2 md=1", csvCount, mdCount)
	}

	// 2) Import → exactly one collection, three rows, Status-driven board.
	s, wsid, uid := newImportTestServer(t)
	created, skipped := s.importZipFiles(entries, nil, wsid, uid)
	t.Logf("created=%d skipped=%d", created, skipped)

	var colCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE type='collection'`).Scan(&colCount)
	if colCount != 1 {
		t.Fatalf("_all dedup failed: got %d collections, want 1", colCount)
	}

	var colID, schemaJSON, viewsJSON string
	if err := s.db.QueryRow(`SELECT p.id, c.schema, c.views FROM pages p JOIN collections c ON c.page_id=p.id WHERE p.type='collection'`).Scan(&colID, &schemaJSON, &viewsJSON); err != nil {
		t.Fatalf("load collection: %v", err)
	}

	var rows int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE parent_id=?`, colID).Scan(&rows)
	if rows != 3 {
		t.Fatalf("got %d rows, want 3", rows)
	}

	// Schema: Status must be a select with 3 options.
	var schema []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Options []struct {
			Name string `json:"name"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("schema json: %v", err)
	}
	var statusID string
	for _, p := range schema {
		if p.Name == "Status" {
			if p.Type != "select" {
				t.Fatalf("Status type = %q, want select", p.Type)
			}
			if len(p.Options) != 3 {
				t.Fatalf("Status options = %d, want 3", len(p.Options))
			}
			statusID = p.ID
		}
	}
	if statusID == "" {
		t.Fatal("Status column missing from schema")
	}

	// Views: first view is a board grouped by Status.
	var views []map[string]any
	if err := json.Unmarshal([]byte(viewsJSON), &views); err != nil {
		t.Fatalf("views json: %v", err)
	}
	if len(views) == 0 || views[0]["type"] != "board" || views[0]["groupBy"] != statusID {
		t.Fatalf("first view = %+v, want board grouped by Status(%s)", views[0], statusID)
	}

	// Row body joined from the paired .md folder.
	var body string
	s.db.QueryRow(`SELECT content FROM pages WHERE parent_id=? AND title='Row One'`, colID).Scan(&body)
	if !bytes.Contains([]byte(body), []byte("Body of row one")) {
		t.Fatalf("Row One body not joined from md: %q", body)
	}
}

// TestStripRowPreambleBlocks checks the retro-cleanup: a Notion-imported row
// body (title H1 + "Property: value" paragraphs + real content) is stripped to
// just the real content; surviving blocks are preserved byte-for-byte.
func TestStripRowPreambleBlocks(t *testing.T) {
	names := []string{"Status", "Priorität", "Notizen"}

	// Pure preamble → empty.
	dump := `[{"type":"heading","props":{"level":1},"content":[{"type":"text","text":"My Row"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Status: To Do"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Priorität: Hoch"}]}]`
	if out, changed := stripRowPreambleBlocks([]byte(dump), names); !changed || string(out) != "[]" {
		t.Fatalf("pure-preamble → want []/true, got %s / %v", out, changed)
	}

	// Preamble + real content → real content kept verbatim.
	real := `{"type":"paragraph","content":[{"type":"text","text":"Actual body text."}]}`
	body := `[{"type":"heading","props":{"level":1},"content":[{"type":"text","text":"My Row"}]},` +
		`{"type":"paragraph","content":[{"type":"text","text":"Notizen: bla"}]},` + real + `]`
	out, changed := stripRowPreambleBlocks([]byte(body), names)
	if !changed || string(out) != "["+real+"]" {
		t.Fatalf("preamble+content → want kept content, got %s / %v", out, changed)
	}

	// No preamble (real content first) → unchanged.
	plain := "[" + real + "]"
	if out, changed := stripRowPreambleBlocks([]byte(plain), names); changed || string(out) != plain {
		t.Fatalf("no-preamble → want unchanged, got %s / %v", out, changed)
	}

	// A paragraph whose label is not a column is not stripped.
	keep := `[{"type":"paragraph","content":[{"type":"text","text":"Note: keep me"}]}]`
	if _, changed := stripRowPreambleBlocks([]byte(keep), names); changed {
		t.Fatal("non-column 'Note:' paragraph should not be stripped")
	}
}

// TestImportRealNotionZip runs the importer against the user's actual export
// when SALT_TEST_ZIP points at it. Skipped otherwise so the suite stays
// self-contained.
func TestImportRealNotionZip(t *testing.T) {
	path := os.Getenv("SALT_TEST_ZIP")
	if path == "" {
		t.Skip("set SALT_TEST_ZIP to the real Notion export to run this")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	entries := flatten(t, data)
	s, wsid, uid := newImportTestServer(t)
	created, skipped := s.importZipFiles(entries, nil, wsid, uid)
	t.Logf("real export: created=%d skipped=%d entries=%d", created, skipped, len(entries))

	var colCount, rowCount int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE type='collection'`).Scan(&colCount)
	var colID, colTitle string
	s.db.QueryRow(`SELECT id, title FROM pages WHERE type='collection' LIMIT 1`).Scan(&colID, &colTitle)
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE parent_id=?`, colID).Scan(&rowCount)
	t.Logf("collections=%d  first=%q  rows=%d", colCount, colTitle, rowCount)
	if colCount != 1 {
		t.Fatalf("want exactly 1 collection, got %d", colCount)
	}
	if rowCount != 37 {
		t.Fatalf("want 37 rows, got %d", rowCount)
	}
	// After stripping Notion's "# title + Property: value" preamble, only rows
	// with real page content beyond their properties keep a body (14 of 37; the
	// rest are pure property-dump → empty, their values live in the panel).
	var withBody int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE parent_id=? AND content <> '[]' AND content <> ''`, colID).Scan(&withBody)
	t.Logf("rows with real body after preamble strip: %d/37", withBody)
	if withBody != 14 {
		t.Fatalf("want 14 rows with real content, got %d", withBody)
	}
	if created != 38 { // exactly 1 collection + 37 rows, no phantom loose pages
		t.Fatalf("want created=38, got %d", created)
	}
	if skipped != 0 { // no images in this export; CSVs are handled, not skipped
		t.Fatalf("want skipped=0, got %d", skipped)
	}
	// No row body should still carry the repeated property preamble.
	var dumpRows int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE parent_id=? AND content LIKE '%Geschätzter Aufwand:%'`, colID).Scan(&dumpRows)
	if dumpRows != 0 {
		t.Fatalf("preamble not stripped: %d rows still dump properties into the body", dumpRows)
	}
}
