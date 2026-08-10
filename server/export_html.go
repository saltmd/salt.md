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
	Cover     bool // a title page of its own
	Icon      bool // the document's emoji beside its title
	Footer    bool // title and date at the foot of every page
	Workspace bool // which workspace and instance this came from
	PageNums  bool // page numbers, counted by us
	Comments  bool // the page's comments, after the document
	Links     bool // links as links, rather than flattened to plain text

	WSName   string
	Instance string
	Date     string
	Region   string
	Language string
	CommentL []commentJSON
}

// printOptionsFor reads the instance's defaults. They are settings rather than
// per-export choices because they describe how documents from THIS instance
// look — a house style, not a decision somebody makes forty times. The bar
// beside a print view can still deviate for one document.
func (s *Server) printOptionsFor(p *page) printOptions {
	o := printOptions{
		Cover:     s.boolSetting("pdf_cover"),
		Icon:      s.setting("pdf_icon", "1") == "1",
		Footer:    s.setting("pdf_footer", "1") == "1",
		Workspace: s.setting("pdf_workspace", "1") == "1",
		PageNums:  s.setting("pdf_pagenums", "1") == "1",
		Comments:  s.boolSetting("pdf_comments"),
		Links:     s.setting("pdf_links", "1") == "1",
		Instance:  s.setting("instance_name", ""),
		Date:      time.Now().UTC().Format("2006-01-02"),
	}
	if p.WorkspaceID != "" {
		s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, p.WorkspaceID).Scan(&o.WSName)
	}
	// Fetched whatever the default says, because the bar can switch comments on
	// without going back to the server — and a toggle that needs a round trip
	// reopens the print dialog, which is the thing being fixed here.
	if list, err := s.pageComments(p.ID); err == nil {
		o.CommentL = list
	}
	return o
}

// classList turns the options into classes on <body>. Every one of them is a
// pure display choice, which is what lets the bar beside the document flip them
// live: no reload, so no print dialog springing open on every click.
func (o printOptions) classList() string {
	on := func(b bool, name string) string {
		if b {
			return " " + name
		}
		return ""
	}
	return "doc" +
		on(o.Cover, "opt-cover") + on(o.Icon, "opt-icon") + on(o.Footer, "opt-foot") +
		on(o.Workspace, "opt-ws") + on(o.PageNums, "opt-nums") +
		on(o.Comments, "opt-comments") + on(o.Links, "opt-links")
}

