package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestAttachedFileDownloadRequiresPageAccess(t *testing.T) {
	s := testServer(t)
	owner, ownerCookie := signedIn(t, s, "file-owner@example.test")
	other, otherCookie := signedIn(t, s, "file-other@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	col := s.makeCollection(t, ws, owner, "Admin files", `[]`)
	page := s.makeRow(t, ws, owner, col, "Invoice", `{}`)
	name := "permission-test.txt"
	if err := os.WriteFile(filepath.Join(s.dataDir, "files", name), []byte("private attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	s.recordFile(name, page, name)
	path := "/files/" + name
	if rr := requestAs(t, s, ownerCookie, "GET", path, ""); rr.Code != 200 {
		t.Fatalf("owner: %d", rr.Code)
	}
	if rr := requestAs(t, s, otherCookie, "GET", path, ""); rr.Code != 404 {
		t.Fatalf("nonmember downloaded attachment: %d", rr.Code)
	}
	s.addMember(t, ws, other, "viewer")
	if rr := requestAs(t, s, otherCookie, "GET", path, ""); rr.Code != 200 {
		t.Fatalf("authorized viewer: %d", rr.Code)
	}
	s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, page)
	if rr := requestAs(t, s, otherCookie, "GET", path, ""); rr.Code != 404 {
		t.Fatalf("private attachment readable: %d", rr.Code)
	}
}

func TestAttachedFileDownloadRespectsTokenWorkspaceScope(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "scoped-files@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "Attachment", "{}")
	name := "scoped-attachment.txt"
	if err := os.WriteFile(filepath.Join(s.dataDir, "files", name), []byte("synthetic attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	s.recordFile(name, page, name)
	for _, test := range []struct {
		scope  string
		status int
	}{{ws, 200}, {"outside-workspace", 404}} {
		secret := newID()
		if _, err := s.db.Exec(`INSERT INTO api_tokens (id,user_id,name,token_hash,scope,workspace_scope,created_at) VALUES (?,?,'file test',?,'read',?,?)`, newID(), uid, tokenHash(secret), test.scope, now()); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/files/"+name, nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("scope %q: got %d, want %d", test.scope, w.Code, test.status)
		}
	}
}
