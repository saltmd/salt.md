package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// An agent sends Markdown — that is the whole MCP writing surface. A diagram
// block that only a person with a mouse can create is a feature built for the
// wrong audience, and this is the line that decides it.
func TestMermaidFenceBecomesADiagram(t *testing.T) {
	md := "# Ablauf\n\n```mermaid\ngraph TD\n  A --> B\n```\n\nDanach.\n"
	blocks := mdToBlocks(md)

	var found *mBlock
	for _, b := range blocks {
		if b.Type == "mermaid" {
			found = b
		}
		if b.Type == "codeBlock" {
			t.Errorf("a mermaid fence came through as a code block")
		}
	}
	if found == nil {
		t.Fatal("no diagram block: an agent cannot create one")
	}
	if code, _ := found.Props["code"].(string); !strings.Contains(code, "graph TD") || !strings.Contains(code, "A --> B") {
		t.Errorf("the source did not survive: %q", found.Props["code"])
	}
	// Never invented here: the server cannot draw, and a wrong picture is worse
	// than none. The page renders it the first time somebody opens it.
	if svg, _ := found.Props["svg"].(string); svg != "" {
		t.Errorf("the import produced a picture out of nothing: %q", svg)
	}
}

// Other languages keep their meaning — the fence is not a diagram just because
// it has a word after the backticks.
func TestOtherFencesStayCode(t *testing.T) {
	for _, lang := range []string{"", "go", "json", "bash"} {
		blocks := mdToBlocks("```" + lang + "\nx := 1\n```\n")
		for _, b := range blocks {
			if b.Type == "mermaid" {
				t.Errorf("```%s was read as a diagram", lang)
			}
		}
	}
}

// And back out again, so a page that goes through Markdown and returns still
// has its diagram rather than a code block that looks like one.
func TestDiagramRoundTripsThroughMarkdown(t *testing.T) {
	in := "```mermaid\ngraph TD\n  A --> B\n```\n"
	raw, err := json.Marshal(mdToBlocks(in))
	if err != nil {
		t.Fatal(err)
	}
	md := blocksToMarkdown(raw)
	if !strings.Contains(md, "```mermaid") || !strings.Contains(md, "A --> B") {
		t.Errorf("the diagram did not come back as a fence:\n%s", md)
	}
	again := mdToBlocks(md)
	n := 0
	for _, b := range again {
		if b.Type == "mermaid" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("second pass produced %d diagrams, want 1", n)
	}
}
