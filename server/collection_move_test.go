package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollectionMoveCheckAndActivity(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "ada@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	source := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"status","name":"Status","type":"select","options":[{"id":"todo"},{"id":"done"}]},
		{"id":"labels","name":"Labels","type":"multiselect","options":[{"id":"a"},{"id":"b"}]},
		{"id":"estimate","name":"Estimate","type":"number"},
		{"id":"notes","name":"Notes","type":"text"},
		{"id":"flag","name":"Flag","type":"checkbox"},
		{"id":"empty","name":"Empty","type":"text"}]`)
	same := s.makeCollection(t, ws, uid, "Same", `[
		{"id":"status","name":"Status","type":"select","options":[{"id":"todo"},{"id":"done"}]},
		{"id":"labels","name":"Labels","type":"multiselect","options":[{"id":"a"},{"id":"b"}]},
		{"id":"estimate","name":"Estimate","type":"number"},
		{"id":"notes","name":"Notes","type":"text"}]`)
	other := s.makeCollection(t, ws, uid, "Other", `[
		{"id":"status","name":"Status","type":"select","options":[{"id":"open"}]},
		{"id":"labels","name":"Labels","type":"multiselect","options":[{"id":"a"}]},
		{"id":"estimate","name":"Estimate","type":"text"}]`)
	projects := s.makeCollection(t, ws, uid, "Projects", `[
		{"id":"tasks","name":"Tasks","type":"relation","relationCollection":"`+source+`"},
		{"id":"other","name":"Elsewhere","type":"relation","relationCollection":"`+other+`"}]`)
	card := s.makeRow(t, ws, uid, source, "Card", `{"status":"done","labels":["a","b"],"estimate":3,"notes":"x","flag":false,"empty":""}`)
	s.makeRow(t, ws, uid, projects, "P1", `{"tasks":["`+card+`"]}`)
	s.makeRow(t, ws, uid, projects, "P2", `{"tasks":["zzz","`+card+`"]}`)
	s.makeRow(t, ws, uid, projects, "P3", `{"other":["`+card+`"]}`)

	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	check := func(dest string) moveCheck {
		t.Helper()
		rec := request("GET", "/api/collections/"+source+"/rows/move?rowId="+card+"&collectionId="+dest, "")
		if rec.Code != 200 {
			t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
		}
		var out moveCheck
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Matching properties carry over; empty values and an unticked box do not count.
	got := check(same)
	if len(got.Hidden) != 0 {
		t.Fatalf("hidden with a matching destination = %v", got.Hidden)
	}
	// Only P1 and P2 point at the card through a relation targeting Tasks.
	if len(got.Relations) != 1 || got.Relations[0].Collection != "Projects" || got.Relations[0].Property != "Tasks" || got.Relations[0].Rows != 2 {
		t.Fatalf("relations = %+v", got.Relations)
	}
	// An unknown option, a partly unknown multi-select, another type, a missing property.
	got = check(other)
	if strings.Join(got.Hidden, ",") != "Status,Labels,Estimate,Notes" {
		t.Fatalf("hidden = %v", got.Hidden)
	}
	// The check changes nothing.
	var parent string
	s.db.QueryRow(`SELECT parent_id FROM pages WHERE id = ?`, card).Scan(&parent)
	if parent != source {
		t.Fatalf("check moved the card to %s", parent)
	}
	if rec := request("GET", "/api/collections/"+source+"/rows/move?rowId="+card+"&collectionId="+source, ""); rec.Code != 400 {
		t.Fatalf("same collection check: %d", rec.Code)
	}

	// A move and its undo each leave an activity entry naming the person.
	move := func(from, to, extra string) map[string]float64 {
		t.Helper()
		rec := request("POST", "/api/collections/"+from+"/rows/move", `{"rowId":"`+card+`","collectionId":"`+to+`"`+extra+`}`)
		if rec.Code != 200 {
			t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
		}
		var out map[string]float64
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	receipt := move(source, same, "")
	restore := func(r map[string]float64) string {
		b, _ := json.Marshal(map[string]float64{"position": r["fromPosition"], "expectedPosition": r["position"],
			"expectedPlacement": r["placement"], "placement": r["fromPlacement"]})
		return `,"restore":` + string(b)
	}
	move(same, source, restore(receipt))
	rows, err := s.db.Query(`SELECT actor_id, page_id, detail FROM audit_log WHERE action = 'move_to_collection' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var actor, page, detail string
		rows.Scan(&actor, &page, &detail)
		if actor != uid || page != card {
			t.Fatalf("entry by %s on %s", actor, page)
		}
		details = append(details, detail)
	}
	if strings.Join(details, " | ") != "Tasks → Same | Same → Tasks" {
		t.Fatalf("activity = %v", details)
	}

	// An old receipt cannot undo a newer move that put the card back on the
	// same position: moved into an empty collection, away, and back again.
	empty := s.makeCollection(t, ws, uid, "Empty", `[]`)
	first := move(source, empty, "")
	move(empty, same, "")
	move(same, empty, "")
	rec := request("POST", "/api/collections/"+empty+"/rows/move", `{"rowId":"`+card+`","collectionId":"`+source+`"`+restore(first)+`}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "collection_move_stale") {
		t.Fatalf("stale receipt: %d %s", rec.Code, rec.Body.String())
	}
	move(empty, source, "")

	// Two moves of the same card, undone newest first: both go back.
	m1 := move(source, same, "")
	m2 := move(same, empty, "")
	move(empty, same, restore(m2))
	move(same, source, restore(m1))
	var parentNow string
	s.db.QueryRow(`SELECT parent_id FROM pages WHERE id = ?`, card).Scan(&parentNow)
	if parentNow != source {
		t.Fatalf("after undoing both moves the card is in %s", parentNow)
	}

	// The same through an ordinary page move: away and back with PATCH.
	second := move(source, same, "")
	for _, parent := range []string{other, same} {
		if rec := request("PATCH", "/api/pages/"+card, `{"parentId":"`+parent+`"}`); rec.Code != 200 {
			t.Fatalf("reparent: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec = request("POST", "/api/collections/"+same+"/rows/move", `{"rowId":"`+card+`","collectionId":"`+source+`"`+restore(second)+`}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "collection_move_stale") {
		t.Fatalf("receipt after a page move: %d %s", rec.Code, rec.Body.String())
	}
	move(same, source, "")

	// A move confirmed against an outdated check is refused.
	stale, _ := json.Marshal(moveCheck{Hidden: []string{}, Relations: []moveCheckRelation{}})
	rec = request("POST", "/api/collections/"+source+"/rows/move", `{"rowId":"`+card+`","collectionId":"`+other+`","check":`+string(stale)+`}`)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "collection_move_changed") {
		t.Fatalf("outdated check: %d %s", rec.Code, rec.Body.String())
	}
	current, _ := json.Marshal(check(other))
	move(source, other, `,"check":`+string(current))
	move(other, source, "")
}

