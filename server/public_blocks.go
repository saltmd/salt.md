package server

import (
	"encoding/json"
	"html"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Public reading preserves the editor's saved properties. Print/export remains
// independent. Attributes use BlockNote's own CSS selectors, not a new palette.
type publicBlock struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Props    map[string]any  `json:"props"`
	Content  json.RawMessage `json:"content"`
	Children []publicBlock   `json:"children"`
}

func publicColor(value any) string {
	s, _ := value.(string)
	switch s {
	case "gray", "brown", "red", "orange", "yellow", "green", "blue", "purple", "pink":
		return s
	}
	return ""
}
func publicAttributes(props map[string]any) string {
	var a strings.Builder
	for _, item := range []struct{ key, attr string }{{"textColor", "data-text-color"}, {"backgroundColor", "data-background-color"}} {
		if c := publicColor(props[item.key]); c != "" {
			a.WriteString(` ` + item.attr + `="` + c + `"`)
		}
	}
	switch align := strProp(props, "textAlignment", ""); align {
	case "left", "center", "right", "justify":
		a.WriteString(` data-text-alignment="` + align + `"`)
	}
	return a.String()
}
func publicInline(raw json.RawMessage) string {
	var items []mdInline
	if json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var out strings.Builder
	for _, it := range items {
		if it.Type == "link" {
			out.WriteString(`<a href="` + html.EscapeString(safeURL(it.Href)) + `" rel="noopener noreferrer">` + publicInline(it.Content) + `</a>`)
			continue
		}
		if it.Type == "pageLink" {
			out.WriteString(`<a href="/p/` + url.PathEscape(strProp(it.Props, "pageId", "")) + `">` + html.EscapeString(strProp(it.Props, "label", "Untitled")) + `</a>`)
			continue
		}
		text := html.EscapeString(it.Text)
		for _, style := range []struct{ key, tag string }{{"bold", "strong"}, {"italic", "em"}, {"strike", "s"}, {"underline", "u"}, {"code", "code"}} {
			if truthy(it.Styles[style.key]) {
				text = "<" + style.tag + ">" + text + "</" + style.tag + ">"
			}
		}
		if attrs := publicAttributes(it.Styles); attrs != "" {
			text = "<span" + attrs + ">" + text + "</span>"
		}
		out.WriteString(text)
	}
	return out.String()
}
func publicBlocksHTML(content []byte) string {
	var blocks []publicBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(`<div class="public-blocks bn-editor">`)
	renderPublicBlocks(&out, blocks, blocks)
	out.WriteString(`</div>`)
	return out.String()
}
func publicBlockAttrs(b publicBlock) string {
	attrs := publicAttributes(b.Props)
	if b.ID != "" {
		attrs += ` id="block-` + html.EscapeString(b.ID) + `"`
	}
	return attrs
}
func renderPublicBlocks(out *strings.Builder, blocks, document []publicBlock) {
	for i := 0; i < len(blocks); {
		b := blocks[i]
		if b.Type == "bulletListItem" || b.Type == "numberedListItem" || b.Type == "checkListItem" {
			tag := "ul"
			if b.Type == "numberedListItem" {
				tag = "ol"
			}
			start := ""
			if tag == "ol" {
				if n := intProp(b.Props, "start", 1); n > 1 && n < 1000000 {
					start = ` start="` + strconv.Itoa(n) + `"`
				}
			}
			out.WriteString("<" + tag + start + ">")
			j := i
			for j < len(blocks) && blocks[j].Type == b.Type {
				item := blocks[j]
				out.WriteString(`<li class="public-block"` + publicBlockAttrs(item) + `>`)
				if b.Type == "checkListItem" {
					checked := ""
					if truthy(item.Props["checked"]) {
						checked = " checked"
					}
					out.WriteString(`<input type="checkbox" disabled` + checked + ` aria-label="Checklist item"> `)
				}
				out.WriteString(publicInline(item.Content))
				renderPublicBlocks(out, item.Children, document)
				out.WriteString("</li>")
				j++
			}
			out.WriteString("</" + tag + ">")
			i = j
			continue
		}
		renderPublicBlock(out, b, document)
		i++
	}
}
func publicPreviewWidth(props map[string]any) int {
	n, ok := props["previewWidth"].(float64)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 16384 {
		return 0
	}
	return int(n)
}
func renderPublicMedia(out *strings.Builder, b publicBlock) {
	raw := strProp(b.Props, "url", "")
	name := strProp(b.Props, "name", b.Type)
	caption := strProp(b.Props, "caption", "")
	preview := b.Type != "file"
	if v, ok := b.Props["showPreview"].(bool); ok {
		preview = v
	}
	out.WriteString(`<div class="public-media"` + publicAttributes(b.Props) + `>`)
	width := ""
	if n := publicPreviewWidth(b.Props); n > 0 {
		width = ` width="` + strconv.Itoa(n) + `"`
	}
	figureStyle := ""
	if n := publicPreviewWidth(b.Props); n > 0 && preview {
		figureStyle = ` style="width:min(100%, ` + strconv.Itoa(n) + `px)"`
	}
	out.WriteString(`<figure` + figureStyle + `>`)
	if !preview || raw == "" {
		out.WriteString(`<a class="public-file" href="` + html.EscapeString(safeURL(raw)) + `">` + html.EscapeString(name) + `</a>`)
	} else {
		switch b.Type {
		case "image":
			out.WriteString(`<img class="bn-visual-media" src="` + html.EscapeString(safeImageURL(raw)) + `" alt="` + html.EscapeString(strProp(b.Props, "name", "")) + `"` + width + `>`)
		case "video":
			out.WriteString(`<video class="bn-visual-media" controls preload="metadata" src="` + html.EscapeString(safeURL(raw)) + `"` + width + `></video>`)
		case "audio":
			out.WriteString(`<audio controls preload="metadata" src="` + html.EscapeString(safeURL(raw)) + `"></audio>`)
		}
	}
	if caption != "" {
		out.WriteString(`<figcaption class="bn-file-caption">` + html.EscapeString(caption) + `</figcaption>`)
	}
	out.WriteString(`</figure></div>`)
}
func renderPublicBlock(out *strings.Builder, b publicBlock, document []publicBlock) {
	// Unknown types cannot inject markup or CSS selectors through their type.
	out.WriteString(`<div class="public-block" data-content-type="` + html.EscapeString(b.Type) + `"` + publicBlockAttrs(b) + `>`)
	inline := func() string { return publicInline(b.Content) }
	children := true
	switch b.Type {
	case "heading":
		n := intProp(b.Props, "level", 1)
		if n < 1 || n > 6 {
			n = 1
		}
		tag := "h" + strconv.Itoa(n)
		heading := "<" + tag + ` class="bn-inline-content">` + inline() + "</" + tag + ">"
		if truthy(b.Props["isToggleable"]) {
			out.WriteString(`<details><summary>` + heading + `</summary>`)
			renderPublicBlocks(out, b.Children, document)
			out.WriteString(`</details>`)
			children = false
		} else {
			out.WriteString(heading)
		}
	case "quote":
		out.WriteString(`<blockquote class="bn-inline-content">` + inline() + `</blockquote>`)
	case "codeBlock":
		out.WriteString(`<pre><code>` + html.EscapeString(plainInline(b.Content)) + `</code></pre>`)
	case "divider":
		out.WriteString(`<hr>`)
	case "image", "video", "audio", "file":
		renderPublicMedia(out, b)
	case "callout":
		emoji := strProp(b.Props, "emoji", "💡")
		tone := "neutral"
		switch emoji {
		case "⚠️":
			tone = "warning"
		case "❗":
			tone = "danger"
		case "✅":
			tone = "success"
		case "🔥":
			tone = "amber"
		case "ℹ️":
			tone = "info"
		}
		out.WriteString(`<div class="bn-callout" data-tone="` + tone + `"><span class="bn-callout-emoji">` + html.EscapeString(emoji) + `</span><div class="bn-callout-content bn-inline-content">` + inline() + `</div></div>`)
	case "toggleListItem":
		out.WriteString(`<details><summary class="bn-inline-content">` + inline() + `</summary>`)
		renderPublicBlocks(out, b.Children, document)
		out.WriteString(`</details>`)
		children = false
	case "bookmark":
		raw := strProp(b.Props, "url", "")
		host := ""
		if u, err := url.Parse(raw); err == nil {
			host = u.Hostname()
		}
		out.WriteString(`<a class="bn-bookmark" href="` + html.EscapeString(safeURL(raw)) + `" target="_blank" rel="noopener noreferrer"><span class="bn-bookmark-icon">🔖</span><span class="bn-bookmark-body"><span class="bn-bookmark-url">` + html.EscapeString(raw) + `</span><span class="bn-bookmark-host">` + html.EscapeString(host) + `</span></span></a>`)
	case "columns", "columnList":
		n := intProp(b.Props, "count", 2)
		if n != 3 {
			n = 2
		}
		out.WriteString(`<div class="cols" data-count="` + strconv.Itoa(n) + `">`)
		for _, col := range b.Children {
			out.WriteString(`<div>`)
			if b.Type == "columnList" {
				renderPublicBlocks(out, col.Children, document)
			} else {
				renderPublicBlocks(out, []publicBlock{col}, document)
			}
			out.WriteString(`</div>`)
		}
		out.WriteString(`</div>`)
		children = false
	case "table":
		renderPublicTable(out, b.Content)
	case "mermaid":
		renderBlockHTML(out, mdBlock{Type: b.Type, Props: b.Props, Content: b.Content})
	case "database":
		out.WriteString(`<a href="/p/` + url.PathEscape(strProp(b.Props, "collectionId", "")) + `">▦ Collection</a>`)
	case "toc":
		out.WriteString(`<nav class="bn-toc" aria-label="Table of contents"><div class="bn-toc-title">Table of contents</div>`)
		var headings func([]publicBlock)
		headings = func(bs []publicBlock) {
			for _, h := range bs {
				if h.Type == "heading" && h.ID != "" {
					out.WriteString(`<a class="bn-toc-entry" href="#block-` + html.EscapeString(h.ID) + `">` + html.EscapeString(plainInline(h.Content)) + `</a>`)
				}
				headings(h.Children)
			}
		}
		headings(document)
		out.WriteString(`</nav>`)
	default:
		if text := inline(); text != "" {
			out.WriteString(`<p class="bn-inline-content">` + text + `</p>`)
		}
	}
	if children && len(b.Children) > 0 {
		out.WriteString(`<div class="public-children">`)
		renderPublicBlocks(out, b.Children, document)
		out.WriteString(`</div>`)
	}
	out.WriteString(`</div>`)
}
func renderPublicTable(out *strings.Builder, raw json.RawMessage) {
	var content struct {
		Rows []struct {
			Cells []json.RawMessage `json:"cells"`
		} `json:"rows"`
		ColumnWidths []float64 `json:"columnWidths"`
		HeaderRows   int       `json:"headerRows"`
		HeaderCols   int       `json:"headerCols"`
	}
	if json.Unmarshal(raw, &content) != nil {
		return
	}
	out.WriteString(`<div class="public-table" tabindex="0" role="region" aria-label="Table"><table>`)
	if len(content.ColumnWidths) > 0 {
		out.WriteString(`<colgroup>`)
		for _, n := range content.ColumnWidths {
			width := ""
			if n > 0 && n <= 16384 && !math.IsInf(n, 0) && !math.IsNaN(n) {
				width = ` style="width:` + strconv.FormatFloat(n, 'f', -1, 64) + `px"`
			}
			out.WriteString(`<col` + width + `>`)
		}
		out.WriteString(`</colgroup>`)
	}
	for i, row := range content.Rows {
		out.WriteString(`<tr>`)
		for j, raw := range row.Cells {
			var cell struct {
				Content json.RawMessage `json:"content"`
				Props   map[string]any  `json:"props"`
			}
			text := ""
			attrs := ""
			if json.Unmarshal(raw, &cell) == nil && len(cell.Content) > 0 {
				text = publicInline(cell.Content)
				attrs = publicAttributes(cell.Props)
			} else {
				text = publicInline(raw)
			}
			tag := "td"
			if i < content.HeaderRows || j < content.HeaderCols {
				tag = "th"
			}
			out.WriteString("<" + tag + attrs + ">" + text + "</" + tag + ">")
		}
		out.WriteString(`</tr>`)
	}
	out.WriteString(`</table></div>`)
}

