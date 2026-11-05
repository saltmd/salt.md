package server

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const testCommentAnchor = `{"gen":0,"quote":"selected text","from":{"type":{"client":1,"clock":0},"item":{"client":1,"clock":1}},"to":{"type":{"client":1,"clock":0},"item":{"client":1,"clock":2}}}`

func TestInlineCommentSurvivesPageReplacement(t *testing.T) {
	s, tab, _ := newEditorTab(t)
	anchor := testCommentAnchor
	send := func(cookie, payload string) int {
		req, _ := http.NewRequest("POST", tab.srv.URL+"/api/pages/"+tab.page+"/comments", strings.NewReader(payload))
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		return res.StatusCode
	}
	if code := send(tab.cookie, `{"body":"feedback","anchor":`+anchor+`}`); code != http.StatusOK {
		t.Fatalf("create: %d", code)
	}
	if code := send("", `{"body":"forbidden","anchor":`+anchor+`}`); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", code)
	}
	if code, _ := tab.patch("", "replacement"); code != http.StatusOK {
		t.Fatalf("replace: %d", code)
	}
	if code := send(tab.cookie, `{"body":"stale","anchor":`+anchor+`}`); code != http.StatusConflict {
		t.Fatalf("stale anchor: %d", code)
	}
	if code := send(tab.cookie, `{"body":"page comment"}`); code != http.StatusOK {
		t.Fatalf("page comment: %d", code)
	}
	comments, err := s.pageComments(tab.page)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments after replacement: %v %v", comments, err)
	}
	var saved struct {
		Gen   int    `json:"gen"`
		Quote string `json:"quote"`
	}
	if err := json.Unmarshal(comments[0].Anchor, &saved); err != nil || saved.Gen != 0 || saved.Quote != "selected text" {
		t.Fatalf("anchor: %s %v", comments[0].Anchor, err)
	}
	if len(comments[1].Anchor) != 0 {
		t.Fatalf("page comment has an anchor: %s", comments[1].Anchor)
	}
}

func TestInlineCommentValidation(t *testing.T) {
	s, tab, _ := newEditorTab(t)
	bad := []string{
		`{}`, `[]`, `true`,
		strings.Replace(testCommentAnchor, `"gen":0`, `"gen":null`, 1),
		strings.Replace(testCommentAnchor, `"gen":0`, `"gen":-1`, 1),
		strings.Replace(testCommentAnchor, `"gen":0`, `"gen":0.5`, 1),
		strings.Replace(testCommentAnchor, `"gen":0`, `"gen":9007199254740992`, 1),
		strings.Replace(testCommentAnchor, `selected text`, `   `, 1),
		strings.Replace(testCommentAnchor, `selected text`, strings.Repeat("x", 2001), 1),
		strings.Replace(testCommentAnchor, `selected text`, strings.Repeat("😀", 2001), 1),
		strings.Replace(testCommentAnchor, `"quote":`, `"extra":"`+strings.Repeat("x", 32000)+`","quote":`, 1),
	}
	for _, position := range []string{
		`{}`, `null`, `[]`, `{"type":{}}`,
		`{"type":{"client":1,"clock":-1}}`,
		`{"type":{"client":1.5,"clock":0}}`,
		`{"type":{"client":9007199254740992,"clock":0}}`,
		`{"tname":""}`, `{"tname":1}`,
		`{"tname":"blocknote","item":{}}`,
		`{"tname":"blocknote","assoc":"left"}`,
		`{"tname":"blocknote","type":{"client":1,"clock":0}}`,
	} {
		bad = append(bad, `{"gen":0,"quote":"text","from":`+position+`,"to":`+position+`}`)
	}
	for i, anchor := range bad {
		rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", `{"body":"feedback","anchor":`+anchor+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("invalid anchor %d: %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if comments, err := s.pageComments(tab.page); err != nil || len(comments) != 0 {
		t.Fatalf("invalid anchors wrote comments: %v %v", comments, err)
	}
	// JavaScript and Go both count Unicode code points, not UTF-16 units.
	anchor := strings.Replace(testCommentAnchor, "selected text", strings.Repeat("😀", 2000), 1)
	if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", `{"body":"feedback","anchor":`+anchor+`}`); rec.Code != http.StatusOK {
		t.Fatalf("2000 Unicode characters: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInlineCommentsPreservePermissionsAndAttribution(t *testing.T) {
	f := newScopeFixture(t)
	page := f.s.makePage(t, f.team, f.owner, "", "Document", `{}`)
	other := f.s.makePage(t, f.other, f.owner, "", "Other document", `{}`)
	payload := `{"body":"feedback","agent":"codex","label":"Review","anchor":` + testCommentAnchor + `}`
	if rec := f.bearer(f.teamTok, "POST", "/api/pages/"+other+"/comments", payload); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-workspace comment: %d", rec.Code)
	}
	if _, err := f.s.db.Exec(`UPDATE api_tokens SET scope = 'read' WHERE token_hash = ?`, tokenHash(f.teamTok)); err != nil {
		t.Fatal(err)
	}
	if rec := f.bearer(f.teamTok, "POST", "/api/pages/"+page+"/comments", payload); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only comment: %d", rec.Code)
	}
	if rec := f.bearer(f.openTok, "POST", "/api/pages/"+page+"/comments", payload); rec.Code != http.StatusOK {
		t.Fatalf("agent comment: %d %s", rec.Code, rec.Body.String())
	}
	if rec := requestAs(t, f.s, f.cookie, "POST", "/api/pages/"+page+"/comments", payload); rec.Code != http.StatusOK {
		t.Fatalf("browser comment: %d %s", rec.Code, rec.Body.String())
	}
	comments, err := f.s.pageComments(page)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments: %v %v", comments, err)
	}
	if comments[0].Agent != "codex" || comments[0].AgentLabel != "Review" || len(comments[0].Anchor) == 0 || comments[0].AuthorID != f.owner {
		t.Fatalf("agent attribution or anchor lost: %+v", comments[0])
	}
	if comments[1].Agent != "" || len(comments[1].Anchor) == 0 {
		t.Fatalf("browser claimed an agent or lost its anchor: %+v", comments[1])
	}
}

