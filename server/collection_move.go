package server

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
)

// Values travel with a moved card by property id, and ids come from names. So
// a destination can lack a property, hold one of the same id with another
// type, or offer different options for a select. Nothing is lost (moving back
// restores it), but the destination will not show it. Relations elsewhere keep
// pointing at the card too, while their picker only offers rows of the
// collection they target. The move check lists both before the move happens.

type moveCheckProp struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Type               string `json:"type"`
	RelationCollection string `json:"relationCollection"`
	Options            []struct {
		ID string `json:"id"`
	} `json:"options"`
}

type moveCheckRelation struct {
	Collection string `json:"collection"`
	Property   string `json:"property"`
	Rows       int    `json:"rows"`
}

type moveCheck struct {
	// Names of properties with a value on the card that the destination will
	// not show as it is shown now.
	Hidden []string `json:"hidden"`
	// Relation properties elsewhere that point at the card and target the
	// collection it is leaving.
	Relations []moveCheckRelation `json:"relations"`
}

func loadMoveCheckSchema(q interface {
	QueryRow(string, ...any) *sql.Row
}, collectionID string) ([]moveCheckProp, error) {
	var raw string
	if err := q.QueryRow(`SELECT COALESCE(schema,'[]') FROM collections WHERE page_id = ?`, collectionID).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var schema []moveCheckProp
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		return nil, err
	}
	return schema, nil
}

// emptyValue: nothing a reader would miss. An unticked checkbox counts as
// empty, since a missing checkbox reads the same.
func emptyValue(v json.RawMessage) bool {
	t := string(bytes.TrimSpace(v))
	return t == "" || t == "null" || t == `""` || t == "[]" || t == "false"
}

// optionIDs reads a select or multi-select value as a list of option ids.
func optionIDs(v json.RawMessage) []string {
	var one string
	if json.Unmarshal(v, &one) == nil {
		return []string{one}
	}
	var many []string
	json.Unmarshal(v, &many)
	return many
}

// hiddenAfterMove compares the card's stored values with both schemas.
func hiddenAfterMove(props map[string]json.RawMessage, from, to []moveCheckProp) []string {
	dest := map[string]moveCheckProp{}
	for _, p := range to {
		dest[p.ID] = p
	}
	hidden := []string{}
	for _, p := range from {
		v, ok := props[p.ID]
		if !ok || emptyValue(v) {
			continue
		}
		d, ok := dest[p.ID]
		switch {
		case !ok || d.Type != p.Type:
		case p.Type == "relation" && d.RelationCollection != p.RelationCollection:
		case p.Type == "select" || p.Type == "multiselect":
			known := map[string]bool{}
			for _, o := range d.Options {
				known[o.ID] = true
			}
			missing := false
			for _, id := range optionIDs(v) {
				if id != "" && !known[id] {
					missing = true
				}
			}
			if !missing {
				continue
			}
		default:
			continue
		}
		hidden = append(hidden, p.Name)
	}
	return hidden
}

