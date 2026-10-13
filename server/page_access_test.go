package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Private pages with direct user grants (fork): the permission core, the
// enumeration filters, the share endpoints and the grant limits.
//
// Pages stay 'private': a grant opens them to chosen members on top ("private
// plus these people"), with no migration, so every visibility = 'private'
// check keeps matching and a rollback stays closed.

// accessFixture builds one workspace: alice (admin), bob (member), carol
// (viewer) and dave (outside). Returns the workspace and an open page in it.
func accessFixture(t *testing.T, s *Server) (ws, open string, ids, cookies map[string]string) {
	t.Helper()
	ids = map[string]string{}
	cookies = map[string]string{}
	for _, mail := range []string{"alice-pa@example.test", "bob-pa@example.test", "carol-pa@example.test", "dave-pa@example.test"} {
		uid, cookie := signedIn(t, s, mail)
		ids[mail] = uid
		cookies[mail] = cookie
	}
	alice := ids["alice-pa@example.test"]
	ws = makeNamedWorkspace(t, s, alice, "Zugang")
	add := func(mail, role string) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
			ws, ids[mail], role); err != nil {
			t.Fatalf("add %s: %v", mail, err)
		}
	}
	add("bob-pa@example.test", "member")
	add("carol-pa@example.test", "viewer")
	open = s.makePage(t, ws, alice, "", "Open", "{}")
	return ws, open, ids, cookies
}

func grant(t *testing.T, s *Server, page, user, access string) {
	t.Helper()
	if _, err := s.db.Exec(`INSERT INTO page_grants (page_id, user_id, subject_type, access, created_at)
		VALUES (?, ?, 'user', ?, ?)`, page, user, access, now()); err != nil {
		t.Fatalf("grant: %v", err)
	}
}

func privatise(t *testing.T, s *Server, page string) {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, page); err != nil {
		t.Fatalf("privatise: %v", err)
	}
}

func TestPrivatePageAccessMatrix(t *testing.T) {
	s := testServer(t)
	ws, open, ids, _ := accessFixture(t, s)
	alice, bob, carol, dave := ids["alice-pa@example.test"], ids["bob-pa@example.test"], ids["carol-pa@example.test"], ids["dave-pa@example.test"]

	locked := s.makePage(t, ws, alice, "", "Locked", "{}")
	privatise(t, s, locked)

	// Baseline: the open page behaves exactly as before.
	for _, u := range []string{alice, bob, carol} {
		if !s.canRead(u, open) {
			t.Errorf("%s cannot read the open page", u)
		}
	}
	if s.canWrite(carol, open) {
		t.Error("a viewer writes the open page")
	}
	if !s.canWrite(bob, open) || !s.canWrite(alice, open) {
		t.Error("member/admin lost the open page")
	}

	// Nobody granted: only the owner and (below) the admin get in.
	if !s.canRead(alice, locked) || !s.canWrite(alice, locked) {
		t.Error("the owner lost their own private page")
	}
	if s.canRead(bob, locked) || s.canWrite(bob, locked) {
		t.Error("an ungranted member reads a private page")
	}
	if s.canRead(dave, locked) {
		t.Error("an outsider reads a private page")
	}

	// View grant: read yes, write no.
	grant(t, s, locked, bob, grantView)
	if !s.canRead(bob, locked) {
		t.Error("a view grant does not read")
	}
	if s.canWrite(bob, locked) {
		t.Error("a view grant writes")
	}

	// Edit grant, but on a viewer: the workspace role caps it.
	grant(t, s, locked, carol, grantEdit)
	if !s.canRead(carol, locked) {
		t.Error("a granted viewer does not read")
	}
	if s.canWrite(carol, locked) {
		t.Error("a grant promoted a viewer to editor")
	}

	// Edit grant on a member: full access.
	if _, err := s.db.Exec(`UPDATE workspace_members SET role = 'member' WHERE workspace_id = ? AND user_id = ?`, ws, carol); err != nil {
		t.Fatal(err)
	}
	if !s.canWrite(carol, locked) {
		t.Error("an edit grant does not write for a member")
	}
}

