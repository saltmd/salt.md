package server

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Markdown → BlockNote block JSON, for API/MCP writers. Covers the common
// subset: headings, lists (incl. nesting by indentation), checklists,
// quotes, fenced code, images, tables, paragraphs, and the inline styles
// bold/italic/strike/code/links.

type mBlock struct {
	Type     string         `json:"type"`
	Props    map[string]any `json:"props,omitempty"`
	Content  any            `json:"content,omitempty"`
	Children []*mBlock      `json:"children,omitempty"`
}

func inlineText(text string, styles map[string]any) map[string]any {
	if styles == nil {
		styles = map[string]any{}
	}
	return map[string]any{"type": "text", "text": text, "styles": styles}
}

var inlinePatterns = []struct {
	re         *regexp.Regexp
	style      string
	underscore bool // needs word-boundary flanking check
}{
	{regexp.MustCompile("^`([^`]+)`"), "code", false},
	{regexp.MustCompile(`^\*\*([^*]+)\*\*`), "bold", false},
	{regexp.MustCompile(`^__([^_]+)__`), "bold", true},
	{regexp.MustCompile(`^\*([^*]+)\*`), "italic", false},
	{regexp.MustCompile(`^_([^_]+)_`), "italic", true},
	{regexp.MustCompile(`^~~([^~]+)~~`), "strike", false},
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

var linkRe = regexp.MustCompile(`^\[([^\]]*)\]\(([^)]+)\)`)

// A link that points at a page of THIS instance becomes a pageLink, not an
// ordinary link (W113).
//
// Why this matters more than it looks: the backlinks index and the graph read
// `pageLink` and nothing else (see links.go). An agent writing
// `[Handbook](/p/abc…)` used to get a plain <a> — it navigated, but the page was
// invisible to "which pages point here", so every structure an agent built was
// an island in the graph. Agents have no other way in: mdToBlocksJSON is the
// only path content takes.
//
// It also closes a round trip. The Markdown export writes a pageLink as
// `[label](/p/id)` (see export.go), so exporting a page and importing it back
// silently downgraded every internal link.
//
// Accepted: a bare `/p/<id>`, or an absolute URL ending in `/p/<id>` — agents
// write the absolute form because that is what share_page hands them. An id is
// 32 hex characters from newID(); anything else stays an ordinary link, which
// is the safe direction to be wrong in.
var pageHrefRe = regexp.MustCompile(`(?:^|/)p/([0-9a-f]{32})/?$`)

// pageLinkHint tells an agent that the conversion above exists. It goes in
// every tool description that takes Markdown, because the schema is the only
// documentation an agent reliably reads — and until this sentence was there,
// the feature was invisible: agents kept writing plain links and wondering why
// get_backlinks came back empty on pages they had just wired together.
const pageLinkHint = `A Markdown link whose target is a page of this instance ` +
	`(` + "`[Handbook](/p/<32-hex-id>)`" + `, or the absolute URL set_sharing ` +
	`returns) becomes a REAL page link: it shows up in get_links and in the ` +
	`graph. Use it whenever you mention another page — a plain link navigates ` +
	`but leaves the page an island.`

// diagramHint is the same idea for diagrams, and it exists for the same reason
// the one above does: a block nobody is told about is a block nobody uses. The
// diagram block was built so that an AGENT could draw — writing "A --> B" is
// something an agent does well, and placing boxes by coordinate is not — and
// agents only ever send Markdown here. Without this sentence the feature would
// have shipped for people with a mouse.
const diagramHint = `A fenced ` + "```mermaid" + ` block becomes a real DIAGRAM, ` +
	`drawn on the page and in its PDF — the same spelling GitHub and Obsidian ` +
	`use. Prefer it over describing a flow in prose; you write the text, salt.md ` +
	`draws it.`

// parseInline converts inline markdown to BlockNote inline content.
func parseInline(md string) []any {
	var out []any
	plain := strings.Builder{}
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, inlineText(plain.String(), nil))
			plain.Reset()
		}
	}
	var prev byte // byte immediately before the current position (0 = start)
	advance := func(n int) {
		prev = md[n-1]
		md = md[n:]
	}
	for len(md) > 0 {
		if m := linkRe.FindStringSubmatch(md); m != nil {
			flush()
			if pm := pageHrefRe.FindStringSubmatch(strings.TrimSpace(m[2])); pm != nil {
				label := strings.TrimSpace(m[1])
				if label == "" {
					label = "Untitled"
				}
				out = append(out, map[string]any{
					"type":  "pageLink",
					"props": map[string]any{"pageId": pm[1], "label": label},
				})
			} else {
				out = append(out, map[string]any{
					"type":    "link",
					"href":    m[2],
					"content": []any{inlineText(m[1], nil)},
				})
			}
			advance(len(m[0]))
			continue
		}
		matched := false
		for _, p := range inlinePatterns {
			m := p.re.FindStringSubmatch(md)
			if m == nil {
				continue
			}
			if p.underscore {
				// Only emphasis when flanked by non-word chars, so intraword
				// underscores in identifiers (my_var_name) stay literal.
				after := byte(0)
				if len(m[0]) < len(md) {
					after = md[len(m[0])]
				}
				if isWordByte(prev) || isWordByte(after) {
					continue
				}
			}
			flush()
			out = append(out, inlineText(m[1], map[string]any{p.style: true}))
			advance(len(m[0]))
			matched = true
			break
		}
		if matched {
			continue
		}
		plain.WriteByte(md[0])
		advance(1)
	}
	flush()
	if out == nil {
		out = []any{}
	}
	return out
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	checkRe    = regexp.MustCompile(`^[-*+]\s+\[([ xX])\]\s+(.*)$`)
	bulletRe   = regexp.MustCompile(`^[-*+]\s+(.*)$`)
	numberedRe = regexp.MustCompile(`^\d+[.)]\s+(.*)$`)
	imageRe    = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)]+)\)\s*$`)
	// Opening fence: 3+ backticks + arbitrary info string (only the first
	// token is used as the language). Closing fence: backticks only.
	fenceOpenRe  = regexp.MustCompile("^(`{3,})\\s*(\\S*).*$")
	fenceCloseRe = regexp.MustCompile("^`{3,}\\s*$")
	tableRowRe   = regexp.MustCompile(`^\|(.+)\|\s*$`)
	tableSepRe   = regexp.MustCompile(`^\|?[\s:|-]+\|?\s*$`)
)

