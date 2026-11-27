package server

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Calculations over a list of cells: the footer of a table column (one cell
// per row) and the function of a rollup (one cell per related row) share them.

// calculationNames lists every calculation, in menu order.
var calculationNames = []string{
	"count_all", "count_values", "count_unique", "count_empty", "count_not_empty",
	"percent_empty", "percent_not_empty",
	"sum", "avg", "median", "min", "max", "range",
	"earliest", "latest", "date_range",
	"checked", "unchecked", "percent_checked", "percent_unchecked",
}

// rollupOnlyCalculations are rollup functions that return the related values
// themselves rather than one figure.
var rollupOnlyCalculations = []string{"show_original", "show_unique"}

// legacyRollupAggs are the rollup functions that existed before calculations:
// count counts the matched related rows and percent is their share of all
// related rows.
var legacyRollupAggs = []string{"count", "percent"}

func isCalculation(name string) bool {
	for _, n := range calculationNames {
		if n == name {
			return true
		}
	}
	return false
}

func isRollupAgg(name string) bool {
	for _, n := range append(append([]string{}, legacyRollupAggs...), rollupOnlyCalculations...) {
		if n == name {
			return true
		}
	}
	return isCalculation(name)
}

func rollupAggList() string {
	return strings.Join(append(append(append([]string{}, legacyRollupAggs...), calculationNames...), rollupOnlyCalculations...), ", ")
}

func isEmptyCell(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case bool:
		return !t
	}
	return false
}

// cellValues flattens cells into their non-empty values: a multi-select cell
// with three options is three values.
func cellValues(cells []any) []any {
	var out []any
	for _, c := range cells {
		if l, ok := c.([]any); ok {
			out = append(out, cellValues(l)...)
			continue
		}
		if !isEmptyCell(c) {
			out = append(out, c)
		}
	}
	return out
}

func cellKey(v any) string { return fmt.Sprintf("%T:%v", v, v) }

func percentOf(k, n int) float64 {
	if n == 0 {
		return 0
	}
	return math.Round(float64(k)/float64(n)*1000) / 10
}

func calcSumOf(n []float64) any {
	sum := 0.0
	for _, x := range n {
		sum += x
	}
	return sum
}

func calcMeanOf(n []float64) any {
	if len(n) == 0 {
		return nil
	}
	return calcSumOf(n).(float64) / float64(len(n))
}

// medianOf is the middle value, or the mean of the two middle values.
func calcMedianOf(n []float64) any {
	if len(n) == 0 {
		return nil
	}
	s := append([]float64(nil), n...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

func calcMinOf(n []float64) any {
	if len(n) == 0 {
		return nil
	}
	m := n[0]
	for _, x := range n[1:] {
		m = math.Min(m, x)
	}
	return m
}

func calcMaxOf(n []float64) any {
	if len(n) == 0 {
		return nil
	}
	m := n[0]
	for _, x := range n[1:] {
		m = math.Max(m, x)
	}
	return m
}

// datesOf returns the cells that read as dates, earliest first, with the text
// each was stored as.
func datesOf(cells []any) []struct {
	d    formulaDate
	text string
} {
	var out []struct {
		d    formulaDate
		text string
	}
	for _, v := range cellValues(cells) {
		if s, ok := v.(string); ok {
			if d, ok := parseFormulaDate(s); ok {
				out = append(out, struct {
					d    formulaDate
					text string
				}{d, s})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].d.t.Before(out[j].d.t) })
	return out
}

// calculate applies one calculation to a list of cells. An unknown name is
// an error; nil means there was nothing to calculate.
func calculate(name string, cells []any) (any, error) {
	var nums []float64
	for _, c := range cells {
		if n, ok := toNumber(c); ok {
			nums = append(nums, n)
		}
	}
	count := func(test func(any) bool) int {
		k := 0
		for _, c := range cells {
			if test(c) {
				k++
			}
		}
		return k
	}
	notEmpty := func(c any) bool { return !isEmptyCell(c) }
	isChecked := func(c any) bool { return c == true }
	switch name {
	case "count_all":
		return float64(len(cells)), nil
	case "count_values":
		return float64(len(cellValues(cells))), nil
	case "count_unique":
		seen := map[string]bool{}
		for _, v := range cellValues(cells) {
			seen[cellKey(v)] = true
		}
		return float64(len(seen)), nil
	case "count_empty":
		return float64(count(isEmptyCell)), nil
	case "count_not_empty":
		return float64(count(notEmpty)), nil
	case "percent_empty":
		return percentOf(count(isEmptyCell), len(cells)), nil
	case "percent_not_empty":
		return percentOf(count(notEmpty), len(cells)), nil
	case "sum":
		if len(nums) == 0 {
			return nil, nil
		}
		return calcSumOf(nums), nil
	case "avg":
		return calcMeanOf(nums), nil
	case "median":
		return calcMedianOf(nums), nil
	case "min":
		return calcMinOf(nums), nil
	case "max":
		return calcMaxOf(nums), nil
	case "range":
		if len(nums) == 0 {
			return nil, nil
		}
		return calcMaxOf(nums).(float64) - calcMinOf(nums).(float64), nil
	case "earliest", "latest", "date_range":
		dates := datesOf(cells)
		if len(dates) == 0 {
			return nil, nil
		}
		first, last := dates[0], dates[len(dates)-1]
		switch name {
		case "earliest":
			return first.text, nil
		case "latest":
			return last.text, nil
		}
		return dateBetween(last.d.t, first.d.t, "days"), nil
	case "checked":
		return float64(count(isChecked)), nil
	case "unchecked":
		return float64(len(cells) - count(isChecked)), nil
	case "percent_checked":
		return percentOf(count(isChecked), len(cells)), nil
	case "percent_unchecked":
		return percentOf(len(cells)-count(isChecked), len(cells)), nil
	case "show_original":
		if v := cellValues(cells); v != nil {
			return v, nil
		}
		return []any{}, nil
	case "show_unique":
		return uniqueValues(cellValues(cells)), nil
	}
	return nil, fmt.Errorf("unknown calculation %q", name)
}

// rollupValue is a rollup's result: cells are the target values of the
// related rows that meet the condition, related is how many related rows
// there are in all. A figure with nothing to work on is 0; an earliest or
// latest date with nothing to work on is empty.
func rollupValue(agg string, cells []any, related int) any {
	switch agg {
	case "percent":
		return percentOf(len(cells), related)
	case "count":
		return len(cells)
	}
	v, err := calculate(agg, cells)
	if err != nil {
		return len(cells)
	}
	if v == nil && agg != "earliest" && agg != "latest" {
		return 0
	}
	return v
}
