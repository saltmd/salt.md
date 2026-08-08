package server

import (
	"encoding/json"
	"strings"
)

// Cutting pages into passages (W110, stage 1).
//
// Why at all: so far the smallest unit of search is the PAGE. A hit says
// "something is in this 4000-word document" and hands back 14 words of
// surroundings. For a human that is enough, they scroll. For an agent it is
// not — it either gets too little to answer with, or it loads the whole page
// and burns its context window on text that has nothing to do with the
// question.
//
// The cut follows BLOCK BOUNDARIES, not a character count. A passage starting
// mid-sentence is worthless as an answer; one starting at a heading carries
// its context with it. The same decision as in the neighbouring project, which
// cuts per conversation turn so a hit points at the actual exchange.
//
// Every passage also remembers the headings above it, as a path:
// "Vertrag › Kündigung › Fristen" — i18n-ok: German example, the compounds
// are the point. That is the information which lets an agent place a
// paragraph without loading the page.

const (
	// Target size of a passage. Big enough for one thought, small enough that a
	// hit still says something.
	chunkTarget = 700
	// From here on even a very long block gets split mid-way — otherwise a
	// table of 20,000 characters would be a single passage.
	chunkHardMax = 1800
)

type pageChunk struct {
	Ord     int
	Heading string // heading path, e.g. "Verträge › Kündigung" (i18n-ok: example)
	Text    string
}

// chunkContent breaks BlockNote JSON into passages.
func chunkContent(raw []byte) []pageChunk {
	var blocks []mdBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var out []pageChunk
	var trail []string // the heading path so far
	var buf strings.Builder
	bufHeading := ""

	flush := func() {
		t := strings.TrimSpace(buf.String())
		buf.Reset()
		if t == "" {
			return
		}
		out = append(out, pageChunk{Ord: len(out), Heading: bufHeading, Text: t})
	}

	var walk func(bs []mdBlock)
	walk = func(bs []mdBlock) {
		for _, blk := range bs {
			text := strings.TrimSpace(blockPlainText(blk))
			if blk.Type == "heading" {
				// A heading starts a new passage: it is the boundary the writer
				// drew themselves.
				flush()
				level := intProp(blk.Props, "level", 1)
				if level < 1 {
					level = 1
				}
				if level > len(trail) {
					trail = append(trail, text)
				} else {
					trail = append(trail[:level-1], text)
				}
				bufHeading = strings.Join(trail, " › ")
				// The heading itself belongs in the text, or the search will not
				// find it again inside the passage.
				buf.WriteString(text)
				buf.WriteString("\n")
				walk(blk.Children)
				continue
			}
			if text != "" {
				if buf.Len() == 0 {
					bufHeading = strings.Join(trail, " › ")
				}
				// Split very long blocks hard so a passage keeps a readable
				// size.
				for len(text) > chunkHardMax {
					cut := lastSpaceBefore(text, chunkHardMax)
					buf.WriteString(text[:cut])
					flush()
					bufHeading = strings.Join(trail, " › ")
					text = strings.TrimSpace(text[cut:])
				}
				buf.WriteString(text)
				buf.WriteString("\n")
				if buf.Len() >= chunkTarget {
					flush()
					bufHeading = strings.Join(trail, " › ")
				}
			}
			walk(blk.Children)
		}
	}
	walk(blocks)
	flush()
	return out
}

// lastSpaceBefore looks for the last word boundary before max — so a hard cut
// at least does not land in the middle of a word.
func lastSpaceBefore(s string, max int) int {
	if len(s) <= max {
		return len(s)
	}
	if i := strings.LastIndexAny(s[:max], " \n\t"); i > max/2 {
		return i
	}
	return max
}

// blockPlainText takes the visible text of ONE block (without its children).
func blockPlainText(blk mdBlock) string {
	if len(blk.Content) == 0 {
		return ""
	}
	var inl []mdInline
	if json.Unmarshal(blk.Content, &inl) != nil {
		// Tables and other special forms have a structure of their own —
		// that is what the general path is for.
		return strings.TrimSpace(extractText(blk.Content))
	}
	var b strings.Builder
	for _, i := range inl {
		if i.Text != "" {
			b.WriteString(i.Text)
			b.WriteString(" ")
		}
		if i.Type == "pageLink" {
			if label, ok := i.Props["label"].(string); ok {
				b.WriteString(label)
				b.WriteString(" ")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// reindexChunks rewrites the passages of a page.
//
// Runs in the same breath as reindexPage. The delete path needs nothing of its
// own: page_chunks hangs off pages by foreign key, and chunks_fts is carried
// along here (a virtual table knows no cascade).
func (s *Server) reindexChunks(pageID, workspaceID, title string, content []byte, trashed bool) error {
	if _, err := s.db.Exec(`DELETE FROM chunks_fts WHERE chunk_id IN
		(SELECT id FROM page_chunks WHERE page_id = ?)`, pageID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM page_chunks WHERE page_id = ?`, pageID); err != nil {
		return err
	}
	if trashed {
		return nil
	}
	chunks := chunkContent(content)
	if len(chunks) == 0 {
		// An empty page still gets one passage from its title, otherwise it
		// disappears from the passage-based search.
		if strings.TrimSpace(title) == "" {
			return nil
		}
		chunks = []pageChunk{{Ord: 0, Text: title}}
	}
	for _, c := range chunks {
		id := newID()
		if _, err := s.db.Exec(`INSERT INTO page_chunks (id, page_id, workspace_id, ord, heading, text)
			VALUES (?, ?, ?, ?, ?, ?)`, id, pageID, workspaceID, c.Ord, c.Heading, c.Text); err != nil {
			return err
		}
		// The title goes into every passage: otherwise a two-word German query
		// finds nothing when one word is in the title and the other in the
		// paragraph — i18n-ok: "Vertrag Kündigung" is the example that showed it.
		if _, err := s.db.Exec(`INSERT INTO chunks_fts (chunk_id, title, heading, text) VALUES (?, ?, ?, ?)`,
			id, title, c.Heading, c.Text); err != nil {
			return err
		}
	}
	return nil
}
