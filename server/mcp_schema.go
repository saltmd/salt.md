package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Agent parity, part 2: databases.
//
// An agent could CREATE a database, and after that its structure was frozen as
// far as the agent was concerned: no adding a column, no adding a select
// option, no creating a view. A human can do all of that in the interface.
//
// A deliberate design decision: the schema is changed by MERGING, not replaced
// wholesale (unlike PUT /api/collections/{id}). An agent that only wants to add
// one column should not delete all the others by accident, merely because it
// did not send them along.

// Schema and views are handled as generic maps and are NOT run through a Go
// type. The reason: the existing propDef in derived.go has no `options` field —
// a round trip through it would delete every select option on save. With maps
// every field survives, including ones added later.

// The types the interface offers. An agent that invents one would otherwise
// create a column nobody can render.
var validPropTypes = map[string]bool{
	"text": true, "number": true, "select": true, "multiselect": true,
	"date": true, "checkbox": true, "url": true, "person": true,
	"relation": true, "rollup": true, "formula": true,
	// checklist holds sub-tasks: [{"id","text","done"}]. Its progress is
	// DERIVED (done/total), so there is no percentage to keep in sync.
	"checklist": true,
	// backrelation — the reverse of a relation someone else declared. It was
	// missing here for two releases after the type shipped: the browser could
	// build the column and an agent could not, so the one database this feature
	// was written for never got it, while the task saying so read "done".
	"backrelation": true,
}

// propTypeNames is the same set in reading order, for the error an agent gets
// and for the tool descriptions. It was spelled out by hand in four places, and
// "backrelation" reached none of them — a test keeps it level with the map now.
var propTypeNames = []string{"text", "number", "select", "multiselect", "date",
	"checkbox", "checklist", "url", "person", "relation", "backrelation",
	"rollup", "formula"}

func propTypeList() string { return strings.Join(propTypeNames, ", ") }

var validViewTypes = map[string]bool{
	"table": true, "board": true, "list": true, "gallery": true,
	"calendar": true, "timeline": true, "form": true,
}

// normalizeSchema brings a property list supplied by an agent into the shape
// the interface expects — and is the whole reason this function exists:
//
// An agent naturally writes select options as `"options": ["Ideas", "Planned"]`.
// The interface, though, expects objects `{id, name, color}`. When the raw
// strings were stored, the WHOLE page crashed on opening
// (`o.name.toLowerCase()` on a string). That happened for real. Rather than
// scold the agent we accept both forms and convert — the convenient spelling is
// the right one.
func normalizeSchema(props []map[string]any) ([]map[string]any, error) {
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	taken := map[string]bool{}
	for _, p := range props {
		if id := str(p, "id"); id != "" {
			taken[id] = true
		}
	}
	for _, p := range props {
		typ := str(p, "type")
		if typ != "" && !validPropTypes[typ] {
			return nil, fmt.Errorf("unknown property type %q on %q — use one of: %s", typ, str(p, "name"), propTypeList())
		}
		if str(p, "name") == "" && str(p, "id") == "" {
			return nil, fmt.Errorf("each property needs a name")
		}
		// A backrelation without its two coordinates is not a broken column, it
		// is an EMPTY one: backrelationIDs returns nothing and the property reads
		// as "no tasks point here", which is indistinguishable from the truth.
		// Say so now rather than let someone trust a zero.
		if typ == "backrelation" && (str(p, "backrelationCollection") == "" || str(p, "backrelationProp") == "") {
			return nil, fmt.Errorf("backrelation %q needs backrelationCollection (the database pointing at this one) and backrelationProp (the relation property over there)", str(p, "name"))
		}
		if str(p, "id") == "" {
			id := slugID(str(p, "name"), taken)
			taken[id] = true
			p["id"] = id
		}
		if typ == "" {
			p["type"] = "text"
		}
		raw, ok := p["options"]
		if !ok || raw == nil {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			return nil, fmt.Errorf("options on %q must be a list", str(p, "name"))
		}
		optTaken := map[string]bool{}
		out := make([]any, 0, len(list))
		for _, o := range list {
			switch v := o.(type) {
			case string: // the convenient short form: ["Ideas", "Planned"]
				if v == "" {
					continue
				}
				id := slugID(v, optTaken)
				optTaken[id] = true
				out = append(out, map[string]any{"id": id, "name": v})
			case map[string]any:
				name, _ := v["name"].(string)
				id, _ := v["id"].(string)
				if name == "" && id == "" {
					return nil, fmt.Errorf("each option on %q needs a name", str(p, "name"))
				}
				if name == "" {
					name = id
					v["name"] = name
				}
				if id == "" {
					id = slugID(name, optTaken)
					v["id"] = id
				}
				optTaken[id] = true
				out = append(out, v)
			default:
				return nil, fmt.Errorf("options on %q must be strings or {id?, name, color?} objects", str(p, "name"))
			}
		}
		p["options"] = out
	}
	return props, nil
}

