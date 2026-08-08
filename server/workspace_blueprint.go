package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Copying a workspace's STRUCTURE into a new one.
//
// A new workspace starts empty, and everything that makes one usable is
// invisible: the rules, the property schemas with their option ids, the
// backrelation, the rollups, the view filters. Rebuilt by hand it comes out
// almost-but-not-quite the same, and the parts people forget are exactly the
// ones they cannot see.
//
// THE SOURCE WORKSPACE IS THE TEMPLATE. There is no stored template object and
// no template lifecycle, deliberately: a saved copy drifts from the workspace it
// was taken from, and then there are two answers to "how do we work here". Point
// at the workspace that already works and say "one like that".
//
// STRUCTURE ONLY — no rows. A blueprint carrying somebody's tasks is not a
// blueprint. Documents are not copied either: they are content, and a workspace
// full of somebody else's notes is not an empty start.

// blueprintWorkspace creates a new workspace with the structure of an existing
// one: its rules, its databases, their schemas and their views.
func (s *Server) blueprintWorkspace(u *user, name, sourceWS string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if !s.isMember(u.ID, sourceWS) || !s.credentialMayEnter(u, sourceWS) {
		return "", fmt.Errorf("workspace %q not found", sourceWS)
	}

	// The databases of the source, and their configuration. Drained before
	// anything else runs — with SetMaxOpenConns(1) a query inside an open cursor
	// blocks the whole server.
	type source struct {
		id, title, icon string
	}
	var sources []source
	rows, err := s.db.Query(`
		SELECT id, title, icon FROM pages
		WHERE workspace_id = ? AND type = 'collection' AND trashed_at IS NULL
		ORDER BY position`, sourceWS)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var c source
		if rows.Scan(&c.id, &c.title, &c.icon) == nil {
			sources = append(sources, c)
		}
	}
	rows.Close()
	if len(sources) == 0 {
		return "", fmt.Errorf("workspace %q has no databases to copy — nothing to use as a blueprint", sourceWS)
	}

	var rules string
	s.db.QueryRow(`SELECT COALESCE(rules, '') FROM workspaces WHERE id = ?`, sourceWS).Scan(&rules)

	// Create the workspace itself through the normal path, so its guards apply.
	msg, err := s.mcpCreateWorkspace(u.ID, name)
	if err != nil {
		return "", err
	}
	newWS := ""
	if i := strings.LastIndex(msg, "with id "); i >= 0 {
		newWS = strings.Fields(msg[i+len("with id "):])[0]
	}
	if newWS == "" {
		return "", fmt.Errorf("could not determine the new workspace id")
	}

	// Pass one: every database, so the ids exist before anything points at them.
	idMap := map[string]string{}
	ts := now()
	for i, c := range sources {
		nid := newID()
		if _, err := s.db.Exec(`INSERT INTO pages
			(id, parent_id, title, icon, content, props, position, created_at, updated_at, type, workspace_id, owner_id, visibility)
			VALUES (?, NULL, ?, ?, '[]', '{}', ?, ?, ?, 'collection', ?, ?, 'workspace')`,
			nid, c.title, c.icon, float64(i+1), ts, ts, newWS, u.ID); err != nil {
			return "", err
		}
		idMap[c.id] = nid
	}

	// Pass two: schemas and views, with every reference pointing at the new ids.
	for _, c := range sources {
		schema, views, err := s.loadCollection(c.id)
		if err != nil {
			return "", err
		}
		remapSchema(schema, idMap)
		views = blueprintViews(views, schema)
		if _, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)`,
			idMap[c.id], mustJSONString(schema), mustJSONString(views)); err != nil {
			return "", err
		}
	}

	if rules != "" {
		if _, err := s.db.Exec(`UPDATE workspaces SET rules = ? WHERE id = ?`, rules, newWS); err != nil {
			return "", err
		}
	}
	s.pagesChanged()

	note := ""
	if rules != "" {
		note = " Its rules came along."
	}
	return fmt.Sprintf("Created workspace %q with id %s from the structure of %s: %d database(s) with their schemas and views, no rows.%s",
		name, newWS, sourceWS, len(sources), note), nil
}

// remapSchema points every reference at the copied database instead of the
// original. Missed here, a relation in the new workspace would quietly read
// rows out of the OLD one — which looks like it works right up until somebody
// notices the numbers belong to another project.
func remapSchema(schema []map[string]any, idMap map[string]string) {
	for _, p := range schema {
		for _, key := range []string{"relationCollection", "backrelationCollection"} {
			if old, _ := p[key].(string); old != "" {
				if nid, ok := idMap[old]; ok {
					p[key] = nid
				} else {
					// It pointed outside the workspace being copied. Leaving the old
					// id would reach across into somebody else's data, so the property
					// loses its target and shows as unconfigured — visible, and
					// fixable, which a silent cross-workspace read is not.
					delete(p, key)
				}
			}
		}
	}
}

// blueprintViews keeps a view's shape and drops what cannot survive the copy.
func blueprintViews(views []map[string]any, schema []map[string]any) []map[string]any {
	isRelation := map[string]bool{}
	for _, p := range schema {
		if id, _ := p["id"].(string); id != "" {
			if t, _ := p["type"].(string); t == "relation" || t == "backrelation" {
				isRelation[id] = true
			}
		}
	}
	for _, v := range views {
		raw, ok := v["filters"].([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(raw))
		for _, f := range raw {
			m, _ := f.(map[string]any)
			prop, _ := m["property"].(string)
			// A filter on a relation compares against ROW ids, and no rows are
			// copied. Kept, it would silently match nothing and the view would look
			// broken; dropped, the view simply shows everything, which is the honest
			// starting state.
			if isRelation[prop] {
				continue
			}
			kept = append(kept, f)
		}
		v["filters"] = kept
	}
	return views
}

func mustJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}
