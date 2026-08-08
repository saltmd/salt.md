package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The raw trail is only worth anything if it cannot be tidied up afterwards.
//
// Every rule here protects the same thing: a note was written before its author
// knew how the story ended, and that is where its value comes from. Anything
// that lets a later, wiser version of the author reach back turns the trail
// into the coherent write-up again — with timestamps in front, which is worse
// than not having one, because it looks like evidence.
//
// So these tests do not check that notes can be written. They check what
// CANNOT be done to them.

// requestAs runs one HTTP call as a signed-in person.
func requestAs(t *testing.T, s *Server, cookie, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	return rec
}

func trailOf(t *testing.T, s *Server, cookie, page string) []pageNote {
	t.Helper()
	rec := requestAs(t, s, cookie, "GET", "/api/pages/"+page+"/notes", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("reading the trail: %d %s", rec.Code, rec.Body.String())
	}
	var list []pageNote
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("trail is not a list: %v (%s)", err, rec.Body.String())
	}
	return list
}

// The central rule. There is deliberately no route and no tool that changes or
// removes ONE note — so the test asks for both in every shape a client would
// try, and insists the trail is untouched afterwards.
func TestASingleNoteCannotBeChangedOrRemoved(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "trail@example.test")
	u := &user{ID: uid, Name: "Test", TokenScope: "write"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "A task", `{}`)

	if _, err := callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"approach A: index per row"}`); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"A is out, cycles in the parent chain"}`); err != nil {
		t.Fatalf("note: %v", err)
	}
	trail := trailOf(t, s, cookie, page)
	if len(trail) != 2 {
		t.Fatalf("expected 2 notes, got %d", len(trail))
	}
	id := trail[0].ID

	// An unknown path under /api falls through to the SPA and answers 200 with
	// HTML, so the status code says nothing here. What says something is
	// whether an API ANSWERED — and, below, whether the trail moved.
	for _, attempt := range []struct{ method, path, body string }{
		{"DELETE", "/api/notes/" + id, ""},
		{"DELETE", "/api/pages/" + page + "/notes/" + id, ""},
		{"PATCH", "/api/notes/" + id, `{"body":"approach A worked fine actually"}`},
		{"PUT", "/api/pages/" + page + "/notes/" + id, `{"body":"never mind"}`},
	} {
		rec := requestAs(t, s, cookie, attempt.method, attempt.path, attempt.body)
		if strings.Contains(rec.Header().Get("Content-Type"), "json") {
			t.Errorf("%s %s is a real endpoint (%d %s) — a single note must not be editable or removable",
				attempt.method, attempt.path, rec.Code, rec.Body.String())
		}
	}

	after := trailOf(t, s, cookie, page)
	if len(after) != 2 || after[0].ID != id || after[0].Body != "approach A: index per row" {
		t.Fatalf("the trail changed under the attempts: %+v", after)
	}

	// The way a correction IS made: another note saying so.
	if _, err := callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"the first note was wrong, see the second"}`); err != nil {
		t.Fatalf("correcting note: %v", err)
	}
	if got := trailOf(t, s, cookie, page); len(got) != 3 {
		t.Fatalf("a correction should ADD, expected 3 notes, got %d", len(got))
	}
}

// Discarding the whole trail is a person's decision. An API token is a second
// key to content, and the agent whose trail it is is the last one that should
// be able to drop it — so the route is sessionOnly and the tool does not exist.
func TestOnlyAPersonMayDiscardTheWholeTrail(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "clear@example.test")
	u := &user{ID: uid, Name: "Test", TokenScope: "write"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "A task", `{}`)
	callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"something honest"}`)

	// No tool clears a trail, whatever it is called.
	for _, name := range []string{"clear_notes", "delete_notes", "notes"} {
		if _, err := callTool(t, s, u, name, `{"page_id":"`+page+`"}`); err == nil {
			t.Errorf("tool %q exists — clearing a trail must not be reachable over MCP", name)
		}
	}

	// The route refuses a token and accepts the session. A write-scoped token,
	// the strongest there is — and it still must not reach this.
	raw := newID()
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'probe', ?, 'write', ?)`, newID(), uid, tokenHash(raw), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	r := httptest.NewRequest("DELETE", "/api/pages/"+page+"/notes", nil)
	r.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatalf("an API token cleared the trail (%d) — that route is sessionOnly", rec.Code)
	}
	if got := trailOf(t, s, cookie, page); len(got) != 1 {
		t.Fatalf("the token's attempt changed the trail: %d notes", len(got))
	}

	if rec := requestAs(t, s, cookie, "DELETE", "/api/pages/"+page+"/notes", ""); rec.Code != http.StatusOK {
		t.Fatalf("a person may clear it: %d %s", rec.Code, rec.Body.String())
	}
	if got := trailOf(t, s, cookie, page); len(got) != 0 {
		t.Fatalf("trail survived the clear: %d notes", len(got))
	}
	// And the decision is recorded, so the gap is not a silence.
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'notes_cleared' AND page_id = ?`, page).Scan(&n)
	if n != 1 {
		t.Fatalf("expected the clearing to be logged once, found %d entries", n)
	}
}

