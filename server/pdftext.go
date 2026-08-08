package server

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Limits for PDF text extraction. Both exist because of one production
// incident: a 24 MB PDF uploaded through the MCP tool took the instance down —
// port still open, every request hanging, the host itself so short on memory
// that SSH could no longer start a session.
//
// The cause is not a leak but the shape of the work. pdf.GetPlainText parses
// the whole document into memory and hands back the complete text, and the
// text used to be capped only AFTER it had been fully built. A large scanned
// PDF therefore allocated a multiple of its own size, and with
// db.SetMaxOpenConns(1) a single blocked request is a blocked server.
//
// So: refuse the big ones outright, and stop reading once we have as much text
// as we would keep anyway.
// How much extracted text we keep. Full-text search over a whole book is not
// worth the database weight.
const maxIndexedTextBytes = 500_000

// Extraction runs one (or a few) at a time. A per-file size limit is worthless
// on its own: one 15 MB extraction is harmless, four at once are not, and an
// import script with several connections produces exactly that. Waiting a
// moment costs nobody anything; not waiting cost us an evening.
//
// Buffered channel as a semaphore — a token in, a token out. Sized once at
// startup from the machine's actual memory.
var extractionGate = make(chan struct{}, extractionSlots())

// extractPDFText best-effort extracts plain text from a PDF for indexing.
// PDF parsing is messy; failures (including panics from malformed files)
// degrade to an empty string, never to an error for the caller. Note that
// recover() does NOT save us from a memory blow-up — the size check above is
// what does, because an OOM kill is not a panic.
func extractPDFText(path string) (text string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pdf extract %s: recovered: %v", path, r)
			text = ""
		}
	}()
	// Cheap guard first: stat before parse.
	limit := pdfExtractLimit()
	if fi, err := os.Stat(path); err == nil && fi.Size() > limit {
		// Bytes, not rounded megabytes: "10 MB exceeds the 10 MB limit" is
		// what integer division produces just over the line, and it reads like
		// a bug to whoever is looking at the log at 3am.
		log.Printf("pdf extract %s: skipped for indexing, %d bytes is over the %d byte limit (the file itself is stored and listed as usual)",
			filepath.Base(path), fi.Size(), limit)
		return ""
	}
	// Queue behind any extraction already running. Held across the parse, which
	// is the expensive part; released by defer so a panic cannot leak the slot
	// and starve every later upload.
	extractionGate <- struct{}{}
	defer func() { <-extractionGate }()
	f, r, err := pdf.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	reader, err := r.GetPlainText()
	if err != nil {
		return ""
	}
	var b strings.Builder
	// CopyN, not Copy: stop at the cap instead of building the whole text and
	// throwing most of it away. io.EOF simply means the document was shorter.
	if _, err := io.CopyN(&b, reader, maxIndexedTextBytes); err != nil && err != io.EOF {
		return ""
	}
	return b.String()
}

// indexFileText stores extracted file text and refreshes the owning page.
func (s *Server) indexFileText(fileName, pageID, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if _, err := s.db.Exec(`INSERT INTO file_texts (file_name, page_id, text) VALUES (?, ?, ?)
		ON CONFLICT(file_name) DO UPDATE SET page_id = excluded.page_id, text = excluded.text`,
		fileName, pageID, text); err != nil {
		log.Printf("index file text: %v", err)
		return
	}
	if pageID != "" {
		if err := s.reindexPage(pageID); err != nil {
			log.Printf("reindex after file: %v", err)
		}
	}
}
