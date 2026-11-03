package server

// i18n-ok-file: the fixtures are deliberately German ("Heiße Kontakte",
// "Erstgespräch", "Fällig") — they are what proves umlauts survive an import
// intact. Prose and failure messages in this file are English.

import (
	"encoding/json"
	"net"
	"testing"
)

// The block is the only defence against SSRF: without it an agent could make
// the server fetch neighbours on the private network that cannot be reached
// from outside, such as the hypervisor, the router or a cloud metadata service.
func TestBlockedIP(t *testing.T) {
	if allowPrivateImport {
		t.Skip("SALT_IMPORT_ALLOW_PRIVATE is set")
	}
	blocked := []string{
		"127.0.0.1", "::1", // Schleife
		"10.0.0.5", "172.16.0.1", "192.168.1.1", // private ranges (RFC1918)
		"169.254.169.254", // Cloud-Metadaten
		"fd00::1",         // eindeutig lokal (IPv6)
		"fe80::1",         // Link-Local (IPv6)
		"0.0.0.0", "::",   // unbestimmt
		"224.0.0.1", // Multicast
	}
	for _, a := range blocked {
		if !blockedIP(net.ParseIP(a)) {
			t.Errorf("%s must be blocked — otherwise the import is an SSRF hole", a)
		}
	}
	for _, a := range []string{"1.1.1.1", "104.16.0.1", "2606:4700::1111"} {
		if blockedIP(net.ParseIP(a)) {
			t.Errorf("%s is public and must be allowed", a)
		}
	}
}

func TestJSONPath(t *testing.T) {
	var doc any
	json.Unmarshal([]byte(`{
		"data": {"results": [1,2]},
		"card": {"due": "2026-08-01"},
		"labels": [{"name":"Hot"},{"name":"B2B"}],
		"n": 42, "f": 1.5, "t": true
	}`), &doc)

	cases := []struct {
		path string
		want string
	}{
		{"card.due", "2026-08-01"},
		{"labels[].name", "Hot, B2B"}, // picking out of a list
		{"n", "42"},                   // a whole number, no .0
		{"f", "1.5"},
		{"t", "true"},
		{"fehlt", ""},
		{"card.fehlt", ""},
		{"n.tiefer", ""}, // running into a scalar must not blow up
	}
	for _, c := range cases {
		if got := scalarString(jsonPath(doc, c.path)); got != c.want {
			t.Errorf("jsonPath(%q) = %q, erwartet %q", c.path, got, c.want)
		}
	}
}

// The real case: a Trello response where the card knows only the id of its
// list and the plain text sits somewhere else. Without resolve the status
// column would hold a meaningless id.
func TestMapItemsTrelloShape(t *testing.T) {
	var doc any
	json.Unmarshal([]byte(`{
		"lists": [{"id":"L1","name":"Heiße Kontakte"},{"id":"L2","name":"Verloren"}],
		"cards": [
			{"name":"Notar Thelen","desc":"Erstgespräch","idList":"L1","due":"2026-08-01",
			 "labels":[{"name":"Hot"},{"name":"B2B"}]},
			{"name":"","desc":"","idList":"L2","labels":[]}
		]
	}`), &doc)

	items, err := mapItems(doc, ingestSpec{
		Items: "cards", Title: "name", Markdown: "desc",
		Properties: map[string]string{"Status": "idList", "Fällig": "due", "Labels": "labels[].name"},
		Resolve:    map[string]ingestResolve{"idList": {From: "lists", Match: "id", To: "name"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("erwartet 2 Eintraege, bekommen %d", len(items))
	}
	if items[0].title != "Notar Thelen" || items[0].md != "Erstgespräch" {
		t.Errorf("Titel/Inhalt falsch: %q / %q", items[0].title, items[0].md)
	}
	if got := scalarString(items[0].props["Status"]); got != "Heiße Kontakte" {
		t.Errorf("Status = %q, expected the resolved list name instead of the id", got)
	}
	if got := scalarString(items[0].props["Labels"]); got != "Hot, B2B" {
		t.Errorf("Labels = %q", got)
	}
	// An empty title must not abort the import, or one untitled card would
	// stop a whole migration.
	if items[1].title != "Untitled 2" {
		t.Errorf("empty title = %q, expected a placeholder", items[1].title)
	}
}

func TestMapItemsRejectsBadPath(t *testing.T) {
	var doc any
	json.Unmarshal([]byte(`{"cards":[{"name":"A"}]}`), &doc)
	if _, err := mapItems(doc, ingestSpec{Items: "karten", Title: "name"}); err == nil {
		t.Error("a wrong items path must fail, not quietly yield 0 entries")
	}
}

func TestValueStrings(t *testing.T) {
	var arr any
	json.Unmarshal([]byte(`["Hot","",  "B2B"]`), &arr)
	got := valueStrings(arr)
	if len(got) != 2 || got[0] != "Hot" || got[1] != "B2B" {
		t.Errorf("valueStrings = %v, leere Werte muessen wegfallen", got)
	}
}
