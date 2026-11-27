package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// computedFixture: an invoice collection with a numeric, a text and a boolean
// formula, and a projects collection rolling the invoices up.
type computedFixture struct {
	s                  *Server
	u                  *user
	invoices, projects string
	apollo, bluebird   string
	cookie             string
}

func newComputedFixture(t *testing.T) computedFixture {
	t.Helper()
	s := testServer(t)
	uid, cookie := signedIn(t, s, "computed@example.test")
	ws := s.firstWorkspaceOf(t, uid)
	f := computedFixture{s: s, u: &user{ID: uid}, cookie: cookie}
	f.projects = s.makeCollection(t, ws, uid, "Projects", `[{"id":"lead","name":"Lead","type":"text"}]`)
	f.invoices = s.makeCollection(t, ws, uid, "Invoices", `[
		{"id":"price","name":"Price","type":"number"},
		{"id":"qty","name":"Qty","type":"number"},
		{"id":"paid","name":"Paid","type":"checkbox"},
		{"id":"sent","name":"Sent","type":"date"},
		{"id":"project","name":"Project","type":"relation","relationCollection":"`+f.projects+`"},
		{"id":"total","name":"Total","type":"formula","formula":"prop(\"Price\") * prop(\"Qty\")"},
		{"id":"size","name":"Size","type":"formula","formula":"if(prop(\"Total\") >= 100, \"large\", \"small\")"},
		{"id":"open","name":"Open","type":"formula","formula":"not prop(\"Paid\")"}]`)
	f.apollo = s.makeRow(t, ws, uid, f.projects, "Apollo", `{}`)
	f.bluebird = s.makeRow(t, ws, uid, f.projects, "Bluebird", `{}`)
	for _, r := range []struct{ title, props string }{
		{"A", `{"price":10,"qty":3,"paid":true,"sent":"2026-01-05","project":["` + f.apollo + `"]}`},
		{"B", `{"price":50,"qty":4,"paid":false,"sent":"2026-03-01","project":["` + f.apollo + `"]}`},
		{"C", `{"price":25,"qty":2,"sent":"2026-02-10","project":["` + f.bluebird + `"]}`},
		{"D", `{"price":100,"qty":1,"paid":true}`},
	} {
		s.makeRow(t, ws, uid, f.invoices, r.title, r.props)
	}
	if _, err := s.mcpUpdateSchema(f.projects, json.RawMessage(`[
		{"name":"Invoices","type":"backrelation","backrelationCollection":"`+f.invoices+`","backrelationProp":"project"},
		{"name":"Billed","type":"rollup","rollupRelation":"invoices","rollupTarget":"total","rollupAgg":"sum"},
		{"name":"Median","type":"rollup","rollupRelation":"invoices","rollupTarget":"price","rollupAgg":"median"},
		{"name":"Last sent","type":"rollup","rollupRelation":"invoices","rollupTarget":"sent","rollupAgg":"latest"},
		{"name":"Sizes","type":"rollup","rollupRelation":"invoices","rollupTarget":"size","rollupAgg":"show_unique"},
		{"name":"Share open","type":"formula","formula":"round(prop(\"Billed\") / 10)"}]`), nil); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return f
}

func titles(rows []map[string]any) string {
	var out []string
	for _, r := range rows {
		out = append(out, r["title"].(string))
	}
	return strings.Join(out, ",")
}

