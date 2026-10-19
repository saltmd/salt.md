package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A relation holds a LIST of target ids. An agent writing {"system": "abc"} for
// a single link is the obvious thing to do, and it used to store exactly that —
// a bare string — after which the row still grouped into its board column and
// still matched its filter (both compare loosely), while every backrelation and
// rollup passed straight over it and the chip stayed blank on the card. Ten
// rows sat that way for weeks before anybody noticed, because every surface a
// person looks at kept working.
//
// Two halves, and both are needed: writes are normalised (normalizePropValues)
// so it cannot happen again, and reads accept the bare string (relationIDs) so
// rows already written that way heal without a migration.

func TestRelationIDsAcceptsASingleIDWithoutItsList(t *testing.T) {
	for _, c := range []struct {
		name string
		in   any
		want []string
	}{
		{"list", []any{"a", "b"}, []string{"a", "b"}},
		{"single id, no list", "a", []string{"a"}},
		{"empty string is not an id", "", nil},
		{"empty list", []any{}, nil},
		{"nothing", nil, nil},
		{"number", float64(7), nil},
		{"list with a non-string in it", []any{"a", float64(7)}, []string{"a"}},
	} {
		got := relationIDs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// The read side alone, on the shape that caused the bug: a system must see the
// task pointing at it even when that task stored its relation as a bare string.
func TestBackrelationFindsARowWrittenWithoutItsList(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "shape@example.test")
	u := &user{ID: uid}
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"}]`)

	sysRow := s.makeRow(t, ws, uid, systems, "Salt", `{}`)
	// One task written the correct way, one written the way agents actually did.
	s.makeRow(t, ws, uid, tasks, "Proper", `{"system":["`+sysRow+`"]}`)
	s.makeRow(t, ws, uid, tasks, "Bare string", `{"system":"`+sysRow+`"}`)

	def := propDef{Type: "backrelation", BackrelationCollection: tasks, BackrelationProp: "system"}
	got := s.backrelationIDs(u, def, []map[string]any{{"id": sysRow, "props": map[string]any{}}})

	if len(got[0]) != 2 {
		t.Fatalf("the system should see both of its tasks, saw %d", len(got[0]))
	}
}

// The write side: what an agent sends is reshaped BEFORE it is stored, so the
// database ends up with one shape rather than two.
func TestSetPropertiesWrapsASingleValueIntoAList(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "write@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	sysRow := s.makeRow(t, ws, uid, systems, "Salt", `{}`)
	tasks := s.makeCollection(t, ws, uid, "Tasks", `[
		{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"},
		{"id":"tags","name":"Tags","type":"multiselect","options":[{"id":"bug","name":"Bug"}]},
		{"id":"status","name":"Status","type":"select","options":[{"id":"open","name":"Open"}]},
		{"id":"note","name":"Note","type":"text"}]`)
	row := s.makeRow(t, ws, uid, tasks, "A task", `{}`)

	// Every value here is written the way an agent naturally writes it.
	if _, err := s.mcpSetProperties(row, json.RawMessage(`{
		"system": "`+sysRow+`",
		"tags": "Bug",
		"status": "Open",
		"note": "just text"}`), nil); err != nil {
		t.Fatalf("set properties: %v", err)
	}

	props := s.propsOf(t, row)
	if got := props["system"]; !sameList(got, []string{sysRow}) {
		t.Errorf("relation: got %#v, want a one-element list", got)
	}
	// The multiselect is wrapped AND its name is still resolved to the option id.
	if got := props["tags"]; !sameList(got, []string{"bug"}) {
		t.Errorf("multiselect: got %#v, want [\"bug\"]", got)
	}
	// A single-valued select must NOT grow a list around it.
	if got, ok := props["status"].(string); !ok || got != "open" {
		t.Errorf("select: got %#v, want \"open\"", props["status"])
	}
	if got, ok := props["note"].(string); !ok || got != "just text" {
		t.Errorf("text: got %#v, want it untouched", props["note"])
	}
}

// An id that is already a list stays exactly as it is — normalising must not
// rewrite correct data.
func TestSetPropertiesLeavesAProperListAlone(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "list@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	systems := s.makeCollection(t, ws, uid, "Systems", `[{"id":"name","name":"Name","type":"text"}]`)
	a := s.makeRow(t, ws, uid, systems, "A", `{}`)
	b := s.makeRow(t, ws, uid, systems, "B", `{}`)
	tasks := s.makeCollection(t, ws, uid, "Tasks",
		`[{"id":"system","name":"System","type":"relation","relationCollection":"`+systems+`"}]`)
	row := s.makeRow(t, ws, uid, tasks, "A task", `{}`)

	if _, err := s.mcpSetProperties(row, json.RawMessage(`{"system":["`+a+`","`+b+`"]}`), nil); err != nil {
		t.Fatalf("set properties: %v", err)
	}
	if got := s.propsOf(t, row)["system"]; !sameList(got, []string{a, b}) {
		t.Errorf("got %#v, want both ids in order", got)
	}
}

// ---- helpers ----

func sameList(got any, want []string) bool {
	arr, ok := got.([]any)
	if !ok || len(arr) != len(want) {
		return false
	}
	for i, w := range want {
		if s, ok := arr[i].(string); !ok || s != w {
			return false
		}
	}
	return true
}

func (s *Server) firstWorkspaceOf(t *testing.T, userID string) string {
	t.Helper()
	var ws string
	if err := s.db.QueryRow(
		`SELECT workspace_id FROM workspace_members WHERE user_id = ? LIMIT 1`, userID).Scan(&ws); err != nil {
		t.Fatalf("workspace of %s: %v", userID, err)
	}
	return ws
}

func (s *Server) makeCollection(t *testing.T, ws, userID, title, schemaJSON string) string {
	t.Helper()
	id := s.makePage(t, ws, userID, "", title, `{}`)
	// pages.type is what the sidebar, the graph and canRead branch on; the
	// collections row alone makes a database only half of one.
	if _, err := s.db.Exec(`UPDATE pages SET type = 'collection' WHERE id = ?`, id); err != nil {
		t.Fatalf("mark as collection: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, '[]')`,
		id, schemaJSON); err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	return id
}

func (s *Server) makeRow(t *testing.T, ws, userID, parent, title, propsJSON string) string {
	t.Helper()
	return s.makePage(t, ws, userID, parent, title, propsJSON)
}

func (s *Server) makePage(t *testing.T, ws, userID, parent, title, propsJSON string) string {
	t.Helper()
	id := newID()
	var p any
	if parent != "" {
		p = parent
	}
	if _, err := s.db.Exec(`INSERT INTO pages
		(id, parent_id, title, icon, content, props, position, created_at, updated_at, workspace_id, owner_id, visibility)
		VALUES (?, ?, ?, '', '[]', ?, 0, ?, ?, ?, ?, 'workspace')`,
		id, p, title, propsJSON, now(), now(), ws, userID); err != nil {
		t.Fatalf("insert page %q: %v", title, err)
	}
	return id
}

func (s *Server) propsOf(t *testing.T, pageID string) map[string]any {
	t.Helper()
	var raw string
	if err := s.db.QueryRow(`SELECT props FROM pages WHERE id = ?`, pageID).Scan(&raw); err != nil {
		t.Fatalf("props of %s: %v", pageID, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("props of %s are not JSON: %v", pageID, err)
	}
	return m
}

// addMember puts an account into a workspace with a given role. Needed because
// the FIRST account is an admin everywhere, and an admin bypasses the private
// ancestor rule — so a permission test written with it proves nothing.
func (s *Server) addMember(t *testing.T, ws, userID, role string) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)`,
		ws, userID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
}

// A relation value has to be a page id, and an agent writes one of two other
// things just as naturally: the title it read somewhere, or the object a read
// handed back. Neither was touched — the option-name resolution covers only
// properties that HAVE options — so the title went into the column as a title,
// `query_rows` reported it back verbatim (the data looked right in a dump), and
// the cell rendered as a chip with no text at all. Twelve of the thirteen rows
// in the report that found this were empty, and the one linked through the
// interface was the only one that read correctly.
func TestRelationValuesAreStoredAsIds(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "relwrite@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	// A self-relation: the property points at the collection it lives in, so the
	// collection has to exist before its schema can name it.
	db := s.makeCollection(t, ws, uid, "Рилсы", `[]`)
	if _, err := s.mcpUpdateSchema(db, json.RawMessage(`[
		{"id":"based","name":"Основано на","type":"relation","relationCollection":"`+db+`"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}

	memnoe := s.makeRow(t, ws, uid, db, "Мемное", `{}`)
	// Two rows with one title: that title names neither of them, so it must not
	// be resolved to whichever came first.
	s.makeRow(t, ws, uid, db, "Дубль", `{}`)
	s.makeRow(t, ws, uid, db, "Дубль", `{}`)

	byTitle := s.makeRow(t, ws, uid, db, "Заголовок", `{}`)
	byObject := s.makeRow(t, ws, uid, db, "Объект", `{}`)
	byId := s.makeRow(t, ws, uid, db, "Id", `{}`)
	ambiguous := s.makeRow(t, ws, uid, db, "Неоднозначно", `{}`)
	unknown := s.makeRow(t, ws, uid, db, "Неизвестно", `{}`)

	for _, c := range []struct {
		row  string
		raw  string
		want []string
	}{
		{byId, `{"based":["` + memnoe + `"]}`, []string{memnoe}},
		{byTitle, `{"based":"Мемное"}`, []string{memnoe}},
		{byObject, `{"based":{"id":"` + memnoe + `","title":"Мемное"}}`, []string{memnoe}},
		// Nothing to resolve: left exactly as sent, so the cell can show what it
		// holds instead of nothing.
		{ambiguous, `{"based":"Дубль"}`, []string{"Дубль"}},
		{unknown, `{"based":["Нет такого"]}`, []string{"Нет такого"}},
	} {
		if _, err := s.mcpSetProperties(c.row, json.RawMessage(c.raw), nil); err != nil {
			t.Fatalf("set %s: %v", c.raw, err)
		}
		if got := s.propsOf(t, c.row)["based"]; !sameList(got, c.want) {
			t.Errorf("%s → %#v, want %#v", c.raw, got, c.want)
		}
	}
}

// A title resolves only among rows the writer may read. Resolving it against
// every row would answer a question the writer never asked: writing the title
// of somebody else's private row would come back stored as an id, telling the
// writer that such a row exists.
func TestRelationTitleNeedsAReadableRow(t *testing.T) {
	s := testServer(t)
	owner, _ := signedIn(t, s, "relowner@example.test")
	ws := s.firstWorkspaceOf(t, owner)
	member, _ := signedIn(t, s, "relmember@example.test")
	s.addMember(t, ws, member, "member")

	db := s.makeCollection(t, ws, owner, "Targets", `[]`)
	if _, err := s.mcpUpdateSchema(db, json.RawMessage(`[
		{"id":"link","name":"Link","type":"relation","relationCollection":"`+db+`"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}
	secret := s.makeRow(t, ws, owner, db, "Секрет", `{}`)
	if _, err := s.db.Exec(`UPDATE pages SET visibility = 'private' WHERE id = ?`, secret); err != nil {
		t.Fatal(err)
	}
	open := s.makeRow(t, ws, owner, db, "Открытая", `{}`)

	writer := s.makeRow(t, ws, member, db, "Писатель", `{}`)
	// The member may not read the private row: its title stays exactly as
	// sent, so nothing about that row leaks through the write.
	if _, err := s.mcpSetProperties(writer, json.RawMessage(`{"link":"Секрет"}`), &user{ID: member}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.propsOf(t, writer)["link"]; !sameList(got, []string{"Секрет"}) {
		t.Errorf("private title resolved for a non-reader: %#v", got)
	}
	// The open row resolves for the member, and the private one by exact id
	// (an id names its row outright and proves nothing by matching).
	if _, err := s.mcpSetProperties(writer, json.RawMessage(`{"link":"Открытая"}`), &user{ID: member}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.propsOf(t, writer)["link"]; !sameList(got, []string{open}) {
		t.Errorf("open title did not resolve: %#v", got)
	}
	if _, err := s.mcpSetProperties(writer, json.RawMessage(`{"link":"`+secret+`"}`), &user{ID: member}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.propsOf(t, writer)["link"]; !sameList(got, []string{secret}) {
		t.Errorf("exact id did not pass through: %#v", got)
	}
	// The owner reads their own private row, so the title resolves for them.
	ownerWriter := s.makeRow(t, ws, owner, db, "Владелец", `{}`)
	if _, err := s.mcpSetProperties(ownerWriter, json.RawMessage(`{"link":"Секрет"}`), &user{ID: owner}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := s.propsOf(t, ownerWriter)["link"]; !sameList(got, []string{secret}) {
		t.Errorf("owner's own title did not resolve: %#v", got)
	}
}

// PATCH normalizes relation values the same way create and set_properties do:
// a title written into a propsPatch comes back stored as the id.
func TestPatchNormalizesRelationTitles(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "relpatch@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	db := s.makeCollection(t, ws, uid, "Targets", `[]`)
	if _, err := s.mcpUpdateSchema(db, json.RawMessage(`[
		{"id":"link","name":"Link","type":"relation","relationCollection":"`+db+`"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}
	target := s.makeRow(t, ws, uid, db, "Цель", `{}`)
	row := s.makeRow(t, ws, uid, db, "Строка", `{}`)

	patch := func(body string, wantCode int) {
		t.Helper()
		r := httptest.NewRequest("PATCH", "/api/pages/"+row, strings.NewReader(body))
		r.Header.Set("Cookie", cookie)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, r)
		if rec.Code != wantCode {
			t.Fatalf("patch %s: %d, want %d", body, rec.Code, wantCode)
		}
	}
	patch(`{"propsPatch":{"link":"Цель"}}`, http.StatusOK)
	if got := s.propsOf(t, row)["link"]; !sameList(got, []string{target}) {
		t.Errorf("PATCH title not resolved: %#v", got)
	}
}
