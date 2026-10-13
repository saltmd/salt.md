package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableExportEmbedsUploadsAndResolvesLinks(t *testing.T) {
	s := testServer(t)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 64)
	seedFile(t, s, "poster.png", png)
	p := &page{Content: json.RawMessage(`[{"type":"image","props":{"url":"/files/poster.png"}},{"type":"paragraph","content":[{"type":"pageLink","props":{"pageId":"other","label":"Open"}}]}]`)}
	p.Title, p.Icon, p.Cover = "Export", "/files/poster.png", "/files/poster.png"
	got, err := s.portableExportHTML(p, httptest.NewRequest("GET", "https://docs.example.test/api/pages/p/export?format=html", nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `src="/files/`) || !strings.Contains(got, "data:image/png;base64,") || !strings.Contains(got, `href="https://docs.example.test/p/other"`) {
		t.Fatal("export is not portable")
	}
}

func TestPortableExportFallsBackForMissingAndEscapingUploads(t *testing.T) {
	s := testServer(t)
	outside := filepath.Join(t.TempDir(), "outside.png")
	os.WriteFile(outside, []byte("private"), 0600)
	if err := os.Symlink(outside, filepath.Join(s.dataDir, "files", "escape.png")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing.png", "escape.png"} {
		content, _ := json.Marshal([]map[string]any{{"type": "image", "props": map[string]any{"url": "/files/" + name}}})
		p := &page{Content: content}
		got, err := s.portableExportHTML(p, httptest.NewRequest("GET", "https://docs.example.test/export", nil))
		if err != nil || !strings.Contains(got, "https://docs.example.test/files/"+name) || strings.Contains(got, "cHJpdmF0ZQ==") {
			t.Fatalf("missing safe fallback for %s", name)
		}
	}
}

func TestPortableExportFallsBackForOversizedUpload(t *testing.T) {
	s := testServer(t)
	f, err := os.Create(filepath.Join(s.dataDir, "files", "large.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate((32 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	p := &page{Content: json.RawMessage(`[{"type":"image","props":{"url":"/files/large.png"}}]`)}
	got, err := s.portableExportHTML(p, httptest.NewRequest("GET", "https://docs.example.test/export", nil))
	if err != nil || !strings.Contains(got, "https://docs.example.test/files/large.png") {
		t.Fatal("missing oversized upload fallback")
	}
}

func TestPortableExportDoesNotFetchRemoteImages(t *testing.T) {
	s := testServer(t)
	p := &page{Content: json.RawMessage(`[{"type":"image","props":{"url":"https://images.example.test/remote.png"}}]`)}
	got, err := s.portableExportHTML(p, httptest.NewRequest("GET", "https://docs.example.test/export", nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "https://images.example.test/remote.png") {
		t.Fatal("remote source changed")
	}
}

func TestPortableExportCountsRepeatedImagesAgainstLimit(t *testing.T) {
	s := testServer(t)
	seedFile(t, s, "repeat.png", "\x89PNG\r\n\x1a\n"+strings.Repeat("\x00", 12<<20))
	p := &page{Content: json.RawMessage(`[{"type":"image","props":{"url":"/files/repeat.png"}},{"type":"image","props":{"url":"/files/repeat.png"}},{"type":"image","props":{"url":"/files/repeat.png"}}]`)}
	got, err := s.portableExportHTML(p, httptest.NewRequest("GET", "https://docs.example.test/export", nil))
	if err != nil || strings.Count(got, "data:image/png;base64,") != 2 || !strings.Contains(got, "https://docs.example.test/files/repeat.png") {
		t.Fatal("repeated images must fall back after the total limit")
	}
}

func TestPortableExportFallsBackForUnsupportedImages(t *testing.T) {
	s := testServer(t)
	for _, name := range []string{"document.pdf", "unsupported.avif", "unsafe.svg"} {
		seedFile(t, s, name, "unsupported image content")
		content, _ := json.Marshal([]map[string]any{{"type": "image", "props": map[string]string{"url": "/files/" + name}}})
		got, err := s.portableExportHTML(&page{Content: content}, httptest.NewRequest("GET", "https://docs.example.test/export", nil))
		if err != nil || !strings.Contains(got, "https://docs.example.test/files/"+name) {
			t.Fatalf("missing fallback: %s", name)
		}
	}
}
