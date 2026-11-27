package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Uploads are streamed to disk, never held in memory: the body is copied into
// a temporary file beside the files folder and renamed into place once it is
// complete and within the limit. Three entry points share it:
//
//   - POST /api/upload: the browser's multipart form, nothing attached;
//   - PUT /api/pages/{id}/files/{name}: a raw body with a session or token,
//     attached to the page as a block (curl -T);
//   - PUT /api/upload/{ticket}: the one-time address upload_file hands an agent
//     that cannot put a file's bytes into a tool call.

const (
	defaultMaxUploadMB = 100
	maxMaxUploadMB     = 2048
	// multipartSlack is the room the multipart envelope gets beyond the file.
	multipartSlack = 1 << 20
	// uploadTempDir holds uploads in flight, relative to the data directory.
	uploadTempDir = "files-incoming"
	// uploadTicketTTL is how long a one-time upload address stays valid.
	uploadTicketTTL = 15 * time.Minute
	// maxExtLen is the longest extension kept on a stored name, dot included.
	maxExtLen = 12
)

var (
	errUploadTooLarge = errors.New("file too large")
	errUploadEmpty    = errors.New("no file content")
)

// uploadRequest is one file about to be stored.
type uploadRequest struct {
	FileName string // the name a person calls it; only its extension reaches the disk
	PageID   string // the page it belongs to, or empty
	Attach   bool   // append a file or image block to the page
}

// storedUpload is what an upload became.
type storedUpload struct {
	Name   string `json:"name"` // the stored name, the /files/ path segment
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	PageID string `json:"page_id,omitempty"`
}

// summary is the one line an agent and the activity log get.
func (u storedUpload) summary(fileName string) string {
	return fmt.Sprintf("Uploaded %s → %s", fileName, u.URL)
}

// cleanExt is the extension a stored name keeps: lower case, letters, digits
// and the dot, dropped entirely when longer than maxExtLen.
func cleanExt(fileName string) string {
	var clean strings.Builder
	for _, c := range strings.ToLower(filepath.Ext(fileName)) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' {
			clean.WriteRune(c)
		}
	}
	ext := clean.String()
	if len(ext) > maxExtLen || ext == "." {
		return ""
	}
	return ext
}

// fileBlockJSON is the block an attached upload appends to its page.
func fileBlockJSON(url, fileName string) string {
	blockType := "file"
	switch cleanExt(fileName) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		blockType = "image"
	}
	b, _ := json.Marshal(map[string]any{
		"type":  blockType,
		"props": map[string]string{"url": url, "name": fileName},
	})
	return string(b)
}

// storeUpload streams src to disk, refuses it past the upload limit or when
// empty, and then attaches, indexes and records it.
func (s *Server) storeUpload(src io.Reader, req uploadRequest) (storedUpload, error) {
	limit := s.maxUploadBytes()
	tmpDir := filepath.Join(s.dataDir, uploadTempDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return storedUpload{}, err
	}
	tmp, err := os.CreateTemp(tmpDir, "upload-*")
	if err != nil {
		return storedUpload{}, err
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, io.LimitReader(src, limit+1))
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return storedUpload{}, err
	case size > limit:
		return storedUpload{}, errUploadTooLarge
	case size == 0:
		return storedUpload{}, errUploadEmpty
	}

	name := newID() + cleanExt(req.FileName)
	path := filepath.Join(s.dataDir, "files", name)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return storedUpload{}, err
	}
	out := storedUpload{Name: name, URL: "/files/" + name, Size: size, PageID: req.PageID}
	if req.PageID != "" {
		if req.Attach {
			if err := s.appendBlockJSON(req.PageID, fileBlockJSON(out.URL, req.FileName)); err != nil {
				return storedUpload{}, err
			}
		}
		if strings.HasSuffix(name, ".pdf") {
			s.indexFileText(name, req.PageID, extractPDFText(path))
		}
	}
	s.recordFile(name, req.PageID, filepath.Base(req.FileName))
	return out, nil
}

// writeUploadError answers a failed upload: 413 for the limit, 400 for an
// empty body, 500 for the rest.
func (s *Server) writeUploadError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, errUploadTooLarge) || errors.As(err, &tooLarge):
		httpError(w, http.StatusRequestEntityTooLarge, s.tooLargeMessage())
	case errors.Is(err, errUploadEmpty):
		httpError(w, http.StatusBadRequest, "no file content: the request body is empty")
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) tooLargeMessage() string {
	return fmt.Sprintf("file too large: max %d MB", s.maxUploadBytes()>>20)
}

// receiveRawUpload stores a raw request body (curl -T) and answers with the
// stored upload.
func (s *Server) receiveRawUpload(w http.ResponseWriter, r *http.Request, req uploadRequest) (storedUpload, bool) {
	limit := s.maxUploadBytes()
	if r.ContentLength > limit {
		httpError(w, http.StatusRequestEntityTooLarge, s.tooLargeMessage())
		return storedUpload{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1)
	out, err := s.storeUpload(r.Body, req)
	if err != nil {
		s.writeUploadError(w, err)
		return storedUpload{}, false
	}
	writeJSON(w, out)
	return out, true
}

// handleUploadLimit tells the browser the per-file cap before it sends a file.
func (s *Server) handleUploadLimit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]int64{"maxBytes": s.maxUploadBytes()})
}