func mdToBlocks(md string) []*mBlock {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var blocks []*mBlock
	// Stack of list items per indent level for nesting.
	listStack := []*mBlock{}

	appendBlock := func(b *mBlock, level int) {
		isList := isListType(b.Type)
		if !isList {
			listStack = listStack[:0]
			blocks = append(blocks, b)
			return
		}
		if level > len(listStack) {
			level = len(listStack)
		}
		listStack = listStack[:level]
		if level == 0 {
			blocks = append(blocks, b)
		} else {
			parent := listStack[level-1]
			parent.Children = append(parent.Children, b)
		}
		listStack = append(listStack, b)
	}

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		indent := 0
		for _, c := range line {
			if c == ' ' {
				indent++
			} else if c == '\t' {
				indent += 2
			} else {
				break
			}
		}
		level := indent / 2

		if trimmed == "" {
			listStack = listStack[:0]
			continue
		}

		if m := fenceOpenRe.FindStringSubmatch(trimmed); m != nil {
			var code []string
			i++
			for i < len(lines) && !fenceCloseRe.MatchString(strings.TrimSpace(lines[i])) {
				code = append(code, lines[i])
				i++
			}
			// ```mermaid becomes a DIAGRAM, not a code block. That convention is
			// how GitHub, GitLab and Obsidian already spell one, so an agent
			// writes it without being told — and being able to write one at all
			// is the entire point of the block. Agents send Markdown here;
			// without this line the feature exists only for people with a mouse.
			//
			// No picture is stored: the server cannot draw. The page renders it
			// the first time somebody opens it, and until then the export prints
			// the source (see the mermaid case in export_html.go).
			if strings.EqualFold(m[2], "mermaid") {
				appendBlock(&mBlock{Type: "mermaid",
					Props: map[string]any{"code": strings.Join(code, "\n"), "svg": ""}}, 0)
				continue
			}
			props := map[string]any{}
			if m[2] != "" {
				props["language"] = m[2]
			}
			appendBlock(&mBlock{Type: "codeBlock", Props: props,
				Content: []any{inlineText(strings.Join(code, "\n"), nil)}}, 0)
			continue
		}
		if m := headingRe.FindStringSubmatch(trimmed); m != nil {
			lvl := len(m[1])
			if lvl > 3 {
				lvl = 3 // BlockNote supports h1–h3
			}
			appendBlock(&mBlock{Type: "heading", Props: map[string]any{"level": lvl},
				Content: parseInline(m[2])}, 0)
			continue
		}
		if m := imageRe.FindStringSubmatch(trimmed); m != nil {
			appendBlock(&mBlock{Type: "image", Props: map[string]any{"url": m[2], "name": m[1]}}, 0)
			continue
		}
		if m := checkRe.FindStringSubmatch(trimmed); m != nil {
			checked := m[1] != " "
			appendBlock(&mBlock{Type: "checkListItem", Props: map[string]any{"checked": checked},
				Content: parseInline(m[2])}, level)
			continue
		}
		if m := bulletRe.FindStringSubmatch(trimmed); m != nil {
			appendBlock(&mBlock{Type: "bulletListItem", Content: parseInline(m[1])}, level)
			continue
		}
		if m := numberedRe.FindStringSubmatch(trimmed); m != nil {
			appendBlock(&mBlock{Type: "numberedListItem", Content: parseInline(m[1])}, level)
			continue
		}
		if strings.HasPrefix(trimmed, "> ") {
			appendBlock(&mBlock{Type: "quote", Content: parseInline(trimmed[2:])}, 0)
			continue
		}
		if tableRowRe.MatchString(trimmed) {
			rows := []map[string]any{}
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				m := tableRowRe.FindStringSubmatch(t)
				if m == nil {
					i--
					break
				}
				if !tableSepRe.MatchString(t) {
					cells := strings.Split(m[1], "|")
					rowCells := make([]any, 0, len(cells))
					for _, c := range cells {
						rowCells = append(rowCells, map[string]any{
							"type":    "tableCell",
							"content": parseInline(strings.TrimSpace(c)),
						})
					}
					rows = append(rows, map[string]any{"cells": rowCells})
				}
				i++
			}
			// A "table" of only separator/alignment rows has no content — emit
			// the original text as a paragraph instead of an empty table.
			if len(rows) == 0 {
				appendBlock(&mBlock{Type: "paragraph", Content: parseInline(trimmed)}, 0)
				continue
			}
			appendBlock(&mBlock{Type: "table",
				Content: map[string]any{"type": "tableContent", "rows": rows}}, 0)
			continue
		}
		appendBlock(&mBlock{Type: "paragraph", Content: parseInline(trimmed)}, 0)
	}
	return blocks
}

