package server

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Operational hardening (Welle 14): login brute-force throttling, a background
// cleanup loop (expired sessions / idempotency keys / stale rate buckets /
// trash auto-purge), readiness health, and per-field input caps.

const (
	maxTitleLen   = 2000
	maxCommentLen = 10000
)

// clientIP extracts the caller's IP. Proxy headers (X-Forwarded-For /
// X-Real-Ip) are only honored when the admin enabled "trust_proxy" (running
// behind Caddy/nginx/Cloudflare) — without a proxy those headers are
// client-controlled and would let an attacker rotate fake IPs past the login
// rate limit.
func (s *Server) clientIP(r *http.Request) string {
	if s.boolSetting("trust_proxy") {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
		if rip := r.Header.Get("X-Real-Ip"); rip != "" {
			return strings.TrimSpace(rip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// evict drops rate-limit buckets that are full (idle), keeping the map from
// growing without bound over the process lifetime.
func (rl *rateLimiter) evict() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, b := range rl.buckets {
		// Refill to current, then drop any bucket back at full capacity.
		elapsed := now.Sub(b.last).Seconds()
		if b.tokens+elapsed*rl.rate >= rl.burst {
			delete(rl.buckets, k)
		}
	}
}

// trashRetentionDays returns how long trashed pages are kept before automatic
// permanent deletion. Admin setting > SALT_TRASH_DAYS env > 30-day default;
// 0 disables auto-purge.
func (s *Server) trashRetentionDays() int {
	if v := s.setting("trash_days", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	if v := Env("TRASH_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 30
}

// startCleanup runs periodic maintenance until stopCleanup is closed.
func (s *Server) startCleanup() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for {
			s.runCleanup()
			select {
			case <-s.stopCleanup:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Server) runCleanup() {
	s.deleteExpiredSessions()
	// Idempotency keys are only useful for a short retry window.
	s.db.Exec(`DELETE FROM idempotency WHERE created_at < ?`, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339Nano))
	s.mcpRate.evict()
	s.loginRate.evict()
	s.tokenRate.evict()
	s.sweepOAuth()
	// Once a day at most, and it returns immediately on the other 47 ticks.
	// Inline rather than in its own goroutine so it cannot outlive shutdown.
	s.checkForUpdate()
	if days := s.trashRetentionDays(); days > 0 {
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339Nano)
		s.db.Exec(`DELETE FROM pages WHERE trashed_at IS NOT NULL AND trashed_at < ?`, cutoff)
	}
}

// handleHealth is a readiness probe: it pings the DB so orchestration can tell a
// live-but-broken instance from a healthy one.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"unavailable"}`))
		return
	}
	writeJSON(w, map[string]string{"status": "ok", "version": Version})
}

// Env reads an environment variable under its SALT_ name.
func Env(name string) string {
	return os.Getenv("SALT_" + name)
}

// EnvOr is Env with a default.
func EnvOr(name, fallback string) string {
	if v := Env(name); v != "" {
		return v
	}
	return fallback
}
