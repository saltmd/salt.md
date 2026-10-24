package server

import (
	"strconv"
	"strings"
)

// columnsStyle is the inline style that gives a Columns block its widths, or ""
// for equal columns. The editor stores them on the block as percentages,
// "70,30" (web/src/columnWidths.ts); the print view and the public page divide
// the row the same way, so a page looks as it does on screen.
//
// It sets a custom property, not grid-template-columns itself, so the narrow-
// screen rule that stacks the columns still wins: an inline declaration would
// beat any stylesheet rule and a phone would get two cramped columns.
//
// Anything that is not a layout for exactly n columns is ignored rather than
// repaired: the block was written by hand or by an agent, and equal columns are
// a safe answer where a guess is not. Only numbers are echoed, so nothing from
// the block reaches the markup as text.
func columnsStyle(props map[string]any, n int) string {
	raw, _ := props["widths"].(string)
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	if len(parts) != n {
		return ""
	}
	tracks := make([]string, n)
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || !(f >= 5 && f <= 95) { // written so that NaN is refused too
			return ""
		}
		tracks[i] = "minmax(0," + strconv.FormatFloat(f, 'f', -1, 64) + "fr)"
	}
	return ` style="--cols:` + strings.Join(tracks, " ") + `"`
}
