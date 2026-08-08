package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Instance operations (Welle 36): admin info panel and one-click backup
// download — the ops counterpart to the settings UI.

var startedAt = time.Now()

// handleAdminInfo returns a health/ops snapshot for the settings dialog:
// version, counts, on-disk sizes and the effective limits. It also echoes the
// client IP as the server sees it — with a spoofed X-Forwarded-For header this
// makes the trust_proxy setting directly observable while setting up
// Caddy/Cloudflare.
func (s *Server) handleAdminInfo(w http.ResponseWriter, r *http.Request) {
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	count := func(q string) int {
		var n int
		s.db.QueryRow(q).Scan(&n)
		return n
	}
	var dbSize int64
	if st, err := os.Stat(filepath.Join(s.dataDir, DBFile)); err == nil {
		dbSize = st.Size()
	}
	var uploadsSize int64
	filepath.Walk(filepath.Join(s.dataDir, "files"), func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			uploadsSize += info.Size()
		}
		return nil
	})
	writeJSON(w, map[string]any{
		"version":     Version,
		"goVersion":   runtime.Version(),
		"os":          runtime.GOOS + "/" + runtime.GOARCH,
		"uptimeSec":   int(time.Since(startedAt).Seconds()),
		"users":       count(`SELECT COUNT(*) FROM users`),
		"workspaces":  count(`SELECT COUNT(*) FROM workspaces`),
		"pages":       count(`SELECT COUNT(*) FROM pages WHERE trashed_at IS NULL`),
		"trashed":     count(`SELECT COUNT(*) FROM pages WHERE trashed_at IS NOT NULL`),
		"dbSize":      dbSize,
		"uploadsSize": uploadsSize,
		"dataDir":     s.dataDir,
		"yourIp":      s.clientIP(r),
		"trustProxy":  s.boolSetting("trust_proxy"),
	})
}

// handleAdminBackup streams a consistent .tar.gz backup (VACUUM'd SQLite
// snapshot + all uploads) — the same archive `salt backup` produces on the
// CLI, but downloadable from the browser.
func (s *Server) handleAdminBackup(w http.ResponseWriter, r *http.Request) {
	// Owner, not admin: this archive holds EVERY workspace, all uploads,
	// password hashes and session tokens. Handed to an admin, the whole
	// separation would be void — a per-workspace export would just be the more
	// laborious route to
	// denselben Daten.
	if !s.isOwner(requestUser(r).ID) {
		httpErrorCode(w, 403, "owner_only_backup", "Only the owner can download an instance backup — it contains every workspace.")
		return
	}
	tmp, err := os.CreateTemp("", "salt-backup-*.tar.gz")
	if err != nil {
		httpError(w, 500, "backup failed")
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	// Backup opens its own read connection — safe alongside the live pool (WAL).
	if err := Backup(s.dataDir, tmpPath); err != nil {
		httpError(w, 500, "backup failed: "+err.Error())
		return
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		httpError(w, 500, "backup failed")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	name := "salt-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if st != nil {
		w.Header().Set("Content-Length", fmt.Sprint(st.Size()))
	}
	http.ServeContent(w, r, name, time.Now(), f)
}
