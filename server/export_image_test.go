package server

import "strings"

import "testing"

// A diagram is shown as an image whose drawing sits in a data: URI, so copying
// one and pasting it stores an image block holding that URI. safeURL turned it
// into "#" and the picture vanished from the PDF while the page on screen still
// showed it — silent loss, and it applied to anything pasted as a data: image.
func TestDataImagesSurviveTheExport(t *testing.T) {
	for _, ok := range []string{
		"data:image/svg+xml;charset=utf-8,%3Csvg%3E%3C/svg%3E",
		"data:image/png;base64,iVBORw0KGgo=",
		"/files/abc.png",
		"https://example.com/a.png",
	} {
		if got := safeImageURL(ok); got != ok {
			t.Errorf("safeImageURL(%.40q) = %q, want it kept", ok, got)
		}
	}

	// Everything a link is refused for stays refused: an image source is not a
	// hole to smuggle a scheme through.
	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"data:image/svg+xml",
		"vbscript:x",
	} {
		if got := safeImageURL(bad); got != "#" {
			t.Errorf("safeImageURL(%q) = %q, want #", bad, got)
		}
	}
}

// And a LINK keeps the stricter rule — that is the one that can execute.
func TestLinksStillRefuseData(t *testing.T) {
	for _, bad := range []string{"data:image/png;base64,x", "javascript:alert(1)"} {
		if got := safeURL(bad); got != "#" {
			t.Errorf("safeURL(%q) = %q, want #", bad, got)
		}
	}
	_ = strings.TrimSpace
}
