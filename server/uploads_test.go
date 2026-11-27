package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var ticketURLPattern = regexp.MustCompile(`/api/upload/[0-9a-f]+`)

// uploadFixture is one account with a workspace page it may write.
type uploadFixture struct {
	s      *Server
	uid    string
	cookie string
	u      *user
	ws     string
	page   string
}

func newUploadFixture(t *testing.T) uploadFixture {
	t.Helper()
	s := testServer(t)
	uid, cookie := signedIn(t, s, "uploader@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	return uploadFixture{
		s:      s,
		uid:    uid,
		cookie: cookie,
		u:      &user{ID: uid, Name: "Test"},
		ws:     ws,
		page:   s.makePage(t, ws, uid, "", "Uploads", `{}`),
	}
}

func (f uploadFixture) token(t *testing.T, scope string) string {
	t.Helper()
	raw := newID()
	if _, err := f.s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'probe', ?, ?, ?)`, newID(), f.uid, tokenHash(raw), scope, now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	return raw
}

func (f uploadFixture) send(method, path string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.s.ServeHTTP(rec, req)
	return rec
}

func (f uploadFixture) issueTicket(t *testing.T, fileName, pageID string) string {
	t.Helper()
	out, err := callTool(t, f.s, f.u, "upload_file", `{"file_name":"`+fileName+`","page_id":"`+pageID+`"}`)
	if err != nil {
		t.Fatalf("upload_file without data: %v", err)
	}
	url := ticketURLPattern.FindString(out)
	if url == "" {
		t.Fatalf("no upload address in the answer: %s", out)
	}
	if !strings.Contains(out, "curl") {
		t.Errorf("the answer should say how to send the file: %s", out)
	}
	return url
}

func (f uploadFixture) pageContent(t *testing.T) string {
	t.Helper()
	var content string
	if err := f.s.db.QueryRow(`SELECT content FROM pages WHERE id = ?`, f.page).Scan(&content); err != nil {
		t.Fatalf("read page: %v", err)
	}
	return content
}

func decodeStored(t *testing.T, rec *httptest.ResponseRecorder) storedUpload {
	t.Helper()
	var out storedUpload
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("answer is not JSON: %s", rec.Body.String())
	}
	return out
}

func filesOnDisk(t *testing.T, s *Server, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(s.dataDir, dir))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	return len(entries)
}

func TestUploadTicketAttachesTheFileOnce(t *testing.T) {
	f := newUploadFixture(t)
	url := f.issueTicket(t, "report.pdf", f.page)
	body := bytes.Repeat([]byte("x"), 820<<10)

	rec := f.send("PUT", url, body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT to the ticket: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeStored(t, rec)
	if out.Size != int64(len(body)) || !strings.HasSuffix(out.Name, ".pdf") {
		t.Errorf("stored %+v, want %d bytes as .pdf", out, len(body))
	}
	if got, err := os.ReadFile(filepath.Join(f.s.dataDir, "files", out.Name)); err != nil || !bytes.Equal(got, body) {
		t.Errorf("the bytes on disk differ from the ones sent (err %v)", err)
	}
	if fi, err := os.Stat(filepath.Join(f.s.dataDir, "files", out.Name)); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("stored file mode: %v (err %v), want 0644 like every other upload", fi.Mode().Perm(), err)
	}
	if content := f.pageContent(t); !strings.Contains(content, out.URL) || !strings.Contains(content, "report.pdf") {
		t.Errorf("no file block on the page: %s", content)
	}
	var indexed string
	f.s.db.QueryRow(`SELECT page_id FROM files WHERE file_name = ?`, out.Name).Scan(&indexed)
	if indexed != f.page {
		t.Errorf("file index names page %q, want %q", indexed, f.page)
	}
	var logged int
	f.s.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'upload_file' AND page_id = ?`, f.page).Scan(&logged)
	if logged != 1 {
		t.Errorf("activity log holds %d upload entries, want 1 (the upload, not the address)", logged)
	}

	if rec := f.send("PUT", url, body, nil); rec.Code != http.StatusNotFound {
		t.Errorf("second PUT to a spent ticket: %d, want 404", rec.Code)
	}
}

