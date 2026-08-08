package server

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The incident this guards against: a 24 MB PDF uploaded through the MCP tool
// took a production instance down — port open, every request hanging, the host
// out of memory. Extraction used to parse any document whole and cap the text
// only afterwards, so a big file allocated a multiple of its own size, and
// with db.SetMaxOpenConns(1) one blocked request is a blocked server.
//
// A unit test cannot assert "the server stays up", but it can assert the
// properties that keep it up: oversized files are never parsed, the text we
// build is bounded, and parses do not all run at once.

func TestExtractPDFTextSkipsOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.pdf")
	// Deliberately not a valid PDF: if the size guard works, nothing ever
	// tries to parse it.
	if err := os.WriteFile(path, make([]byte, pdfExtractLimit()+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := extractPDFText(path); got != "" {
		t.Errorf("oversized PDF returned %d bytes of text, want none", len(got))
	}
}

// The cap has to hold whatever the parser hands back. Reading through a
// strings.Builder without a bound was the actual defect: the whole book was
// built in memory and then thrown away down to 500 KB.
func TestIndexedTextCapIsBounded(t *testing.T) {
	if maxIndexedTextBytes > pdfExtractLimit() {
		t.Fatalf("indexed-text cap (%d) above the file cap (%d) — the bound would never bite",
			maxIndexedTextBytes, pdfExtractLimit())
	}
}

// A file under the limit must still be attempted: the guard exists to stop the
// monsters, not to quietly switch off search for ordinary documents.
func TestExtractPDFTextAcceptsNormalSizes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.pdf")
	pdf := "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\n" +
		"trailer<</Root 1 0 R>>\n"
	if err := os.WriteFile(path, []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	got := extractPDFText(path)
	if len(got) > maxIndexedTextBytes {
		t.Errorf("returned %d bytes, above the %d cap", len(got), maxIndexedTextBytes)
	}
	if strings.Contains(got, "\x00") {
		t.Error("extracted text contains NUL bytes")
	}
}

// Concurrency is the axis that gets forgotten: a per-file limit is worthless
// while ten uploads parse at once. This asserts the gate actually serialises —
// with the semaphore removed, all callers enter together and the observed peak
// jumps to the number of goroutines.
func TestExtractionsAreSerialised(t *testing.T) {
	slots := cap(extractionGate)
	var mu sync.Mutex
	inside, peak := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < slots+4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			extractionGate <- struct{}{}
			defer func() { <-extractionGate }()

			mu.Lock()
			inside++
			if inside > peak {
				peak = inside
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond) // hold the slot long enough to overlap
			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if peak > slots {
		t.Errorf("%d extractions ran at once, the gate allows %d", peak, slots)
	}
	if len(extractionGate) != 0 {
		t.Errorf("gate leaked %d token(s) — a later upload would wait forever", len(extractionGate))
	}
}

// The panic path must release its slot. Before the defer, a single malformed
// PDF could take a slot with it and, on a one-slot instance, wedge every
// upload that followed.
func TestExtractionSlotSurvivesBadInput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\nnot really a pdf at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < cap(extractionGate)+2; i++ {
		extractPDFText(path) // must not panic, must not hang
	}
	if len(extractionGate) != 0 {
		t.Errorf("gate leaked %d token(s) after malformed input", len(extractionGate))
	}
}
