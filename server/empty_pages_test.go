package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBlankPageContent(t *testing.T) {
	for _, raw := range []string{`[]`, `[{"type":"paragraph","content":[],"children":[]}]`, `[{"type":"paragraph","content":[{"type":"text","text":"  \n"}]}]`} {
		if !blankPageContent([]byte(raw)) {
			t.Fatalf("expected blank: %s", raw)
		}
	}
	for _, raw := range []string{`invalid`, `{}`, `[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]`, `[{"type":"table"}]`, `[{"type":"database"}]`, `[{"type":"paragraph","content":[{"type":"pageLink"}]}]`, `[{"type":"paragraph","children":[{"type":"heading","content":[]}]}]`} {
		if blankPageContent([]byte(raw)) {
			t.Fatalf("must retain: %s", raw)
		}
	}
}

func TestDiscardOnlyUntouchedNewPage(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "empty-pages@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	create := func() (string, emptyPageBaseline) {
		id := s.makePage(t, ws, uid, "", "Untitled", `{"event":["event-a"]}`)
		p, err := s.getPage(id)
		if err != nil {
			t.Fatal(err)
		}
		return id, emptyPageBaseline{CreatedAt: p.CreatedAt, Title: p.Title, Props: p.Props, ParentID: p.ParentID, WorkspaceID: p.WorkspaceID, Visibility: p.Visibility}
	}
	discard := func(id string, b emptyPageBaseline, auth string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(b)
		r := httptest.NewRequest("POST", "/api/pages/"+id+"/discard-empty", strings.NewReader(string(raw)))
		if auth != "" {
			r.Header.Set("Cookie", auth)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	id, b := create()
	if w := discard(id, b, ""); w.Code == 200 {
		t.Fatal("unauthenticated discard")
	}
	if w := discard(id, b, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"discarded":true`) {
		t.Fatalf("discard: %d %s", w.Code, w.Body.String())
	}
	p, _ := s.getPage(id)
	if !p.Trashed {
		t.Fatal("not in recoverable trash")
	}
	if w := trashReq(t, s, "POST", "/api/pages/"+id+"/restore", cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p, _ = s.getPage(id)
	if p.Trashed {
		t.Fatal("could not restore")
	}
	for _, test := range []struct {
		name, sql string
	}{
		{"title", `UPDATE pages SET title='Named' WHERE id=?`},
		{"text", `UPDATE pages SET content='[{"type":"paragraph","content":[{"type":"text","text":"Keep"}]}]' WHERE id=?`},
		{"table", `UPDATE pages SET content='[{"type":"table"}]' WHERE id=?`},
		{"property", `UPDATE pages SET props='{"event":["event-b"]}' WHERE id=?`},
		{"description", `UPDATE pages SET description='Keep' WHERE id=?`},
		{"icon", `UPDATE pages SET icon='x' WHERE id=?`},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, b := create()
			if _, err := s.db.Exec(test.sql, id); err != nil {
				t.Fatal(err)
			}
			if w := discard(id, b, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"discarded":false`) {
				t.Fatalf("retained: %d %s", w.Code, w.Body.String())
			}
			p, _ := s.getPage(id)
			if p.Trashed {
				t.Fatal("edited page was discarded")
			}
		})
	}
	id, b = create()
	s.makePage(t, ws, uid, id, "Child", `{}`)
	if w := discard(id, b, cookie); !strings.Contains(w.Body.String(), `"discarded":false`) {
		t.Fatal("discarded parent with child")
	}
	id, b = create()
	s.makePage(t, ws, uid, "", "Linked row", `{"event":["`+id+`"]}`)
	if w := discard(id, b, cookie); !strings.Contains(w.Body.String(), `"discarded":false`) {
		t.Fatal("discarded relation target")
	}
	id, b = create()
	if _, err := s.db.Exec(`INSERT INTO favorites(user_id,page_id) VALUES (?,?)`, uid, id); err != nil {
		t.Fatal(err)
	}
	if w := discard(id, b, cookie); !strings.Contains(w.Body.String(), `"discarded":false`) {
		t.Fatal("discarded favorite")
	}
	id, b = create()
	b.CreatedAt = "wrong"
	if w := discard(id, b, cookie); !strings.Contains(w.Body.String(), `"discarded":false`) {
		t.Fatal("accepted wrong creation baseline")
	}
}
