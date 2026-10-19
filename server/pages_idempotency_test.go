package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Retried creates must not duplicate: the offline outbox flushes the same op
// from two tabs (and retries after ambiguous failures), keyed by the op key.
func TestCreatePageIdempotencyKey(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "idem@example.test")

	create := func(key string) (int, string) {
		t.Helper()
		rec := requestAs(t, s, cookie, "POST", "/api/pages",
			`{"title":"Queued","type":"doc","idempotencyKey":"`+key+`"}`)
		return rec.Code, rec.Body.String()
	}

	code, first := create("op-1")
	if code != http.StatusOK {
		t.Fatalf("create: %d %s", code, first)
	}
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(first), &p); err != nil || p.ID == "" {
		t.Fatalf("create gave no id: %s", first)
	}
	// Same key again: the first answer, not a second page.
	code, second := create("op-1")
	if code != http.StatusOK {
		t.Fatalf("retry: %d %s", code, second)
	}
	var p2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(second), &p2); err != nil || p2.ID != p.ID {
		t.Fatalf("retry gave a different page: %s vs %s", second, first)
	}
	// A different key still creates.
	code, _ = create("op-2")
	if code != http.StatusOK {
		t.Fatalf("second key: %d", code)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE title = 'Queued'`).Scan(&n)
	if n != 2 {
		t.Errorf("%d pages titled Queued, want 2 (one per key)", n)
	}
}
