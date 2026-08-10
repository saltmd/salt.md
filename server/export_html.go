package server

import (
	"encoding/json"
	"html"
	"net/url"
	"strings"
	"time"
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

// printOptions is the house style for a printed document: what appears besides
// the text itself. Everything here is off-by-default-safe — an option nobody
// touches produces the plain document people expected before any of this
// existed.
type printOptions struct {
	Cover     bool   // a title page of its own
	Icon      bool   // the document's emoji beside its title
	Footer    bool   // title and date repeated at the foot of every page
	Workspace bool   // which workspace and instance this came from
	PageNums  bool   // see the comment on the stylesheet — this one has a cost
	WSName    string // filled in only when Workspace is on
	Instance  string
	Date      string
}

// printOptionsFor reads the instance's defaults. They are settings rather than
// per-export choices because they describe how documents from THIS instance
// look — a house style, not a decision somebody makes forty times.
func (s *Server) printOptionsFor(p *page) printOptions {
	o := printOptions{
		Cover:     s.boolSetting("pdf_cover"),
		Icon:      s.setting("pdf_icon", "1") == "1",
		Footer:    s.setting("pdf_footer", "1") == "1",
		Workspace: s.setting("pdf_workspace", "1") == "1",
		PageNums:  s.boolSetting("pdf_pagenums"),
		Instance:  s.setting("instance_name", ""),
		Date:      time.Now().UTC().Format("2006-01-02"),
	}
	if o.Workspace && p.WorkspaceID != "" {
		s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, p.WorkspaceID).Scan(&o.WSName)
	}
	return o
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
.cover{display:none}
.page-frame{border-collapse:collapse;width:100%}
.page-frame>thead>tr>td,.page-frame>tbody>tr>td,.page-frame>tfoot>tr>td{border:none;padding:0}
.doc-foot{display:none}

/* Printing, and the one rule everything else hangs off: @page has NO margin.
   Chrome draws its own header and footer INTO the page margin — the date and
   title at the top, the URL and page number at the bottom — and a page cannot
   turn that off. Take the margin away and there is nowhere for them to go.
 
   That alone would ruin the document: with no page margin, page two starts at
   the paper edge and printers cannot print there. So the whole document sits in
   a table, and a table's thead and tfoot REPEAT on every printed page with
   their space reserved. That is what buys a real top and bottom margin on every
   sheet, and a footer of our own along with it.
 
   The archaic-looking table is therefore load-bearing. CSS has a proper answer
   for this — @page margin boxes — and no browser implements it. */
@media print{
@page{margin:0}
body{margin:0;max-width:100%;padding:0;font-size:11.5pt}
.page-frame>thead>tr>td{height:14mm}
.page-frame>tfoot>tr>td{height:14mm}
.page-body{padding:0 15mm}
a{color:#1a1a1a}
h2,h3,img,table,pre,blockquote,li{break-inside:avoid;page-break-inside:avoid}
.print-bar{display:none!important}
.doc-foot{display:block;padding:0 15mm;font-size:8.5pt;color:#8a8a85;border-top:1px solid #e3e2df;margin:0 15mm;padding:3mm 0 0}
.cover{display:flex;flex-direction:column;justify-content:center;height:247mm;padding:25mm 15mm;break-after:page;page-break-after:always}
.cover h1{font-size:2.6em;margin:0 0 .3em}
.cover .cover-meta{font-size:1em;color:#5b5b57;line-height:1.9}
.cover .cover-rule{width:56px;border-top:3px solid #2f7d4f;margin:0 0 1.4em}
.no-icon .doc-icon{display:none}
/* Page numbers are the browser's own, and it only draws them when there IS a
   margin to draw them in — which brings its header and the URL back with them.
   Offered honestly rather than silently: the label says what it costs. */
}`

// pageNumStyle is the opposite choice, and it is a trade rather than an option:
// giving the margin back lets the browser number the pages, and the browser
// puts the address and the date up there at the same time. Our own frame steps
// aside so the two do not stack.
const pageNumStyle = `@media print{
@page{margin:16mm 15mm}
.page-frame>thead>tr>td,.page-frame>tfoot>tr>td{height:0}
.page-body{padding:0}
.doc-foot{display:none}
.cover{padding:0}
}`

// pageHTML renders a document page as a full standalone HTML document. In print
// mode it shows a small (screen-only) print bar and auto-opens the print dialog
// on load — on mobile, where window.print() is unreliable, the clean page is
// itself the deliverable (share → "Print"/"Save to Files as PDF").
func pageHTML(p *page, printMode bool, o printOptions) string {
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	esc := html.EscapeString
	icon := ""
	if p.Icon != "" && o.Icon {
		icon = `<span class="doc-icon">` + esc(p.Icon) + `</span> `
	}

	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>")
	b.WriteString(esc(title))
	b.WriteString("</title><style>" + htmlDocStyle + "</style>")
	if printMode && o.PageNums {
		b.WriteString("<style>" + pageNumStyle + "</style>")
	}
	b.WriteString("</head><body>")
	if printMode {
		b.WriteString(printBarHTML(o))
	}

	// The cover sits OUTSIDE the frame, and that is not tidiness: a forced page
	// break inside the table stopped Chrome repeating the running footer on
	// every following sheet. Proven on a rendered PDF — the footer was on every
	// page without a cover and on none with one.
	if printMode && o.Cover {
		b.WriteString(`<section class="cover"><div class="cover-rule"></div><h1>` + icon + esc(title) + "</h1>")
		if d := strings.TrimSpace(p.Description); d != "" {
			b.WriteString(`<p class="doc-desc">` + esc(d) + "</p>")
		}
		b.WriteString(`<div class="cover-meta">`)
		for _, line := range coverLines(o) {
			b.WriteString(esc(line) + "<br>")
		}
		b.WriteString("</div></section>")
	}

	// Everything else goes inside the frame — see the stylesheet for why a
	// table, of all things, is what gives every sheet its margins back.
	b.WriteString(`<table class="page-frame"><thead><tr><td></td></tr></thead>`)
	if printMode && o.Footer {
		b.WriteString(`<tfoot><tr><td><div class="doc-foot">` + esc(title))
		if o.Date != "" {
			b.WriteString(" &middot; " + esc(o.Date))
		}
		b.WriteString(`</div></td></tr></tfoot>`)
	} else {
		b.WriteString(`<tfoot><tr><td></td></tr></tfoot>`)
	}
	b.WriteString(`<tbody><tr><td><div class="page-body">`)

	b.WriteString("<h1>" + icon + esc(title) + "</h1>")
	// On a cover the description has already been said; repeating it under the
	// heading reads like a mistake.
	if d := strings.TrimSpace(p.Description); d != "" && !(printMode && o.Cover) {
		b.WriteString(`<p class="doc-desc">` + esc(d) + "</p>")
	}
	b.WriteString(blocksToHTML(p.Content))
	b.WriteString(`</div></td></tr></tbody></table>`)

	if printMode {
		// Desktop: open the print dialog automatically. Mobile browsers ignore
		// this (no-op), leaving the clean page for the OS share/print sheet.
		b.WriteString(`<script>window.addEventListener('load',function(){setTimeout(function(){try{window.print()}catch(e){}},250)})</script>`)
	}
	b.WriteString("</body></html>")
	return b.String()
}

// coverLines is what the title page says about where the document came from.
// Empty entries are dropped rather than printed as blank lines, so an instance
// that has set no name simply shows one line fewer.
func coverLines(o printOptions) []string {
	out := []string{}
	if o.Workspace {
		if o.WSName != "" {
			out = append(out, o.WSName)
		}
		if o.Instance != "" && o.Instance != o.WSName {
			out = append(out, o.Instance)
		}
	}
	if o.Date != "" {
		out = append(out, o.Date)
	}
	return out
}

// printBarHTML is the strip above the document, on screen only. The toggles
// reload with a query parameter rather than flipping a class, because one of
// them — page numbers — changes an @page rule, and an at-rule cannot be scoped
// to a class. Doing all five the same way keeps the URL an honest description
// of what will come out of the printer, which also makes it worth bookmarking.
func printBarHTML(o printOptions) string {
	box := func(param, label string, on bool) string {
		checked := ""
		if on {
			checked = " checked"
		}
		return `<label><input type="checkbox" data-opt="` + param + `"` + checked + `> ` + html.EscapeString(label) + `</label>`
	}
	return `<div class="print-bar"><button onclick="window.print()">Print / Save as PDF</button>` +
		box("cover", "Title page", o.Cover) +
		box("icon", "Icon", o.Icon) +
		box("foot", "Footer", o.Footer) +
		box("ws", "Workspace", o.Workspace) +
		box("nums", "Page numbers (the browser also prints the address)", o.PageNums) +
		`<script>document.querySelectorAll('.print-bar input[data-opt]').forEach(function(c){
c.addEventListener('change',function(){
var u=new URL(location.href);u.searchParams.set(c.dataset.opt,c.checked?'1':'0');location.href=u.toString();});});</script>` +
		`</div>`
}

// applyPrintQuery lets a link, a bookmark or the bar above the document deviate
// from the instance's house style. Absent parameters keep the default: this is
// an override, not a form, so an old link keeps meaning what it meant.
func applyPrintQuery(o printOptions, q url.Values) printOptions {
	set := func(name string, target *bool) {
		switch q.Get(name) {
		case "1":
			*target = true
		case "0":
			*target = false
		}
	}
	set("cover", &o.Cover)
	set("icon", &o.Icon)
	set("foot", &o.Footer)
	set("ws", &o.Workspace)
	set("nums", &o.PageNums)
	return o
}
