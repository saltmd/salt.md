package server

import "testing"

// The stored picture is page content: it is written by whoever last edited the
// page, and it lands in a document other people open. Mermaid rendering with
// securityLevel 'strict' is the first line; this is the second, and it has to
// hold even when the first was bypassed by writing the block over the API.
func TestSanitizeSVGDropsExecutableMarkup(t *testing.T) {
	good := `<svg viewBox="0 0 10 10"><path d="M0 0h10v10H0z"/><text>A --&gt; B</text></svg>`
	if got := sanitizeSVG(good); got != good {
		t.Errorf("a plain diagram was altered:\n%s", got)
	}

	for _, bad := range []string{
		`<svg><script>alert(1)</script></svg>`,
		`<svg><SCRIPT>alert(1)</SCRIPT></svg>`,
		`<svg onload="alert(1)"></svg>`,
		`<svg><rect onclick="alert(1)"/></svg>`,
		`<svg><image onerror="alert(1)" href="x"/></svg>`,
		`<svg><a href="javascript:alert(1)">x</a></svg>`,
		`<svg><foreignObject><iframe src="x"></iframe></foreignObject></svg>`,
	} {
		if got := sanitizeSVG(bad); got != "" {
			t.Errorf("sanitizeSVG kept executable markup: %q → %q", bad, got)
		}
	}
}
