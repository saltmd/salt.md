package server

import "testing"

// The extension rule decides whether an unknown path 404s or loads the app.
// Cast too wide and navigation breaks; cast too narrow and image requests get
// HTML with status 200 again.
func TestIsStaticFileName(t *testing.T) {
	static := []string{
		"favicon.ico", "icon-192.png", "logo.svg", "manifest.webmanifest",
		"sw.js", "assets/x.css", "fonts/inter.woff2", "robots.txt",
	}
	for _, p := range static {
		if !isStaticFileName(p) {
			t.Errorf("%q should count as a file (otherwise index.html arrives instead of a 404)", p)
		}
	}
	routes := []string{
		"", "p/abc123", "t/urlaub", "settings", "index",
		"t/version.2", "p/a.b.c", // client routes may contain dots
	}
	for _, p := range routes {
		if isStaticFileName(p) {
			t.Errorf("%q is a client route and must not 404", p)
		}
	}
}
