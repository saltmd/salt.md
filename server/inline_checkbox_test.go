package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInlineCheckboxExports(t *testing.T) {
	raw := json.RawMessage(`[{"type":"inlineCheckbox","props":{"checked":false}},{"type":"text","text":" Review ","styles":{}},{"type":"inlineCheckbox","props":{"checked":true}}]`)
	for name, render := range map[string]func(json.RawMessage) string{"markdown": renderInline, "plain": plainInline, "html": inlineHTML} {
		if got := render(raw); got != "☐ Review ☑" {
			t.Fatalf("%s: %q", name, got)
		}
	}
	nested := json.RawMessage(`[{"type":"link","href":"https://example.test","content":` + string(raw) + `}]`)
	if got := inlineHTML(nested); !strings.Contains(got, "☐ Review ☑") {
		t.Fatalf("nested checkbox missing: %s", got)
	}
}