func TestPrivateInheritanceAndNarrowing(t *testing.T) {
	s := testServer(t)
	ws, _, ids, _ := accessFixture(t, s)
	alice, bob := ids["alice-pa@example.test"], ids["bob-pa@example.test"]

	root := s.makePage(t, ws, alice, "", "Root", "{}")
	privatise(t, s, root)
	child := s.makePage(t, ws, alice, root, "Child", "{}")
	nested := s.makePage(t, ws, alice, child, "Nested", "{}")
	privatise(t, s, nested)

	// A grant on the root covers the subtree…
	grant(t, s, root, bob, grantEdit)
	if !s.canRead(bob, child) || !s.canWrite(bob, child) {
		t.Error("a root grant does not cover the child")
	}
	// …until a nested private page narrows it again.
	if s.canRead(bob, nested) || s.canWrite(bob, nested) {
		t.Error("a root grant leaks through a nested private page")
	}
	grant(t, s, nested, bob, grantView)
	if !s.canRead(bob, nested) {
		t.Error("a nested view grant does not read")
	}
	if s.canWrite(bob, nested) {
		t.Error("a nested view grant writes")
	}
}

func TestPrivateHiddenFromListings(t *testing.T) {
	s := testServer(t)
	ws, _, ids, _ := accessFixture(t, s)
	alice, bob := ids["alice-pa@example.test"], ids["bob-pa@example.test"]

	locked := s.makePage(t, ws, alice, "", "Locked", "{}")
	privatise(t, s, locked)

	visible := func(u string) map[string]bool {
		rows, err := s.db.Query(`SELECT id, parent_id, workspace_id, owner_id, visibility FROM pages WHERE workspace_id = ?`, ws)
		if err != nil {
			t.Fatalf("metas: %v", err)
		}
		var all []pageMeta
		for rows.Next() {
			var m pageMeta
			if rows.Scan(&m.ID, &m.ParentID, &m.WorkspaceID, &m.OwnerID, &m.Visibility) == nil {
				all = append(all, m)
			}
		}
		rows.Close()
		out := map[string]bool{}
		for _, m := range s.filterReadable(u, all) {
			out[m.ID] = true
		}
		return out
	}
	if !visible(alice)[locked] {
		t.Error("the owner lost the page in the list")
	}
	if visible(bob)[locked] {
		t.Error("a private page leaks into a stranger's list")
	}
	grant(t, s, locked, bob, grantView)
	if !visible(bob)[locked] {
		t.Error("a granted page stays out of the list")
	}
}

func TestPrivateCollectionRowsNeedAReadableCollection(t *testing.T) {
	s := testServer(t)
	ws, _, ids, _ := accessFixture(t, s)
	alice, bob := ids["alice-pa@example.test"], ids["bob-pa@example.test"]

	col := s.makeCollection(t, ws, alice, "Base", `[]`)
	row := s.makePage(t, ws, alice, col, "Row", `{"status":"todo"}`)
	_ = row

	// Privatising the collection closes the door at the handler level: the
	// row query itself only ever hid row-private rows.
	privatise(t, s, col)
	if s.canRead(bob, col) {
		t.Error("an ungranted member reads a private collection")
	}
	// A view grant on the collection opens its (non-private) rows.
	grant(t, s, col, bob, grantView)
	if !s.canRead(bob, col) {
		t.Error("a grant does not open the collection")
	}
	list, total, err := s.collectionRowsQuery(&user{ID: bob, Name: "Bob"}, col, nil, nil, "", 100, 0)
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("granted rows = %d/%d, want 1/1", total, len(list))
	}
	// Row-private rows stay hidden from the granted reader: a grant opens the
	// collection, not other people's private rows. Fail closed.
	if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, row); err != nil {
		t.Fatal(err)
	}
	if _, total, err := s.collectionRowsQuery(&user{ID: bob, Name: "Bob"}, col, nil, nil, "", 100, 0); err != nil || total != 0 {
		t.Errorf("a private row leaks to a granted collection reader: total=%d err=%v", total, err)
	}
}

