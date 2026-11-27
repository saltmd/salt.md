package server

import (
	"strings"
	"testing"
	"time"
)

var formulaTestSchema = []propDef{
	{ID: "price", Name: "Price", Type: "number"},
	{ID: "qty", Name: "Qty", Type: "number"},
	{ID: "label", Name: "Label", Type: "text"},
	{ID: "done", Name: "Done", Type: "checkbox"},
	{ID: "due", Name: "Due date", Type: "date"},
	{ID: "status", Name: "Status", Type: "select", Options: []propOption{{ID: "doing", Name: "In progress"}, {ID: "done", Name: "Done"}}},
	{ID: "tags", Name: "Tags", Type: "multiselect", Options: []propOption{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}}},
	{ID: "total", Name: "Total", Type: "formula", Formula: `prop("Price") * prop("Qty")`},
	{ID: "loop1", Name: "Loop 1", Type: "formula", Formula: `{loop2} + 1`},
	{ID: "loop2", Name: "Loop 2", Type: "formula", Formula: `{loop1} + 1`},
}

var formulaTestNow = time.Date(2026, 11, 17, 9, 30, 0, 0, time.Local)

func evalTestFormula(t *testing.T, expr string, props map[string]any) (any, error) {
	t.Helper()
	schema := append([]propDef{{ID: "f", Name: "F", Type: "formula", Formula: expr}}, formulaTestSchema...)
	env := newFormulaEnv(formulaRow{id: "row1", title: "Row one", props: props}, newPropIndex(schema), compileFormulas(schema), formulaTestNow)
	v, err := env.formula("f")
	return formulaJSON(v), err
}

func testProps() map[string]any {
	return map[string]any{
		"price": 12.5, "qty": float64(4), "label": "Widget", "done": true,
		"due": "2026-12-01", "status": "doing", "tags": []any{"a", "b"},
	}
}

