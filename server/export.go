package server

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"
)

// ---- BlockNote JSON → Markdown ----

type mdBlock struct {
	Type     string          `json:"type"`
	Props    map[string]any  `json:"props"`
	Content  json.RawMessage `json:"content"`
	Children []mdBlock       `json:"children"`
}

type mdInline struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Styles  map[string]any  `json:"styles"`
	Href    string          `json:"href"`
	Props   map[string]any  `json:"props"`
	Content json.RawMessage `json:"content"`
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func strProp(props map[string]any, key, fallback string) string {
	if s, ok := props[key].(string); ok && s != "" {
		return s
	}
	return fallback
}

func intProp(props map[string]any, key string, fallback int) int {
	if f, ok := props[key].(float64); ok {
		return int(f)
	}
	return fallback
}

func styleText(text string, styles map[string]any) string {
	if text == "" {
		return ""
	}
	if truthy(styles["code"]) {
		return "`" + text + "`"
	}
	if truthy(styles["bold"]) {
		text = "**" + text + "**"
	}
	if truthy(styles["italic"]) {
		text = "*" + text + "*"
	}
	if truthy(styles["strike"]) {
		text = "~~" + text + "~~"
	}
	if truthy(styles["underline"]) {
		text = "<u>" + text + "</u>" // Markdown has no underline; HTML is portable
	}
	return text
}

func renderInline(raw json.RawMessage) string {
	var items []mdInline
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		switch it.Type {
		case "link":
			b.WriteString("[" + renderInline(it.Content) + "](" + it.Href + ")")
		case "pageLink":
			label := strProp(it.Props, "label", "Untitled")
			id := strProp(it.Props, "pageId", "")
			b.WriteString("[" + label + "](/p/" + id + ")")
		default:
			b.WriteString(styleText(it.Text, it.Styles))
		}
	}
	return b.String()
}

func plainInline(raw json.RawMessage) string {
	var items []mdInline
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		if it.Type == "link" {
			b.WriteString(plainInline(it.Content))
		} else {
			b.WriteString(it.Text)
		}
	}
	return b.String()
}

func isListType(t string) bool {
	return t == "bulletListItem" || t == "numberedListItem" || t == "checkListItem"
}

func blocksToMarkdown(content []byte) string {
	var blocks []mdBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	renderBlocks(&b, blocks, 0)
	return b.String()
}

func renderBlocks(b *strings.Builder, blocks []mdBlock, depth int) {
	indent := strings.Repeat("    ", depth)
	num := 0
	for i, blk := range blocks {
		if blk.Type == "numberedListItem" {
			num++
		} else {
			num = 0
		}
		switch blk.Type {
		case "heading":
			level := intProp(blk.Props, "level", 1)
			if level < 1 || level > 6 {
				level = 1
			}
			b.WriteString(indent + strings.Repeat("#", level) + " " + renderInline(blk.Content) + "\n\n")
		case "bulletListItem":
			b.WriteString(indent + "- " + renderInline(blk.Content) + "\n")
		case "numberedListItem":
			b.WriteString(indent + fmt.Sprintf("%d. ", num) + renderInline(blk.Content) + "\n")
		case "checkListItem":
			mark := " "
			if truthy(blk.Props["checked"]) {
				mark = "x"
			}
			b.WriteString(indent + "- [" + mark + "] " + renderInline(blk.Content) + "\n")
		case "toggleListItem":
			// No native Markdown collapse; render as a list item, children follow.
			b.WriteString(indent + "- " + renderInline(blk.Content) + "\n")
		case "divider":
			b.WriteString(indent + "---\n\n")
		case "callout":
			emoji := strProp(blk.Props, "emoji", "💡")
			b.WriteString(indent + "> " + emoji + " " + renderInline(blk.Content) + "\n\n")
		case "bookmark":
			if raw := strProp(blk.Props, "url", ""); raw != "" {
				b.WriteString(indent + "[" + raw + "](" + safeURL(raw) + ")\n\n")
			}
		case "database":
			// Embedded database: the block holds only a reference, the data lives
			// in the database page. So the Markdown carries a link there — a dump
			// of the rows would be a copy that goes stale at once.
			if id := strProp(blk.Props, "collectionId", ""); id != "" {
				b.WriteString(indent + "[Datenbank](/p/" + id + ")\n\n")
			}
		case "toc":
			// Generated client-side from headings; nothing meaningful to export.
		case "columnList", "column":
			// Layout containers: flatten children at the SAME depth (an indent
			// would misread as nesting). Children handled below via the generic
			// recursion, so nothing to emit here — but skip the extra indent by
			// rendering them now and clearing Children.
			renderBlocks(b, blk.Children, depth)
			blk.Children = nil
		case "quote":
			b.WriteString(indent + "> " + renderInline(blk.Content) + "\n\n")
		case "codeBlock":
			b.WriteString(indent + "```" + strProp(blk.Props, "language", "") + "\n" + plainInline(blk.Content) + "\n" + indent + "```\n\n")
		case "image":
			b.WriteString(indent + "![" + strProp(blk.Props, "name", "image") + "](" + strProp(blk.Props, "url", "") + ")\n\n")
		case "video", "audio", "file":
			b.WriteString(indent + "[" + strProp(blk.Props, "name", blk.Type) + "](" + strProp(blk.Props, "url", "") + ")\n\n")
		case "table":
			renderTable(b, blk.Content, indent)
		default: // paragraph and unknown block types
			if t := renderInline(blk.Content); t != "" {
				b.WriteString(indent + t + "\n\n")
			}
		}
		if len(blk.Children) > 0 {
			renderBlocks(b, blk.Children, depth+1)
		}
		if isListType(blk.Type) && (i == len(blocks)-1 || !isListType(blocks[i+1].Type)) {
			b.WriteString("\n")
		}
	}
}