// Same permission as the page, not one bit narrower: a trail that looks
// different per reader is worthless as evidence, and one that is readable to
// someone who may not read the page is a leak of the page itself.
func TestTheTrailFollowsThePagePermission(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "owner@example.test")
	otherID, otherCookie := signedIn(t, s, "stranger@example.test")
	_ = otherID
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "A task", `{}`)
	u := &user{ID: uid, Name: "Test", TokenScope: "write"}
	callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"confidential reasoning"}`)

	if rec := requestAs(t, s, otherCookie, "GET", "/api/pages/"+page+"/notes", ""); rec.Code == http.StatusOK {
		t.Fatalf("a stranger read the trail: %s", rec.Body.String())
	}
	if rec := requestAs(t, s, otherCookie, "POST", "/api/pages/"+page+"/notes", `{"body":"hello"}`); rec.Code == http.StatusOK {
		t.Fatal("a stranger appended to the trail")
	}
	if got := trailOf(t, s, cookie, page); len(got) != 1 {
		t.Fatalf("the owner's trail changed: %d notes", len(got))
	}
}

// The bridge: checking out leaves the last presence note behind. Those notes
// are already exactly what a trail entry is — short, written in the moment,
// before the ending was known — and were thrown away at check-out.
func TestCheckingOutLeavesTheLastNoteBehind(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "bridge@example.test")
	u := &user{ID: uid, Name: "Test", TokenScope: "write"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "A task", `{}`)

	if _, err := callTool(t, s, u, "working_on",
		`{"page_id":"`+page+`","agent":"claude","label":"Claude Code","note":"tidying the file index"}`); err != nil {
		t.Fatalf("check-in: %v", err)
	}
	// Checking in is presence, not a trail entry — that is the whole point of
	// keeping the two apart.
	if got := trailOf(t, s, cookie, page); len(got) != 0 {
		t.Fatalf("checking in wrote to the trail: %+v", got)
	}

	if _, err := callTool(t, s, u, "working_on", `{"page_id":"`+page+`","agent":"claude","done":true}`); err != nil {
		t.Fatalf("check-out: %v", err)
	}
	trail := trailOf(t, s, cookie, page)
	if len(trail) != 1 {
		t.Fatalf("expected the last note to be kept, got %d entries", len(trail))
	}
	if trail[0].Body != "tidying the file index" {
		t.Fatalf("kept the wrong text: %q", trail[0].Body)
	}
	if trail[0].Agent != "claude" {
		t.Fatalf("the entry lost who wrote it: %+v", trail[0])
	}

	// A note passed ON the check-out call wins — "done, and here is how it
	// went" is the most useful last line there is.
	callTool(t, s, u, "working_on", `{"page_id":"`+page+`","agent":"claude","note":"still going"}`)
	if _, err := callTool(t, s, u, "working_on",
		`{"page_id":"`+page+`","agent":"claude","note":"done, the index was double-counting logos","done":true}`); err != nil {
		t.Fatalf("check-out with a note: %v", err)
	}
	trail = trailOf(t, s, cookie, page)
	if len(trail) != 2 || trail[1].Body != "done, the index was double-counting logos" {
		t.Fatalf("the closing note is not the last entry: %+v", trail)
	}

	// Checking out of nothing must not invent an entry.
	callTool(t, s, u, "working_on", `{"page_id":"`+page+`","agent":"claude","done":true}`)
	if got := trailOf(t, s, cookie, page); len(got) != 2 {
		t.Fatalf("an empty check-out added something: %d entries", len(got))
	}
}

// Every write lands in addNote, so the cap cannot drift between the three ways
// in. A note that is silently dropped for being long is worse than a truncated
// one: the author believes it was recorded.
func TestALongNoteIsKeptShortenedNotRefused(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "long@example.test")
	u := &user{ID: uid, Name: "Test", TokenScope: "write"}
	ws := s.firstWorkspaceOf(t, uid)
	page := s.makePage(t, ws, uid, "", "A task", `{}`)

	long, _ := json.Marshal(strings.Repeat("x", maxNoteLen+500))
	if _, err := callTool(t, s, u, "note", `{"page_id":"`+page+`","text":`+string(long)+`}`); err != nil {
		t.Fatalf("a long note was refused: %v", err)
	}
	trail := trailOf(t, s, cookie, page)
	if len(trail) != 1 || len([]rune(trail[0].Body)) != maxNoteLen {
		t.Fatalf("expected one note capped at %d runes, got %d entries", maxNoteLen, len(trail))
	}
	// An empty one IS refused — there is nothing to record.
	if _, err := callTool(t, s, u, "note", `{"page_id":"`+page+`","text":"   "}`); err == nil {
		t.Fatal("an empty note was accepted")
	}
}