func TestUploadTicketExpires(t *testing.T) {
	f := newUploadFixture(t)
	url := f.issueTicket(t, "a.txt", f.page)
	tok := strings.TrimPrefix(url, "/api/upload/")
	f.s.uploadTickets.mu.Lock()
	ticket := f.s.uploadTickets.tickets[tok]
	ticket.Expires = time.Now().Add(-time.Second)
	f.s.uploadTickets.tickets[tok] = ticket
	f.s.uploadTickets.mu.Unlock()

	if rec := f.send("PUT", url, []byte("late"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("PUT to an expired ticket: %d, want 404", rec.Code)
	}
}

func TestUploadTicketFollowsPagePermissions(t *testing.T) {
	f := newUploadFixture(t)
	otherID, _ := signedIn(t, f.s, "other@example.test")
	foreign := f.s.makePage(t, makeWorkspace(t, f.s, otherID), otherID, "", "Not yours", `{}`)
	if _, err := callTool(t, f.s, f.u, "upload_file", `{"file_name":"a.txt","page_id":"`+foreign+`"}`); err == nil {
		t.Error("an upload address was issued for a page the caller cannot write")
	}

	// Write access lost between issuing and sending.
	url := f.issueTicket(t, "a.txt", f.page)
	f.s.db.Exec(`UPDATE workspace_members SET role = 'viewer' WHERE user_id = ?`, f.uid)
	if rec := f.send("PUT", url, []byte("hello"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("PUT after losing write access: %d, want 404", rec.Code)
	}
	if n := filesOnDisk(t, f.s, "files"); n != 0 {
		t.Errorf("%d files on disk after a refused upload", n)
	}
}

func TestUploadTicketIsRefusedToAReadOnlyToken(t *testing.T) {
	f := newUploadFixture(t)
	reader := &user{ID: f.uid, TokenScope: "read"}
	if _, err := f.s.mcpCall(reader, "upload_file", json.RawMessage(`{"file_name":"a.txt"}`), ""); err == nil {
		t.Error("a read-only token received an upload address")
	}
}

func TestPutPageFileWithAToken(t *testing.T) {
	f := newUploadFixture(t)
	auth := map[string]string{"Authorization": "Bearer " + f.token(t, "write")}

	rec := f.send("PUT", "/api/pages/"+f.page+"/files/logo.png", []byte("png bytes"), auth)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT with a write token: %d %s", rec.Code, rec.Body.String())
	}
	out := decodeStored(t, rec)
	if content := f.pageContent(t); !strings.Contains(content, `"type":"image"`) || !strings.Contains(content, out.URL) {
		t.Errorf("no image block on the page: %s", content)
	}

	readOnly := map[string]string{"Authorization": "Bearer " + f.token(t, "read")}
	if rec := f.send("PUT", "/api/pages/"+f.page+"/files/a.txt", []byte("x"), readOnly); rec.Code != http.StatusForbidden {
		t.Errorf("PUT with a read-only token: %d, want 403", rec.Code)
	}
	if rec := f.send("PUT", "/api/pages/"+f.page+"/files/a.txt", []byte("x"), nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("PUT without credentials: %d, want 401", rec.Code)
	}
	if rec := f.send("PUT", "/api/pages/"+newID()+"/files/a.txt", []byte("x"), auth); rec.Code != http.StatusNotFound {
		t.Errorf("PUT to an unknown page: %d, want 404", rec.Code)
	}
	if rec := f.send("PUT", "/api/pages/"+f.page+"/files/empty.txt", nil, auth); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT of an empty body: %d, want 400", rec.Code)
	}
}

func TestUploadsOverTheLimitLeaveNothingBehind(t *testing.T) {
	f := newUploadFixture(t)
	f.s.setSetting("max_upload_mb", "1")
	tooBig := bytes.Repeat([]byte("x"), 1<<20+1)
	auth := map[string]string{"Cookie": f.cookie}

	rec := f.send("PUT", "/api/pages/"+f.page+"/files/big.bin", tooBig, auth)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "max 1 MB") {
		t.Errorf("raw PUT over the limit: %d %s", rec.Code, rec.Body.String())
	}

	// Without a Content-Length the limit bites while streaming.
	req := httptest.NewRequest("PUT", f.issueTicket(t, "big.bin", f.page), bytes.NewReader(tooBig))
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	f.s.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("streamed PUT over the limit: %d %s", rec.Code, rec.Body.String())
	}

	if n := filesOnDisk(t, f.s, "files") + filesOnDisk(t, f.s, uploadTempDir); n != 0 {
		t.Errorf("%d files left on disk after refused uploads", n)
	}
	if strings.Contains(f.pageContent(t), "/files/") {
		t.Error("a refused upload left a block on the page")
	}
}

func multipartBody(t *testing.T, fileName string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("note", "ignored")
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	fw.Write(data)
	mw.Close()
	return &body, mw.FormDataContentType()
}

func TestBrowserUploadStreamsTheMultipartFile(t *testing.T) {
	f := newUploadFixture(t)
	f.s.setSetting("max_upload_mb", "1")
	data := bytes.Repeat([]byte("y"), 1<<20)
	body, contentType := multipartBody(t, "Offer.PDF", data)

	rec := f.send("POST", "/api/upload?page="+f.page, body.Bytes(), map[string]string{"Cookie": f.cookie, "Content-Type": contentType})
	if rec.Code != http.StatusOK {
		t.Fatalf("multipart upload at the limit: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ URL string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.HasSuffix(out.URL, ".pdf") {
		t.Errorf("stored as %q, want a lower-case .pdf", out.URL)
	}
	if strings.Contains(f.pageContent(t), "/files/") {
		t.Error("the browser upload appended a block itself; the editor places it")
	}

	body, contentType = multipartBody(t, "big.bin", append(data, 'z'))
	rec = f.send("POST", "/api/upload", body.Bytes(), map[string]string{"Cookie": f.cookie, "Content-Type": contentType})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("multipart upload over the limit: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInlineBase64UploadStillWorks(t *testing.T) {
	f := newUploadFixture(t)
	out, err := callTool(t, f.s, f.u, "upload_file", `{"file_name":"note.txt","data_base64":"aGVsbG8=","page_id":"`+f.page+`"}`)
	if err != nil {
		t.Fatalf("upload_file with data: %v", err)
	}
	if !strings.HasPrefix(out, "Uploaded note.txt → /files/") {
		t.Errorf("answer: %s", out)
	}
	if !strings.Contains(f.pageContent(t), "note.txt") {
		t.Error("no file block on the page")
	}
}

func TestUploadLimitIsAskable(t *testing.T) {
	f := newUploadFixture(t)
	rec := f.send("GET", "/api/upload/limit", nil, map[string]string{"Cookie": f.cookie})
	var limit struct{ MaxBytes int64 }
	json.Unmarshal(rec.Body.Bytes(), &limit)
	if limit.MaxBytes != defaultMaxUploadMB<<20 {
		t.Errorf("maxBytes = %d, want %d", limit.MaxBytes, defaultMaxUploadMB<<20)
	}
}