// loadCollection reads the schema and the views of a database.
func (s *Server) loadCollection(pageID string) ([]map[string]any, []map[string]any, error) {
	var schemaJSON, viewsJSON string
	err := s.db.QueryRow(`SELECT schema, views FROM collections WHERE page_id = ?`, pageID).Scan(&schemaJSON, &viewsJSON)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("page %q is not a database", pageID)
	}
	if err != nil {
		return nil, nil, err
	}
	var schema []map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		schema = []map[string]any{}
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(viewsJSON), &views); err != nil {
		views = []map[string]any{}
	}
	return schema, views, nil
}

func (s *Server) saveCollection(pageID string, schema []map[string]any, views []map[string]any) error {
	sb, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	vb, err := json.Marshal(views)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE collections SET schema = ?, views = ? WHERE page_id = ?`,
		string(sb), string(vb), pageID); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE pages SET updated_at = ? WHERE id = ?`, now(), pageID); err != nil {
		return err
	}
	s.pagesChanged()
	return nil
}

// slugID turns a name into a stable, readable identifier. Readable ids help
// the agent: "due-date" says more than "p7". On a collision it counts up. The
// umlaut cases below spell German letters out, because an id carries none.
func slugID(name string, taken map[string]bool) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == 'ä': // i18n-ok: German letter, spelled out below
			b.WriteString("ae")
		case r == 'ö': // i18n-ok: German letter, spelled out below
			b.WriteString("oe")
		case r == 'ü': // i18n-ok: German letter, spelled out below
			b.WriteString("ue")
		case r == 'ß': // i18n-ok: German letter, spelled out below
			b.WriteString("ss")
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "prop"
	}
	id := base
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

// --- Schema ----------------------------------------------------------------

