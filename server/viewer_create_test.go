package server

import "testing"

func TestViewerCannotCreateRootPageOrInstantiateTemplate(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "viewer-owner@example.test")
	viewer, cookie := signedIn(t, s, "viewer-user@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	s.addMember(t, ws, viewer, "viewer")
	col := s.makeCollection(t, ws, owner, "Records", `[]`)
	tpl := s.makeRow(t, ws, owner, col, "Brief", `{}`)
	s.db.Exec(`UPDATE pages SET is_template = 1 WHERE id = ?`, tpl)
	if !s.canRead(viewer, tpl) || s.canWrite(viewer, tpl) {
		t.Fatal("invalid viewer fixture")
	}
	for _, body := range []string{
		`{"title":"Forbidden root","workspaceId":"` + ws + `"}`,
		`{"title":"Forbidden collection","workspaceId":"` + ws + `","type":"collection"}`,
		`{"title":"Forbidden row","parentId":"` + col + `"}`,
	} {
		rr := requestAs(t, s, cookie, "POST", "/api/pages", body)
		if rr.Code != 403 {
			t.Errorf("viewer create returned %d: %s", rr.Code, rr.Body.String())
		}
	}
	if _, err := s.mcpCreateFromTemplate(&user{ID: viewer}, tpl, "Forbidden copy"); err == nil {
		t.Error("viewer instantiated a template in a read-only workspace")
	}
	if _, err := s.duplicatePage(tpl, viewer, true, false); err == nil {
		t.Error("duplicate accepted a viewer")
	}
	s.db.Exec(`UPDATE workspace_members SET role = 'member' WHERE workspace_id = ? AND user_id = ?`, ws, viewer)
	if _, err := s.mcpCreateFromTemplate(&user{ID: viewer}, tpl, "Allowed copy"); err != nil {
		t.Fatal(err)
	}
	rr := requestAs(t, s, cookie, "POST", "/api/pages", `{"title":"Allowed root","workspaceId":"`+ws+`"}`)
	if rr.Code != 200 {
		t.Fatalf("member create returned %d: %s", rr.Code, rr.Body.String())
	}
}
