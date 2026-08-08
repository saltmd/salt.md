package server

import (
	"encoding/json"
	"html"
	"net/url"
	"strings"
)

// safeURL returns s only if it is a benign http(s)/mailto (or scheme-relative)
// link; anything else — notably javascript: and data: — collapses to "#" so an
// attacker-planted block URL cannot execute script in an exported document.
func safeURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return "#"
	}
	switch strings.ToLower(u.Scheme) {
	case "", "http", "https", "mailto":
		return s
	default:
		return "#"
	}
}

// BlockNote JSON → self-contained HTML. Used by the export endpoint's ?format=html
// so a page can be opened in a browser or imported elsewhere with real structure
// (headings, lists, tables) rather than only Markdown.

func inlineHTML(raw json.RawMessage) string {
	var items []mdInline
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, it := range items {
		switch it.Type {
		case "link":
			b.WriteString(`<a href="` + html.EscapeString(it.Href) + `">` + inlineHTML(it.Content) + "</a>")
		case "pageLink":
			label := strProp(it.Props, "label", "Untitled")
			id := strProp(it.Props, "pageId", "")
			b.WriteString(`<a href="/p/` + html.EscapeString(id) + `">` + html.EscapeString(label) + "</a>")
		default:
			t := html.EscapeString(it.Text)
			if t == "" {
				continue
			}
			if truthy(it.Styles["code"]) {
				b.WriteString("<code>" + t + "</code>")
				continue
			}
			if truthy(it.Styles["bold"]) {
				t = "<strong>" + t + "</strong>"
			}
			if truthy(it.Styles["italic"]) {
				t = "<em>" + t + "</em>"
			}
			if truthy(it.Styles["strike"]) {
				t = "<s>" + t + "</s>"
			}
			if truthy(it.Styles["underline"]) {
				t = "<u>" + t + "</u>"
			}
			b.WriteString(t)
		}
	}
	return b.String()
}

func blocksToHTML(content []byte) string {
	var blocks []mdBlock
	if json.Unmarshal(content, &blocks) != nil {
		return ""
	}
	var b strings.Builder
	renderBlocksHTML(&b, blocks)
	return b.String()
}

// renderBlocksHTML walks a block list, grouping consecutive list items of the
// same kind into a single <ul>/<ol>.
func renderBlocksHTML(b *strings.Builder, blocks []mdBlock) {
	i := 0
	for i < len(blocks) {
		blk := blocks[i]
		switch {
		case blk.Type == "bulletListItem" || blk.Type == "checkListItem":
			j := i
			b.WriteString("<ul>")
			for j < len(blocks) && (blocks[j].Type == "bulletListItem" || blocks[j].Type == "checkListItem") {
				renderListItemHTML(b, blocks[j])
				j++
			}
			b.WriteString("</ul>")
			i = j
		case blk.Type == "numberedListItem":
			j := i
			b.WriteString("<ol>")
			for j < len(blocks) && blocks[j].Type == "numberedListItem" {
				renderListItemHTML(b, blocks[j])
				j++
			}
			b.WriteString("</ol>")
			i = j
		default:
			renderBlockHTML(b, blk)
			i++
		}
	}
}

func renderListItemHTML(b *strings.Builder, blk mdBlock) {
	b.WriteString("<li>")
	if blk.Type == "checkListItem" {
		if truthy(blk.Props["checked"]) {
			b.WriteString(`<input type="checkbox" checked disabled> `)
		} else {
			b.WriteString(`<input type="checkbox" disabled> `)
		}
	}
	b.WriteString(inlineHTML(blk.Content))
	if len(blk.Children) > 0 {
		renderBlocksHTML(b, blk.Children)
	}
	b.WriteString("</li>")
}

func renderBlockHTML(b *strings.Builder, blk mdBlock) {
	switch blk.Type {
	case "heading":
		level := intProp(blk.Props, "level", 1)
		if level < 1 || level > 6 {
			level = 1
		}
		tag := "h" + string(rune('0'+level))
		b.WriteString("<" + tag + ">" + inlineHTML(blk.Content) + "</" + tag + ">")
	case "quote":
		b.WriteString("<blockquote>" + inlineHTML(blk.Content) + "</blockquote>")
	case "codeBlock":
		b.WriteString("<pre><code>" + html.EscapeString(plainInline(blk.Content)) + "</code></pre>")
	case "divider":
		b.WriteString("<hr>")
	case "image":
		b.WriteString(`<img src="` + html.EscapeString(safeURL(strProp(blk.Props, "url", ""))) + `" alt="` + html.EscapeString(strProp(blk.Props, "name", "")) + `">`)
	case "video", "audio", "file":
		link := html.EscapeString(safeURL(strProp(blk.Props, "url", "")))
		b.WriteString(`<p><a href="` + link + `">` + html.EscapeString(strProp(blk.Props, "name", blk.Type)) + "</a></p>")
	case "toggleListItem":
		b.WriteString("<details><summary>" + inlineHTML(blk.Content) + "</summary>")
		if len(blk.Children) > 0 {
			renderBlocksHTML(b, blk.Children)
		}
		b.WriteString("</details>")
		return
	case "callout":
		emoji := html.EscapeString(strProp(blk.Props, "emoji", "💡"))
		b.WriteString(`<div style="display:flex;gap:10px;background:rgba(47,125,79,.1);border-radius:10px;padding:12px 14px"><span>` + emoji + "</span><div>" + inlineHTML(blk.Content) + "</div></div>")
	case "bookmark":
		if raw := strProp(blk.Props, "url", ""); raw != "" {
			link := html.EscapeString(safeURL(raw))
			b.WriteString(`<p><a href="` + link + `">🔖 ` + html.EscapeString(raw) + "</a></p>")
		}
	case "database":
		// Only the reference, as in the Markdown export — the rows live in the
		// database page and a copy of them would go stale immediately.
		if id := strProp(blk.Props, "collectionId", ""); id != "" {
			b.WriteString(`<p><a href="/p/` + html.EscapeString(id) + `">▦ Datenbank</a></p>`)
		}
	case "toc":
		// Generated client-side; skip in export.
	case "columnList":
		b.WriteString(`<div style="display:flex;gap:24px;flex-wrap:wrap">`)
		for _, col := range blk.Children {
			b.WriteString(`<div style="flex:1;min-width:200px">`)
			renderBlocksHTML(b, col.Children)
			b.WriteString("</div>")
		}
		b.WriteString("</div>")
		return
	case "table":
		renderTableHTML(b, blk.Content)
	default: // paragraph & unknown
		if t := inlineHTML(blk.Content); t != "" {
			b.WriteString("<p>" + t + "</p>")
		}
	}
	if len(blk.Children) > 0 {
		renderBlocksHTML(b, blk.Children)
	}
}

