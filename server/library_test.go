package server

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

// The shelf ships inside the binary, which means a broken blueprint is a broken
// RELEASE — there is no config to fix it in afterwards. These tests are the
// build step that catches it: every entry is imported for real and the result is
// inspected, so a typo in a relation target fails here rather than on somebody's
// first day with the product.

func TestLibraryEntriesAreWellFormed(t *testing.T) {
	items, byID := loadLibrary()
	if len(items) == 0 {
		t.Fatal("the library is empty — go:embed found nothing, or every entry failed to parse")
	}
	seen := map[string]bool{}
	for _, e := range items {
		if e.ID == "" || e.Title == "" || e.Tagline == "" || e.Icon == "" {
			t.Errorf("%q is missing something the shelf renders: %+v", e.ID, e)
		}
		if seen[e.ID] {
			t.Errorf("two entries share the id %q — the second is unreachable", e.ID)
		}
		seen[e.ID] = true
		if byID[e.ID].ID != e.ID {
			t.Errorf("%q is listed but not reachable by id", e.ID)
		}
		if len(e.Databases) == 0 {
			t.Errorf("%q has no databases — an empty blueprint is not a blueprint", e.ID)
		}
		// The rules are the most valuable thing a workspace carries. An entry
		// without them is a set of empty tables.
		if strings.TrimSpace(e.Rules) == "" {
			t.Errorf("%q carries no rules", e.ID)
		}
		if e.Price != "" {
			t.Errorf("%q has a price, and nothing can be bought yet — it would be unusable", e.ID)
		}
		for _, db := range e.Databases {
			if len(db.Props) == 0 || len(db.Views) == 0 {
				t.Errorf("%s/%s has no properties or no views", e.ID, db.Title)
			}
		}
	}
}

// Every property type and view type in a shipped blueprint has to be one this
// build knows. A blueprint is data, so a renamed type would not fail to compile
// — it would produce a column that renders as nothing.
func TestLibraryUsesKnownTypes(t *testing.T) {
	items, _ := loadLibrary()
	for _, e := range items {
		for _, db := range e.Databases {
			for _, p := range db.Props {
				if !validPropTypes[p.Type] {
					t.Errorf("%s/%s: property %q has unknown type %q (known: %s)",
						e.ID, db.Title, p.Name, p.Type, propTypeList())
				}
			}
			for _, v := range db.Views {
				if !validViewTypes[v.Type] {
					t.Errorf("%s/%s: view %q has unknown type %q", e.ID, db.Title, v.Name, v.Type)
				}
			}
		}
	}
}

// THE test. Every shipped blueprint is actually created, and the result is
// checked for the failure that looks like success: a relation still pointing at
// the id written in the file. Rows are not copied, so such a relation reads
// nothing — and a column that silently shows nothing is exactly what nobody
// reports as a bug.
func TestEveryBlueprintCreatesAUsableWorkspace(t *testing.T) {
	items, _ := loadLibrary()
	for _, e := range items {
		t.Run(e.ID, func(t *testing.T) {
			s := testServer(t)
			uid, _ := signedIn(t, s, e.ID+"@example.test")
			u := &user{ID: uid, Name: "Jeremia"}

			res, err := s.useBlueprintForTest(u, e.ID, "")
			if err != nil {
				t.Fatalf("creating from blueprint %q failed: %v", e.ID, err)
			}

			// Only databases came across.
			var pages, dbs int
			s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ?`, res.WorkspaceID).Scan(&pages)
			s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE workspace_id = ? AND type = 'collection'`, res.WorkspaceID).Scan(&dbs)
			if dbs != len(e.Databases) {
				t.Errorf("created %d databases, the shelf promised %d", dbs, len(e.Databases))
			}
			if pages != dbs {
				t.Errorf("%d pages for %d databases — rows or documents came along", pages, dbs)
			}

			// The rules travelled.
			var rules string
			s.db.QueryRow(`SELECT COALESCE(rules,'') FROM workspaces WHERE id = ?`, res.WorkspaceID).Scan(&rules)
			if strings.TrimSpace(rules) == "" {
				t.Error("the rules did not come along — the most valuable part of the blueprint")
			}

			// Collect the ids that now exist, and the ids the FILE named.
			live := map[string]bool{}
			rows, err := s.db.Query(`SELECT id FROM pages WHERE workspace_id = ?`, res.WorkspaceID)
			if err != nil {
				t.Fatalf("pages: %v", err)
			}
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					live[id] = true
				}
			}
			rows.Close()

			var ids []string
			for id := range live {
				ids = append(ids, id)
			}
			for _, id := range ids {
				schema, views, err := s.loadCollection(id)
				if err != nil {
					continue // not a collection
				}
				for _, p := range schema {
					// A relation must still HAVE a target, and it must live in the new
					// workspace. Both directions matter and the first is the subtle one:
					// an id typo in the file does not survive as a wrong target, it gets
					// deleted on import — leaving a column that looks configured in the
					// file and is unconfigured in the product. Asserting only "no foreign
					// target" would never catch that.
					typ, _ := p["type"].(string)
					key := map[string]string{"relation": "relationCollection", "backrelation": "backrelationCollection"}[typ]
					if key == "" {
						continue
					}
					target, _ := p[key].(string)
					if target == "" {
						t.Errorf("%v is a %s with no target — the id in the file names a database that is not in it",
							p["name"], typ)
					} else if !live[target] {
						t.Errorf("%v points at %q, which is outside the new workspace — it would read another workspace's rows",
							p["name"], target)
					}
					// A rollup names a relation property on the SAME database. A typo
					// here produces a column that is silently always empty.
					if rel, _ := p["rollupRelation"].(string); rel != "" {
						found := false
						for _, q := range schema {
							if q["id"] == rel {
								found = true
							}
						}
						if !found {
							t.Errorf("rollup %q points at property %q, which this database does not have",
								p["name"], rel)
						}
					}
				}
				if len(views) == 0 {
					t.Errorf("collection %s came across without views", id)
				}
				// A board has to group by a property that exists, or it renders as one
				// nameless column holding everything.
				for _, v := range views {
					for _, key := range []string{"groupBy", "dateProp"} {
						want, _ := v[key].(string)
						if want == "" {
							continue
						}
						found := false
						for _, p := range schema {
							if p["id"] == want {
								found = true
							}
						}
						if !found {
							t.Errorf("view %q groups on %q, which is not a property of the database", v["name"], want)
						}
					}
				}
			}
		})
	}
}