// handleUpload is the browser's upload: one multipart field named "file",
// optionally under ?page=, streamed part by part.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	limit := s.maxUploadBytes()
	pageID := r.URL.Query().Get("page")
	if pageID != "" && !s.canWriteReq(r, pageID) {
		httpError(w, 403, "forbidden")
		return
	}
	if r.ContentLength > limit+multipartSlack {
		httpError(w, http.StatusRequestEntityTooLarge, s.tooLargeMessage())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+multipartSlack)
	mr, err := r.MultipartReader()
	if err != nil {
		httpError(w, 400, "upload malformed")
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			httpError(w, 400, "missing file field")
			return
		}
		if err != nil {
			s.writeUploadError(w, err)
			return
		}
		if part.FormName() != "file" {
			continue
		}
		out, err := s.storeUpload(part, uploadRequest{FileName: part.FileName(), PageID: pageID})
		if err != nil {
			s.writeUploadError(w, err)
			return
		}
		writeJSON(w, map[string]string{"url": out.URL})
		return
	}
}

// handlePutPageFile attaches a raw request body to a page as a file block:
// curl -T report.pdf -H "Authorization: Bearer …" …/api/pages/{id}/files/
func (s *Server) handlePutPageFile(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	if !s.canWriteReq(r, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	fileName := r.PathValue("name")
	out, ok := s.receiveRawUpload(w, r, uploadRequest{FileName: fileName, PageID: pageID, Attach: true})
	if !ok {
		return
	}
	u := requestUser(r)
	actorType := "human"
	if u.TokenKind != "" {
		actorType = "agent"
	}
	s.audit(actorType, u.ID, u.Name, "upload_file", pageID, s.pageWorkspace(pageID), out.summary(fileName))
}

// uploadTicket is a one-time upload address issued by upload_file.
type uploadTicket struct {
	User     user // the credential as it called upload_file, scope and kind included
	FileName string
	PageID   string
	Expires  time.Time
}

// uploadTicketStore keeps the tickets in memory; a restart voids them.
type uploadTicketStore struct {
	mu      sync.Mutex
	tickets map[string]uploadTicket
}

func newUploadTicketStore() *uploadTicketStore {
	return &uploadTicketStore{tickets: map[string]uploadTicket{}}
}

// issue stores a ticket and returns its token.
func (st *uploadTicketStore) issue(t uploadTicket) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	for tok, old := range st.tickets {
		if now.After(old.Expires) {
			delete(st.tickets, tok)
		}
	}
	tok := randomToken(24)
	st.tickets[tok] = t
	return tok
}

// take removes a ticket and returns it while it is still valid.
func (st *uploadTicketStore) take(tok string) (uploadTicket, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	t, ok := st.tickets[tok]
	delete(st.tickets, tok)
	return t, ok && time.Now().Before(t.Expires)
}

// mcpUploadTicket answers upload_file without data: a one-time address the
// agent sends the file to.
func (s *Server) mcpUploadTicket(u *user, fileName, pageID, publicBase string) string {
	tok := s.uploadTickets.issue(uploadTicket{
		User:     *u,
		FileName: fileName,
		PageID:   pageID,
		Expires:  time.Now().Add(uploadTicketTTL),
	})
	url := publicBase + "/api/upload/" + tok
	target := "stored without a page"
	if pageID != "" {
		target = "attached to page " + pageID
	}
	return fmt.Sprintf("Upload address for %s, valid for %d minutes and one upload:\n%s\n\n"+
		"Send the file's bytes as the request body, no other headers needed:\n"+
		"curl -sS --fail-with-body -T '<path to %s>' '%s'\n\n"+
		"The answer is JSON with the stored file's url; the file is %s. Limit: %d MB.",
		fileName, int(uploadTicketTTL/time.Minute), url, fileName, url, target, s.maxUploadBytes()>>20)
}

// handlePutTicketUpload receives the body for a ticket. The ticket is spent on
// the first attempt, and the page is checked again against the credential that
// asked for it.
func (s *Server) handlePutTicketUpload(w http.ResponseWriter, r *http.Request) {
	if !s.agentAddressAllowed(r) {
		s.refuseAgentAddress(w, r)
		return
	}
	t, ok := s.uploadTickets.take(r.PathValue("ticket"))
	if !ok {
		httpError(w, 404, "upload address unknown, used or expired: call upload_file again for a new one")
		return
	}
	current := s.userByID(t.User.ID)
	if current == nil || current.Disabled {
		httpError(w, 403, "this account has been deactivated")
		return
	}
	if t.PageID != "" && (!s.canWrite(t.User.ID, t.PageID) || !s.credentialMayEnter(&t.User, s.pageWorkspace(t.PageID))) {
		httpError(w, 404, "page not found")
		return
	}
	out, ok := s.receiveRawUpload(w, r, uploadRequest{FileName: t.FileName, PageID: t.PageID, Attach: t.PageID != ""})
	if !ok {
		return
	}
	ws := s.pageWorkspace(t.PageID)
	if ws == "" {
		ws = s.defaultWorkspaceFor(&t.User)
	}
	s.audit("agent", t.User.ID, t.User.Name+" (MCP)", "upload_file", t.PageID, ws, out.summary(t.FileName))
}
