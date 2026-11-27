package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Filters, sorts and calculations over computed properties. A formula, a
// rollup or a backrelation has no stored value for SQLite to compare, so a
// query that touches one loads every row that passes the stored conditions,
// computes the values, and finishes the filtering, the sort and the paging
// here.

// titleCalcKey names the title column in a view's calculations, the same key
// the view's column widths use.
const titleCalcKey = "__title"

// rowCond is one filter condition in a query that runs partly in Go: either
// the SQL condition's result, read as a column, or a test of a computed value.
type rowCond struct {
	filter   rowFilter
	computed bool
	column   int // index among the selected condition columns, when not computed
}

func computedPropIDs(schema []propDef) map[string]bool {
	out := map[string]bool{}
	for _, d := range schema {
		if computedTypes[d.Type] {
			out[d.ID] = true
		}
	}
	return out
}

// cellText is how a cell compares as text: the same spelling a browser gets
// from String(value).
func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return formatNumberText(t)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	case []any:
		parts := make([]string, len(t))
		for i, x := range t {
			parts[i] = cellText(x)
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v)
}

// matchesCell applies a filter to a computed value, the way the browser's
// matchesFilter applies it to any value. An unfinished condition matches.
func (f rowFilter) matchesCell(v any) bool {
	op := f.Op
	if op == "" {
		op = "is"
		if f.Value == "" {
			op = "is_not_empty"
		}
	}
	vals := f.vals()
	switch op {
	case "is_empty":
		return isEmptyCell(v)
	case "is_not_empty":
		return !isEmptyCell(v)
	}
	if len(vals) == 0 || op == "between" && f.Value2 == "" {
		return true
	}
	holds := func(x any, want string) bool {
		if n, ok := toNumber(x); ok && isNumeric(want) {
			if _, isBool := x.(bool); !isBool {
				w, _ := strconv.ParseFloat(strings.TrimSpace(want), 64)
				return n == w
			}
		}
		if want == "true" || want == "false" {
			return strconv.FormatBool(x == true) == want
		}
		return cellText(x) == want
	}
	holdsAny := func() bool {
		items := []any{v}
		if l, ok := v.([]any); ok {
			items = l
		}
		for _, x := range items {
			for _, want := range vals {
				if holds(x, want) {
					return true
				}
			}
		}
		return false
	}
	compareTo := func(want string) int {
		if n, ok := toNumber(v); ok && isNumeric(want) {
			w, _ := strconv.ParseFloat(strings.TrimSpace(want), 64)
			switch {
			case n < w:
				return -1
			case n > w:
				return 1
			}
			return 0
		}
		return strings.Compare(cellText(v), want)
	}
	switch op {
	case "is_not":
		return !holdsAny()
	case "contains":
		needle := strings.ToLower(f.Value)
		if l, ok := v.([]any); ok {
			for _, x := range l {
				if strings.Contains(strings.ToLower(cellText(x)), needle) {
					return true
				}
			}
			return false
		}
		return strings.Contains(strings.ToLower(cellText(v)), needle)
	case "gt":
		return compareTo(f.Value) > 0
	case "lt":
		return compareTo(f.Value) < 0
	case "between":
		return !isEmptyCell(v) && compareTo(f.Value) >= 0 && compareTo(f.Value2) <= 0
	}
	return holdsAny()
}

// compareCells orders two values for a sort: numbers as numbers, everything
// else as text, ignoring case.
func compareCells(a, b any) int {
	na, aok := toNumber(a)
	nb, bok := toNumber(b)
	_, aBool := a.(bool)
	_, bBool := b.(bool)
	if aok && bok && !aBool && !bBool {
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return 0
	}
	return strings.Compare(strings.ToLower(cellText(a)), strings.ToLower(cellText(b)))
}

// cellOf reads one column of a row for a calculation: a property, or the
// title.
func cellOf(row map[string]any, key string) any {
	if key == titleCalcKey {
		return row["title"]
	}
	props, _ := row["props"].(map[string]any)
	return props[key]
}

// calculateColumns runs each column's calculation over the rows.
func calculateColumns(rows []map[string]any, calcs map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(calcs))
	for key, name := range calcs {
		cells := make([]any, len(rows))
		for i, r := range rows {
			cells[i] = cellOf(r, key)
		}
		v, err := calculate(name, cells)
		if err != nil {
			return nil, err
		}
		out[key] = v
	}
	return out, nil
}

// sortRowsBy sorts rows by a property's value, keeping the order they came in
// among equal values.
func sortRowsBy(rows []map[string]any, sortParam string) {
	prop, dir, _ := strings.Cut(sortParam, ":")
	desc := strings.EqualFold(dir, "desc")
	sort.SliceStable(rows, func(i, j int) bool {
		c := compareCells(cellOf(rows[i], prop), cellOf(rows[j], prop))
		if desc {
			return c > 0
		}
		return c < 0
	})
}

// parseCalcParams reads repeatable calc=<propId>:<calculation> parameters.
func parseCalcParams(vals []string) (map[string]string, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, v := range vals {
		key, name, _ := strings.Cut(v, ":")
		if key == "" || !isCalculation(name) {
			return nil, fmt.Errorf("calc %q: use <propertyId>:<calculation>, one of %s", v, strings.Join(calculationNames, ", "))
		}
		out[key] = name
	}
	return out, nil
}

// formulaPreviewRows is how many rows the formula editor shows a result for.
const formulaPreviewRows = 5

const formulaErrorPrefix = "⚠ "

// handleFormulaPreview checks an expression and evaluates it against the
// first rows, for the formula editor. The schema may be the editor's unsaved
// draft, so a formula can name a property that is not saved yet.
func (s *Server) handleFormulaPreview(w http.ResponseWriter, r *http.Request) {
	colID := r.PathValue("id")
	if !s.canReadReq(r, colID) {
		httpError(w, 404, "not a collection")
		return
	}
	var body struct {
		Formula string          `json:"formula"`
		ID      string          `json:"id"`
		Schema  json.RawMessage `json:"schema"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	schemaJSON := string(body.Schema)
	if len(body.Schema) == 0 || string(body.Schema) == "null" {
		if err := s.db.QueryRow(`SELECT schema FROM collections WHERE page_id = ?`, colID).Scan(&schemaJSON); err != nil {
			httpError(w, 404, "not a collection")
			return
		}
	}
	schema := parseSchema(schemaJSON)
	id := body.ID
	if id == "" {
		id = "__preview"
	}
	def := propDef{ID: id, Name: id, Type: "formula", Formula: body.Formula}
	if i := slices.IndexFunc(schema, func(d propDef) bool { return d.ID == id }); i >= 0 {
		def.Name = schema[i].Name
		schema[i] = def
	} else {
		schema = append(schema, def)
	}
	if err := checkFormula(body.Formula, schema); err != nil {
		writeJSON(w, map[string]any{"error": err.Error(), "rows": []any{}})
		return
	}
	u := requestUser(r)
	res, err := s.queryRows(u, colID, rowsRequest{limit: formulaPreviewRows})
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.computeDerived(u, schema, res.rows)
	rows := make([]map[string]any, 0, len(res.rows))
	for _, row := range res.rows {
		props, _ := row["props"].(map[string]any)
		entry := map[string]any{"id": row["id"], "title": row["title"]}
		if msg, ok := props[id].(string); ok && strings.HasPrefix(msg, formulaErrorPrefix) {
			entry["error"] = strings.TrimPrefix(msg, formulaErrorPrefix)
		} else {
			entry["value"] = props[id]
		}
		rows = append(rows, entry)
	}
	writeJSON(w, map[string]any{"rows": rows})
}