const htmlDocStyle = `*{box-sizing:border-box}
body{margin:0;font:16px/1.65 -apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#1f1f1d;background:#f0efec;-webkit-print-color-adjust:exact;print-color-adjust:exact}
h1{font-size:2.1em;line-height:1.2;margin:0 0 .1em;font-weight:700}
h2{font-size:1.5em;line-height:1.25;margin:1.5em 0 .4em}
h3{font-size:1.2em;margin:1.2em 0 .3em}
p{margin:.55em 0}
.doc-desc{font-size:1.05em;color:#5b5b57;margin:.15em 0 1.7em}
img{max-width:100%;height:auto;border-radius:6px}
pre{background:#f5f5f4;padding:14px 16px;border-radius:8px;overflow-x:auto}
code{background:#f5f5f4;padding:1px 5px;border-radius:4px;font-size:.9em}
pre code{background:none;padding:0}
blockquote{border-left:3px solid #e3e2df;margin:1em 0;padding:.2em 0 .2em 16px;color:#5b5b57}
table{border-collapse:collapse;width:100%;margin:1em 0}
td,th{border:1px solid #e3e2df;padding:7px 11px;text-align:left}
hr{border:none;border-top:1px solid #e3e2df;margin:2em 0}
ul,ol{padding-left:1.4em}li{margin:.25em 0}
a{color:#2f6fb0}
body:not(.opt-links) a{color:inherit;text-decoration:none}
body:not(.opt-icon) .doc-icon{display:none}
/* All three kinds sit on the text baseline at the size of the heading they
   belong to, so a document with an emoji and one with a drawn icon look like
   the same document. */
.doc-icon-svg,.doc-icon-img{display:inline-block;width:1em;height:1em;vertical-align:-0.12em}
.doc-icon-img{border-radius:.15em;object-fit:contain}
body:not(.opt-comments) .doc-comments{display:none}
body:not(.opt-ws) .cover-ws{display:none}
.doc-comments{margin-top:2.2em;border-top:1px solid #e3e2df;padding-top:1em}
.doc-comments h2{font-size:1.2em;margin:0 0 .6em}
.doc-comment{margin:0 0 .8em;font-size:.95em}
.doc-comment .who{font-weight:600}
.doc-comment .when{color:#8a8a85;font-size:.85em;margin-left:.4em}

/* A sheet is a real sheet of paper: fixed size, its own margins, its own foot.
   The document is cut into these by the script below rather than left to the
   browser, and that is what makes a page NUMBER possible at all. Nothing in a
   web page can count printed pages — the CSS feature meant for it exists on
   paper and no browser implements it. Cutting the pages ourselves means we
   already know how many there are, and which one we are on. */
/* 296mm, not 297. A box exactly as tall as the paper plus a forced break after
   it lands the break at the very start of the next sheet, and the browser then
   advances one MORE — every sheet came out followed by a blank one. One
   millimetre of slack is the whole fix.
 
   And no overflow:hidden. A block taller than any sheet has to run on and be
   ugly; clipping it would lose the end of a document silently, which is the one
   failure nobody would notice until a customer did. */
.sheet{width:210mm;height:296mm;padding:16mm 15mm 20mm;position:relative;background:#fff}
.sheet-body{height:260mm}
.sheet-foot{position:absolute;left:15mm;right:15mm;bottom:10mm;display:flex;justify-content:space-between;gap:12px;font-size:8.5pt;color:#8a8a85;border-top:1px solid #e3e2df;padding-top:2.5mm}
body:not(.opt-foot) .foot-title{visibility:hidden}
body:not(.opt-nums) .foot-num{visibility:hidden}
/* A title page carries no number and no running foot. Nobody numbers a cover. */
.sheet-cover .sheet-foot{display:none}
.sheet-cover .sheet-body{display:flex;flex-direction:column;justify-content:center}
.sheet-cover h1{font-size:2.6em;margin:0 0 .3em}
.cover-rule{width:56px;border-top:3px solid #2f7d4f;margin:0 0 1.4em}
.cover-meta{font-size:1em;color:#5b5b57;line-height:1.9}

/* On screen the sheets lie on a desk, so what you see is what comes out. */
@media screen{
.sheets{padding:26px 0 44px}
.sheet{margin:0 auto 20px;box-shadow:0 1px 3px rgba(0,0,0,.12),0 8px 24px rgba(0,0,0,.08);border-radius:2px}
.doc-side{position:fixed;top:20px;right:20px;width:252px;background:#fff;border:1px solid #e3e2df;border-radius:12px;padding:14px 16px;box-shadow:0 6px 20px rgba(0,0,0,.1);font-size:14px;z-index:10}
.doc-side h3{margin:0 0 8px;font-size:12px;text-transform:uppercase;letter-spacing:.05em;color:#8a8a85;font-weight:600}
.doc-side label{display:flex;gap:9px;align-items:flex-start;padding:5px 0;cursor:pointer;line-height:1.35}
.doc-side input{margin-top:3px}
.doc-side .go{width:100%;margin-top:12px;font:inherit;font-weight:600;cursor:pointer;background:#2f7d4f;color:#fff;border:none;border-radius:8px;padding:9px 14px}
.doc-side .dep-cover{opacity:.4}
body.opt-cover .doc-side .dep-cover{opacity:1}
.doc-side .note{margin:9px 0 0;font-size:12px;color:#8a8a85;line-height:1.45}
@media (max-width:1180px){.doc-side{position:static;width:auto;margin:16px auto 0;max-width:210mm;box-shadow:none}}
}

/* Printing. @page has NO margin, because Chrome draws its own header and footer
   INTO the margin — date and title above, address and page number below — and a
   page cannot switch that off. With no margin there is nowhere for them to go,
   and every margin you see is the sheet's own padding instead. */
@media print{
@page{margin:0;size:A4}
html,body{background:#fff}
.doc-side{display:none!important}
.sheets{padding:0}
.sheet{margin:0;box-shadow:none;border-radius:0;break-after:page;page-break-after:always}
.sheet:last-child{break-after:auto;page-break-after:auto}
a{color:#1a1a1a}
}`

