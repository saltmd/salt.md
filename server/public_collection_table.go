package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"strings"
)

// This is presentation only: the rows and values are the same immediate,
// non-trashed children that collectionMarkdown exports. It grants no row-page
// navigation or child content access.
func (s *Server) publicCollectionTable(p *page) (string, error) {
	var schemaJSON string
	if err := s.db.QueryRow(`SELECT schema FROM collections WHERE page_id=?`, p.ID).Scan(&schemaJSON); err != nil {
		if err == sql.ErrNoRows {
			return s.collectionDocument(p.Title, `<h1>`+html.EscapeString(p.Title)+`</h1>`+blocksToHTML(p.Content)), nil
		}
		return "", err
	}
	var schema []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Options []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"options"`
	}
	json.Unmarshal([]byte(schemaJSON), &schema)
	options := map[string]string{}
	for _, prop := range schema {
		for _, opt := range prop.Options {
			options[prop.ID+"/"+opt.ID] = opt.Name
		}
	}
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	var body strings.Builder
	body.WriteString(`<h1>` + html.EscapeString(title) + `</h1>`)
	if p.Description != "" {
		body.WriteString(`<p class="collection-description">` + html.EscapeString(p.Description) + `</p>`)
	}
	body.WriteString(`<div class="collection-scroll" tabindex="0" role="region" aria-label="` + html.EscapeString(title) + `"><table><thead><tr><th scope="col">Title</th>`)
	for _, prop := range schema {
		body.WriteString(`<th scope="col">` + html.EscapeString(prop.Name) + `</th>`)
	}
	body.WriteString(`</tr></thead><tbody>`)
	rows, err := s.db.Query(`SELECT title, props FROM pages WHERE parent_id = ? AND trashed_at IS NULL ORDER BY position, created_at`, p.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var rowTitle, raw string
		if err := rows.Scan(&rowTitle, &raw); err != nil {
			return "", err
		}
		if rowTitle == "" {
			rowTitle = "Untitled"
		}
		var props map[string]any
		json.Unmarshal([]byte(raw), &props)
		body.WriteString(`<tr><th scope="row">` + html.EscapeString(rowTitle) + `</th>`)
		for _, prop := range schema {
			body.WriteString(`<td>` + html.EscapeString(publicCollectionCell(props[prop.ID], prop.ID, options)) + `</td>`)
		}
		body.WriteString(`</tr>`)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if count == 0 {
		body.WriteString(`<tr><td class="collection-empty" colspan="` + fmt.Sprint(len(schema)+1) + `">No rows yet.</td></tr>`)
	}
	body.WriteString(`</tbody></table></div>`)
	return s.collectionDocument(title, body.String()), nil
}

func (s *Server) collectionDocument(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + ` · salt.md</title>` + s.collectionCSS + `<style>` + publicCollectionStyle + `</style></head><body class="public-collection"><main>` + body + `</main></body></html>`
}

func publicCollectionCell(value any, property string, options map[string]string) string {
	option := func(raw string) string {
		if name, ok := options[property+"/"+raw]; ok {
			return name
		}
		return raw
	}
	switch v := value.(type) {
	case string:
		return option(v)
	case bool:
		if v {
			return "✓"
		}
	case float64:
		return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.4f", v), "0000"), ".")
	case []any:
		var parts []string
		for _, item := range v {
			if text, ok := item.(string); ok {
				parts = append(parts, option(text))
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// Only the table scrolls on a small screen; headings and empty states remain
// part of the ordinary document flow. Native tokens come from the app build.
const publicCollectionStyle = `*{box-sizing:border-box}.public-collection{margin:0;height:auto;min-height:100vh;background:var(--bg,#fbfaf7);color:var(--fg,#1c1a15);font:16px/1.6 var(--mono,ui-monospace,monospace)}.public-collection main{max-width:1100px;margin:auto;padding:48px 32px}.public-collection h1{font-size:clamp(28px,4vw,40px);line-height:1.2;margin:0 0 20px}.collection-description{color:var(--muted,#7e7d78);margin:0 0 24px}.collection-scroll{overflow:auto;max-width:100%;border:1px solid var(--border,#ddd);border-radius:var(--radius,10px)}.collection-scroll:focus-visible{outline:2px solid var(--accent,#b3123f);outline-offset:3px}.collection-scroll table{border-collapse:collapse;min-width:100%;width:max-content;margin:0}.collection-scroll th,.collection-scroll td{min-width:140px;max-width:360px;padding:12px 16px;text-align:left;vertical-align:top;white-space:pre-wrap;overflow-wrap:anywhere;border-right:1px solid var(--border,#ddd);border-bottom:1px solid var(--border,#ddd)}.collection-scroll thead{background:var(--sidebar-bg,#f2f1ea)}.collection-scroll thead th{font-size:13px;font-weight:600;color:var(--muted,#7e7d78)}.collection-scroll tbody th{font-weight:500}.collection-scroll tr>*:last-child{border-right:0}.collection-scroll tbody tr:last-child>*{border-bottom:0}.collection-scroll .collection-empty{padding:32px;text-align:center;color:var(--muted,#7e7d78)}@media(max-width:600px){.public-collection main{padding:28px 16px}}@media print{.public-collection{background:white;color:black}.public-collection main{padding:0;max-width:none}.collection-scroll{overflow:visible}.collection-scroll table{width:100%;table-layout:fixed}.collection-scroll th,.collection-scroll td{min-width:0}}`

func collectionStylesheet(index []byte) string {
	for _, tag := range strings.Split(string(index), "<link") {
		end := strings.Index(tag, ">")
		if end < 0 {
			continue
		}
		tag = tag[:end]
		if !strings.Contains(tag, `rel="stylesheet"`) {
			continue
		}
		start := strings.Index(tag, `href="`)
		if start < 0 {
			continue
		}
		raw := tag[start+6:]
		end = strings.Index(raw, `"`)
		if end < 0 {
			continue
		}
		href := html.UnescapeString(raw[:end])
		if strings.HasPrefix(href, "/assets/") && strings.HasSuffix(href, ".css") && !strings.ContainsAny(href, "<>\"'?#") {
			return `<link rel="stylesheet" href="` + html.EscapeString(href) + `">`
		}
	}
	return ""
}