func TestBreakGlassDoesNotOpenPrivate(t *testing.T) {
	s := testServer(t)
	ws, open, ids, _ := accessFixture(t, s)
	alice, dave := ids["alice-pa@example.test"], ids["dave-pa@example.test"]

	locked := s.makePage(t, ws, alice, "", "Locked", "{}")
	privatise(t, s, locked)
	grant(t, s, locked, alice, grantView)

	expires := time.Now().UTC().Add(breakGlassTTL).Format(tsFixed)
	if _, err := s.db.Exec(`INSERT INTO break_glass (id, workspace_id, user_id, reason, created_at, expires_at)
		VALUES (?, ?, ?, 'audit review of shared material', ?, ?)`,
		newID(), ws, dave, now(), expires); err != nil {
		t.Fatalf("break glass: %v", err)
	}
	if !s.canRead(dave, open) {
		t.Error("emergency access lost the open page")
	}
	if s.canRead(dave, locked) {
		t.Error("emergency access opens a granted private page, it never opened private ones either")
	}
	if s.canWrite(dave, open) {
		t.Error("emergency access writes")
	}
}

// No migration: private pages stay private, and the account-deletion cleanup
// that matches visibility = 'private' keeps matching them.
func TestPrivateStaysPrivate(t *testing.T) {
	s := testServer(t)
	ws, _, ids, _ := accessFixture(t, s)
	alice, bob := ids["alice-pa@example.test"], ids["bob-pa@example.test"]

	secret := s.makePage(t, ws, alice, "", "Secret", "{}")
	privatise(t, s, secret)
	grant(t, s, secret, bob, grantView)
	if !s.canRead(bob, secret) {
		t.Error("a grant does not read")
	}

	// A grant whose user is gone deletes with it instead of failing startup.
	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, bob); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM page_grants WHERE page_id = ?`, secret).Scan(&n)
	if n != 0 {
		t.Errorf("%d grants survived their user", n)
	}
	// The page itself is untouched and still private.
	var vis string
	s.db.QueryRow(`SELECT visibility FROM pages WHERE id = ?`, secret).Scan(&vis)
	if vis != "private" {
		t.Errorf("visibility = %q, want private", vis)
	}
	if !s.canRead(alice, secret) {
		t.Error("the owner lost their page")
	}
}

// shareAPI exercises the share endpoints through the real router.
func TestPageShareEndpoints(t *testing.T) {
	s := testServer(t)
	ws, open, ids, cookies := accessFixture(t, s)
	aliceMail, bobMail := "alice-pa@example.test", "bob-pa@example.test"
	alice, bob := ids[aliceMail], ids[bobMail]
	aliceCookie, bobCookie := cookies[aliceMail], cookies[bobMail]

	call := func(method, url, cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		s.ServeHTTP(rec, req)
		return rec
	}

	locked := s.makePage(t, ws, alice, "", "Locked", "{}")
	privatise(t, s, locked)

	// Granting on an open page is refused: nothing to grant while all read it.
	if rec := call("POST", "/api/pages/"+open+"/shares", aliceCookie,
		`{"userId":"`+bob+`","access":"view"}`); rec.Code != 400 {
		t.Errorf("grant on open page = %d, want 400", rec.Code)
	}

	// A non-manager learns nothing — not even that the page exists.
	if rec := call("GET", "/api/pages/"+locked+"/shares", bobCookie, ""); rec.Code != 404 {
		t.Errorf("share list to stranger = %d, want 404", rec.Code)
	}
	if rec := call("POST", "/api/pages/"+locked+"/shares", bobCookie,
		`{"userId":"`+bob+`","access":"view"}`); rec.Code != 404 {
		t.Errorf("grant by stranger = %d, want 404", rec.Code)
	}

	// The owner grants, lists, and the grant takes effect…
	if rec := call("POST", "/api/pages/"+locked+"/shares", aliceCookie,
		`{"userId":"`+bob+`","access":"edit"}`); rec.Code != 200 {
		t.Fatalf("grant = %d: %s", rec.Code, rec.Body.String())
	}
	rec := call("GET", "/api/pages/"+locked+"/shares", aliceCookie, "")
	if rec.Code != 200 {
		t.Fatalf("list = %d", rec.Code)
	}
	var listed []pageGrantJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed) != 1 || listed[0].UserID != bob {
		t.Fatalf("list = %s, err = %v", rec.Body.String(), err)
	}
	if !s.canWrite(bob, locked) {
		t.Error("an endpoint grant does not write")
	}
	// …bad access values are refused…
	if rec := call("POST", "/api/pages/"+locked+"/shares", aliceCookie,
		`{"userId":"`+bob+`","access":"owner"}`); rec.Code != 400 {
		t.Errorf("bad access = %d, want 400", rec.Code)
	}
	// …and revoking closes the door again.
	if rec := call("DELETE", "/api/pages/"+locked+"/shares/"+bob, aliceCookie, ""); rec.Code != 200 {
		t.Fatalf("revoke = %d", rec.Code)
	}
	if s.canRead(bob, locked) {
		t.Error("a revoked grant still reads")
	}

	// Opening the page prunes its grants instead of leaving them armed.
	grant(t, s, locked, bob, grantView)
	if rec := call("PATCH", "/api/pages/"+locked, aliceCookie, `{"visibility":"workspace"}`); rec.Code != 200 {
		t.Fatalf("open = %d: %s", rec.Code, rec.Body.String())
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM page_grants WHERE page_id = ?`, locked).Scan(&n)
	if n != 0 {
		t.Errorf("%d grants survived opening", n)
	}
}

