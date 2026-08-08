package server

import (
	"encoding/csv"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Notion database import (Welle 46): Notion's "Export → Markdown & CSV" writes
// each database as a "<Name> <id>.csv" (all rows + property columns) plus a
// "<Name> <id>/" folder with one .md per row (the row body). The plain-Markdown
// importer only ingested the .md files, so databases arrived as loose pages
// with their columns lost. This turns the CSV into a real collection: columns →
// typed properties (select/date/number/text inferred from the values), rows →
// database rows, and the paired .md bodies fill each row's content.

var optionPalette = []string{
	"#c4554d", "#b58a3b", "#2f7d4f", "#3b6fb5", "#7d4fb0",
	"#c1548a", "#8a6d3b", "#4a8a8a", "#6a6a6a", "#a8574d",
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func propSlug(name string, used map[string]bool) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if s == "" {
		s = "prop"
	}
	base, i := s, 1
	for used[s] {
		i++
		s = base + strconv.Itoa(i)
	}
	used[s] = true
	return s
}

func normTitle(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

var dateLayouts = []string{
	"2006-01-02", time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04",
	"January 2, 2006", "Jan 2, 2006", "02.01.2006", "01/02/2006", "2.1.2006",
}

// parseDate returns an ISO date (YYYY-MM-DD) if v looks like a date, else "".
func parseDate(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Notion date ranges look like "Start → End"; take the start.
	if i := strings.Index(v, "→"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// inferColumn guesses a property type from a column's non-empty values.
func inferColumn(rows [][]string, j int) (typ string, options map[string]string, order []string) {
	var vals []string
	for _, r := range rows {
		if j < len(r) {
			if v := strings.TrimSpace(r[j]); v != "" {
				vals = append(vals, v)
			}
		}
	}
	if len(vals) == 0 {
		return "text", nil, nil
	}
	allNum := true
	for _, v := range vals {
		if _, e := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); e != nil {
			allNum = false
			break
		}
	}
	if allNum {
		return "number", nil, nil
	}
	allDate := true
	for _, v := range vals {
		if parseDate(v) == "" {
			allDate = false
			break
		}
	}
	if allDate {
		return "date", nil, nil
	}
	seen := map[string]bool{}
	for _, v := range vals {
		if !seen[v] {
			seen[v] = true
			order = append(order, v)
		}
	}
	// A small, repeating set of distinct values = a Select column; long unique
	// free text (e.g. a Notes column) stays text.
	if len(order) <= 12 && (len(order) < len(vals) || len(order) <= 6) {
		options = map[string]string{}
		for _, v := range order {
			options[v] = "o" + newID()[:8]
		}
		return "select", options, order
	}
	return "text", nil, nil
}

// stripRowPreambleBlocks removes the leading blocks a Notion row import wrote as
// a redundant preamble: a level-1 heading (the repeated row title) followed by
// contiguous "Property: value" paragraphs for known columns. Blocks that survive
// are kept byte-for-byte (raw JSON), so real page content is never re-serialised
// or altered. Used to retro-clean rows imported before the importer stripped
// this preamble at import time. Returns the new content and whether it changed.
func stripRowPreambleBlocks(content []byte, propNames []string) ([]byte, bool) {
	var blocks []json.RawMessage
	if err := json.Unmarshal(content, &blocks); err != nil || len(blocks) == 0 {
		return content, false
	}
	names := map[string]bool{}
	for _, n := range propNames {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			names[n] = true
		}
	}
	type peek struct {
		Type  string `json:"type"`
		Props struct {
			Level int `json:"level"`
		} `json:"props"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	read := func(raw json.RawMessage) (p peek, text string) {
		json.Unmarshal(raw, &p)
		var sb strings.Builder
		for _, c := range p.Content {
			sb.WriteString(c.Text)
		}
		return p, sb.String()
	}

	i := 0
	if p, _ := read(blocks[i]); p.Type == "heading" && p.Props.Level == 1 {
		i++ // tentatively the repeated title
	}
	propStart := i
	for i < len(blocks) {
		p, text := read(blocks[i])
		if p.Type != "paragraph" {
			break
		}
		t := strings.TrimSpace(text)
		c := strings.IndexByte(t, ':')
		if c <= 0 || !names[strings.ToLower(strings.TrimSpace(t[:c]))] {
			break
		}
		i++
	}
	// Require at least one "Property: value" line before stripping anything — a
	// lone leading H1 with no property lines is likely genuine user content, not
	// Notion's title dump, so leave such a body untouched.
	if i == propStart {
		return content, false
	}
	out, err := json.Marshal(blocks[i:])
	if err != nil {
		return content, false
	}
	return out, true
}

// cleanupNotionRowBodies strips the redundant Notion import preamble from every
// existing collection-row body (see stripRowPreambleBlocks). Returns the number
// of rows changed. Cursors are drained before any write — the single SQLite
// connection deadlocks if an UPDATE runs while a SELECT cursor is open.
func (s *Server) cleanupNotionRowBodies() (int, error) {
	rows, err := s.db.Query(`SELECT page_id, schema FROM collections`)
	if err != nil {
		return 0, err
	}
	type col struct {
		id    string
		names []string
	}
	var cols []col
	for rows.Next() {
		var id, schema string
		if rows.Scan(&id, &schema) != nil {
			continue
		}
		var props []struct {
			Name string `json:"name"`
		}
		json.Unmarshal([]byte(schema), &props)
		names := make([]string, 0, len(props))
		for _, p := range props {
			names = append(names, p.Name)
		}
		cols = append(cols, col{id, names})
	}
	rows.Close()

	n := 0
	for _, c := range cols {
		r2, err := s.db.Query(`SELECT id, content FROM pages WHERE parent_id = ?`, c.id)
		if err != nil {
			continue
		}
		type rc struct{ id, content string }
		var list []rc
		for r2.Next() {
			var id, content string
			if r2.Scan(&id, &content) == nil {
				list = append(list, rc{id, content})
			}
		}
		r2.Close()
		for _, r := range list {
			if out, changed := stripRowPreambleBlocks([]byte(r.content), c.names); changed {
				if _, err := s.db.Exec(`UPDATE pages SET content = ?, updated_at = ? WHERE id = ?`, string(out), now(), r.id); err == nil {
					s.reindexPage(r.id)
					n++
				}
			}
		}
	}
	return n, nil
}

// FixNotionRows opens the data directory's database directly and retro-cleans
// Notion-import row bodies. Run with the server stopped (it takes the sole
// SQLite connection). Exposed for the "fix-notion-rows" CLI subcommand.
func FixNotionRows(dataDir string) (int, error) {
	db, err := openDB(filepath.Join(dataDir, DBFile))
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return (&Server{db: db}).cleanupNotionRowBodies()
}

// matchBody picks a row's page body from the paired export files, tolerant of
// how Notion names them: a filename is filesystem-sanitized (illegal chars like
// "/" or ":" dropped) and truncated to ~50 chars, so the file's normalized
// title is an exact match, a leading token prefix, or an ordered token
// subsequence of the full row title. Exact wins, then the longest candidate; a
// matched body is consumed via `used` so no two rows claim the same file.
func matchBody(bodies map[string]string, used map[string]bool, rowTitle string) (body, key string) {
	rt := normTitle(rowTitle)
	if rt == "" {
		return "", ""
	}
	if b, ok := bodies[rt]; ok && !used[rt] {
		return b, rt
	}
	rowTok := strings.Fields(rt)
	bestKey, bestScore := "", 0
	for k := range bodies {
		if used[k] || k == "" || len(k) > len(rt) {
			continue
		}
		score := 0
		switch {
		case strings.HasPrefix(rt, k):
			// Notion truncates long filenames mid-word, so the file's title is a
			// raw string prefix of the row title (not a clean token boundary).
			score = 2*len(k) + 1
		case isSubsequence(rowTok, strings.Fields(k)):
			// Filesystem-illegal runs are dropped from the filename (e.g. the
			// "http://" in a URL), leaving an ordered subsequence of the tokens.
			score = 2 * len(k)
		}
		if score > bestScore {
			bestScore, bestKey = score, k
		}
	}
	if bestKey != "" {
		return bodies[bestKey], bestKey
	}
	return "", ""
}

// stripNotionRowPreamble drops the redundant header Notion prepends to every
// row's .md export — a "# <title>" heading followed by one "Property: value"
// line per column — so the row body keeps only its real page content (often
// nothing). The structured properties are shown by the row's property panel, so
// repeating them as body text is pure duplication.
func stripNotionRowPreamble(md string, header []string) string {
	names := map[string]bool{}
	for _, h := range header {
		if n := strings.ToLower(strings.TrimSpace(h)); n != "" {
			names[n] = true
		}
	}
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	// The leading "# <title>" heading — Notion always writes the row title as
	// the first line, so a leading H1 is the repeated title, never real content.
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "# ") {
		i++
	}
	// Contiguous "Property: value" lines whose label is one of the columns.
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			i++
			continue
		}
		c := strings.IndexByte(t, ':')
		if c <= 0 || !names[strings.ToLower(strings.TrimSpace(t[:c]))] {
			break
		}
		i++
	}
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}

func isSubsequence(hay, needle []string) bool {
	i := 0
	for _, h := range hay {
		if i < len(needle) && h == needle[i] {
			i++
		}
	}
	return i == len(needle)
}

// importNotionCSV creates a collection (database) from a Notion CSV export.
// `bodies` maps a row's normalized title → its page content (blocks JSON) from
// the paired export folder. Returns the collection page id and rows created.
func (s *Server) importNotionCSV(data []byte, title, parentID, workspaceID, userID string, bodies map[string]string) (string, int, error) {
	// Notion CSVs start with a UTF-8 BOM; strip it so the first column header
	// (and thus its slug) isn't polluted by the marker.
	rd := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
	rd.FieldsPerRecord = -1
	records, err := rd.ReadAll()
	if err != nil || len(records) < 1 || len(records[0]) == 0 {
		return "", 0, err
	}
	header, rows := records[0], records[1:]

	used := map[string]bool{}
	ids := make([]string, len(header))
	typs := make([]string, len(header))
	opts := make([]map[string]string, len(header))
	orders := make([][]string, len(header))
	for j := range header {
		ids[j] = propSlug(strings.TrimSpace(header[j]), used)
		if j == 0 {
			typs[j] = "title"
			continue
		}
		typs[j], opts[j], orders[j] = inferColumn(rows, j)
	}

	// Build schema JSON (every column except the title).
	type opt struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	type prop struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Options []opt  `json:"options,omitempty"`
	}
	var schema []prop
	firstSelect, statusSelect := "", ""
	for j := 1; j < len(header); j++ {
		p := prop{ID: ids[j], Name: strings.TrimSpace(header[j]), Type: typs[j]}
		if typs[j] == "select" {
			for oi, v := range orders[j] {
				p.Options = append(p.Options, opt{ID: opts[j][v], Name: v, Color: optionPalette[oi%len(optionPalette)]})
			}
			if firstSelect == "" {
				firstSelect = ids[j]
			}
			if statusSelect == "" && strings.EqualFold(strings.TrimSpace(header[j]), "Status") {
				statusSelect = ids[j]
			}
		}
		schema = append(schema, p)
	}
	// A board groups most naturally by a "Status" column; else the first select.
	groupBy := statusSelect
	if groupBy == "" {
		groupBy = firstSelect
	}
	schemaJSON, _ := json.Marshal(schema)

	views := []map[string]any{}
	if groupBy != "" {
		views = append(views, map[string]any{"id": "board", "name": "Board", "type": "board", "groupBy": groupBy})
	}
	views = append(views, map[string]any{"id": "table", "name": "Table", "type": "table"})
	viewsJSON, _ := json.Marshal(views)

	// Create the collection page (mirror the proven page insert, then flag it).
	var parentArg any
	if parentID != "" {
		parentArg = parentID
	}
	colID, ts := newID(), now()
	var pos float64
	s.db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM pages WHERE parent_id IS ?`, parentArg).Scan(&pos)
	if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, ?, ?, '', '[]', ?, ?, ?, ?, ?, 'workspace')`,
		colID, parentArg, title, pos, ts, ts, workspaceID, userID); err != nil {
		return "", 0, err
	}
	if _, err := s.db.Exec(`UPDATE pages SET type = 'collection' WHERE id = ?`, colID); err != nil {
		return "", 0, err
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)`, colID, string(schemaJSON), string(viewsJSON)); err != nil {
		return "", 0, err
	}
	s.reindexPage(colID)

	// Create the rows.
	n := 0
	usedBody := map[string]bool{}
	for _, rec := range rows {
		rowTitle := ""
		if len(rec) > 0 {
			rowTitle = strings.TrimSpace(rec[0])
		}
		empty := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		props := map[string]any{}
		for j := 1; j < len(header) && j < len(rec); j++ {
			v := strings.TrimSpace(rec[j])
			if v == "" {
				continue
			}
			switch typs[j] {
			case "number":
				if f, e := strconv.ParseFloat(strings.ReplaceAll(v, ",", "."), 64); e == nil {
					props[ids[j]] = f
				}
			case "date":
				if iso := parseDate(v); iso != "" {
					props[ids[j]] = iso
				}
			case "select":
				if oid, ok := opts[j][v]; ok {
					props[ids[j]] = oid
				}
			default:
				props[ids[j]] = v
			}
		}
		body := "[]"
		if b, key := matchBody(bodies, usedBody, rowTitle); key != "" && b != "" {
			usedBody[key] = true
			// b is raw markdown; drop Notion's property preamble, then convert
			// what remains (the row's real content, if any) to blocks.
			if blocks, e := mdToBlocksJSON(stripNotionRowPreamble(b, header)); e == nil && blocks != "" {
				body = blocks
			}
		}
		rid, rts := newID(), now()
		var rpos float64
		s.db.QueryRow(`SELECT COALESCE(MAX(position),0)+1 FROM pages WHERE parent_id IS ?`, colID).Scan(&rpos)
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
			VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, 'workspace')`,
			rid, colID, rowTitle, body, rpos, rts, rts, workspaceID, userID); err != nil {
			continue
		}
		propsJSON, _ := json.Marshal(props)
		s.db.Exec(`UPDATE pages SET props = ? WHERE id = ?`, string(propsJSON), rid)
		s.reindexPage(rid)
		n++
	}
	return colID, n, nil
}