func TestFilterAndSortByFormula(t *testing.T) {
	f := newComputedFixture(t)
	q := func(req rowsRequest) rowsResult {
		t.Helper()
		if req.limit == 0 {
			req.limit = 100
		}
		res, err := f.s.queryRows(f.u, f.invoices, req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	// Totals: A 30, B 200, C 50, D 100.
	cases := []struct {
		name string
		req  rowsRequest
		want string
	}{
		{"gt", rowsRequest{flat: []rowFilter{{Prop: "total", Op: "gt", Value: "40"}}}, "B,C,D"},
		{"is number", rowsRequest{flat: []rowFilter{{Prop: "total", Op: "is", Value: "50"}}}, "C"},
		{"between", rowsRequest{flat: []rowFilter{{Prop: "total", Op: "between", Value: "50", Value2: "100"}}}, "C,D"},
		{"text formula", rowsRequest{flat: []rowFilter{{Prop: "size", Op: "is", Value: "large"}}}, "B,D"},
		{"contains", rowsRequest{flat: []rowFilter{{Prop: "size", Op: "contains", Value: "SMA"}}}, "A,C"},
		{"boolean formula", rowsRequest{flat: []rowFilter{{Prop: "open", Op: "is", Value: "true"}}}, "B,C"},
		{"is_empty never matches a number", rowsRequest{flat: []rowFilter{{Prop: "total", Op: "is_empty"}}}, ""},
		{"stored and computed together", rowsRequest{flat: []rowFilter{{Prop: "paid", Op: "is", Value: "true"}, {Prop: "total", Op: "lt", Value: "50"}}}, "A"},
		{"OR groups mixing both", rowsRequest{groups: [][]rowFilter{
			{{Prop: "total", Op: "gt", Value: "150"}},
			{{Prop: "price", Op: "is", Value: "25"}},
		}}, "B,C"},
		{"sort asc", rowsRequest{sort: "total:asc"}, "A,C,D,B"},
		{"sort desc", rowsRequest{sort: "total:desc"}, "B,D,C,A"},
		{"sort text formula, stable", rowsRequest{sort: "size:asc"}, "B,D,A,C"},
		{"paging after the sort", rowsRequest{sort: "total:desc", limit: 2, offset: 1}, "D,C"},
	}
	for _, c := range cases {
		res := q(c.req)
		if got := titles(res.rows); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if res := q(rowsRequest{sort: "total:desc", limit: 2, offset: 1}); res.total != 4 {
		t.Errorf("total counts every match, not the page: %d", res.total)
	}
}

func TestCalculationsRespectFilters(t *testing.T) {
	f := newComputedFixture(t)
	res, err := f.s.queryRows(f.u, f.invoices, rowsRequest{
		limit: 1,
		flat:  []rowFilter{{Prop: "total", Op: "gt", Value: "40"}},
		calcs: map[string]string{
			"total": "sum", "price": "median", "qty": "range", "paid": "percent_checked",
			"sent": "earliest", "project": "count_values", titleCalcKey: "count_all",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// B 200, C 50, D 100 pass; only one row is on the page.
	want := map[string]any{
		"total": 350.0, "price": 50.0, "qty": 3.0, "paid": 33.3,
		"sent": "2026-02-10", "project": 2.0, titleCalcKey: 3.0,
	}
	for k, v := range want {
		if res.calcs[k] != v {
			t.Errorf("%s: got %#v, want %#v", k, res.calcs[k], v)
		}
	}
	if len(res.rows) != 1 || res.total != 3 {
		t.Errorf("paging: %d rows, total %d", len(res.rows), res.total)
	}
}

func TestCalculate(t *testing.T) {
	cells := []any{1.0, "3", nil, []any{"a", "b"}, []any{}, true, "2026-05-01", "2026-04-01", "⚠ division by zero"}
	cases := map[string]any{
		"count_all": 9.0, "count_values": 8.0, "count_unique": 8.0,
		"count_empty": 2.0, "count_not_empty": 7.0, "percent_empty": 22.2, "percent_not_empty": 77.8,
		"sum": 5.0, "avg": 5.0 / 3, "median": 1.0, "min": 1.0, "max": 3.0, "range": 2.0,
		"earliest": "2026-04-01", "latest": "2026-05-01", "date_range": 30.0,
		"checked": 1.0, "unchecked": 8.0, "percent_checked": 11.1, "percent_unchecked": 88.9,
	}
	for name, want := range cases {
		got, err := calculate(name, cells)
		if err != nil || got != want {
			t.Errorf("%s: got %#v (%v), want %#v", name, got, err, want)
		}
	}
	if got, _ := calculate("median", []any{4.0, 1.0, 3.0, 2.0}); got != 2.5 {
		t.Errorf("median of an even count is the mean of the middle two: %v", got)
	}
	if got, _ := calculate("avg", nil); got != nil {
		t.Errorf("an average of nothing is empty, got %v", got)
	}
	if _, err := calculate("nope", cells); err == nil {
		t.Error("an unknown calculation must be refused")
	}
}

func TestRollupOverFormulaOnRelatedRows(t *testing.T) {
	f := newComputedFixture(t)
	res, err := f.s.queryRows(f.u, f.projects, rowsRequest{limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	byTitle := map[string]map[string]any{}
	for _, r := range res.rows {
		byTitle[r["title"].(string)] = r["props"].(map[string]any)
	}
	apollo, bluebird := byTitle["Apollo"], byTitle["Bluebird"]
	if apollo["billed"] != 230.0 || bluebird["billed"] != 50.0 {
		t.Errorf("sum of a formula over related rows: %v, %v", apollo["billed"], bluebird["billed"])
	}
	if apollo["median"] != 30.0 {
		t.Errorf("median: %v", apollo["median"])
	}
	if apollo["last-sent"] != "2026-03-01" {
		t.Errorf("latest: %v", apollo["last-sent"])
	}
	if sizes, _ := json.Marshal(apollo["sizes"]); string(sizes) != `["small","large"]` {
		t.Errorf("show_unique: %s", sizes)
	}
	if apollo["share-open"] != 23.0 {
		t.Errorf("a formula over a rollup: %v", apollo["share-open"])
	}
	// Sorting a rollup works too.
	res, _ = f.s.queryRows(f.u, f.projects, rowsRequest{limit: 10, sort: "billed:asc"})
	if got := titles(res.rows); got != "Bluebird,Apollo" {
		t.Errorf("sort by rollup: %s", got)
	}
}

func TestRowsEndpointCalculations(t *testing.T) {
	f := newComputedFixture(t)
	get := func(query string) (*httptest.ResponseRecorder, map[string]any) {
		req := httptest.NewRequest("GET", "/api/collections/"+f.invoices+"/rows?"+query, nil)
		req.Header.Set("Cookie", f.cookie)
		rec := httptest.NewRecorder()
		f.s.ServeHTTP(rec, req)
		var body map[string]any
		json.Unmarshal(rec.Body.Bytes(), &body)
		return rec, body
	}
	rec, body := get("calc=total:sum&calc=__title:count_all&filter=" + url.QueryEscape("size:is:large") + "&sort=total:asc")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	calcs, _ := body["calculations"].(map[string]any)
	if calcs["total"] != 300.0 || calcs["__title"] != 2.0 {
		t.Errorf("calculations: %v", body["calculations"])
	}
	if rec, _ := get("calc=total:bogus"); rec.Code != 400 {
		t.Errorf("an unknown calculation should be a 400, got %d", rec.Code)
	}
}

func TestMCPFormulasAndCalculations(t *testing.T) {
	f := newComputedFixture(t)
	s := f.s
	// A formula that cannot work is refused with the reason.
	for props, want := range map[string]string{
		`[{"name":"Bad","type":"formula","formula":"prop(\"Nope\") + 1"}]`:            `formula "Bad": unknown property "Nope"`,
		`[{"name":"Bad","type":"formula","formula":"round(1"}]`:                       "missing )",
		`[{"name":"Bad","type":"rollup","rollupRelation":"project","rollupAgg":"x"}]`: `unknown rollupAgg "x"`,
	} {
		if _, err := s.mcpUpdateSchema(f.invoices, json.RawMessage(props), nil); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", props, want, err)
		}
	}
	if _, err := s.mcpUpdateSchema(f.invoices, json.RawMessage(`[{"name":"Net","type":"formula","formula":"prop(\"Total\") / 1.2"}]`), nil); err != nil {
		t.Fatalf("a valid formula: %v", err)
	}

	if _, err := s.mcpCreateView(f.invoices, viewSpec{Name: "Table", Type: "table"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.mcpUpdateView(f.invoices, "table", viewSpec{Calculations: &map[string]string{"total": "sum", "__title": "count_all", "qty": "none"}}); err != nil {
		t.Fatalf("set calculations: %v", err)
	}
	if _, err := s.mcpUpdateView(f.invoices, "table", viewSpec{Calculations: &map[string]string{"qty": ""}}); err != nil {
		t.Fatalf("clear one calculation: %v", err)
	}
	_, views, _ := s.loadCollection(f.invoices)
	for _, v := range views {
		if v["id"] == "table" {
			got, _ := json.Marshal(v["calculations"])
			if string(got) != `{"__title":"count_all","total":"sum"}` {
				t.Errorf("stored calculations: %s", got)
			}
		}
	}
	for calcs, want := range map[string]string{
		`{"nope":"sum"}`:   "not a property",
		`{"total":"avg2"}`: "not a calculation",
	} {
		var m map[string]string
		json.Unmarshal([]byte(calcs), &m)
		if _, err := s.mcpUpdateView(f.invoices, "table", viewSpec{Calculations: &m}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", calcs, want, err)
		}
	}

	out, err := s.mcpQueryRows(f.u, f.invoices, rowsRequest{
		flat:  []rowFilter{{Prop: "size", Op: "is", Value: "small"}},
		calcs: map[string]string{"total": "avg"},
		sort:  "net:desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"calculations":{"total":40}`) || !strings.Contains(out, `"total":2`) {
		t.Errorf("query_rows reply: %s", out)
	}
	if strings.Index(out, `"title":"C"`) > strings.Index(out, `"title":"A"`) {
		t.Errorf("sorted by a formula, C (50) comes before A (30): %s", out)
	}
}

func TestFormulaPreviewEndpoint(t *testing.T) {
	f := newComputedFixture(t)
	post := func(body string) map[string]any {
		req := httptest.NewRequest("POST", "/api/collections/"+f.invoices+"/formula-preview", strings.NewReader(body))
		req.Header.Set("Cookie", f.cookie)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.s.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var out map[string]any
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	out := post(`{"formula":"prop(\"Total\") * 2"}`)
	rows, _ := out["rows"].([]any)
	if out["error"] != nil || len(rows) != 4 || rows[0].(map[string]any)["value"] != 60.0 {
		t.Errorf("preview: %v", out)
	}
	if out := post(`{"formula":"prop(\"Nope\")"}`); out["error"] != `unknown property "Nope"` {
		t.Errorf("a bad reference is reported before any row: %v", out)
	}
	// Per-row errors come back per row.
	out = post(`{"formula":"10 / (prop(\"Qty\") - 1)"}`)
	rows, _ = out["rows"].([]any)
	if last := rows[3].(map[string]any); last["error"] != "division by zero" {
		t.Errorf("row D has qty 1: %v", last)
	}
	// The editor's unsaved schema counts: a property that only exists there.
	out = post(`{"id":"draft","formula":"prop(\"Tax\") + 1","schema":[{"id":"tax","name":"Tax","type":"number"},{"id":"draft","name":"Draft","type":"formula"}]}`)
	if out["error"] != nil {
		t.Errorf("draft schema: %v", out)
	}
}