// pageHTML renders a document page as a full standalone HTML document.
//
// In print mode it does NOT hand the document to the browser and hope. It hands
// over finished sheets: the script cuts the flow into A4 pages, and the browser
// only puts them on paper. That is what a page number needs — and the reason
// the print dialog no longer springs open on load, because every option is now
// a class on <body> and changes nothing that needs the server.
func (srv *Server) pageHTML(p *page, printMode bool, o printOptions) string {
	title := p.Title
	if title == "" {
		title = "Untitled"
	}
	esc := html.EscapeString
	icon := ""
	if h := srv.iconHTML(p.Icon); h != "" {
		icon = h + " "
	}

	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>`)
	b.WriteString(esc(title))
	b.WriteString("</title><style>" + htmlDocStyle + "</style></head>")
	b.WriteString(`<body class="` + o.classList() + `">`)

	if printMode {
		b.WriteString(sidePanelHTML())
	}

	// The document, as one flow. The script takes it from here; without the
	// script (or before it runs) this is still the whole document, readable and
	// printable — just with the browser choosing where the pages fall.
	b.WriteString(`<template id="doc-src">`)
	b.WriteString("<h1>" + icon + esc(title) + "</h1>")
	if d := strings.TrimSpace(p.Description); d != "" {
		b.WriteString(`<p class="doc-desc">` + esc(d) + "</p>")
	}
	b.WriteString(blocksToHTML(p.Content))
	if len(o.CommentL) > 0 {
		b.WriteString(`<section class="doc-comments"><h2>Comments</h2>`)
		for _, c := range o.CommentL {
			b.WriteString(`<p class="doc-comment"><span class="who">` + esc(c.AuthorName) + `</span>`)
			if len(c.CreatedAt) >= 10 {
				b.WriteString(`<span class="when">` + esc(c.CreatedAt[:10]) + `</span>`)
			}
			b.WriteString("<br>" + esc(c.Body) + "</p>")
		}
		b.WriteString("</section>")
	}
	b.WriteString(`</template>`)

	// The cover, kept apart so paginating never has to reason about it.
	b.WriteString(`<template id="doc-cover"><div class="cover-rule"></div><h1>` + icon + esc(title) + "</h1>")
	if d := strings.TrimSpace(p.Description); d != "" {
		b.WriteString(`<p class="doc-desc">` + esc(d) + "</p>")
	}
	// Both parts are always rendered and hidden by class, never left out here.
	// Rendered conditionally, the panel beside the document could not switch
	// them back on without asking the server — and that round trip is exactly
	// what reopens the print dialog on every click.
	b.WriteString(`<div class="cover-meta"><span class="cover-ws">`)
	for _, line := range originLines(o) {
		b.WriteString(esc(line) + "<br>")
	}
	b.WriteString(`</span><span class="cover-day"></span></div></template>`)

	b.WriteString(`<div class="sheets" id="sheets"></div>`)
	// The date travels as a plain calendar day and is written out in the
	// browser, in the reader's own regional format — the same rule the rest of
	// the product follows. Formatting it here would have shipped one spelling to
	// everybody, and it would have been ISO, which no reader writes by hand.
	b.WriteString(`<script>` + paginateJS + "\n" +
		`SALT_TITLE=` + jsString(title) + `;` +
		`SALT_DAY=` + jsString(o.Date) + `;` +
		`SALT_REGION=` + jsString(o.Region) + `;` +
		`SALT_LANG=` + jsString(o.Language) + `;` +
		`saltStart();</script>`)
	b.WriteString("</body></html>")
	return b.String()
}

func jsString(v string) string {
	out, _ := json.Marshal(v)
	return string(out)
}

// originLines is where the document came from — the workspace and the instance,
// which is the part the "Workspace and instance" switch hides. The date is not
// in here: it belongs to the document, not to the place it lives, and it stays
// on the title page either way.
//
// Empty entries are dropped rather than printed as blank lines, so an instance
// that has set no name simply shows one line fewer.
func originLines(o printOptions) []string {
	out := []string{}
	if o.WSName != "" {
		out = append(out, o.WSName)
	}
	if o.Instance != "" && o.Instance != o.WSName {
		out = append(out, o.Instance)
	}
	return out
}

// sidePanelHTML is the strip of choices beside the document, on screen only.
// Beside and not above: it stays put while you scroll through the sheets, and
// the document underneath keeps the exact width it will have on paper.
func sidePanelHTML() string {
	box := func(cls, label string) string {
		return `<label><input type="checkbox" data-cls="` + cls + `"> ` + html.EscapeString(label) + `</label>`
	}
	return `<aside class="doc-side"><h3>Page setup</h3>` +
		box("opt-cover", "Title page") +
		box("opt-icon", "Icon") +
		box("opt-foot", "Title and date at the foot") +
		box("opt-nums", "Page numbers") +
		// Only the title page has room for it, so with no title page the switch
		// does nothing — and a switch that does nothing without saying why is
		// the thing people report as broken. It dims instead.
		`<label class="dep-cover" title="Shown on the title page"><input type="checkbox" data-cls="opt-ws"> Workspace and instance</label>` +
		box("opt-comments", "Comments") +
		box("opt-links", "Links as links") +
		`<button class="go" onclick="window.print()">Print / Save as PDF</button>` +
		`<p class="note">What you see is what comes out. Nothing is sent anywhere.</p></aside>`
}

// applyPrintQuery lets a link or a bookmark deviate
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
	set("comments", &o.Comments)
	set("links", &o.Links)
	return o
}

// paginateJS cuts the document into sheets, and it is the piece that makes a
// page number possible: a browser will not tell a page how many sheets it will
// use, so the page decides instead. Every sheet is an explicit box of exactly
// one A4, forced to break after itself, which leaves the browser nothing to
// disagree about.
//
// The measurement is honest rather than clever: put a block on the sheet, ask
// whether the sheet now overflows, and if it does move it to the next one. A
// table too tall for any sheet is cut between its ROWS, because a table is what
// most documents that get printed are made of and the alternative is a page and
// a half of white space.
const paginateJS = `
var SALT_TITLE='',SALT_DAY='',SALT_REGION='',SALT_LANG='';

// saltLocale is the same rule i18n.ts follows: the account's region wins, and
// with none, the browser tag whose base matches the interface language — so a
// German interface in an English browser still writes 10.08.2026 rather than
// 08/10/2026. Falling back to the browser's own default instead was wrong in
// exactly that case, which is the common one for anybody who keeps their
// browser in English.
function saltLocale(){
  if(SALT_REGION)return SALT_REGION;
  var tags=navigator.languages||[navigator.language||''];
  if(SALT_LANG){
    for(var i=0;i<tags.length;i++)if(tags[i].split('-')[0]===SALT_LANG)return tags[i];
    return SALT_LANG;
  }
  return tags[0]||undefined;
}

// saltDay writes a calendar day out the way the reader writes one. Same rule as
// format.ts, and the same trap avoided: new Date('2026-08-10') is UTC midnight,
// which renders as the 9th west of Greenwich. Built from the parts instead, so
// the day asked for is the day shown, everywhere on earth.
function saltDay(iso){
  if(!iso)return '';
  var m=/^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if(!m)return iso;
  var d=new Date(+m[1],+m[2]-1,+m[3]);
  try{
    return new Intl.DateTimeFormat(saltLocale()||undefined,
      {year:'numeric',month:'2-digit',day:'2-digit'}).format(d);
  }catch(e){return iso;}
}

function saltStart(){
  var day=saltDay(SALT_DAY);
  SALT_FOOT=day?SALT_TITLE+' \u00b7 '+day:SALT_TITLE;
  var c=document.querySelector('#doc-cover');
  if(c){
    var slot=c.content.querySelector('.cover-day');
    if(slot)slot.textContent=day;
  }
  saltPaginate();
}
var SALT_FOOT='';
function saltPaginate(){
  var src=document.getElementById('doc-src'),cov=document.getElementById('doc-cover');
  var host=document.getElementById('sheets');
  if(!src||!host)return;
  host.textContent='';
  var body=document.body,withCover=body.classList.contains('opt-cover');

  function sheet(cls){
    var s=document.createElement('section');
    s.className='sheet'+(cls?' '+cls:'');
    var b=document.createElement('div');b.className='sheet-body';s.appendChild(b);
    var f=document.createElement('div');f.className='sheet-foot';
    f.innerHTML='<span class="foot-title"></span><span class="foot-num"></span>';
    f.firstChild.textContent=SALT_FOOT;
    s.appendChild(f);host.appendChild(s);return b;
  }
  function over(b){return b.scrollHeight>b.clientHeight+1;}

  if(withCover){
    var cb=sheet('sheet-cover');
    cb.appendChild(cov.content.cloneNode(true));
  }

  var cur=sheet(''),flow=src.content.cloneNode(true);
  var items=[].slice.call(flow.children);
  for(var i=0;i<items.length;i++){
    var el=items[i];
    cur.appendChild(el);
    if(!over(cur))continue;
    // It does not fit. If the sheet already held something, the block starts a
    // fresh one; if it was alone, the block is simply taller than any sheet.
    if(cur.children.length>1){
      cur.removeChild(el);
      // A heading directly above takes the move with it. A section title alone
      // at the foot of a sheet, with its table overleaf, reads like a mistake —
      // and it is the commonest thing a document made of tables does.
      var prev=cur.lastElementChild,carry=null;
      if(prev&&/^H[1-4]$/.test(prev.tagName)&&cur.children.length>1){
        carry=prev;cur.removeChild(prev);
      }
      cur=sheet('');
      if(carry)cur.appendChild(carry);
      cur.appendChild(el);
      if(!over(cur))continue;
    }
    if(el.tagName==='TABLE'){
      var rest=el;
      while(over(cur)){
        var moved=saltCutTable(rest,cur);
        if(!moved)break;
        cur=sheet('');
        cur.appendChild(moved);
        rest=moved;
      }
    }
    // Anything else that is taller than a sheet stays where it is and runs on.
    // Forcing it would only hide the end of it.
  }
  saltNumber();
}

// saltCutTable moves rows off the end of a table until it fits, and returns the
// clone that carries them. The head row goes along, so a table continued on the
// next sheet still says what its columns are.
function saltCutTable(tbl,body){
  var rows=tbl.querySelectorAll('tr');
  if(rows.length<2)return null;
  var moved=[];
  while(body.scrollHeight>body.clientHeight+1){
    rows=tbl.querySelectorAll('tr');
    if(rows.length<2)break;
    var last=rows[rows.length-1];
    last.parentNode.removeChild(last);
    moved.unshift(last);
  }
  if(!moved.length)return null;
  var clone=tbl.cloneNode(false);
  var tb=document.createElement('tbody');
  for(var i=0;i<moved.length;i++)tb.appendChild(moved[i]);
  clone.appendChild(tb);
  return clone;
}

// The cover is not page one. Nobody numbers a title page, and starting the
// count after it is what makes "2 / 5" mean the second page of the document.
function saltNumber(){
  var sheets=[].slice.call(document.querySelectorAll('.sheet:not(.sheet-cover)'));
  for(var i=0;i<sheets.length;i++){
    var n=sheets[i].querySelector('.foot-num');
    if(n)n.textContent=(i+1)+' / '+sheets.length;
  }
}

document.addEventListener('change',function(e){
  var t=e.target;
  if(!t||!t.dataset||!t.dataset.cls)return;
  document.body.classList.toggle(t.dataset.cls,t.checked);
  saltPaginate();
});
document.addEventListener('DOMContentLoaded',function(){
  document.querySelectorAll('.doc-side input[data-cls]').forEach(function(c){
    c.checked=document.body.classList.contains(c.dataset.cls);
  });
});
`

// iconHTML draws a page icon in an exported document. A page icon is one of
// four things in a single string, and until this existed the export printed
// whichever one it was as TEXT — so a document came off the printer with
// "lucide:Rocket" or "/files/8c94….svg" where its icon belonged. Only emoji
// happened to work, because an emoji IS its own text.
//
//   emoji            "🚀"                     text, sized by CSS
//   Lucide           "lucide:Rocket[:#hex]"   inlined from the generated set
//   MDI              "mdi:Rocket[:#hex]"      nothing — see below
//   uploaded image   "/files/abc.png"         an <img>
//
// MDI is left out on purpose rather than guessed at: the app loads those paths
// from a lazy browser chunk that the server has no copy of. Nothing is better
// than the wrong glyph, and better than the literal word "mdi:Rocket".
func (s *Server) iconHTML(icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return ""
	}
	if strings.HasPrefix(icon, "/") || strings.HasPrefix(icon, "http") || strings.HasPrefix(icon, "data:") {
		return `<img class="doc-icon doc-icon-img" src="` + html.EscapeString(icon) + `" alt="">`
	}
	if strings.HasPrefix(icon, "lucide:") {
		name, colour := iconRef(icon)
		inner, ok := s.lucide[name]
		if !ok {
			return ""
		}
		if colour == "" {
			colour = "currentColor"
		}
		// The generated markup is ours, from a fixed set, so it goes in as it
		// is. The COLOUR comes from the page and is checked before it is used.
		return `<svg class="doc-icon doc-icon-svg" viewBox="0 0 24 24" fill="none" stroke="` +
			html.EscapeString(colour) + `" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
			inner + `</svg>`
	}
	if strings.HasPrefix(icon, "mdi:") {
		return ""
	}
	return `<span class="doc-icon">` + html.EscapeString(icon) + `</span>`
}

// iconRef splits "lucide:Rocket:#e03131" into its name and colour. A trailing
// ":fill" from the old filled variants is ignored, exactly as the app ignores
// it, so an icon picked back then still resolves instead of vanishing.
//
// The colour is only accepted as a plain hex value. It reaches a stroke
// attribute, and a value from a page is not something to hand to a stylesheet
// unchecked.
func iconRef(ref string) (name, colour string) {
	parts := strings.Split(ref, ":")
	if len(parts) > 1 {
		name = parts[1]
	}
	if len(parts) > 2 && parts[2] != "fill" && hexColour(parts[2]) {
		colour = parts[2]
	}
	return name, colour
}

func hexColour(v string) bool {
	if len(v) != 4 && len(v) != 7 || v[0] != '#' {
		return false
	}
	for _, c := range v[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