// mdToBlocksJSON returns the blocks as a JSON array string.
func mdToBlocksJSON(md string) (string, error) {
	blocks := mdToBlocks(md)
	if blocks == nil {
		blocks = []*mBlock{}
	}
	b, err := json.Marshal(blocks)
	return string(b), err
}

// appendMarkdownToPage appends converted markdown blocks to a page's
// content, refreshes the index, and resets any live CRDT doc so open
// editors reload the new content. The read-modify-write runs in a
// transaction so concurrent appends can't clobber each other.
func (s *Server) appendMarkdownToPage(pageID, md string) error {
	blocks := mdToBlocks(md)
	if len(blocks) == 0 {
		return nil // nothing to append; don't disturb open editors
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var content string
	if err := tx.QueryRow(`SELECT content FROM pages WHERE id = ?`, pageID).Scan(&content); err != nil {
		return err
	}
	var existing []json.RawMessage
	if err := json.Unmarshal([]byte(content), &existing); err != nil {
		existing = []json.RawMessage{}
	}
	for _, b := range blocks {
		raw, err := json.Marshal(b)
		if err != nil {
			return err
		}
		existing = append(existing, raw)
	}
	merged, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE pages SET content = ?, updated_at = ? WHERE id = ?`, string(merged), now(), pageID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.reindexPage(pageID); err != nil {
		return err
	}
	// Record a version-history snapshot of the appended state (throttled).
	s.snapshotRevision(pageID, "", "agent") // Autor unbekannt -> get_page_history meldet "unknown"
	// Discard the live CRDT doc so open editors reload from the new content
	// (they re-seed via isNew). This is inherently lossy for edits made in the
	// last debounce window before the append — documented as an append-vs-live
	// edit conflict.
	s.resetYjsDoc(pageID)
	return nil
}