// Responsive geometry only; colours, highlights, callouts, typefaces and the
// rainbow stay in the original bundled theme.css/styles.css and BlockNote CSS.
const publicBlockLayout = `.public-blocks{padding:0}.public-block{margin:.35em 0;min-width:0;scroll-margin-top:24px}.public-blocks [data-text-alignment="left"]{text-align:left}.public-blocks [data-text-alignment="center"]{text-align:center}.public-blocks [data-text-alignment="right"]{text-align:right}.public-blocks [data-text-alignment="justify"]{text-align:justify}.public-blocks .public-media{display:block;max-width:100%}.public-media figure{display:inline-flex;flex-direction:column;vertical-align:top;margin:0;max-width:100%;width:fit-content}.public-media img,.public-media video{display:block;max-width:100%;height:auto;border-radius:4px}.public-media figcaption{font-size:.8em;line-height:1.5;padding-block:4px;overflow-wrap:anywhere;max-width:100%}.public-media audio{max-width:100%}.public-children{padding-left:1.5em;border-left:1px solid var(--border)}.public-blocks .bn-callout{margin:.6em 0}.public-blocks .bn-callout-emoji{cursor:default}.public-blocks .bn-bookmark{color:inherit;text-decoration:none}.public-blocks .bn-toc-entry{display:block}.public-block[data-text-color] blockquote{color:inherit}.public-doc .cols[data-count="3"]{grid-template-columns:repeat(3,minmax(0,1fr))}.public-blocks [data-background-color]{border-radius:4px;padding:3px 6px}.public-blocks span[data-background-color]{padding:0 2px}.public-blocks .public-table tr:first-child{background:inherit}@media(min-width:901px){.public-view{background-color:var(--ground)!important;background-image:radial-gradient(var(--dot) 1px,transparent 1.25px)!important;background-size:22px 22px!important}.public-doc{position:relative;margin:24px auto;border-radius:16px;background:var(--bg);box-shadow:var(--sheet-drop);min-height:60vh}.public-doc::after{content:"";position:absolute;inset:0;border-radius:inherit;box-shadow:inset 0 0 0 1px var(--sheet-line);pointer-events:none}.public-doc::before{content:"";position:absolute;top:0;left:12%;right:12%;height:1px;background:var(--rainbow);mask-image:linear-gradient(90deg,transparent,#000 25%,#000 75%,transparent);pointer-events:none}}@media(max-width:600px){.public-doc .cols[data-count="3"]{grid-template-columns:1fr}}@media print{.public-view{background-image:none!important}.public-doc{box-shadow:none;margin:0}.public-doc::before,.public-doc::after{display:none}}`
