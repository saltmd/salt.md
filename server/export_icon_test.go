package server

import "testing"

// A page icon is four different things in one string, and the export used to
// print whichever it was as TEXT. Only emoji looked right — because an emoji IS
// its own text — so the defect hid behind the commonest case and reached a
// released version.
func TestIconHTMLByKind(t *testing.T) {
	s := &Server{lucide: map[string]string{"Rocket": `<path d="M4 4"/>`}}

	cases := []struct {
		icon, want, why string
	}{
		{"🚀", `<span class="doc-icon">🚀</span>`, "an emoji is text and stays text"},
		{"/files/abc.svg", `<img class="doc-icon doc-icon-img" src="/files/abc.svg" alt="">`, "an upload is an image"},
		{"", "", "no icon, no markup"},
		{"mdi:Rocket", "", "MDI paths live in a browser chunk the server has no copy of — nothing beats the literal words"},
		{"lucide:Unbekannt", "", "a name that is not in the set draws nothing rather than its own name"},
	}
	for _, c := range cases {
		if got := s.iconHTML(c.icon); got != c.want {
			t.Errorf("iconHTML(%q) = %q, want %q — %s", c.icon, got, c.want, c.why)
		}
	}

	// The known ones carry the drawing itself, never the reference.
	for _, icon := range []string{"lucide:Rocket", "lucide:Rocket:#e03131", "lucide:Rocket:fill"} {
		got := s.iconHTML(icon)
		if !contains(got, `<path d="M4 4"/>`) {
			t.Errorf("iconHTML(%q) = %q, want the drawing inlined", icon, got)
		}
		if contains(got, "lucide:") {
			t.Errorf("iconHTML(%q) printed the reference: %q", icon, got)
		}
	}
}

// The colour reaches a stroke attribute, so it is a value from a page landing in
// markup. Only a plain hex colour is allowed through; anything else falls back
// rather than being escaped and hoped for.
func TestIconColourIsCheckedNotEscaped(t *testing.T) {
	s := &Server{lucide: map[string]string{"Rocket": `<path d="M4 4"/>`}}

	if got := s.iconHTML("lucide:Rocket:#e03131"); !contains(got, `stroke="#e03131"`) {
		t.Errorf("a hex colour was dropped: %q", got)
	}
	for _, bad := range []string{
		"lucide:Rocket:red",
		`lucide:Rocket:" onload="x`,
		"lucide:Rocket:url(javascript:alert(1))",
		"lucide:Rocket:fill",
		"lucide:Rocket:#12345",
	} {
		got := s.iconHTML(bad)
		if !contains(got, `stroke="currentColor"`) {
			t.Errorf("iconHTML(%q) = %q, want it to fall back to currentColor", bad, got)
		}
	}
}

func contains(h, needle string) bool {
	return len(h) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(h); i++ {
			if h[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