// mcpGetCollection returns the schema AND the views. get_schema gives only the
// schema; without the views an agent cannot edit a board or a calendar, because
// it does not know their ids.
func (s *Server) mcpGetCollection(pageID string) (string, error) {
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(map[string]any{"page_id": pageID, "schema": schema, "views": views})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// mcpUpdateSchema adds or changes properties — merging, never replacing.
func (s *Server) mcpUpdateSchema(pageID string, props json.RawMessage, remove []string) (string, error) {
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	var incoming []map[string]any
	if len(props) > 0 {
		if err := json.Unmarshal(props, &incoming); err != nil {
			return "", fmt.Errorf("properties must be a list of {id?, name, type, options?}: %v", err)
		}
		if incoming, err = normalizeSchema(incoming); err != nil {
			return "", err
		}
	}
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }

	taken := map[string]bool{}
	for _, p := range schema {
		taken[str(p, "id")] = true
	}

	added, changed := []string{}, []string{}
	for _, in := range incoming {
		name, typ, id := str(in, "name"), str(in, "type"), str(in, "id")
		if strings.TrimSpace(name) == "" && id == "" {
			return "", fmt.Errorf("each property needs at least a name")
		}
		if typ != "" && !validPropTypes[typ] {
			return "", fmt.Errorf("unknown property type %q — use one of: %s", typ, propTypeList())
		}
		// An existing property? Then merge field by field — whatever the agent
		// does not name stays untouched.
		idx := -1
		if id != "" {
			for i, p := range schema {
				if str(p, "id") == id {
					idx = i
					break
				}
			}
		}
		if idx >= 0 {
			for k, v := range in {
				if k == "id" {
					continue
				}
				schema[idx][k] = v
			}
			changed = append(changed, id)
			continue
		}
		if typ == "" {
			in["type"] = "text"
		}
		if id == "" {
			id = slugID(name, taken)
			in["id"] = id
		}
		taken[id] = true
		schema = append(schema, in)
		added = append(added, id)
	}

	removed := []string{}
	if len(remove) > 0 {
		keep := schema[:0]
		for _, p := range schema {
			drop := false
			for _, r := range remove {
				if str(p, "id") == r {
					drop = true
					break
				}
			}
			if drop {
				removed = append(removed, str(p, "id"))
			} else {
				keep = append(keep, p)
			}
		}
		schema = keep
	}
	if len(added)+len(changed)+len(removed) == 0 {
		return "", fmt.Errorf("nothing to do: pass properties to add/change or remove_properties")
	}
	if err := s.saveCollection(pageID, schema, views); err != nil {
		return "", err
	}
	// The values of deleted columns deliberately stay in the rows: that way an
	// accidental removal is curable by creating the column again.
	parts := []string{}
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, ", "))
	}
	if len(changed) > 0 {
		parts = append(parts, "changed "+strings.Join(changed, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removed "+strings.Join(removed, ", ")+" (row values kept, re-adding the property brings them back)")
	}
	return "Schema updated: " + strings.Join(parts, "; "), nil
}

// mcpAddSelectOption adds a select option. Without this tool an agent could
// call set_properties with a value that does not exist at all.
func (s *Server) mcpAddSelectOption(pageID, propID, name, color string) (string, error) {
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	for i, p := range schema {
		if id, _ := p["id"].(string); id != propID {
			continue
		}
		if t, _ := p["type"].(string); t != "select" && t != "multiselect" {
			return "", fmt.Errorf("property %q is a %s, not a select", propID, t)
		}
		opts, _ := p["options"].([]any)
		taken := map[string]bool{}
		for _, o := range opts {
			om, _ := o.(map[string]any)
			if n, _ := om["name"].(string); strings.EqualFold(n, name) {
				return "", fmt.Errorf("option %q already exists on %q with id %q", name, propID, om["id"])
			}
			if id, _ := om["id"].(string); id != "" {
				taken[id] = true
			}
		}
		opt := map[string]any{"id": slugID(name, taken), "name": name}
		if color != "" {
			opt["color"] = color
		}
		schema[i]["options"] = append(opts, opt)
		if err := s.saveCollection(pageID, schema, views); err != nil {
			return "", err
		}
		return fmt.Sprintf("Added option %q (id %s) to property %q", name, opt["id"], propID), nil
	}
	return "", fmt.Errorf("property %q not found — call get_schema for the ids", propID)
}

// --- Ansichten -------------------------------------------------------------

// viewSpec is everything an agent may say about a view. Every field is
// optional on update, which is why the collections are pointers: a nil Filters
// means "leave the filters alone", an empty (but present) list means "clear
// them". Without that distinction there is no way to remove a filter, and a
// view whose filter cannot be removed is worse than one that has none.
type viewSpec struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	GroupBy     *string           `json:"group_by"`
	DateProp    *string           `json:"date_prop"`
	EndDateProp *string           `json:"end_date_prop"`
	Filters     *[]map[string]any `json:"filters"`
	// "propertyId:asc" — deliberately the same spelling query_rows uses, so an
	// agent learns one form and not two. "" clears the sort.
	Sort   *string   `json:"sort"`
	Hidden *[]string `json:"hidden"`
}

var validFilterOps = map[string]bool{
	"is": true, "is_not": true, "contains": true,
	"gt": true, "lt": true, "is_empty": true, "is_not_empty": true,
}

