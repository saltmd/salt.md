package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestBoardMoveReceiptAndAtomicHistory(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ada@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	collection := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"status","name":"Status","type":"text"}]`)
	row := s.makeRow(t, ws, uid, collection, "Task", `{"status":"open","owner":"ada"}`)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	patch := func(body string) int64 {
		t.Helper()
		rec := request("PATCH", "/api/pages/"+row, body)
		if rec.Code != 200 {
			t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			ID       string `json:"id"`
			ChangeID int64  `json:"changeId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.ID != row {
			t.Fatalf("page response was not preserved: %s", rec.Body.String())
		}
		return out.ChangeID
	}
	assertProps := func(status, owner string) {
		t.Helper()
		p, err := s.getPage(row)
		if err != nil {
			t.Fatal(err)
		}
		var props map[string]string
		json.Unmarshal(p.Props, &props)
		if props["status"] != status || props["owner"] != owner {
			t.Fatalf("props = %s, want status %s and owner %s", p.Props, status, owner)
		}
	}

	changeID := patch(`{"propsPatch":{"status":"done"}}`)
	if changeID <= 0 {
		t.Fatal("saved move returned no audit id")
	}
	var pageID, changes string
	s.db.QueryRow(`SELECT page_id, changes FROM audit_log WHERE id = ?`, changeID).Scan(&pageID, &changes)
	if pageID != row || !strings.Contains(changes, `"from":"open","to":"done"`) {
		t.Fatalf("receipt points to the wrong change: %s %s", pageID, changes)
	}
	if id := patch(`{"propsPatch":{"status":"done"}}`); id != 0 {
		t.Fatalf("unchanged value returned undo entry %d", id)
	}
	// A later, unrelated save must neither become the move's undo target nor
	// be overwritten when its earlier status change is restored.
	if id := patch(`{"propsPatch":{"owner":"bea"}}`); id <= changeID {
		t.Fatal("unrelated save did not get its own history id")
	}

	// If the history write fails, neither a move nor a revert may partially
	// commit. These triggers exercise the real database transaction boundary.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_revert BEFORE INSERT ON audit_log
		WHEN NEW.action = 'revert_change' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	revertPath := fmt.Sprintf("/api/audit/%d/revert", changeID)
	if rec := request("POST", revertPath, ""); rec.Code != 500 {
		t.Fatalf("failed revert = %d, want 500", rec.Code)
	}
	assertProps("done", "bea")
	s.db.Exec(`DROP TRIGGER reject_revert`)
	if rec := request("POST", revertPath, ""); rec.Code != 200 {
		t.Fatalf("revert: %d %s", rec.Code, rec.Body.String())
	}
	assertProps("open", "bea")

	if _, err := s.db.Exec(`CREATE TRIGGER reject_move BEFORE INSERT ON audit_log
		WHEN NEW.action = 'set_properties' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if rec := request("PATCH", "/api/pages/"+row, `{"propsPatch":{"status":"done"}}`); rec.Code != 500 {
		t.Fatalf("failed save = %d, want 500", rec.Code)
	}
	assertProps("open", "bea")
}

func TestConcurrentRevertsPreserveEachProperty(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ada@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	collection := s.makeCollection(t, ws, uid, "Tasks", `[]`)
	row := s.makeRow(t, ws, uid, collection, "Task", `{}`)
	const count = 12
	ids := make([]int64, count)
	for i := range ids {
		key := fmt.Sprintf("p%d", i)
		if _, err := s.mcpSetProperties(row, json.RawMessage(fmt.Sprintf(`{"%s":"changed"}`, key)), nil); err != nil {
			t.Fatal(err)
		}
		// MCP calls without an actor deliberately skip audit; make the exact
		// property receipt for each concurrent restoration explicitly.
		var err error
		ids[i], err = insertAudit(s.db, "human", uid, "Ada", "set_properties", row, ws, "",
			fmt.Sprintf(`{"%s":{"from":null,"to":"changed"}}`, key))
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, id := range ids {
		workers.Add(1)
		go func(id int64) {
			defer workers.Done()
			<-start
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/audit/%d/revert", id), nil)
			req.Header.Set("Cookie", cookie)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Errorf("concurrent revert: %d %s", rec.Code, rec.Body.String())
			}
		}(id)
	}
	close(start)
	workers.Wait()
	p, err := s.getPage(row)
	if err != nil {
		t.Fatal(err)
	}
	var props map[string]any
	json.Unmarshal(p.Props, &props)
	if len(props) != 0 {
		t.Fatalf("concurrent restoration lost another revert: %s", p.Props)
	}
}
