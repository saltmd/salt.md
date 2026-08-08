package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What makes this skill worth downloading is that the instance filled it in.
// A generic playbook could sit in the repository; this one has to carry THIS
// instance's address, THIS workspace's id and the rules somebody actually
// wrote, because an agent that still has to look those up will not.
//
// And one thing must never end up in it: the bundle is written into a
// repository, and a repository gets committed and often pushed.

func downloadSkill(t *testing.T, s *Server, cookie, query string) map[string]string {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/skill"+query, nil)
	r.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/skill%s: %d %s", query, rec.Code, rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func TestTheSkillCarriesThisInstanceAndThisWorkspace(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "skill@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	rules := "Every task hangs off a system. The title names the symptom, not the fix."
	if _, err := s.db.Exec(`UPDATE workspaces SET name = 'Entwicklung', rules = ? WHERE id = ?`, rules, ws); err != nil {
		t.Fatalf("set rules: %v", err)
	}

	files := downloadSkill(t, s, cookie, "?workspace="+ws)
	skill := files["saltmd/SKILL.md"]
	block := files["saltmd/reference/block.md"]
	if skill == "" || block == "" {
		t.Fatalf("bundle is missing its parts: %v", keysOf(files))
	}

	// The three things an agent would otherwise have to ask a human for.
	for _, want := range []string{ws, "Entwicklung", rules} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md does not carry %q — then the agent has to look it up, and it will not", want)
		}
	}
	// The workspace id has to reach the installed block too: that is the file
	// the next session reads, and it is no use if it names no workspace.
	for _, want := range []string{ws, "Entwicklung"} {
		if !strings.Contains(block, want) {
			t.Errorf("the block to install does not carry %q", want)
		}
	}

	// The instruction that makes the whole thing work — writing into the file
	// that survives a lost context — must be in the skill, not only the README.
	for _, want := range []string{"CLAUDE.md", "AGENTS.md"} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md never tells the agent to install anything into %s", want)
		}
	}
	// And the tools it is all about.
	for _, want := range []string{"working_on", "note(", "get_workspace"} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md does not mention %s", want)
		}
	}
}

// The bundle is unpacked into a repository, and repositories get pushed. A
// token or a member's email address in there would be published by the person
// following the instructions correctly.
func TestTheSkillCarriesNoSecrets(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "secret-holder@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	raw := "tok-" + newID()
	if _, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, created_at)
		VALUES (?, ?, 'agent', ?, 'write', ?)`, newID(), uid, tokenHash(raw), now()); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	for _, body := range downloadSkill(t, s, cookie, "?workspace="+ws) {
		for _, forbidden := range []string{raw, "secret-holder@example.test", tokenHash(raw)} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("the bundle carries %q — it is written into a repository and repositories get pushed", forbidden)
			}
		}
	}
}

// A workspace somebody is not in must not be describable by asking for it, and
// its rules least of all.
func TestTheSkillRefusesAWorkspaceYouAreNotIn(t *testing.T) {
	s := testServer(t)
	ownerID, _ := signedIn(t, s, "owner@example.test")
	_, otherCookie := signedIn(t, s, "outsider@example.test")
	ws := s.firstWorkspaceOf(t, ownerID)
	s.db.Exec(`UPDATE workspaces SET rules = 'internal conventions' WHERE id = ?`, ws)

	r := httptest.NewRequest("GET", "/api/skill?workspace="+ws, nil)
	r.Header.Set("Cookie", otherCookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, r)
	if rec.Code == http.StatusOK {
		t.Fatalf("an outsider downloaded a skill for a workspace they are not in (%d)", rec.Code)
	}

	// Their own download must not mention it either.
	for name, body := range downloadSkill(t, s, otherCookie, "") {
		if strings.Contains(body, ws) || strings.Contains(body, "internal conventions") {
			t.Fatalf("%s leaks a workspace the caller is not a member of", name)
		}
	}
}

// No rules yet is the normal state of a new workspace, and the skill has to
// stay useful — and honest — rather than printing an empty quote block.
func TestTheSkillSaysWhenThereAreNoRulesYet(t *testing.T) {
	s := testServer(t)
	uid, cookie := signedIn(t, s, "fresh@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	skill := downloadSkill(t, s, cookie, "?workspace="+ws)["saltmd/SKILL.md"]
	if !strings.Contains(skill, "propose_workspace_rules") {
		t.Error("with no rules the skill should point at how to draft some")
	}
	if strings.Contains(skill, "\n> \n") {
		t.Error("empty quote block where the rules would be")
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
