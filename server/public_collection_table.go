package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
)

// This is presentation only: the rows and values are the same immediate,
// non-trashed children that collectionMarkdown exports. It grants no row-page
// navigation or child content access.
func (s *Server) publicCollectionTable(p *page, nonces ...string) (string, error) {
	nonce := ""
	if len(nonces) > 0 {
		nonce = nonces[0]
	}
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	var schemaJSON string
	if err := s.db.QueryRow(`SELECT schema FROM collections WHERE page_id=?`, p.ID).Scan(&schemaJSON); err != nil {
		if err == sql.ErrNoRows {
			return s.publicDocument(title, `<h1>`+html.EscapeString(title)+`</h1>`+publicBlocksHTML(p.Content), nonce), nil
		}
		return "", err
	}
	var schema []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
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
	rule, args := s.privateRowRule("", p.ID)
	queryArgs := append([]any{p.ID}, args...)
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE parent_id = ? AND trashed_at IS NULL AND `+rule, queryArgs...).Scan(&total); err != nil {
		return "", err
	}
	rows, err := s.db.Query(`SELECT title, props FROM pages WHERE parent_id = ? AND trashed_at IS NULL AND `+rule+` ORDER BY position, created_at LIMIT 1000`, queryArgs...)
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
			body.WriteString(`<td>` + html.EscapeString(publicCollectionTypedCell(props[prop.ID], prop.Type, prop.ID, options)) + `</td>`)
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
	if total > count {
		body.WriteString(`<p>` + strconv.Itoa(total-count) + ` more rows.</p>`)
	}
	return s.publicDocument(title, `<style>`+publicCollectionStyle+`</style>`+body.String(), nonce), nil
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
const publicCollectionStyle = `*{box-sizing:border-box}.public-collection{margin:0;height:auto;min-height:100vh;background:var(--bg,#fbfaf7);color:var(--fg,#1c1a15);font:16px/1.6 var(--mono,ui-monospace,monospace)}.public-view .public-doc{max-width:1100px;margin:auto;padding:48px 32px}.public-doc h1{font-size:clamp(28px,4vw,40px);line-height:1.2;margin:0 0 20px}.collection-description{color:var(--muted,#7e7d78);margin:0 0 24px}.collection-scroll{overflow:auto;max-width:100%;border:1px solid var(--border,#ddd);border-radius:var(--radius,10px)}.public-view .collection-scroll{scrollbar-width:auto}.public-view .collection-scroll::-webkit-scrollbar{display:block;height:12px;width:12px}.public-view .collection-scroll::-webkit-scrollbar-thumb{background:var(--muted,#7e7d78);border-radius:6px}.public-view .collection-scroll::-webkit-scrollbar-track{background:var(--hover,#eee)}.collection-scroll:focus-visible{outline:2px solid var(--accent,#b3123f);outline-offset:3px}.collection-scroll table{border-collapse:collapse;min-width:100%;width:max-content;margin:0}.collection-scroll th,.collection-scroll td{min-width:140px;max-width:360px;padding:12px 16px;text-align:left;vertical-align:top;white-space:pre-wrap;overflow-wrap:anywhere;border-right:1px solid var(--border,#ddd);border-bottom:1px solid var(--border,#ddd)}.collection-scroll thead{background:var(--sidebar-bg,#f2f1ea)}.collection-scroll thead th{font-size:13px;font-weight:600;color:var(--muted,#7e7d78)}.collection-scroll tbody th{font-weight:500}.collection-scroll tr>*:last-child{border-right:0}.collection-scroll tbody tr:last-child>*{border-bottom:0}.collection-scroll .collection-empty{padding:32px;text-align:center;color:var(--muted,#7e7d78)}@media(max-width:600px){.public-view .public-doc{padding:28px 16px}}@media print{.public-collection{background:white;color:black}.public-view .public-doc{padding:0;max-width:none}.collection-scroll{overflow:visible}.collection-scroll table{width:100%;table-layout:fixed}.collection-scroll th,.collection-scroll td{min-width:0}}`

// Values from an old property type must not survive a schema change. Relations
// and people stay blank rather than exposing internal IDs or unpublished names.
func publicCollectionTypedCell(value any, kind, property string, options map[string]string) string {
	switch kind {
	case "text", "url", "email", "phone", "date":
		if v, ok := value.(string); ok {
			return v
		}
	case "number":
		if v, ok := value.(float64); ok {
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	case "checkbox":
		if v, ok := value.(bool); ok && v {
			return "✓"
		}
	case "select":
		if v, ok := value.(string); ok {
			return options[property+"/"+v]
		}
	case "multiselect":
		if values, ok := value.([]any); ok {
			var names []string
			for _, item := range values {
				if v, ok := item.(string); ok {
					if name := options[property+"/"+v]; name != "" {
						names = append(names, name)
					}
				}
			}
			return strings.Join(names, ", ")
		}
	}
	return ""
}