// An edit grant is content access, not management: its holder may read and
// write the page, but may not change visibility, publish links, move the
// page, or hand out grants.
func TestEditGrantIsNotManagement(t *testing.T) {
	s := testServer(t)
	ws, open, ids, cookies := accessFixture(t, s)
	aliceMail, bobMail := "alice-pa@example.test", "bob-pa@example.test"
	alice, bob := ids[aliceMail], ids[bobMail]
	bobCookie := cookies[bobMail]

	call := func(method, url, cookie, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		s.ServeHTTP(rec, req)
		return rec
	}

	locked := s.makePage(t, ws, alice, "", "Locked", "{}")
	privatise(t, s, locked)
	grant(t, s, locked, bob, grantEdit)

	// Lifting the restriction is refused…
	if rec := call("PATCH", "/api/pages/"+locked, bobCookie, `{"visibility":"workspace"}`); rec.Code != 403 {
		t.Errorf("visibility change by edit grant = %d, want 403", rec.Code)
	}
	// …publishing a public link is refused…
	if rec := call("POST", "/api/pages/"+locked+"/share", bobCookie, `{}`); rec.Code != 403 {
		t.Errorf("share by edit grant = %d, want 403", rec.Code)
	}
	// …and moving a page without write access on it is refused, even under a
	// parent they may write.
	secret := s.makePage(t, ws, alice, "", "Secret", "{}")
	privatise(t, s, secret)
	if _, err := s.mcpMovePage(bob, secret, open); err == nil {
		t.Error("moving a page without write access succeeded")
	}
	// Grant management stays with the owner.
	if rec := call("POST", "/api/pages/"+locked+"/shares", bobCookie,
		`{"userId":"`+alice+`","access":"view"}`); rec.Code != 404 {
		t.Errorf("grant by edit grant = %d, want 404", rec.Code)
	}
}