// A blueprint may be used twice. The second workspace must be its own — if the
// ids were reused, editing one would edit the other.
func TestUsingABlueprintTwiceGivesTwoWorkspaces(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "twice@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	items, _ := loadLibrary()
	id := items[0].ID

	first, err := s.useBlueprintForTest(u, id, "One")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := s.useBlueprintForTest(u, id, "Two")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.WorkspaceID == second.WorkspaceID {
		t.Fatal("both uses landed in the same workspace")
	}

	var shared int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages a JOIN pages b ON a.id = b.id
		WHERE a.workspace_id = ? AND b.workspace_id = ?`, first.WorkspaceID, second.WorkspaceID).Scan(&shared)
	if shared != 0 {
		t.Errorf("%d pages exist in both workspaces — the ids were not made fresh", shared)
	}
	if second.Name != "Two" {
		t.Errorf("the name given was ignored: %q", second.Name)
	}
}

// The library must not be a way past the instance rule about who may create
// workspaces. It is the same guard as everywhere else, and it is easy to lose
// when a new entry point is added.
func TestBlueprintObeysTheWorkspaceRule(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "guarded@example.test")
	u := &user{ID: uid, Name: "Jeremia"}
	s.db.Exec(`UPDATE users SET is_admin = 0 WHERE id = ?`, uid)
	s.setSetting("allow_user_workspaces", "0")

	items, _ := loadLibrary()
	if _, err := s.useBlueprintForTest(u, items[0].ID, ""); err == nil {
		t.Error("a blueprint created a workspace on an instance where that is disabled")
	}
}

// useBlueprintForTest is the handler's body without HTTP. The handler stays a
// thin wrapper on purpose, so this exercises the same code the browser reaches.
func (s *Server) useBlueprintForTest(u *user, id, name string) (*importResult, error) {
	_, byID := loadLibrary()
	entry, ok := byID[id]
	if !ok {
		return nil, coded("blueprint_not_found", "no blueprint "+id)
	}
	sub, err := fs.Sub(blueprintFS, "blueprints/"+entry.ID)
	if err != nil {
		return nil, err
	}
	return s.importWorkspaceFS(u, sub, importOptions{StructureOnly: true, Name: name})
}

// A blueprint archive must not smuggle rows in. keepDatabasesOnly is the guard,
// and it runs on every use — the shipped files being clean is not the reason it
// is safe.
func TestBlueprintDropsRowsEvenIfTheFileHasThem(t *testing.T) {
	pages := []transferPage{
		{ID: "a", Type: "collection", Title: "Tasks"},
		{ID: "b", Type: "row", Title: "Somebody else's task"},
		{ID: "c", Type: "doc", Title: "Somebody else's notes"},
	}
	kept := keepDatabasesOnly(pages)
	if len(kept) != 1 || kept[0].Title != "Tasks" {
		b, _ := json.Marshal(kept)
		t.Errorf("rows or documents survived: %s", b)
	}
}