func TestFormulaValues(t *testing.T) {
	cases := []struct {
		expr string
		want any
	}{
		// the original syntax keeps working
		{`{price} * {qty}`, 50.0},
		{`({price} - 2.5) * {qty}`, 40.0},
		{`-{price}`, -12.5},
		{`{missing} + 1`, 1.0},
		{`{label} + 1`, 1.0},
		// operators
		{`2 ^ 3 ^ 2`, 512.0},
		{`7 % 3`, 1.0},
		{`1e3 + 1`, 1001.0},
		{`1 + 2 * 3`, 7.0},
		{`1 < 2 and 2 <= 2`, true},
		{`not (1 == 2) && true`, true},
		{`1 == 2 or 3 != 3`, false},
		{`prop("Qty") > 3 ? "many" : "few"`, "many"},
		// properties by name, case-insensitive name, id; title
		{`prop("Price") * prop("qty")`, 50.0},
		{`prop("Total") + 1`, 51.0},
		{`{total} / 2`, 25.0},
		{`prop("Status")`, "In progress"},
		{`prop("Tags")`, []any{"Alpha", "Beta"}},
		{`prop("Done")`, true},
		{`prop("Name")`, "Row one"},
		{`prop("Label") + " x" + prop("Qty")`, "Widget x4"},
		// logic
		{`if(prop("Done"), "yes", "no")`, "yes"},
		{`if(prop("Qty") == 0, 0, prop("Price") / prop("Qty"))`, 3.125},
		{`ifs(prop("Qty") > 10, "a", prop("Qty") > 3, "b", "c")`, "b"},
		{`ifs(false, 1)`, nil},
		{`empty(prop("Missing date"))`, nil},
		{`empty("")`, true},
		{`let(x, 2, x * x)`, 4.0},
		{`lets(a, 2, b, a + 1, a * b)`, 6.0},
		// math
		{`round(3.14159, 2)`, 3.14},
		{`round(2.5)`, 3.0},
		{`floor(-1.5) + ceil(1.2) + abs(-3)`, 3.0},
		{`sum(1, 2, [3, 4])`, 10.0},
		{`mean(1, 2, 3, 4)`, 2.5},
		{`median([5, 1, 3])`, 3.0},
		{`min(3, 1, 2) + max(3, 1, 2)`, 4.0},
		{`sqrt(16) + pow(2, 3) + mod(10, 4)`, 14.0},
		{`toNumber("42") + 1`, 43.0},
		// text
		{`length("héllo")`, 5.0},
		{`upper(prop("Label")).length()`, 6.0},
		{`prop("Label").lower()`, "widget"},
		{`concat("a", "b", 1)`, "ab1"},
		{`concat([1], [2, 3])`, []any{1.0, 2.0, 3.0}},
		{`join(prop("Tags"), " / ")`, "Alpha / Beta"},
		{`contains(prop("Label"), "dg")`, true},
		{`prop("Tags").includes("Beta")`, true},
		{`replace("a-b-c", "-", "+")`, "a+b-c"},
		{`replaceAll("a-b-c", "-", "+")`, "a+b+c"},
		{`test("abc123", "[0-9]+")`, true},
		{`substring("salt.md", 0, 4)`, "salt"},
		{`slice("salt.md", -2)`, "md"},
		{`split("a,b", ",")`, []any{"a", "b"}},
		{`format(1.5) + "!"`, "1.5!"},
		{`format(0.1 + 0.2)`, "0.3"},
		{`trim("  x ")`, "x"},
		{`repeat("ab", 3)`, "ababab"},
		// dates
		{`dateAdd(prop("Due date"), 1, "months")`, "2027-01-01"},
		{`dateSubtract(prop("Due date"), 2, "weeks")`, "2026-11-17"},
		{`dateBetween(prop("Due date"), today(), "days")`, 14.0},
		{`dateBetween(parseDate("2027-03-01"), parseDate("2026-12-01"), "months")`, 3.0},
		{`dateBetween(parseDate("2026-12-01"), parseDate("2027-03-01"), "months")`, -3.0},
		{`formatDate(prop("Due date"), "MMMM D, YYYY")`, "December 1, 2026"},
		{`formatDate(now(), "HH:mm")`, "09:30"},
		{`year(prop("Due date")) + month(prop("Due date")) + date(prop("Due date"))`, 2039.0},
		{`day(parseDate("2026-11-22"))`, 7.0},
		{`prop("Due date") > today()`, true},
		{`dateAdd(parseDate("2026-01-01"), 90, "minutes")`, "2026-01-01T01:30"},
		// lists
		{`[3, 1, 2].sort()`, []any{1.0, 2.0, 3.0}},
		{`[1, 2, 3].map(current * 2)`, []any{2.0, 4.0, 6.0}},
		{`[1, 2, 3].filter(current > 1).length()`, 2.0},
		{`[1, 2, 3].find(current > 1)`, 2.0},
		{`[1, 2, 3].findIndex(current == 3)`, 2.0},
		{`[1, 2].some(current > 1) and [1, 2].every(current > 0)`, true},
		{`[1, 2, 3].map(index).last()`, 2.0},
		{`[1, 1, 2].unique()`, []any{1.0, 2.0}},
		{`at([1, [2, 3]].flat(), -1)`, 3.0},
		{`first(reverse([1, 2]))`, 2.0},
		{`id()`, "row1"},
	}
	for _, c := range cases {
		got, err := evalTestFormula(t, c.expr, testProps())
		if c.expr == `empty(prop("Missing date"))` {
			if err == nil || !strings.Contains(err.Error(), "unknown property") {
				t.Errorf("%s: want unknown property, got %v %v", c.expr, got, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.expr, err)
			continue
		}
		if !formulaEqual(got, c.want) || typeName(got) != typeName(c.want) {
			t.Errorf("%s = %#v, want %#v", c.expr, got, c.want)
		}
	}
}

func TestFormulaErrors(t *testing.T) {
	cases := []struct{ expr, want string }{
		{``, "unexpected end"},
		{`1 + `, "unexpected end"},
		{`(1 + 2`, "missing )"},
		{`{price`, "missing }"},
		{`1 / 0`, "division by zero"},
		{`1 # 2`, `unexpected "# 2"`},
		{`foo(1)`, "unknown function foo()"},
		{`if(1, 2)`, "if() takes 3 arguments"},
		{`prop("Nope")`, `unknown property "Nope"`},
		{`prop("Label") * 2`, "expected a number, got text"},
		{`prop("Label") < 2`, "cannot compare text with number"},
		{`"unterminated`, "missing closing"},
		{`x + 1`, `unknown name "x"`},
		{`dateAdd(prop("Due date"), 1, "fortnights")`, "unknown unit"},
		{`repeat("x", 1000000)`, "formula does too much work"},
		{`test("a", "(")`, "invalid pattern"},
		{`{loop1}`, "circular reference in formula"},
		{`0 / 0 + sqrt(-1)`, "division by zero"},
		{`sqrt(-1)`, "result is not a number"},
		{strings.Repeat("(", 500) + "1" + strings.Repeat(")", 500), "nested too deeply"},
	}
	for _, c := range cases {
		_, err := evalTestFormula(t, c.expr, testProps())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", c.expr, c.want, err)
		}
	}
}

// A runaway list function stops at the step budget instead of holding the
// server.
func TestFormulaStepBudget(t *testing.T) {
	expr := `split(repeat("x", 5000), "").map(split(repeat("y", 100), "").map(current).length()).length()`
	if _, err := evalTestFormula(t, expr, testProps()); err == nil || err != errFormulaTooLarge {
		t.Fatalf("want the step budget to stop it, got %v", err)
	}
}

func TestCheckFormula(t *testing.T) {
	if err := checkFormula(`prop("Price") * lets(a, 1, a)`, formulaTestSchema); err != nil {
		t.Errorf("valid formula refused: %v", err)
	}
	for expr, want := range map[string]string{
		`prop("Nope")`:      "unknown property",
		`nope()`:            "unknown function",
		`round()`:           "round() takes 1 to 2 arguments",
		`[1].map(item + 1)`: `unknown name "item"`,
		`let("x", 1, x)`:    "needs a name",
	} {
		if err := checkFormula(expr, formulaTestSchema); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want %q, got %v", expr, want, err)
		}
	}
}

// A box nobody touched reads as unticked, not as empty.
func TestFormulaUntouchedCheckboxIsFalse(t *testing.T) {
	got, err := evalTestFormula(t, `prop("Done") == false and not prop("Done")`, map[string]any{})
	if err != nil || got != true {
		t.Fatalf("got %v, %v", got, err)
	}
}

// A date rollup with nothing to choose from is empty, not 0.
func TestDateRollupOfNothingIsEmpty(t *testing.T) {
	if got := rollupValue("latest", nil, 0); got != nil {
		t.Errorf("latest of nothing: %#v", got)
	}
	if got := rollupValue("avg", nil, 0); got != 0 {
		t.Errorf("an average of nothing stays 0: %#v", got)
	}
}
