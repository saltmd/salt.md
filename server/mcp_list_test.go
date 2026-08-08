package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// Seven list_* tools became one list(kind:). The danger in every step of this
// consolidation is the same: a capability quietly disappears, and nobody
// notices because no error is ever raised — the tool simply is not offered any
// more. These tests are the guard against that, and they are the pattern the
// remaining steps should copy.

// Every kind the tool advertises must actually answer. A kind named in the
// description but missing from the switch would fail only at the moment an
// agent needed it.
func TestListAnswersEveryKindItOffers(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "list@example.test")
	u := &user{ID: uid}

	for _, kind := range listKinds {
		out, err := s.mcpList(u, kind, "", "")
		if err != nil {
			t.Errorf("kind %q: %v", kind, err)
			continue
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("kind %q answered with nothing", kind)
		}
	}
	// Not every kind returns JSON — "pages" is a readable tree, deliberately, and
	// that predates the merge. Asserting a shape here would either be wrong or
	// force a change nobody asked for; asserting an ANSWER is what matters.
	if out, _ := s.mcpList(u, "workspaces", "", ""); !json.Valid([]byte(out)) {
		t.Error("workspaces should still be JSON")
	}
}

// The description, the error text and the switch have to agree. Four
// hand-written type lists disagreeing is exactly how "backrelation" stayed
// unusable for two releases.
func TestListKindsAgreeWithTheCatalogue(t *testing.T) {
	var tool map[string]any
	for _, entry := range mcpTools {
		if entry["name"] == "list" {
			tool = entry
		}
	}
	if tool == nil {
		t.Fatal("there is no list tool in the catalogue")
	}
	b, _ := json.Marshal(tool)
	for _, kind := range listKinds {
		if !strings.Contains(string(b), kind) {
			t.Errorf("kind %q is implemented but never offered to an agent", kind)
		}
	}
	// And the other way round: the seven tools it replaced must be gone, or the
	// catalogue grew instead of shrinking.
	for _, entry := range mcpTools {
		name, _ := entry["name"].(string)
		if strings.HasPrefix(name, "list_") {
			t.Errorf("%q is still in the catalogue — it should have folded into list(kind:)", name)
		}
	}
}

// A wrong kind must say what the right ones are. Silence here sends the agent
// looking for the mistake in its arguments instead of its vocabulary.
func TestListRefusesAnUnknownKindHelpfully(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "kind@example.test")
	u := &user{ID: uid}

	for _, bad := range []string{"", "page", "nonsense"} {
		_, err := s.mcpList(u, bad, "", "")
		if err == nil {
			t.Errorf("kind %q should be refused", bad)
			continue
		}
		if !strings.Contains(err.Error(), "pages") || !strings.Contains(err.Error(), "cover_presets") {
			t.Errorf("kind %q: the error should list the valid kinds, says %q", bad, err)
		}
	}
}

// No description or error text may name a tool that no longer exists. This is
// the same defect class as get_graph promising to find orphans: an agent is
// told to call something that is not there, and only finds out by failing.
func TestNoToolNamesAVanishedListTool(t *testing.T) {
	gone := []string{"list_pages", "list_templates", "list_tags", "list_workspaces",
		"list_files", "list_users", "list_cover_presets"}
	b, err := json.Marshal(mcpTools)
	if err != nil {
		t.Fatalf("marshal catalogue: %v", err)
	}
	for _, name := range gone {
		if strings.Contains(string(b), name) {
			t.Errorf("the catalogue still tells agents to call %q", name)
		}
	}
}
