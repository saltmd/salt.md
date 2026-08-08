package server

import (
	"encoding/json"
	"strings"
)

// Note-list metadata: every page carries a short plain-text snippet and the URL
// of its first image so the notes list (Bear-style middle column) can render
// preview cards without loading full page content. Both are derived from the
// Blocks-JSON on every save (reindexPage) and backfilled once at startup.

const snippetMax = 240 // runes

// extractSnippetAndThumb walks the BlockNote JSON in DOCUMENT ORDER (unlike
// extractText, whose map iteration is fine for FTS but scrambles order) and
// returns the leading plain text plus the first image URL.
func extractSnippetAndThumb(raw []byte) (snippet, thumb string) {
	var blocks []any
	if json.Unmarshal(raw, &blocks) != nil {
		return "", ""
	}
	var b strings.Builder
	walkBlocksOrdered(blocks, &b, &thumb)
	s := strings.Join(strings.Fields(b.String()), " ")
	if r := []rune(s); len(r) > snippetMax {
		s = string(r[:snippetMax])
	}
	return s, thumb
}

func walkBlocksOrdered(blocks []any, b *strings.Builder, thumb *string) {
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := block["type"].(string); t == "image" && *thumb == "" {
			if props, ok := block["props"].(map[string]any); ok {
				if url, ok := props["url"].(string); ok {
					*thumb = url
				}
			}
		}
		// Inline content in order: plain text runs + page-mention labels.
		if content, ok := block["content"].([]any); ok {
			collectInlineText(content, b)
		}
		// Table content nests one level deeper.
		if content, ok := block["content"].(map[string]any); ok {
			if rows, ok := content["rows"].([]any); ok {
				collectInlineText(rows, b)
			}
		}
		if children, ok := block["children"].([]any); ok {
			walkBlocksOrdered(children, b, thumb)
		}
	}
}

func collectInlineText(items []any, b *strings.Builder) {
	for _, raw := range items {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if s, ok := it["text"].(string); ok {
			b.WriteString(s)
			b.WriteString(" ")
		}
		if it["type"] == "pageLink" {
			if props, ok := it["props"].(map[string]any); ok {
				if label, ok := props["label"].(string); ok {
					b.WriteString(label)
					b.WriteString(" ")
				}
			}
		}
		// Table rows nest cells → arrays of inline items.
		if cells, ok := it["cells"].([]any); ok {
			for _, c := range cells {
				if arr, ok := c.([]any); ok {
					collectInlineText(arr, b)
				}
			}
		}
		if nested, ok := it["content"].([]any); ok {
			collectInlineText(nested, b)
		}
	}
}

// backfillSnippets computes snippet/thumb for pages written before the columns
// existed. Runs once (guarded by a settings flag); cursor is drained into a
// slice before updating (MaxOpenConns(1) — a live cursor plus a write would
// deadlock).
func (s *Server) backfillSnippets() {
	if s.setting("snippet_backfill", "") != "" {
		return
	}
	type row struct{ id, content string }
	var todo []row
	rows, err := s.db.Query(`SELECT id, content FROM pages WHERE snippet = '' AND content != '' AND content != '[]'`)
	if err == nil {
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.content) == nil {
				todo = append(todo, r)
			}
		}
		rows.Close()
	}
	for _, r := range todo {
		sn, th := extractSnippetAndThumb([]byte(r.content))
		if sn != "" || th != "" {
			s.db.Exec(`UPDATE pages SET snippet = ?, thumb = ? WHERE id = ?`, sn, th, r.id)
		}
	}
	s.setSetting("snippet_backfill", "1")
}