// applyViewSpec validates a spec against the schema and writes it onto a view
// map. Shared by create and update so the two cannot drift — the whole reason
// an agent could not configure a view before was that only one of them existed.
func applyViewSpec(view map[string]any, spec viewSpec, schema []map[string]any) error {
	has := func(id string) bool {
		for _, p := range schema {
			if pid, _ := p["id"].(string); pid == id {
				return true
			}
		}
		return false
	}
	set := func(key string, val *string) error {
		if val == nil {
			return nil
		}
		if *val == "" {
			delete(view, key)
			return nil
		}
		if !has(*val) {
			return fmt.Errorf("%q is not a property of this database", *val)
		}
		view[key] = *val
		return nil
	}
	if spec.Name != "" {
		view["name"] = spec.Name
	}
	for key, val := range map[string]*string{
		"groupBy": spec.GroupBy, "dateProp": spec.DateProp, "endDateProp": spec.EndDateProp,
	} {
		if err := set(key, val); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	// Fail early and clearly: a board without a grouping, or a calendar without
	// a date, renders EMPTY — and an empty view sends the agent hunting for the
	// mistake in the data instead of in the call.
	viewType, _ := view["type"].(string)
	if viewType == "board" {
		if g, _ := view["groupBy"].(string); g == "" {
			return fmt.Errorf("a board needs group_by (the property to make columns from)")
		}
	}
	if viewType == "calendar" || viewType == "timeline" {
		if d, _ := view["dateProp"].(string); d == "" {
			return fmt.Errorf("a %s needs date_prop (a date property id)", viewType)
		}
	}
	if spec.Filters != nil {
		out := make([]any, 0, len(*spec.Filters))
		for i, f := range *spec.Filters {
			prop, _ := f["property"].(string)
			if prop == "" {
				return fmt.Errorf("filter %d needs a property", i+1)
			}
			if !has(prop) {
				return fmt.Errorf("filter %d: %q is not a property of this database", i+1, prop)
			}
			op, _ := f["op"].(string)
			if op == "" {
				op = "is"
			}
			if !validFilterOps[op] {
				return fmt.Errorf("filter %d: unknown op %q — use is, is_not, contains, gt, lt, is_empty or is_not_empty", i+1, op)
			}
			entry := map[string]any{"property": prop, "op": op}
			if v, ok := f["value"]; ok {
				entry["value"] = v
			}
			out = append(out, entry)
		}
		view["filters"] = out
	}
	if spec.Sort != nil {
		if strings.TrimSpace(*spec.Sort) == "" {
			delete(view, "sort")
		} else {
			prop, dir, _ := strings.Cut(*spec.Sort, ":")
			if !has(prop) {
				return fmt.Errorf("sort: %q is not a property of this database", prop)
			}
			if dir != "desc" {
				dir = "asc"
			}
			view["sort"] = map[string]any{"property": prop, "dir": dir}
		}
	}
	if spec.Hidden != nil {
		out := make([]any, 0, len(*spec.Hidden))
		for _, id := range *spec.Hidden {
			if !has(id) {
				return fmt.Errorf("hidden: %q is not a property of this database", id)
			}
			out = append(out, id)
		}
		view["hidden"] = out
	}
	return nil
}

// mcpCreateView creates a view — board, calendar, timeline, gallery, list,
// form or table. "Views are what makes a database different from a table."
func (s *Server) mcpCreateView(pageID string, spec viewSpec) (string, error) {
	if !validViewTypes[spec.Type] {
		return "", fmt.Errorf("unknown view type %q — use table, board, gallery, calendar, timeline, list or form", spec.Type)
	}
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, v := range views {
		if id, ok := v["id"].(string); ok {
			taken[id] = true
		}
	}
	name := spec.Name
	if strings.TrimSpace(name) == "" {
		name = strings.ToUpper(spec.Type[:1]) + spec.Type[1:]
		spec.Name = name
	}
	view := map[string]any{"id": slugID(name, taken), "type": spec.Type}
	if err := applyViewSpec(view, spec, schema); err != nil {
		return "", err
	}
	views = append(views, view)
	if err := s.saveCollection(pageID, schema, views); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s view %q (id %s)", spec.Type, name, view["id"]), nil
}

// mcpUpdateView changes an existing view. MERGES, like update_schema: what you
// do not mention stays as it is. This did not exist at all, so a view created
// over MCP could never be given a filter, a sort or a hidden column — the
// interface could do all three, which made "create a working board" an
// instruction to a human rather than something an agent could finish.
func (s *Server) mcpUpdateView(pageID, viewID string, spec viewSpec) (string, error) {
	if spec.Type != "" {
		return "", fmt.Errorf("a view's type cannot be changed — delete it and create the new one")
	}
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	for _, v := range views {
		if id, _ := v["id"].(string); id != viewID {
			continue
		}
		if err := applyViewSpec(v, spec, schema); err != nil {
			return "", err
		}
		if err := s.saveCollection(pageID, schema, views); err != nil {
			return "", err
		}
		name, _ := v["name"].(string)
		return fmt.Sprintf("Updated view %q (id %s)", name, viewID), nil
	}
	return "", fmt.Errorf("view %q not found — call get_collection for the ids", viewID)
}

// mcpDeleteView removes a view; the last one stays, or the database would have
// nothing left to show in the interface.
func (s *Server) mcpDeleteView(pageID, viewID string) (string, error) {
	schema, views, err := s.loadCollection(pageID)
	if err != nil {
		return "", err
	}
	if len(views) <= 1 {
		return "", fmt.Errorf("cannot delete the last view — a database needs at least one")
	}
	keep := views[:0]
	found := false
	for _, v := range views {
		if id, _ := v["id"].(string); id == viewID {
			found = true
			continue
		}
		keep = append(keep, v)
	}
	if !found {
		return "", fmt.Errorf("view %q not found — call get_collection for the ids", viewID)
	}
	if err := s.saveCollection(pageID, schema, keep); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted view %s", viewID), nil
}

// --- Massenoperationen ------------------------------------------------------

// mcpCreateRows creates several rows in one call. "For 40 rows that is 40
// calls" — and every single one of them can fail and leave half a state
// behind.
func (s *Server) mcpCreateRows(userID, pageID string, rows json.RawMessage) (string, error) {
	var list []struct {
		Title      string          `json:"title"`
		Icon       string          `json:"icon"`
		Properties json.RawMessage `json:"properties"`
	}
	// Strict, and that is the point. A row is {title, icon?, properties?} — but
	// the obvious guess is flat, {title, status, due}, because create_page takes
	// its properties beside the title. Go's decoder ignores fields it does not
	// know, so that guess used to produce rows with a title and NOTHING else,
	// reported as a success. An empty board and no error is the worst answer
	// available: the agent believes it is done and moves on.
	dec := json.NewDecoder(bytes.NewReader(rows))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&list); err != nil {
		return "", fmt.Errorf("rows must be a list of {title, icon?, properties?} — property values go INSIDE properties, not beside title: %v", err)
	}
	if len(list) == 0 {
		return "", fmt.Errorf("rows is empty")
	}
	if len(list) > 200 {
		return "", fmt.Errorf("at most 200 rows per call (got %d) — split into batches", len(list))
	}
	var ws string
	if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ? AND trashed_at IS NULL`, pageID).Scan(&ws); err != nil {
		return "", fmt.Errorf("database %q not found", pageID)
	}
	var pos float64
	s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id = ?`, pageID).Scan(&pos)

	ids := []string{}
	for _, r := range list {
		if strings.TrimSpace(r.Title) == "" {
			return "", fmt.Errorf("every row needs a title (row %d is empty) — nothing was created", len(ids)+1)
		}
		id := newID()
		ts := now()
		if _, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, icon, content, position, created_at, updated_at, workspace_id, owner_id, visibility) VALUES (?, ?, ?, ?, '[]', ?, ?, ?, ?, ?, 'workspace')`,
			id, pageID, r.Title, r.Icon, pos, ts, ts, ws, userID); err != nil {
			return "", fmt.Errorf("created %d row(s), then failed on %q: %w", len(ids), r.Title, err)
		}
		pos++
		if len(r.Properties) > 0 {
			if _, err := s.mcpSetProperties(id, r.Properties, nil); err != nil {
				return "", fmt.Errorf("created %d row(s), then failed setting properties on %q: %w", len(ids)+1, r.Title, err)
			}
		}
		s.reindexPage(id)
		ids = append(ids, id)
	}
	s.pagesChanged()
	s.rowsChanged(pageID)
	b, _ := json.Marshal(map[string]any{"created": len(ids), "ids": ids})
	return string(b), nil
}

// mcpBatchSetProperties updates many rows in one call.
func (s *Server) mcpBatchSetProperties(userID string, updates json.RawMessage) (string, error) {
	var list []struct {
		PageID     string          `json:"page_id"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(updates, &list); err != nil {
		return "", fmt.Errorf("updates must be a list of {page_id, properties}: %v", err)
	}
	if len(list) == 0 {
		return "", fmt.Errorf("updates is empty")
	}
	if len(list) > 200 {
		return "", fmt.Errorf("at most 200 updates per call (got %d)", len(list))
	}
	// Check the permissions for ALL of them BEFORE the first change — otherwise
	// the call breaks off halfway and leaves a half-updated database.
	for _, u := range list {
		if !s.canWrite(userID, u.PageID) {
			return "", fmt.Errorf("page %q not found (nothing was changed)", u.PageID)
		}
	}
	done := 0
	for _, u := range list {
		if _, err := s.mcpSetProperties(u.PageID, u.Properties, nil); err != nil {
			return "", fmt.Errorf("updated %d row(s), then failed on %q: %w", done, u.PageID, err)
		}
		done++
	}
	s.pagesChanged()
	return fmt.Sprintf("Updated properties on %d row(s)", done), nil
}

