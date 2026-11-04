package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// A search for two words found nothing as soon as one of them had a second
// spelling: a stem, or a sharp s swapped for ss (#107). ftsMatch put such a
// word in a group, ("release"* OR "releas"*), FTS5 refuses a group next to
// another term without an explicit AND, and the failed query was reported as
// "no results".

// Every pattern ftsMatch builds has to be one FTS5 accepts, on both indexes.
// The inputs are the shapes a person types: one word, several, words with and
// without a stem, quotes, operators FTS5 would otherwise read as syntax.
func TestFtsMatchIsValidFTS5(t *testing.T) {
	s := testServer(t)
	inputs := []string{
		"release", "release notes", "march release", "attached invoices",
		"draft notes", "error 123", "price display cockpit",
		"strasse map", // i18n-ok: a typed ss that gets an ß variant
		`say "hello"`, "a OR b", "NOT this", "NEAR(x y)", "C++ -", "(x) y*", "col:umn",
		"数据库", "数据库 迁移", "API接口", "今天，天气", "データベース", "데이터베이스", `"数据库"`,
	}
	for _, q := range inputs {
		for _, table := range []string{"chunks_fts", "pages_fts"} {
			var n int
			if err := s.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+table+` MATCH ?`, ftsMatch(q)).Scan(&n); err != nil {
				t.Errorf("%s: %q became %s, which FTS5 refuses: %v", table, q, ftsMatch(q), err)
			}
		}
	}
}

// And the search itself finds the page for every query whose words are all
// in it, through the route the browser uses.
func TestSearchFindsEveryWordCombination(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "finder@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	id := newID()
	body := `[{"type":"paragraph","content":[{"type":"text","text":"Draft of the release notes. The invoices are attached."}]}]`
	if _, err := s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility, type)
		VALUES (?, 'Release notes for March', ?, 0, ?, ?, ?, ?, 'workspace', 'doc')`,
		id, body, now(), now(), ws, uid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	s.reindexPage(id)

	for _, q := range []string{"release", "notes", "draft notes", "release notes", "march release", "attached invoices", "invoices release march"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(q), nil)
		req.Header.Set("Cookie", cookie)
		s.ServeHTTP(rec, req)
		var hits []searchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatalf("%q: %d %s", q, rec.Code, rec.Body.String())
		}
		if len(hits) != 1 || hits[0].ID != id {
			t.Errorf("%q found %d pages, want the one page", q, len(hits))
		}
	}
	// A word that is not on the page still narrows: AND, not OR.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape("release elephants"), nil)
	req.Header.Set("Cookie", cookie)
	s.ServeHTTP(rec, req)
	if rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Errorf("a word the page does not contain still found it: %s", rec.Body.String())
	}
}

// Chinese puts no spaces between its words, so the tokenizer read a whole clause
// as one word and a search reached its first characters and nothing else. Every
// character is a token now, and a word is a phrase of them.
func TestSearchFindsCJKWordsInsideASentence(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "cjk@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	id := newID()
	body := `[{"type":"paragraph","content":[{"type":"text","text":"今天天气很好，我们讨论了数据库迁移方案和上线时间。"}]},
		{"type":"paragraph","content":[{"type":"text","text":"API接口文档 needs review in 2026年。"}]},
		{"type":"paragraph","content":[{"type":"text","text":"データベースの移行を行います。데이터베이스를 옮겼습니다."}]}]`
	if _, err := s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility, type)
		VALUES (?, '项目周报模板', ?, 0, ?, ?, ?, ?, 'workspace', 'doc')`,
		id, body, now(), now(), ws, uid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	s.reindexPage(id)

	search := func(q string) []searchResult {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape(q), nil)
		req.Header.Set("Cookie", cookie)
		s.ServeHTTP(rec, req)
		var hits []searchResult
		if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
			t.Fatalf("%q: %d %s", q, rec.Code, rec.Body.String())
		}
		return hits
	}

	for _, q := range []string{
		"周报", "模板", "项目", "天气", "今天", "数据库", "迁移方案", "上线时间", "讨论", "周",
		"数据库 迁移", "模板 天气", "API接口", "接口", "api", "2026年", "needs review",
		"データベース", "移行", "베이스", "옮겼습니다",
	} {
		if hits := search(q); len(hits) != 1 {
			t.Errorf("%q: got %d hits, want the page", q, len(hits))
		}
	}

	// A word is its characters in that order and next to each other. All of these
	// characters are on the page.
	for _, q := range []string{"今气", "库数据", "迁移数据库", "天好"} {
		if hits := search(q); len(hits) != 0 {
			t.Errorf("%q: got %d hits, want none", q, len(hits))
		}
	}

	plain := func(snippet string) string { return strings.NewReplacer("\x01", "", "\x02", "").Replace(snippet) }
	hits := search("数据库")
	if len(hits) != 1 {
		t.Fatalf("got %d hits", len(hits))
	}
	if got := plain(hits[0].Snippet); !strings.Contains(got, "我们讨论了数据库迁移方案") {
		t.Errorf("excerpt does not read like the text: %q", got)
	}
	if strings.Contains(hits[0].Snippet, cjkGap) {
		t.Errorf("excerpt carries the index's gaps: %q", hits[0].Snippet)
	}
	if !strings.Contains(hits[0].Snippet, "\x01") {
		t.Errorf("excerpt marks no match: %q", hits[0].Snippet)
	}
	// A space somebody wrote is still there.
	hits = search("review")
	if len(hits) != 1 || !strings.Contains(plain(hits[0].Snippet), "API接口文档 needs review in 2026年") {
		t.Errorf("excerpt of mixed text: %+v", hits)
	}
}

