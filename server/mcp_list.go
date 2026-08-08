package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// One list tool instead of seven.
//
// list_pages, list_templates, list_tags, list_workspaces, list_files,
// list_users and list_cover_presets were seven catalogue entries answering one
// question: "what is there of kind X?". Every description costs an agent
// context before it has done anything, and the larger the menu, the more often
// it reaches for the wrong item. Bundling by OBJECT rather than by verb is the
// rule the whole consolidation follows; this is the biggest single case of it.
//
// What it is NOT is a step towards one salt(action, …) tool. That would take
// the saving too far: the schema stops describing anything and the description
// stops helping. A tool should still be able to say what it does in a sentence.

// listKinds is the menu, in the order the description names them. A slice, not
// a map, so the error message and the description cannot drift from the code —
// the same guard the property types now have, after "backrelation" was missing
// from four hand-written lists at once.
var listKinds = []string{"pages", "templates", "tags", "workspaces", "files", "users", "cover_presets"}

func listKindList() string { return strings.Join(listKinds, ", ") }

// mcpList answers "what is there of this kind?".
//
// workspace_id narrows the kinds that live in a workspace and is ignored by the
// ones that do not (users are per instance, cover presets are constants).
// Ignoring rather than refusing is deliberate: an agent that passes it out of
// habit gets its answer instead of an error it has to reason about.
func (s *Server) mcpList(u *user, kind, wsID, under string) (string, error) {
	switch kind {
	case "":
		return "", fmt.Errorf("kind is required — use one of: %s", listKindList())
	case "pages":
		return s.mcpListPages(u)
	case "templates":
		// The only one that wraps its own answer in the untrusted-content
		// markers; the others are wrapped by the caller. Left as it is rather
		// than "tidied", because unwrapping to re-wrap is how a marker gets lost.
		return s.mcpListTemplates(u)
	case "tags":
		return s.mcpListTags(u, wsID)
	case "workspaces":
		return s.mcpListWorkspaces(u)
	case "files":
		return s.mcpListFiles(u, wsID, under)
	case "users":
		return s.mcpListUsers(u)
	case "cover_presets":
		b, err := json.Marshal(map[string]any{"covers": coverPresets})
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", fmt.Errorf("unknown kind %q — use one of: %s", kind, listKindList())
}
