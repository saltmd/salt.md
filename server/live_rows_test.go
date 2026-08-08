package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What a second screen is told when a row comes and goes.
//
// He reported it plainly: live updates do not arrive when something is created
// or deleted. Creating and updating had always named the database;
// trashing and restoring only said "the page list changed" — and a bare row is
// deliberately NOT in that list (the tens-of-thousands argument), so the event
// arrived, the tree reloaded, and looked exactly the same. The card stayed on
// the board until somebody pressed reload.
func TestTrashingAndRestoringANameTheirDatabase(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "live@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"status","name":"Status","type":"text"}]`)
	row := s.makeRow(t, ws, uid, db, "A task", `{}`)

	// Listen the way a browser does.
	events := s.events.subscribe(uid)
	defer s.events.unsubscribe(events)

	drain := func() []string {
		var out []string
		for {
			select {
			case m := <-events:
				out = append(out, m)
			default:
				return out
			}
		}
	}

	call := func(method, path string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(""))
		r.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
	}

	named := func(msgs []string) bool {
		for _, m := range msgs {
			if strings.Contains(m, `"type":"rows"`) && strings.Contains(m, db) {
				return true
			}
		}
		return false
	}

	drain()
	call("DELETE", "/api/pages/"+row)
	if !named(drain()) {
		t.Error("trashing a row never named its database — an open board keeps the card")
	}

	call("POST", "/api/pages/"+row+"/restore")
	if !named(drain()) {
		t.Error("restoring a row never named its database — it stays missing on an open board")
	}
}

// Changing the shape of a database is a change to the database. An open view on
// a second screen was still drawing the old columns.
func TestChangingTheSchemaNamesTheDatabase(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "schema@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"a","name":"A","type":"text"}]`)

	events := s.events.subscribe(uid)
	defer s.events.unsubscribe(events)
	for len(events) > 0 {
		<-events
	}

	body := `{"schema":[{"id":"a","name":"A","type":"text"},{"id":"b","name":"B","type":"text"}],"views":[{"id":"v1","name":"Table","type":"table"}]}`
	r := httptest.NewRequest("PUT", "/api/collections/"+db, strings.NewReader(body))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("put: %d %s", rec.Code, rec.Body.String())
	}

	found := false
	for len(events) > 0 {
		if m := <-events; strings.Contains(m, `"type":"rows"`) && strings.Contains(m, db) {
			found = true
		}
	}
	if !found {
		t.Error("a schema change never named the database — an open view keeps the old columns")
	}
}
