package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// A collection turns a page into a small database: its child pages are the
// rows, `schema` defines typed properties, `views` are saved view configs
// (table, board, list). Property values live in pages.props.

const defaultSchema = `[
 {"id":"status","name":"Status","type":"select","options":[
   {"id":"todo","name":"To do","color":"#c4554d"},
   {"id":"doing","name":"In progress","color":"#b58a3b"},
   {"id":"done","name":"Done","color":"#2f7d4f"}]}
]`

const defaultViews = `[
 {"id":"board","name":"Board","type":"board","groupBy":"status"},
 {"id":"table","name":"Table","type":"table"}
]`

func (s *Server) handleGetCollection(w http.ResponseWriter, r *http.Request) {
	if !s.canReadReq(r, r.PathValue("id")) {
		httpError(w, 404, "not a collection")
		return
	}
	var schema, views string
	err := s.db.QueryRow(`SELECT schema, views FROM collections WHERE page_id = ?`, r.PathValue("id")).Scan(&schema, &views)
	if err == sql.ErrNoRows {
		httpError(w, 404, "not a collection")
		return
	}
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]json.RawMessage{
		"schema": json.RawMessage(schema),
		"views":  json.RawMessage(views),
	})
}

func (s *Server) handlePutCollection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.canWriteReq(r, id) {
		httpError(w, 404, "page not found")
		return
	}
	var body struct {
		Schema json.RawMessage `json:"schema"`
		Views  json.RawMessage `json:"views"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if len(body.Schema) == 0 || !json.Valid(body.Schema) || len(body.Views) == 0 || !json.Valid(body.Views) {
		httpError(w, 400, "schema and views must be valid JSON")
		return
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ?`, id).Scan(&exists); err != nil || exists == 0 {
		httpError(w, 404, "page not found")
		return
	}
	_, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)
		ON CONFLICT(page_id) DO UPDATE SET schema = excluded.schema, views = excluded.views`,
		id, string(body.Schema), string(body.Views))
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if _, err := s.db.Exec(`UPDATE pages SET type = 'collection', updated_at = ? WHERE id = ?`, now(), id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.pagesChanged()
	// The schema and the views ARE the database: adding a column or removing a
	// view changes it for everyone, and an open view on a second screen kept
	// drawing the old shape.
	s.rowsChanged(id)
	writeJSON(w, map[string]json.RawMessage{"schema": body.Schema, "views": body.Views})
}

// handleCollectionRows returns a collection's child rows filtered, sorted and
// paginated SERVER-SIDE via SQLite json_extract, so a 50k-row database doesn't
// force the client to pull everything. Query params:
//
//	limit, offset          — pagination (default 100, max 500)
//	filter=<propId>:<value> — equals (repeatable); empty value = "is set";
//	                           matches a scalar OR an array element (multiselect)
//	sort=<propId>:<asc|desc>
type collectionRow struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Icon     string          `json:"icon"`
	Cover    string          `json:"cover"`
	Position float64         `json:"position"`
	Props    json.RawMessage `json:"props"`
}

// jsonPath builds a safe SQLite JSON path for a property id we control the
// charset of (schema slugs are [a-z0-9_-]); reject anything else.
func safePropID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// rowFilter is one condition on a property. Op is one of
// is|is_not|contains|gt|lt|between|is_empty|is_not_empty; empty Op defaults to
// is (or is_not_empty when Value is also empty, for backward compatibility).
//
// Values carries a SET for is/is_not — "class is none of A, H" as one condition
// instead of two rows that happen to sit next to each other. Value stays for the
// single-value case and every other operator.
//
// Value2 is the upper bound of `between`. A date range was the one thing people
// asked for that could not be said at all: "after X" and "before Y" as two
// conditions works, but nobody finds it.
type rowFilter struct {
	Prop, Op, Value string
	Values          []string
	Value2          string
}

// vals is what the condition actually compares against: the set if there is one,
// otherwise the single value. Never both.
func (f rowFilter) vals() []string {
	if len(f.Values) > 0 {
		return f.Values
	}
	if f.Value == "" {
		return nil
	}
	return []string{f.Value}
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

// collectionRowsQuery filters, sorts and paginates a collection's rows in
// SQLite and injects computed (rollup/formula/relation) values. Shared by the
// REST handler and the MCP query_rows tool so both behave identically.
func (s *Server) collectionRowsQuery(u *user, colID string, filters []rowFilter, sortParam string, limit, offset int) (list []map[string]any, total int, err error) {
	where := []string{"parent_id = ?", "trashed_at IS NULL"}
	args := []any{colID}
	// Hide other people's private rows. The visibility switch in the page header
	// applies to database rows too, but this query never honoured it: through the
	// row list every workspace member read titles and properties of ALL rows,
	// including the ones marked private.
	//
	// The condition belongs in the SQL, not behind the LIMIT: filtering afterwards
	// would falsify paging and the total count. Workspace admins still see
	// everything — the same rule as in forbiddenPrivateAncestor.
	wsAdmin := 0
	if s.isWorkspaceAdmin(u.ID, s.pageWorkspace(colID)) {
		wsAdmin = 1
	}
	where = append(where, "(? = 1 OR visibility != 'private' OR owner_id = ?)")
	args = append(args, wsAdmin, u.ID)
	for _, f := range filters {
		if !safePropID(f.Prop) {
			continue
		}
		ex := "json_extract(props, '$." + f.Prop + "')"
		op := f.Op
		if op == "" {
			if f.Value == "" {
				op = "is_not_empty"
			} else {
				op = "is"
			}
		}
		vals := f.vals()
		// A condition with nothing to compare against does not filter. It used
		// to compare with the empty string and therefore match nothing, so the
		// moment you added "Date is …" the table went blank before you had
		// typed anything — which read as the date filter being broken. The
		// deliberate version of that question is is_empty.
		if op != "is_empty" && op != "is_not_empty" && len(vals) == 0 {
			continue
		}
		if op == "between" && f.Value2 == "" {
			continue
		}
		// One placeholder per value, for `IN (?, ?, …)`.
		holders := strings.TrimSuffix(strings.Repeat("?, ", len(vals)), ", ")
		anyOf := make([]any, len(vals))
		for i, v := range vals {
			anyOf[i] = v
		}
		// The value may be stored as a scalar or inside a list (multiselect, a
		// relation): both spellings have to answer the same question.
		inList := "EXISTS (SELECT 1 FROM json_each(props, '$." + f.Prop + "') WHERE value IN (" + holders + "))"
		set := "(" + ex + " IS NOT NULL AND " + ex + " != '' AND " + ex + " != json('[]'))"
		switch op {
		case "is_empty":
			where = append(where, "NOT "+set)
		case "is_not_empty":
			where = append(where, set)
		case "is":
			where = append(where, "("+ex+" IN ("+holders+") OR "+inList+")")
			args = append(args, anyOf...)
			args = append(args, anyOf...)
		case "is_not":
			// A missing/empty value counts as "is not X" — and with a set, as
			// "is none of them".
			where = append(where, "("+ex+" IS NULL OR ("+ex+" NOT IN ("+holders+") AND NOT "+inList+"))")
			args = append(args, anyOf...)
			args = append(args, anyOf...)
		case "contains":
			like := "%" + f.Value + "%"
			where = append(where, "("+ex+" LIKE ? OR EXISTS (SELECT 1 FROM json_each(props, '$."+f.Prop+"') WHERE value LIKE ?))")
			args = append(args, like, like)
		case "gt", "lt":
			cmp := ">"
			if op == "lt" {
				cmp = "<"
			}
			if isNumeric(f.Value) {
				where = append(where, "CAST("+ex+" AS REAL) "+cmp+" CAST(? AS REAL)")
			} else {
				where = append(where, ex+" "+cmp+" ?")
			}
			args = append(args, f.Value)
		case "between":
			// Inclusive at both ends: a range named by two dates includes the
			// days it is named after. ISO dates compare correctly as text, so
			// only numbers need the cast.
			if isNumeric(f.Value) && isNumeric(f.Value2) {
				where = append(where, "(CAST("+ex+" AS REAL) >= CAST(? AS REAL) AND CAST("+ex+" AS REAL) <= CAST(? AS REAL))")
			} else {
				where = append(where, "("+ex+" >= ? AND "+ex+" <= ?)")
			}
			args = append(args, f.Value, f.Value2)
		}
	}
	whereSQL := strings.Join(where, " AND ")

	orderSQL := "position, created_at"
	if sortParam != "" {
		propID, dir, _ := strings.Cut(sortParam, ":")
		if safePropID(propID) {
			d := "ASC"
			if strings.EqualFold(dir, "desc") {
				d = "DESC"
			}
			orderSQL = "json_extract(props, '$." + propID + "') " + d + ", position"
		}
	}

	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE `+whereSQL, args...).Scan(&total)

	rows, err := s.db.Query(`SELECT id, title, icon, cover, position, props, tags FROM pages WHERE `+whereSQL+` ORDER BY `+orderSQL+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list = []map[string]any{}
	for rows.Next() {
		var id, title, icon, cover, props, tags string
		var position float64
		if rows.Scan(&id, &title, &icon, &cover, &position, &props, &tags) != nil {
			continue
		}
		var pm map[string]any
		if json.Unmarshal([]byte(props), &pm) != nil || pm == nil {
			pm = map[string]any{}
		}
		tagList := []string{}
		if tags != "" {
			json.Unmarshal([]byte(tags), &tagList)
		}
		list = append(list, map[string]any{
			"id": id, "title": title, "icon": icon, "cover": cover, "position": position, "props": pm, "tags": tagList,
		})
	}

	// Compute rollups and formulas server-side (relations resolved against the
	// target rows), so they're correct and consistent regardless of client.
	//
	// From here on we query again (the schema, and inside computeDerived a
	// canRead per target page), even though `defer rows.Close()` only fires on
	// leaving. That works solely because the loop above runs to the end:
	// database/sql then closes the rows itself and frees the — single —
	// connection. Anyone adding a `break` here keeps the cursor open and brings
	// the whole server to a standstill.
	var schemaJSON string
	if s.db.QueryRow(`SELECT schema FROM collections WHERE page_id = ?`, colID).Scan(&schemaJSON) == nil {
		s.computeDerived(u, parseSchema(schemaJSON), list)
	}
	return list, total, nil
}