func renderTable(b *strings.Builder, raw json.RawMessage, indent string) {
	var tc struct {
		Rows []struct {
			Cells []json.RawMessage `json:"cells"`
		} `json:"rows"`
	}
	if json.Unmarshal(raw, &tc) != nil || len(tc.Rows) == 0 {
		return
	}
	renderCell := func(c json.RawMessage) string {
		// A cell is either a plain inline array or a tableCell object with content.
		var obj struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(c, &obj); err == nil && len(obj.Content) > 0 {
			return renderInline(obj.Content)
		}
		return renderInline(c)
	}
	for ri, row := range tc.Rows {
		b.WriteString(indent + "|")
		for _, c := range row.Cells {
			b.WriteString(" " + strings.ReplaceAll(renderCell(c), "|", "\\|") + " |")
		}
		b.WriteString("\n")
		if ri == 0 {
			b.WriteString(indent + "|")
			for range row.Cells {
				b.WriteString(" --- |")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
}

// extractText pulls all human-readable text out of BlockNote JSON for indexing.
func extractText(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var b strings.Builder
	walkText(v, &b)
	return b.String()
}

func walkText(v any, b *strings.Builder) {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t["text"].(string); ok {
			b.WriteString(s)
			b.WriteString(" ")
		}
		// Index the visible label of a page mention too.
		if t["type"] == "pageLink" {
			if props, ok := t["props"].(map[string]any); ok {
				if label, ok := props["label"].(string); ok {
					b.WriteString(label)
					b.WriteString(" ")
				}
			}
		}
		for _, val := range t {
			walkText(val, b)
		}
	case []any:
		for _, val := range t {
			walkText(val, b)
		}
	}
}

// ---- Export handlers ----

func pageMarkdown(p *page) string {
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	h := "# "
	if p.Icon != "" {
		h += p.Icon + " "
	}
	return h + title + "\n\n" + blocksToMarkdown(p.Content)
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	repl := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "'", "<", "(", ">", ")", "|", "-", "\n", " ", "\r", " ", "\t", " ")
	name = strings.Trim(repl.Replace(name), ". ")
	if name == "" {
		return "Untitled"
	}
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

// collectionMarkdown renders a database page as a Markdown table of its rows.
func (s *Server) collectionMarkdown(p *page) (string, error) {
	var schemaJSON string
	if err := s.db.QueryRow(`SELECT schema FROM collections WHERE page_id = ?`, p.ID).Scan(&schemaJSON); err != nil {
		return pageMarkdown(p), nil
	}
	type option struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var schema []struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Type    string   `json:"type"`
		Options []option `json:"options"`
	}
	json.Unmarshal([]byte(schemaJSON), &schema)
	optionName := map[string]string{}
	for _, prop := range schema {
		for _, o := range prop.Options {
			optionName[prop.ID+"/"+o.ID] = o.Name
		}
	}

	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	var b strings.Builder
	b.WriteString("# " + title + "\n\n| Title |")
	for _, prop := range schema {
		b.WriteString(" " + prop.Name + " |")
	}
	b.WriteString("\n| --- |")
	for range schema {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	rows, err := s.db.Query(`SELECT title, props FROM pages WHERE parent_id = ? AND trashed_at IS NULL ORDER BY position, created_at`, p.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var rowTitle, propsJSON string
		if rows.Scan(&rowTitle, &propsJSON) != nil {
			continue
		}
		if rowTitle == "" {
			rowTitle = "Untitled"
		}
		var props map[string]any
		json.Unmarshal([]byte(propsJSON), &props)
		b.WriteString("| " + strings.ReplaceAll(rowTitle, "|", "\\|") + " |")
		for _, prop := range schema {
			cell := ""
			switch v := props[prop.ID].(type) {
			case string:
				if n, ok := optionName[prop.ID+"/"+v]; ok {
					cell = n
				} else {
					cell = v
				}
			case bool:
				if v {
					cell = "✓"
				}
			case float64:
				cell = strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.4f", v), "0000"), ".")
			case []any:
				var parts []string
				for _, item := range v {
					if str, ok := item.(string); ok {
						if n, ok := optionName[prop.ID+"/"+str]; ok {
							parts = append(parts, n)
						} else {
							parts = append(parts, str)
						}
					}
				}
				cell = strings.Join(parts, ", ")
			}
			b.WriteString(" " + strings.ReplaceAll(cell, "|", "\\|") + " |")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (s *Server) handleExportPage(w http.ResponseWriter, r *http.Request) {
	if !s.canReadReq(r, r.PathValue("id")) {
		httpError(w, 404, "page not found")
		return
	}
	p, err := s.getPage(r.PathValue("id"))
	if err == sql.ErrNoRows {
		httpError(w, 404, "page not found")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// HTML export (?format=html) is offered for document pages — real structure
	// for opening in a browser or importing elsewhere. Databases stay Markdown
	// (a table is the faithful representation of their rows).
	if r.URL.Query().Get("format") == "html" && p.Type != "collection" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// print=1 opens inline in a new tab (a beautiful print/PDF view that works
		// on mobile too); otherwise it's a downloadable .html file.
		if r.URL.Query().Get("print") == "1" {
			w.Header().Set("X-Robots-Tag", "noindex")
			w.Write([]byte(pageHTML(p, true)))
		} else {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(p.Title) + ".html"}))
			w.Write([]byte(pageHTML(p, false)))
		}
		return
	}
	md := ""
	if p.Type == "collection" {
		md, err = s.collectionMarkdown(p)
		if err != nil {
			httpError(w, 500, err.Error())
			return
		}
	} else {
		md = pageMarkdown(p)
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": safeFilename(p.Title) + ".md"}))
	w.Write([]byte(md))
}

func (s *Server) handleExportAll(w http.ResponseWriter, r *http.Request) {
	userID := requestUser(r).ID
	ws := s.scopeWorkspacesFor(requestUser(r), s.visibleWorkspaces(userID))
	// ?workspace= narrows to ONE workspace — "export workspace" in the menu
	// must not quietly take the whole instance along.
	if only := r.URL.Query().Get("workspace"); only != "" {
		found := false
		for _, v := range ws {
			if v == only {
				found = true
				break
			}
		}
		if !found {
			httpError(w, 404, "workspace not found")
			return
		}
		ws = []string{only}
	}
	if len(ws) == 0 {
		httpError(w, 400, "no workspace")
		return
	}
	wargs := make([]any, len(ws))
	for i, v := range ws {
		wargs[i] = v
	}
	rows, err := s.db.Query(`SELECT id, parent_id, title, icon, content FROM pages WHERE trashed_at IS NULL AND workspace_id IN (`+placeholders(len(ws))+`) ORDER BY position, created_at`, wargs...)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}

	type expPage struct {
		id, title, icon, content string
		parentID                 *string
	}
	var scanned []expPage
	for rows.Next() {
		var p expPage
		if err := rows.Scan(&p.id, &p.parentID, &p.title, &p.icon, &p.content); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		scanned = append(scanned, p)
	}
	rows.Close() // drain before per-row canRead (single DB connection)
	var all []expPage
	ids := map[string]bool{}
	for _, p := range scanned {
		if !s.canRead(userID, p.id) { // exclude private subtrees the user can't read
			continue
		}
		all = append(all, p)
		ids[p.id] = true
	}

	children := map[string][]expPage{}
	for _, p := range all {
		key := ""
		if p.parentID != nil && ids[*p.parentID] {
			key = *p.parentID
		}
		children[key] = append(children[key], p)
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="salt-export.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()

	var walk func(parentKey, dir string)
	walk = func(parentKey, dir string) {
		used := map[string]int{}
		for _, p := range children[parentKey] {
			base := safeFilename(p.title)
			used[strings.ToLower(base)]++
			if n := used[strings.ToLower(base)]; n > 1 {
				base = fmt.Sprintf("%s (%d)", base, n)
			}
			f, err := zw.Create(path.Join(dir, base+".md"))
			if err != nil {
				return
			}
			pg := &page{Content: json.RawMessage(p.content)}
			pg.Title = p.title
			pg.Icon = p.icon
			f.Write([]byte(pageMarkdown(pg)))
			if len(children[p.id]) > 0 {
				walk(p.id, path.Join(dir, base))
			}
		}
	}
	walk("", "")
}
