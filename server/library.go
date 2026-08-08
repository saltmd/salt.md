package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
)

// The blueprint library: ready-made workspaces you can pick off a shelf.
//
// A new workspace is an empty sheet, and that does not answer the question most
// tools fail at — not *what can it do* but *how do I start*. The shelf answers
// it on the first screen somebody sees.
//
// THE FREE ONES LIVE IN THE BINARY. go:embed, the same way the interface does.
// Three consequences, all of them the point:
//   - a fresh self-hosted install has the shelf immediately, with no network,
//     no account and no sign-up;
//   - nothing phones home, which for a tool people choose *because* it does not
//     is the promise rather than a detail;
//   - the catalogue cannot disagree with the binary, because they ship together.
//
// The cost is binary size, so a shipped blueprint carries no uploads and the
// look comes from an icon and an accent colour.
//
// PAID ONES COME LATER, and the shape here is what keeps that cheap: every entry
// carries an id, a source and a price that is empty today. A bought blueprint
// becomes an entry whose files the server fetches after an entitlement check —
// and nothing about the free path changes when that arrives.

//go:embed all:blueprints
var blueprintFS embed.FS

// libraryEntry is one shelf item. Everything above the line is written in
// library.json; everything below it is READ OUT OF THE BLUEPRINT, so the shelf
// cannot advertise something the blueprint does not contain. A count typed
// beside a thing is wrong within two edits.
type libraryEntry struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Tagline string   `json:"tagline"`
	Icon    string   `json:"icon"`
	Accent  string   `json:"accent"`
	Tags    []string `json:"tags"`
	Price   string   `json:"price"` // empty = free

	Source    string            `json:"source"` // "built-in" — later: where a bought one came from
	Databases []libraryDatabase `json:"databases"`
	Rules     string            `json:"rules"`
}

type libraryDatabase struct {
	Title       string        `json:"title"`
	Icon        string        `json:"icon"`
	Description string        `json:"description"`
	Props       []libraryProp `json:"props"`
	Views       []libraryView `json:"views"`
}

type libraryProp struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Options []libraryOption `json:"options,omitempty"`
}

type libraryOption struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

type libraryView struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

var (
	libraryOnce  sync.Once
	libraryItems []libraryEntry
	libraryByID  map[string]libraryEntry
)

// loadLibrary reads the shelf once. The files are in the binary and cannot
// change while it runs, so a failure here is a build mistake, not a runtime one:
// a broken entry is left out rather than taking the shelf down with it.
func loadLibrary() ([]libraryEntry, map[string]libraryEntry) {
	libraryOnce.Do(func() {
		libraryByID = map[string]libraryEntry{}
		raw, err := blueprintFS.ReadFile("blueprints/library.json")
		if err != nil {
			return
		}
		var listed []libraryEntry
		if json.Unmarshal(raw, &listed) != nil {
			return
		}
		for _, e := range listed {
			sub, err := fs.Sub(blueprintFS, "blueprints/"+e.ID)
			if err != nil {
				continue
			}
			if !describeBlueprint(sub, &e) {
				continue
			}
			e.Source = "built-in"
			libraryItems = append(libraryItems, e)
			libraryByID[e.ID] = e
		}
	})
	return libraryItems, libraryByID
}

// describeBlueprint builds the preview out of the blueprint itself.
//
// Deliberately not a screenshot. A picture is a promise that ages: after the
// third change to a blueprint it shows something you no longer get, and nobody
// notices, because an image does not break. Generated from the file, the preview
// can only ever show what is actually in there.
func describeBlueprint(fsys fs.FS, e *libraryEntry) bool {
	var manifest transferManifest
	b, err := fs.ReadFile(fsys, "salt-workspace.json")
	if err != nil || json.Unmarshal(b, &manifest) != nil {
		return false
	}
	e.Rules = manifest.Workspace.Rules

	b, err = fs.ReadFile(fsys, "pages.json")
	if err != nil {
		return false
	}
	var pages []transferPage
	if json.Unmarshal(b, &pages) != nil {
		return false
	}
	for _, p := range pages {
		if p.Type != "collection" {
			continue
		}
		db := libraryDatabase{Title: p.Title, Icon: p.Icon, Description: p.Description}
		var schema []map[string]any
		json.Unmarshal(p.Schema, &schema)
		for _, prop := range schema {
			name, _ := prop["name"].(string)
			typ, _ := prop["type"].(string)
			lp := libraryProp{Name: name, Type: typ}
			if opts, ok := prop["options"].([]any); ok {
				for _, o := range opts {
					m, _ := o.(map[string]any)
					on, _ := m["name"].(string)
					oc, _ := m["color"].(string)
					lp.Options = append(lp.Options, libraryOption{Name: on, Color: oc})
				}
			}
			db.Props = append(db.Props, lp)
		}
		var views []map[string]any
		json.Unmarshal(p.Views, &views)
		for _, v := range views {
			vn, _ := v["name"].(string)
			vt, _ := v["type"].(string)
			db.Views = append(db.Views, libraryView{Name: vn, Type: vt})
		}
		e.Databases = append(e.Databases, db)
	}
	return len(e.Databases) > 0
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	items, _ := loadLibrary()
	if items == nil {
		items = []libraryEntry{}
	}
	writeJSON(w, items)
}

// handleUseBlueprint creates a workspace from a shelf item.
//
// StructureOnly is not a choice the caller gets to make: a blueprint has no rows
// to begin with, and the mode is also what strips references to things that were
// never there. Trusting the file to be clean would make a hand-edited one able to
// point a relation at a database in somebody else's workspace.
func (s *Server) handleUseBlueprint(w http.ResponseWriter, r *http.Request) {
	u := requestUser(r)
	id := r.PathValue("id")
	_, byID := loadLibrary()
	entry, ok := byID[id]
	if !ok {
		httpErrorCode(w, 404, "blueprint_not_found", fmt.Sprintf("no blueprint %q in the library", id))
		return
	}
	if entry.Price != "" {
		// Nothing costs anything yet; this is the door, not the lock. It refuses
		// rather than quietly handing out a paid blueprint the day one is added.
		httpErrorCode(w, 402, "blueprint_not_owned", "this blueprint has to be bought first")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	sub, err := fs.Sub(blueprintFS, "blueprints/"+entry.ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	res, err := s.importWorkspaceFS(u, sub, importOptions{
		StructureOnly: true,
		Name:          strings.TrimSpace(body.Name),
	})
	if err != nil {
		httpErrorFrom(w, importStatus(err), err)
		return
	}
	s.audit("human", u.ID, u.Name, "use_blueprint", "", res.WorkspaceID,
		fmt.Sprintf("%s from blueprint %s", res.Name, entry.ID))
	writeJSON(w, res)
}