func (s *Server) handleCollectionRows(w http.ResponseWriter, r *http.Request) {
	colID := r.PathValue("id")
	if !s.canReadReq(r, colID) {
		httpError(w, 404, "not a collection")
		return
	}
	q := r.URL.Query()
	limit := 100
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	offset := 0
	if v, err := strconv.Atoi(q.Get("offset")); err == nil && v >= 0 {
		offset = v
	}
	var filters []rowFilter
	for _, f := range q["filter"] {
		// Two spellings, and both stay. `prop:op:value` is what curl, a
		// bookmarked URL and everything written before today sends; the JSON
		// object is what the interface sends, because a set of values and a
		// range have no place in a colon-separated string. Anything starting
		// with '{' is the second kind.
		if strings.HasPrefix(strings.TrimSpace(f), "{") {
			var jf struct {
				Property string   `json:"property"`
				Op       string   `json:"op"`
				Value    string   `json:"value"`
				Values   []string `json:"values"`
				Value2   string   `json:"value2"`
			}
			if json.Unmarshal([]byte(f), &jf) == nil && jf.Property != "" {
				filters = append(filters, rowFilter{
					Prop: jf.Property, Op: jf.Op, Value: jf.Value,
					Values: jf.Values, Value2: jf.Value2,
				})
			}
			continue
		}
		// Format: prop:op:value (op/value may be empty). Legacy prop:value still
		// works — a 2-part filter is treated as an equality/is-set condition.
		propID, rest, ok := strings.Cut(f, ":")
		var op, value string
		if ok {
			if o, v, ok2 := strings.Cut(rest, ":"); ok2 {
				op, value = o, v
			} else {
				value = rest
			}
		}
		filters = append(filters, rowFilter{Prop: propID, Op: op, Value: value})
	}
	list, total, err := s.collectionRowsQuery(requestUser(r), colID, filters, q.Get("sort"), limit, offset)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]any{"rows": list, "total": total, "offset": offset, "limit": limit})
}

// createDefaultCollection initializes a fresh database page.
func (s *Server) createDefaultCollection(pageID string) error {
	_, err := s.db.Exec(`INSERT INTO collections (page_id, schema, views) VALUES (?, ?, ?)
		ON CONFLICT(page_id) DO NOTHING`, pageID, defaultSchema, defaultViews)
	return err
}

// extractPropsText pulls indexable text out of a props object for FTS.
func extractPropsText(props []byte) string {
	var m map[string]any
	if json.Unmarshal(props, &m) != nil {
		return ""
	}
	out := ""
	for _, v := range m {
		switch t := v.(type) {
		case string:
			out += t + " "
		case []any:
			for _, item := range t {
				if str, ok := item.(string); ok {
					out += str + " "
				}
			}
		}
	}
	return out
}