// normalizePropValues brings a property patch into the shape the rest of the
// code reads, before any of it is stored. Two corrections, both of them from
// watching agents write perfectly reasonable JSON that then went quiet.
//
// A select value written as a NAME becomes the matching option id. An agent
// naturally sets `{"status": "Planned"}` — the name it handed out itself when
// creating the column. What has to be stored is the id ("planned"). Left
// unresolved the values did land in the database, but no board column and no
// filter found them again: 51 values were lying around as quiet dead entries.
// The matching is case insensitive; if nothing matches the value stays as it is
// (then it is not a select field, or it is a new value that add_select_option
// is meant to add).
//
// A LIST-SHAPED property (relation, multiselect) written as a single value
// becomes a one-element list. `{"system": "abc"}` is the obvious way to link
// one row to one other row, and it used to store exactly like that. Nothing
// looked wrong afterwards — the row still grouped into its board column and
// still matched its filter, because both compare loosely — while every
// backrelation and every rollup passed straight over it and the chip stayed
// blank on the card. Ten rows sat that way for weeks.
//
// The wrap runs AFTER the name resolution above, so `{"tags": "Bug"}` on a
// multiselect ends up as ["bug"] and not ["Bug"].
func (s *Server) normalizePropValues(pageID string, patch map[string]json.RawMessage) {
	var parentID string
	if err := s.db.QueryRow(`SELECT COALESCE(parent_id, '') FROM pages WHERE id = ?`, pageID).Scan(&parentID); err != nil || parentID == "" {
		return
	}
	schema, _, err := s.loadCollection(parentID)
	if err != nil {
		return
	}
	byProp := map[string]map[string]string{} // propID -> lowercased name or id -> id
	listShaped := map[string]bool{}
	for _, p := range schema {
		id, _ := p["id"].(string)
		if id == "" {
			continue
		}
		switch t, _ := p["type"].(string); t {
		case "relation", "multiselect":
			listShaped[id] = true
		}
		opts, _ := p["options"].([]any)
		if len(opts) == 0 {
			continue
		}
		m := map[string]string{}
		for _, o := range opts {
			om, _ := o.(map[string]any)
			oid, _ := om["id"].(string)
			name, _ := om["name"].(string)
			if oid == "" {
				continue
			}
			m[strings.ToLower(oid)] = oid
			if name != "" {
				m[strings.ToLower(name)] = oid
			}
		}
		byProp[id] = m
	}
	for prop, raw := range patch {
		if m, ok := byProp[prop]; ok {
			// Single value
			var one string
			if json.Unmarshal(raw, &one) == nil {
				if id, hit := m[strings.ToLower(one)]; hit && id != one {
					if b, err := json.Marshal(id); err == nil {
						raw = b
					}
				}
			} else {
				// Multiple choice
				var many []string
				if json.Unmarshal(raw, &many) == nil {
					out, changed := make([]string, len(many)), false
					for i, v := range many {
						out[i] = v
						if id, hit := m[strings.ToLower(v)]; hit && id != v {
							out[i] = id
							changed = true
						}
					}
					if changed {
						if b, err := json.Marshal(out); err == nil {
							raw = b
						}
					}
				}
			}
		}
		if listShaped[prop] {
			var one string
			if json.Unmarshal(raw, &one) == nil && one != "" {
				if b, err := json.Marshal([]string{one}); err == nil {
					raw = b
				}
			}
		}
		patch[prop] = raw
	}
}

// resolveFilterValues maps filter values written as an option NAME onto the id
// — the same leniency as on writing. An agent that writes with "In progress"
// searches with "In progress" too.
func (s *Server) resolveFilterValues(collectionID string, filters []rowFilter) []rowFilter {
	schema, _, err := s.loadCollection(collectionID)
	if err != nil {
		return filters
	}
	byProp := map[string]map[string]string{}
	for _, p := range schema {
		id, _ := p["id"].(string)
		opts, _ := p["options"].([]any)
		if id == "" || len(opts) == 0 {
			continue
		}
		m := map[string]string{}
		for _, o := range opts {
			om, _ := o.(map[string]any)
			oid, _ := om["id"].(string)
			name, _ := om["name"].(string)
			if oid == "" {
				continue
			}
			m[strings.ToLower(oid)] = oid
			if name != "" {
				m[strings.ToLower(name)] = oid
			}
		}
		byProp[id] = m
	}
	out := make([]rowFilter, len(filters))
	copy(out, filters)
	for i, f := range out {
		if m, ok := byProp[f.Prop]; ok {
			if id, hit := m[strings.ToLower(f.Value)]; hit {
				out[i].Value = id
			}
		}
	}
	return out
}
