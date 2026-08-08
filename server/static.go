package server

import (
	"io/fs"
	"net/http"
	"net/url"
	"strings"
)

// spaHandler serves the embedded frontend build and falls back to index.html
// for client-side routes like /p/<id>.
func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				// Vite content-hashes everything under assets/, so those may
				// be cached forever — but only when the file actually exists.
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				// Go's mime table doesn't know .webmanifest; the service worker
				// must never be cached (it IS the cache).
				if strings.HasSuffix(p, ".webmanifest") {
					w.Header().Set("Content-Type", "application/manifest+json")
				}
				if p == "sw.js" {
					w.Header().Set("Cache-Control", "no-cache")
				}
				// The HTML document is the one file that must NOT be cached: it
				// is what names the hashed assets, so a stale copy points a
				// browser at the previous build's file names and the whole
				// deploy is invisible until somebody knows to hard-reload.
				// This was the actual reason an update "did not arrive" —
				// nothing was set here at all, and a browser then caches it
				// heuristically. no-cache, not no-store: it may be kept, it
				// just has to ask first.
				if strings.HasSuffix(p, ".html") {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			// A missing hashed asset (e.g. after a redeploy) must 404 instead
			// of returning index.html, or browsers cache the wrong response.
			// The same goes for anything recognisably meant to be a file: a
			// request for /favicon.ico used to get index.html with status 200, and
			// whoever expected an image then saw nothing at all instead of an
			// honest 404. Deliberately a fixed extension list rather than "contains
			// a dot" — client routes like /t/<tag> may contain dots.
			if strings.HasPrefix(p, "assets/") || isStaticFileName(p) {
				http.NotFound(w, r)
				return
			}
		}
		// Same for the fallback, which is how index.html is served for every
		// client route — that is the common case, not the exception.
		w.Header().Set("Cache-Control", "no-cache")
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

// staticFileExts are extensions where a match MUST be a real file — if it is
// missing, that is a 404 and not a client route.
var staticFileExts = []string{
	".ico", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif",
	".css", ".js", ".mjs", ".map", ".json", ".webmanifest",
	".woff", ".woff2", ".ttf", ".otf", ".txt", ".xml",
}

func isStaticFileName(p string) bool {
	i := strings.LastIndexByte(p, '/')
	name := p[i+1:]
	for _, ext := range staticFileExts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
