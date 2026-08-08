package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Why a deploy could land and stay invisible.
//
// Vite content-hashes everything under assets/, so those are served immutable —
// correct, and the whole point of hashing. But index.html is the file that NAMES
// those hashes, and it went out with no cache headers at all. A browser then
// caches it heuristically, keeps pointing at the previous build's file names,
// and the update simply does not arrive until somebody knows to hard-reload.
//
// So: the document revalidates, the hashed files do not. That pairing is the
// only one that works — either half alone is wrong.
func TestTheHTMLDocumentIsNeverCachedButItsAssetsAlwaysAre(t *testing.T) {
	s := testServer(t)

	// Every client route ends up at index.html through the fallback, which is
	// the common case rather than the exception.
	for _, path := range []string{"/", "/p/abc", "/oauth/consent", "/index.html"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		cc := rec.Header().Get("Cache-Control")
		if !strings.Contains(cc, "no-cache") && !strings.Contains(cc, "no-store") {
			t.Errorf("%s went out with Cache-Control %q — a stale copy points at the previous build's assets", path, cc)
		}
	}

}

// The other half of the pairing, checked against a real build rather than the
// test fixture: the hashed files keep their long life, or every page load
// re-fetches the whole app. Skipped where no assets are embedded.
func TestHashedAssetsStayImmutable(t *testing.T) {
	s := testServer(t)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	i := strings.Index(body, "/assets/")
	if i < 0 {
		t.Skip("this build embeds no assets")
	}
	asset := body[i : i+strings.IndexAny(body[i:], `"'`)]

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", asset, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s answered %d", asset, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("%s is not immutable: %q", asset, rec.Header().Get("Cache-Control"))
	}
}