func TestCollectionMovePrivacy(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "owner@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	secret := s.makeCollection(t, ws, uid, "Secret plans", `[]`)
	shared := s.makeCollection(t, ws, uid, "Shared", `[]`)
	if _, err := s.db.Exec(`UPDATE pages SET visibility='private', owner_id=? WHERE id=?`, uid, secret); err != nil {
		t.Fatal(err)
	}
	card := s.makeRow(t, ws, uid, secret, "Card", `{}`)
	req := httptest.NewRequest("POST", "/api/collections/"+secret+"/rows/move", strings.NewReader(`{"rowId":"`+card+`","collectionId":"`+shared+`"}`))
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	var detail string
	s.db.QueryRow(`SELECT detail FROM audit_log WHERE action='move_to_collection' AND page_id=?`, card).Scan(&detail)
	if strings.Contains(detail, "Secret") || detail != "a private collection → Shared" {
		t.Fatalf("activity names a private collection: %q", detail)
	}

	// Row events of a private collection reach its readers only.
	other, _ := signedIn(t, s, "member@example.com")
	if _, err := s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'member')`, ws, other); err != nil {
		t.Fatal(err)
	}
	member := s.events.subscribeAs(&user{ID: other})
	defer s.events.unsubscribe(member)
	owner := s.events.subscribeAs(&user{ID: uid})
	defer s.events.unsubscribe(owner)
	got := func(ch chan string) bool {
		seen := false
		for {
			select {
			case e := <-ch:
				seen = seen || strings.Contains(e, secret)
			default:
				return seen
			}
		}
	}
	got(member)
	got(owner)
	s.rowsChanged(secret)
	if got(member) || !got(owner) {
		t.Fatal("a private collection's row event reached a member who cannot read it")
	}
}

func TestMoveCheckFindsRelationsInOtherWorkspaces(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "multi@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	second := newID()
	if _, err := s.db.Exec(`INSERT INTO workspaces(id,name,created_at) VALUES(?,?,?)`, second, "Second", now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'admin')`, second, uid); err != nil {
		t.Fatal(err)
	}
	source := s.makeCollection(t, ws, uid, "Tasks", `[]`)
	dest := s.makeCollection(t, ws, uid, "Done", `[]`)
	elsewhere := s.makeCollection(t, second, uid, "Clients", `[{"id":"tasks","name":"Tasks","type":"relation","relationCollection":"`+source+`"}]`)
	card := s.makeRow(t, ws, uid, source, "Card", `{}`)
	s.makeRow(t, second, uid, elsewhere, "Acme", `{"tasks":["`+card+`"]}`)
	req := httptest.NewRequest("GET", "/api/collections/"+source+"/rows/move?rowId="+card+"&collectionId="+dest, nil)
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out moveCheck
	json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != 200 || len(out.Relations) != 1 || out.Relations[0].Collection != "Clients" {
		t.Fatalf("relations across workspaces: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMoveCheckInputsFollowEveryInput(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "inputs@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	source := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"status","name":"Status","type":"select","options":[{"id":"a"}]}]`)
	dest := s.makeCollection(t, ws, uid, "Done", `[{"id":"status","name":"Status","type":"select","options":[{"id":"a"}]}]`)
	projects := s.makeCollection(t, ws, uid, "Projects", `[{"id":"tasks","name":"Tasks","type":"relation","relationCollection":"`+source+`"}]`)
	card := s.makeRow(t, ws, uid, source, "Card", `{"status":"a"}`)
	digest := func() string {
		t.Helper()
		d, err := moveCheckInputs(s.db, uid, source, card, dest)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	steps := []string{
		`UPDATE collections SET schema='[{"id":"status","name":"Status","type":"select","options":[{"id":"b"}]}]' WHERE page_id='` + dest + `'`,
		`UPDATE pages SET props='{"status":"b"}' WHERE id='` + card + `'`,
		`INSERT INTO pages (id, title, parent_id, workspace_id, props, created_at, updated_at) VALUES ('rel-row', 'Launch', '` + projects + `', '` + ws + `', '{"tasks":["` + card + `"]}', '2026-01-01', '2026-01-01')`,
		`UPDATE pages SET title='Renamed projects' WHERE id='` + projects + `'`,
		`UPDATE pages SET visibility='private', owner_id='someone-else' WHERE id='rel-row'`,
		`UPDATE workspace_members SET role='viewer' WHERE user_id='` + uid + `'`,
		`UPDATE workspaces SET agent_access='closed' WHERE id='` + ws + `'`,
	}
	before := digest()
	if again := digest(); again != before {
		t.Fatal("the digest is not stable")
	}
	for _, step := range steps {
		if _, err := s.db.Exec(step); err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		after := digest()
		if after == before {
			t.Fatalf("the digest missed: %s", step)
		}
		before = after
	}
}

func TestMoveCheckRefusesWhatTheMoveRefuses(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "refuse@example.com")
	ws := s.firstWorkspaceOf(t, uid)
	second := newID()
	s.db.Exec(`INSERT INTO workspaces(id,name,created_at) VALUES(?,?,?)`, second, "Second", now())
	s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(?,?,'admin')`, second, uid)
	source := s.makeCollection(t, ws, uid, "Tasks", `[]`)
	elsewhere := s.makeCollection(t, second, uid, "Elsewhere", `[]`)
	card := s.makeRow(t, ws, uid, source, "Card", `{}`)
	inside := s.makeCollection(t, ws, uid, "Inside", `[]`)
	s.db.Exec(`UPDATE pages SET parent_id=? WHERE id=?`, card, inside)
	check := func(dest string) int {
		req := httptest.NewRequest("GET", "/api/collections/"+source+"/rows/move?rowId="+card+"&collectionId="+dest, nil)
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := check(elsewhere); code != 404 {
		t.Fatalf("a destination in another workspace: %d, want 404 like the move", code)
	}
	if code := check(inside); code != 400 {
		t.Fatalf("a destination inside the card: %d, want 400 like the move", code)
	}
}