func TestInlineCommentMigrationPreservesOldComments(t *testing.T) {
	s, tab, u := newEditorTab(t)
	if _, err := s.db.Exec(`INSERT INTO comments(id,page_id,author_id,author_name,body,created_at) VALUES('legacy',?,?, 'Test','old feedback',?)`, tab.page, u.ID, now()); err != nil {
		t.Fatal(err)
	}
	// Reproduce an old database that has comments but no anchor column.
	if _, err := s.db.Exec(`ALTER TABLE comments DROP COLUMN anchor`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		db, err := openDB(filepath.Join(s.dataDir, DBFile))
		if err != nil {
			t.Fatalf("migration %d: %v", i, err)
		}
		var body, anchor string
		err = db.QueryRow(`SELECT body,anchor FROM comments WHERE id = 'legacy'`).Scan(&body, &anchor)
		db.Close()
		if err != nil || body != "old feedback" || anchor != "" {
			t.Fatalf("migration %d changed the old comment: %q %q %v", i, body, anchor, err)
		}
	}
}

func TestInlineCommentStaleAfterAgentWriteAndRestore(t *testing.T) {
	s, tab, u := newEditorTab(t)
	for _, restore := range []bool{false, true} {
		var gen int64
		if err := s.db.QueryRow(`SELECT doc_gen FROM pages WHERE id = ?`, tab.page).Scan(&gen); err != nil {
			t.Fatal(err)
		}
		anchor := strings.Replace(testCommentAnchor, `"gen":0`, `"gen":`+strconv.FormatInt(gen, 10), 1)
		payload := `{"body":"feedback","anchor":` + anchor + `}`
		if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", payload); rec.Code != http.StatusOK {
			t.Fatalf("create before replacement: %d %s", rec.Code, rec.Body.String())
		}
		if !restore {
			if _, err := callTool(t, s, u, "write_content", `{"page_id":"`+tab.page+`","markdown":"replacement","mode":"replace"}`); err != nil {
				t.Fatal(err)
			}
		} else {
			s.snapshotRevision(tab.page, u.ID, u.Name)
			var revision string
			if err := s.db.QueryRow(`SELECT id FROM page_revisions WHERE page_id = ? LIMIT 1`, tab.page).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/revisions/"+revision+"/restore", ""); rec.Code != http.StatusOK {
				t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
			}
		}
		if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", payload); rec.Code != http.StatusConflict {
			t.Fatalf("stale after replacement (restore=%v): %d", restore, rec.Code)
		} else {
			var problem struct{ Code, Error string }
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil || problem.Code != "comment_anchor_stale" || problem.Error != "The page changed. Select the text again." {
				t.Fatalf("stale anchor must carry a translatable code and readable fallback: %s (%v)", rec.Body.String(), err)
			}
		}
	}
	if comments, err := s.pageComments(tab.page); err != nil || len(comments) != 2 {
		t.Fatalf("replacement removed comments or wrote a stale one: %v %v", comments, err)
	}
}

