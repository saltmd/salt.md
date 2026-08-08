package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// An agent's only way to put content into a page is Markdown, and the backlinks
// index reads `pageLink` and nothing else. If a Markdown link to a page of this
// instance does not become a pageLink, then everything an agent writes is an
// island in the graph — which is exactly what happened until W113.
func TestMarkdownLinkToOwnPageBecomesAPageLink(t *testing.T) {
	const id = "6430997b50672ea702cab5f43a31794c"

	cases := []struct {
		name string
		href string
	}{
		{"a bare path", "/p/" + id},
		{"an absolute URL, which is what share_page hands out", "https://salt.example/p/" + id},
		{"with a trailing slash", "/p/" + id + "/"},
		{"behind a port", "http://localhost:8420/p/" + id},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := mdToBlocksJSON("See [What it can do](" + c.href + ") for details.")
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if !strings.Contains(out, `"pageLink"`) {
				t.Fatalf("no pageLink produced for %s:\n%s", c.href, out)
			}
			if !strings.Contains(out, id) {
				t.Errorf("the target id is missing:\n%s", out)
			}
			if !strings.Contains(out, "What it can do") {
				t.Errorf("the label is missing:\n%s", out)
			}
			// The whole point: the derived index has to see it.
			links := extractLinks([]byte(out))
			if len(links) != 1 || links[0] != id {
				t.Errorf("extractLinks got %v, want [%s] — backlinks would be blind", links, id)
			}
		})
	}
}

// Everything else stays an ordinary link. Being wrong in this direction only
// costs a backlink; being wrong the other way would turn a customer's website
// into a broken internal reference.
func TestOrdinaryLinksAreLeftAlone(t *testing.T) {
	for _, href := range []string{
		"https://example.com",
		"https://example.com/p/not-an-id",
		"/p/short",
		"/p/6430997b50672ea702cab5f43a31794cEXTRA",
		"mailto:someone@example.com",
		"/files/abc123.pdf",
	} {
		out, err := mdToBlocksJSON("A [link](" + href + ") here.")
		if err != nil {
			t.Fatalf("convert %s: %v", href, err)
		}
		if strings.Contains(out, `"pageLink"`) {
			t.Errorf("%s was turned into a page link:\n%s", href, out)
		}
		if len(extractLinks([]byte(out))) != 0 {
			t.Errorf("%s ended up in the backlink index", href)
		}
	}
}

// Export writes a pageLink as [label](/p/id). Importing that back has to give
// the same thing, or a workspace loses every internal link on a round trip.
func TestPageLinkSurvivesAMarkdownRoundTrip(t *testing.T) {
	const id = "6430997b50672ea702cab5f43a31794c"
	content, err := mdToBlocksJSON("Start at [Handbook](/p/" + id + ").")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	// Shape it the way the rest of the server expects to read it.
	var blocks []map[string]any
	if err := json.Unmarshal([]byte(content), &blocks); err != nil {
		t.Fatalf("the produced JSON is not a block array: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatal("no blocks")
	}
	again, err := mdToBlocksJSON(blocksToMarkdown([]byte(content)))
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if links := extractLinks([]byte(again)); len(links) != 1 || links[0] != id {
		t.Errorf("the link did not survive the round trip: %v", links)
	}
}