// The index of an existing instance holds the old tokens until it is rebuilt,
// and a bump of ftsVersion is what rebuilds it.
func TestChineseSearchReachesAnExistingIndex(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "old-index@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO pages (id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility, type)
		VALUES (?, '项目周报模板', '[]', 0, ?, ?, ?, ?, 'workspace', 'doc')`,
		id, now(), now(), ws, uid); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// As version 3 wrote it: the title as one run of characters.
	if _, err := s.db.Exec(`INSERT INTO pages_fts (id, title, body) VALUES (?, '项目周报模板', '')`, id); err != nil {
		t.Fatalf("old row: %v", err)
	}
	s.setSetting("fts_version", "3")
	if err := s.migrateSearchIndex(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/search?q="+url.QueryEscape("周报"), nil)
	req.Header.Set("Cookie", cookie)
	s.ServeHTTP(rec, req)
	var hits []searchResult
	if err := json.Unmarshal(rec.Body.Bytes(), &hits); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(hits) != 1 {
		t.Errorf("got %d hits after the rebuild, want the page", len(hits))
	}
}

// A paragraph of Chinese has no space to cut at, so a passage ended wherever the
// byte count did: inside a character, as soon as anything of another width had
// shifted the text. Both halves then held a broken character.
func TestLongChineseParagraphIsCutBetweenCharacters(t *testing.T) {
	text := "a" + strings.Repeat("数据库迁移方案", 400)
	raw, err := json.Marshal([]map[string]any{{
		"type": "paragraph", "content": []map[string]any{{"type": "text", "text": text}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := chunkContent(raw)
	if len(chunks) < 2 {
		t.Fatalf("got %d passages, want the paragraph cut", len(chunks))
	}
	var joined strings.Builder
	for _, c := range chunks {
		if !utf8.ValidString(c.Text) {
			t.Errorf("passage %d is cut inside a character", c.Ord)
		}
		joined.WriteString(strings.TrimSpace(c.Text))
	}
	if joined.String() != text {
		t.Errorf("the passages hold %d bytes of %d", joined.Len(), len(text))
	}
}