func renderTableHTML(b *strings.Builder, raw json.RawMessage) {
	var tc struct {
		Rows []struct {
			Cells []json.RawMessage `json:"cells"`
		} `json:"rows"`
	}
	if json.Unmarshal(raw, &tc) != nil || len(tc.Rows) == 0 {
		return
	}
	renderCell := func(c json.RawMessage) string {
		var obj struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(c, &obj); err == nil && len(obj.Content) > 0 {
			return inlineHTML(obj.Content)
		}
		return inlineHTML(c)
	}
	b.WriteString("<table>")
	for _, row := range tc.Rows {
		b.WriteString("<tr>")
		for _, c := range row.Cells {
			b.WriteString("<td>" + renderCell(c) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")
}

// htmlDocStyle is a clean, print-first stylesheet: readable measure, real
// typography and @page rules so "Save as PDF" produces a beautiful document
// with no app chrome. Light-only (a printed page should look like paper).
const htmlDocStyle = `*{box-sizing:border-box}
body{max-width:740px;margin:44px auto;padding:0 24px;font:16px/1.65 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1a1a1a;-webkit-text-size-adjust:100%}
h1{font-size:2.1em;line-height:1.2;margin:0 0 .1em;font-weight:700}
h2{font-size:1.5em;line-height:1.25;margin:1.5em 0 .4em}
h3{font-size:1.2em;margin:1.2em 0 .3em}
h1,h2,h3,h4{break-after:avoid;page-break-after:avoid}
p{margin:.55em 0}
.doc-desc{font-size:1.05em;color:#5b5b57;margin:.15em 0 1.7em}
img{max-width:100%;height:auto;border-radius:6px}
pre{background:#f5f5f4;padding:14px 16px;border-radius:8px;overflow-x:auto;break-inside:avoid;page-break-inside:avoid}
code{background:#f5f5f4;padding:1px 5px;border-radius:4px;font-size:.9em}
pre code{background:none;padding:0}
blockquote{border-left:3px solid #e3e2df;margin:1em 0;padding:.2em 0 .2em 16px;color:#5b5b57}
table{border-collapse:collapse;width:100%;margin:1em 0;break-inside:avoid}
td,th{border:1px solid #e3e2df;padding:7px 11px;text-align:left}
hr{border:none;border-top:1px solid #e3e2df;margin:2em 0}
ul,ol{padding-left:1.4em}li{margin:.25em 0;break-inside:avoid}
a{color:#2f6fb0}
.print-bar{position:sticky;top:0;display:flex;gap:10px;align-items:center;background:#f7f6f3;border:1px solid #e3e2df;border-radius:10px;padding:10px 14px;margin:-16px 0 24px;font-size:14px;color:#5b5b57}
.print-bar button{font:inherit;cursor:pointer;background:#2f7d4f;color:#fff;border:none;border-radius:7px;padding:7px 14px}
@media print{@page{margin:18mm 15mm}body{margin:0 auto;max-width:100%;font-size:11.5pt}a{color:#1a1a1a}h2,h3,img,table,pre,blockquote,li{break-inside:avoid;page-break-inside:avoid}.print-bar{display:none!important}}`

// pageHTML renders a document page as a full standalone HTML document. In print
// mode it shows a small (screen-only) print bar and auto-opens the print dialog
// on load — on mobile, where window.print() is unreliable, the clean page is
// itself the deliverable (share → "Print"/"Save to Files as PDF").
func pageHTML(p *page, printMode bool) string {
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	head := html.EscapeString(title)
	if p.Icon != "" {
		head = html.EscapeString(p.Icon) + " " + head
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title><style>" + htmlDocStyle + "</style></head><body>")
	if printMode {
		b.WriteString(`<div class="print-bar"><button onclick="window.print()">Print / Save as PDF</button><span>On a phone: Share&nbsp;→&nbsp;Print, or "Save to Files".</span></div>`)
	}
	b.WriteString("<h1>" + head + "</h1>")
	if d := strings.TrimSpace(p.Description); d != "" {
		b.WriteString(`<p class="doc-desc">` + html.EscapeString(d) + "</p>")
	}
	b.WriteString(blocksToHTML(p.Content))
	if printMode {
		// Desktop: open the print dialog automatically. Mobile browsers ignore
		// this (no-op), leaving the clean page for the OS share/print sheet.
		b.WriteString(`<script>window.addEventListener('load',function(){setTimeout(function(){try{window.print()}catch(e){}},250)})</script>`)
	}
	b.WriteString("</body></html>")
	return b.String()
}