func TestInlineCommentMCPQuotesWithoutPositions(t *testing.T) {
	s, tab, u := newEditorTab(t)
	read := func() []map[string]json.RawMessage {
		t.Helper()
		out, err := callTool(t, s, u, "comments", `{"page_id":"`+tab.page+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		start, end := strings.Index(out, "["), strings.LastIndex(out, "]")
		var comments []map[string]json.RawMessage
		if start < 0 || end < start || !strings.Contains(out, "BEGIN UNTRUSTED CONTENT") {
			t.Fatalf("comments must remain a marked JSON list: %s", out)
		}
		if err := json.Unmarshal([]byte(out[start:end+1]), &comments); err != nil {
			t.Fatal(err)
		}
		return comments
	}
	if comments := read(); comments == nil || len(comments) != 0 {
		t.Fatalf("empty comments must stay []: %v", comments)
	}
	const quote = "A quoted \"passage\" with 中文 and 😀"
	var anchor map[string]any
	if err := json.Unmarshal([]byte(testCommentAnchor), &anchor); err != nil {
		t.Fatal(err)
	}
	anchor["quote"] = quote
	payload, err := json.Marshal(map[string]any{"body": "feedback", "anchor": anchor})
	if err != nil {
		t.Fatal(err)
	}
	if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", string(payload)); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := callTool(t, s, u, "comments", `{"page_id":"`+tab.page+`","action":"add","body":"page feedback"}`); err != nil {
		t.Fatal(err)
	}
	// Both attached and detached comments must still explain their passage.
	for _, replace := range []bool{false, true} {
		if replace {
			if code, _ := tab.patch("", "replacement"); code != http.StatusOK {
				t.Fatalf("replace: %d", code)
			}
		}
		comments := read()
		if len(comments) != 2 {
			t.Fatalf("comments: %v", comments)
		}
		for _, c := range comments {
			for _, key := range []string{"anchor", "from", "to", "gen"} {
				if _, ok := c[key]; ok {
					t.Fatalf("MCP leaked editor positions: %v", c)
				}
			}
			if string(c["body"]) == `"feedback"` {
				var got string
				if err := json.Unmarshal(c["quote"], &got); err != nil || got != quote {
					t.Fatalf("quote: %q (%v)", got, err)
				}
			} else if _, ok := c["quote"]; ok {
				t.Fatal("page comments must not invent a quote")
			}
		}
	}
	if rec := requestAs(t, s, tab.cookie, "GET", "/api/pages/"+tab.page+"/comments", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"anchor":`) || !strings.Contains(rec.Body.String(), `"from":`) {
		t.Fatalf("the browser must retain its stored anchor: %d %s", rec.Code, rec.Body.String())
	}
}

func TestInlineCommentQuoteStaysOutOfPublicPage(t *testing.T) {
	s, tab, _ := newEditorTab(t)
	anchor := strings.Replace(testCommentAnchor, "selected text", "PRIVATE-REVIEW-QUOTE", 1)
	if rec := requestAs(t, s, tab.cookie, "POST", "/api/pages/"+tab.page+"/comments", `{"body":"private feedback","anchor":`+anchor+`}`); rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	s.setSetting("pdf_comments", "1")
	src := publicSource(t, s, shareForTest(t, s, tab.cookie, tab.page))
	if strings.Contains(src, "PRIVATE-REVIEW-QUOTE") || strings.Contains(src, "private feedback") {
		t.Fatal("private comment or quote leaked through the public page")
	}
	if rec := requestAs(t, s, "", "GET", "/api/pages/"+tab.page+"/comments", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous comment read: %d", rec.Code)
	}
}

// A comment printed without its passage reads as a remark about nothing: paper
// has no highlight to point at.
func TestPrintedCommentCarriesItsQuote(t *testing.T) {
	_, tab, _ := newEditorTab(t)
	anchor := strings.Replace(testCommentAnchor, `selected text`, `a <b>passage</b> & more`, 1)
	req, _ := http.NewRequest("POST", tab.srv.URL+"/api/pages/"+tab.page+"/comments", strings.NewReader(`{"body":"anchored remark","anchor":`+anchor+`}`))
	req.Header.Set("Cookie", tab.cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("GET", tab.srv.URL+"/api/export/"+tab.page+"?format=html&print=1", nil)
	req.Header.Set("Cookie", tab.cookie)
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	got := string(body)
	if !strings.Contains(got, `<span class="quote">a &lt;b&gt;passage&lt;/b&gt; &amp; more</span>anchored remark`) {
		t.Fatalf("printed comment has no quote:\n%s", got[strings.Index(got, "doc-comments\"")-20:])
	}
}
