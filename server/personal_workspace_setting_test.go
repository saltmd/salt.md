package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPersonalWorkspaceSettingDefault(t *testing.T) {
	s := testServer(t)
	if !s.loadSettings().CreatePersonalWorkspaces {
		t.Fatal("expected CreatePersonalWorkspaces to be true by default")
	}

	uid := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
		VALUES (?, 'ada@example.test', 'Ada Lovelace', '#2f7d4f', 'pass', 0, ?)`, uid, now()); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	s.onboardUser(uid, "Ada Lovelace")

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE owner_id = ? AND is_personal = 1`, uid).Scan(&count); err != nil {
		t.Fatalf("query personal workspaces: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 personal workspace, got %d", count)
	}
}

func TestPersonalWorkspaceSettingDisabled(t *testing.T) {
	s := testServer(t)
	s.setSetting("create_personal_workspaces", "0")
	if s.loadSettings().CreatePersonalWorkspaces {
		t.Fatal("expected CreatePersonalWorkspaces to be false when set to 0")
	}

	uid := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
		VALUES (?, 'grace@example.test', 'Grace Hopper', '#2f7d4f', 'pass', 0, ?)`, uid, now()); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	s.onboardUser(uid, "Grace Hopper")

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE owner_id = ? AND is_personal = 1`, uid).Scan(&count); err != nil {
		t.Fatalf("query personal workspaces: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 personal workspaces when disabled, got %d", count)
	}
}

func TestPersonalWorkspaceSettingAPI(t *testing.T) {
	s := testServer(t)
	_, cookie := signedIn(t, s, "admin@example.test")

	putReq := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	getReq := func() appSettings {
		req := httptest.NewRequest("GET", "/api/settings", nil)
		req.Header.Set("Cookie", cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("GET /api/settings failed: %d", rec.Code)
		}
		var res appSettings
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal appSettings: %v", err)
		}
		return res
	}

	// Initially true
	if settings := getReq(); !settings.CreatePersonalWorkspaces {
		t.Fatal("expected CreatePersonalWorkspaces to be true initially in GET /api/settings")
	}

	// Disable via PUT
	if rec := putReq(`{"createPersonalWorkspaces": false}`); rec.Code != 200 {
		t.Fatalf("PUT /api/settings failed: %d %s", rec.Code, rec.Body.String())
	}
	if settings := getReq(); settings.CreatePersonalWorkspaces {
		t.Fatal("expected CreatePersonalWorkspaces to be false after PUT")
	}

	// Onboarding a user with setting disabled
	uid := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
		VALUES (?, 'margaret@example.test', 'Margaret Hamilton', '#2f7d4f', 'pass', 0, ?)`, uid, now()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	s.onboardUser(uid, "Margaret Hamilton")

	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE owner_id = ? AND is_personal = 1`, uid).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 personal workspaces, got %d", count)
	}

	// Re-enable via PUT
	if rec := putReq(`{"createPersonalWorkspaces": true}`); rec.Code != 200 {
		t.Fatalf("PUT /api/settings re-enable failed: %d %s", rec.Code, rec.Body.String())
	}
	if settings := getReq(); !settings.CreatePersonalWorkspaces {
		t.Fatal("expected CreatePersonalWorkspaces to be true after re-enabling")
	}

	uid2 := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
		VALUES (?, 'katherine@example.test', 'Katherine Johnson', '#2f7d4f', 'pass', 0, ?)`, uid2, now()); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	s.onboardUser(uid2, "Katherine Johnson")

	s.db.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE owner_id = ? AND is_personal = 1`, uid2).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 personal workspace after re-enabling, got %d", count)
	}
}
