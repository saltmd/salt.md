package server

import (
	"strings"
	"testing"
)

func TestPublicCollectionTableKeepsExportRowsAndEscapesText(t *testing.T) {
	s := testServer(t)
	user, _ := signedIn(t, s, "table-test@example.test")
	ws := makeWorkspace(t, s, user)
	seedPage(t, s, "table-root", "", ws, user, "workspace", `[]`)
	schema := `[{"id":"status","name":"Status <script>","options":[{"id":"done","name":"Done <img>"}]},{"id":"url","name":"Website"},{"id":"check","name":"Checked"}]`
	s.db.Exec(`INSERT INTO collections(page_id,schema,views)VALUES('table-root',?,'[]')`, schema)
	seedPage(t, s, "private-row", "table-root", ws, user, "private", `[{"type":"paragraph","content":[{"type":"text","text":"BODY-MUST-NOT-LEAK"}]}]`)
	s.db.Exec(`UPDATE pages SET title='A <script>',props='{"status":"done","url":"https://example.test/a|b","check":true}' WHERE id='private-row'`)
	seedPage(t, s, "trashed-row", "table-root", ws, user, "workspace", `[]`)
	s.db.Exec(`UPDATE pages SET trashed_at=? WHERE id='trashed-row'`, now())
	seedPage(t, s, "nested-row", "private-row", ws, user, "workspace", `[]`)
	p, _ := s.getPage("table-root")
	got, err := s.publicCollectionTable(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<thead>`, `scope="col"`, `scope="row"`, `A &lt;script&gt;`, `Done &lt;img&gt;`, `https://example.test/a|b`, `✓`, `tabindex="0"`, `role="region"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
	for _, bad := range []string{`BODY-MUST-NOT-LEAK`, `Page trashed-row`, `Page nested-row`, `<script>`, `href="/p/`, `<pre`, `doc-src`, `saltStart`} {
		if strings.Contains(got, bad) {
			t.Fatalf("unexpected %s", bad)
		}
	}
	markdown, _ := s.collectionMarkdown(p)
	if !strings.Contains(markdown, "A <script>") || strings.Contains(markdown, "Page trashed-row") {
		t.Fatal("export row contract changed")
	}
}

func TestPublicCollectionTableEmptyAndCellValues(t *testing.T) {
	s := testServer(t)
	user, _ := signedIn(t, s, "empty-table@example.test")
	ws := makeWorkspace(t, s, user)
	seedPage(t, s, "empty-table", "", ws, user, "workspace", `[]`)
	s.db.Exec(`INSERT INTO collections(page_id,schema,views)VALUES('empty-table','[]','[]')`)
	p, _ := s.getPage("empty-table")
	got, err := s.publicCollectionTable(p)
	if err != nil || !strings.Contains(got, `colspan="1">No rows yet.`) {
		t.Fatal(err, got)
	}
	options := map[string]string{"status/a": "Alpha", "status/b": "Beta"}
	for _, pair := range []struct {
		value any
		want  string
	}{{[]any{"a", "b"}, "Alpha, Beta"}, {false, ""}, {true, "✓"}, {float64(3.25), "3.2500"}, {float64(3), "3"}, {nil, ""}} {
		if got := publicCollectionCell(pair.value, "status", options); got != pair.want {
			t.Fatalf("cell %v = %q", pair.value, got)
		}
	}
	if got := collectionStylesheet([]byte(`<link rel="stylesheet" href="https://evil.test/x.css">`)); got != "" {
		t.Fatal("foreign CSS", got)
	}
	if got := collectionStylesheet([]byte(`<link rel="stylesheet" crossorigin href="/assets/native.css">`)); !strings.Contains(got, "/assets/native.css") {
		t.Fatal("missing native CSS")
	}
}

func TestPublicCollectionTableMissingSchemaKeepsDocumentFallback(t *testing.T) {
	s := testServer(t)
	p := &page{Content: []byte(`[{"type":"paragraph","content":[{"type":"text","text":"Fallback <content>"}]}]`)}
	got, err := s.publicCollectionTable(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<title>Untitled · salt.md</title>`, `<h1>Untitled</h1>`, `Fallback &lt;content&gt;`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