// relationsToCard finds the rows, in collections the reader may see, whose
// relation targets the collection the card is leaving and names the card.
// A relation may target a collection in another workspace, so every
// collection is considered; the reader's access decides which are listed.
func (s *Server) relationsToCard(r *http.Request, sourceID, rowID string) ([]moveCheckRelation, error) {
	rows, err := s.db.Query(`SELECT c.page_id, p.title, COALESCE(c.schema,'[]') FROM collections AS c
		JOIN pages AS p ON p.id = c.page_id
		WHERE p.trashed_at IS NULL AND c.schema LIKE ? ORDER BY c.page_id`, "%"+sourceID+"%")
	if err != nil {
		return nil, err
	}
	type target struct {
		id, title string
		props     []moveCheckProp
	}
	var targets []target
	for rows.Next() {
		var t target
		var raw string
		if err := rows.Scan(&t.id, &t.title, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var schema []moveCheckProp
		if json.Unmarshal([]byte(raw), &schema) != nil {
			continue
		}
		for _, p := range schema {
			if p.Type == "relation" && p.RelationCollection == sourceID {
				t.props = append(t.props, p)
			}
		}
		if len(t.props) > 0 {
			targets = append(targets, t)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	found := []moveCheckRelation{}
	for _, t := range targets {
		if !s.canReadReq(r, t.id) {
			continue
		}
		// Read the rows first and check access afterwards: the server holds one
		// database connection, so a query issued while these rows are open waits
		// for itself.
		type child struct {
			id    string
			props map[string]json.RawMessage
		}
		var candidates []child
		children, err := s.db.Query(`SELECT id, COALESCE(props,'{}') FROM pages
			WHERE parent_id = ? AND trashed_at IS NULL AND props LIKE ?`, t.id, "%"+rowID+"%")
		if err != nil {
			return nil, err
		}
		for children.Next() {
			var c child
			var raw string
			if err := children.Scan(&c.id, &raw); err != nil {
				children.Close()
				return nil, err
			}
			if json.Unmarshal([]byte(raw), &c.props) == nil {
				candidates = append(candidates, c)
			}
		}
		children.Close()
		if err := children.Err(); err != nil {
			return nil, err
		}
		counts := map[string]int{}
		for _, c := range candidates {
			if !s.canReadReq(r, c.id) {
				continue
			}
			for _, p := range t.props {
				for _, id := range optionIDs(c.props[p.ID]) {
					if id == rowID {
						counts[p.ID]++
						break
					}
				}
			}
		}
		for _, p := range t.props {
			if n := counts[p.ID]; n > 0 {
				found = append(found, moveCheckRelation{Collection: t.title, Property: p.Name, Rows: n})
			}
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].Collection != found[j].Collection {
			return found[i].Collection < found[j].Collection
		}
		return found[i].Property < found[j].Property
	})
	return found, nil
}

// handleMoveCollectionRowCheck answers, without changing anything, what a
// move would leave unshown. It asks for the same access as the move itself.
func (s *Server) handleMoveCollectionRowCheck(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("id")
	rowID, destID := r.URL.Query().Get("rowId"), r.URL.Query().Get("collectionId")
	if rowID == "" || destID == "" || destID == sourceID {
		httpErrorCode(w, 400, "collection_move_invalid", "Choose a card and a different collection.")
		return
	}
	if !s.canWriteReq(r, sourceID) || !s.canWriteReq(r, rowID) || !s.canWriteReq(r, destID) {
		httpErrorCode(w, 404, "collection_move_unavailable", "The card or collection is no longer available.")
		return
	}
	check, status, err := s.moveCheckFor(r, sourceID, rowID, destID)
	if err != nil {
		log.Printf("collection move check %s from %s to %s: %v", rowID, sourceID, destID, err)
		httpErrorCode(w, 500, "collection_move_failed", "The card could not be moved.")
		return
	}
	if status == 400 {
		httpErrorCode(w, 400, "collection_move_cycle", "A card cannot be moved into a collection inside it.")
		return
	}
	if status != 0 {
		httpErrorCode(w, 404, "collection_move_unavailable", "The card or collection is no longer available.")
		return
	}
	writeJSON(w, check)
}

// moveCheckFor computes the check. Its status is the answer the move itself
// would give for the same card and collections: 404 when the move could not
// happen (the conditions of its UPDATE), 400 when the destination lies inside
// the card. A check never approves a move the move would refuse.
func (s *Server) moveCheckFor(r *http.Request, sourceID, rowID, destID string) (moveCheck, int, error) {
	var raw string
	err := s.db.QueryRow(`SELECT COALESCE(row.props,'{}') FROM pages AS row
		JOIN pages AS src ON src.id = row.parent_id
		JOIN pages AS dst ON dst.id = ?
		WHERE row.id = ? AND row.parent_id = ? AND row.type = 'doc' AND row.trashed_at IS NULL AND row.is_template = 0
		AND src.workspace_id = row.workspace_id AND dst.workspace_id = row.workspace_id
		AND src.type = 'collection' AND dst.type = 'collection'
		AND src.trashed_at IS NULL AND dst.trashed_at IS NULL AND src.is_template = 0 AND dst.is_template = 0`,
		destID, rowID, sourceID).Scan(&raw)
	if err == sql.ErrNoRows {
		return moveCheck{}, 404, nil
	} else if err != nil {
		return moveCheck{}, 0, err
	}
	if within, err := isWithinSubtree(s.db, rowID, destID); err != nil {
		return moveCheck{}, 0, err
	} else if within {
		return moveCheck{}, 400, nil
	}
	props := map[string]json.RawMessage{}
	json.Unmarshal([]byte(raw), &props)
	from, err := loadMoveCheckSchema(s.db, sourceID)
	if err != nil {
		return moveCheck{}, 0, err
	}
	to, err := loadMoveCheckSchema(s.db, destID)
	if err != nil {
		return moveCheck{}, 0, err
	}
	relations, err := s.relationsToCard(r, sourceID, rowID)
	if err != nil {
		return moveCheck{}, 0, err
	}
	return moveCheck{Hidden: hiddenAfterMove(props, from, to), Relations: relations}, 0, nil
}

// moveCheckInputs digests everything the move check reads: the card's
// values, both schemas, every collection that could relate to the source
// (with its title) and its rows naming the card, and what decides whether
// the reader may see each of those: the page's private ancestors, the
// reader's role and emergency access in its workspace, and the workspace's
// rule for agents. Equal digests mean the same check, so it can be compared
// inside the move's transaction, where the permission-aware check cannot run.
// Each query is read to the end before the next, as one connection requires.
func moveCheckInputs(q interface {
	QueryRow(string, ...any) *sql.Row
	Query(string, ...any) (*sql.Rows, error)
}, userID, sourceID, rowID, destID string) (string, error) {
	h := sha256.New()
	add := func(parts ...string) {
		for _, p := range parts {
			fmt.Fprintf(h, "%d:%s;", len(p), p)
		}
	}
	var props string
	if err := q.QueryRow(`SELECT COALESCE(props,'{}') FROM pages WHERE id = ?`, rowID).Scan(&props); err != nil && err != sql.ErrNoRows {
		return "", err
	}
	add("card", props)
	for _, id := range []string{sourceID, destID} {
		var schema string
		if err := q.QueryRow(`SELECT COALESCE(schema,'') FROM collections WHERE page_id = ?`, id).Scan(&schema); err != nil && err != sql.ErrNoRows {
			return "", err
		}
		add("schema", id, schema)
	}
	read := func(query string, args ...any) ([][]string, error) {
		rows, err := q.Query(query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		var out [][]string
		for rows.Next() {
			vals := make([]sql.NullString, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return nil, err
			}
			row := make([]string, len(cols))
			for i, v := range vals {
				row[i] = v.String
			}
			out = append(out, row)
		}
		return out, rows.Err()
	}
	// Who may see a page: its workspace, its private ancestors and their owners.
	access := func(id string) error {
		chain, err := read(`
			WITH RECURSIVE anc(id, parent_id, visibility, owner_id, workspace_id) AS (
				SELECT id, parent_id, visibility, owner_id, workspace_id FROM pages WHERE id = ?
				UNION
				SELECT p.id, p.parent_id, p.visibility, p.owner_id, p.workspace_id FROM pages p JOIN anc ON p.id = anc.parent_id
			) SELECT id, visibility, owner_id, workspace_id FROM anc ORDER BY id`, id)
		if err != nil {
			return err
		}
		for _, c := range chain {
			add("access", c[0], c[1], c[2], c[3])
		}
		return nil
	}
	workspaces := map[string]bool{}
	collections, err := read(`SELECT c.page_id, COALESCE(c.schema,''), p.title, p.workspace_id FROM collections AS c JOIN pages AS p ON p.id = c.page_id
		WHERE p.trashed_at IS NULL AND c.schema LIKE ? ORDER BY c.page_id`, "%"+sourceID+"%")
	if err != nil {
		return "", err
	}
	for _, c := range collections {
		add("relating", c[0], c[1], c[2])
		workspaces[c[3]] = true
		if err := access(c[0]); err != nil {
			return "", err
		}
		rows, err := read(`SELECT id, COALESCE(props,'{}') FROM pages WHERE parent_id = ? AND trashed_at IS NULL AND props LIKE ? ORDER BY id`,
			c[0], "%"+rowID+"%")
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			add("row", row[0], row[1])
			if err := access(row[0]); err != nil {
				return "", err
			}
		}
	}
	ids := make([]string, 0, len(workspaces))
	for ws := range workspaces {
		ids = append(ids, ws)
	}
	sort.Strings(ids)
	for _, ws := range ids {
		rule, err := read(`SELECT
			COALESCE((SELECT role FROM workspace_members WHERE user_id = ? AND workspace_id = ?), ''),
			COALESCE((SELECT GROUP_CONCAT(g, ',') FROM (SELECT id || '/' || expires_at || '/' || COALESCE(revoked_at,'') AS g
				FROM break_glass WHERE user_id = ? AND workspace_id = ? ORDER BY id)), ''),
			COALESCE((SELECT agent_access FROM workspaces WHERE id = ?), '')`, userID, ws, userID, ws, ws)
		if err != nil {
			return "", err
		}
		for _, r := range rule {
			add("workspace", ws, r[0], r[1], r[2])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sameMoveCheck(a, b moveCheck) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// auditCollectionName is a collection's title for the activity log, unless
// it or a page above it is private.
func auditCollectionName(tx *sql.Tx, id string) (string, error) {
	var title string
	var private int
	err := tx.QueryRow(`
		WITH RECURSIVE anc(id, parent_id, visibility) AS (
			SELECT id, parent_id, visibility FROM pages WHERE id = ?
			UNION
			SELECT p.id, p.parent_id, p.visibility FROM pages p JOIN anc ON p.id = anc.parent_id
		) SELECT (SELECT title FROM pages WHERE id = ?), COUNT(*) FROM anc WHERE visibility = 'private'`, id, id).Scan(&title, &private)
	if err != nil {
		return "", err
	}
	if private > 0 {
		return "a private collection", nil
	}
	return untitled(title), nil
}

func untitled(title string) string {
	if strings.TrimSpace(title) == "" {
		return "Untitled"
	}
	return title
}
