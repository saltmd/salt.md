package server

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Version is the app/API version. Bump on any client/server contract change so
// a stale client can detect a mismatch (audit Q42).
// A release build sets it from the git tag via -ldflags
// "-X salt/server.Version=..."; locally it stays at the default below.
// The frontend is stamped from the same string (SALT_VERSION → vite define),
// because two hand-kept numbers drift and the mismatch banner then fires
// forever.
//
// It is also the LAST thing to trust when asking what is running on a box: it
// is a string, and a mislabelled image proves nothing. Verify a deploy by
// behaviour — a route that answers, a marker in the bundle.
var Version = "1.6.8"

// Backup writes a consistent gzip'd tar of the workspace: the SQLite DB
// (snapshotted with VACUUM INTO so WAL contents are included) plus all uploads.
// Safe to run against a live instance (WAL allows a concurrent reader).
func Backup(dataDir, dest string) error {
	dbPath := filepath.Join(dataDir, DBFile)
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no database at %s", dbPath)
	}
	// VACUUM INTO a temp file for a transactionally-consistent snapshot.
	snap := dest + ".db.tmp"
	os.Remove(snap)
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	if _, err := db.Exec(`VACUUM INTO ?`, snap); err != nil {
		db.Close()
		return fmt.Errorf("snapshot: %w", err)
	}
	db.Close()
	defer os.Remove(snap)

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	if err := tarFile(tw, snap, DBFile); err != nil {
		return err
	}
	filesDir := filepath.Join(dataDir, "files")
	filepath.Walk(filesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dataDir, path)
		return tarFile(tw, path, filepath.ToSlash(rel))
	})
	return nil
}

func tarFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: info.Size()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Restore extracts a Backup archive into dataDir. Refuses to overwrite an
// existing DB unless SALT_RESTORE_FORCE is set, to prevent accidents.
func Restore(dataDir, src string) error {
	if _, err := os.Stat(filepath.Join(dataDir, DBFile)); err == nil && Env("RESTORE_FORCE") == "" {
		return fmt.Errorf("%s/"+DBFile+" already exists; set SALT_RESTORE_FORCE=1 to overwrite", dataDir)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gz, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o755); err != nil {
		return err
	}
	// Drop stale WAL/SHM so the restored DB isn't mixed with old journal state.
	os.Remove(filepath.Join(dataDir, DBFile+"-wal"))
	os.Remove(filepath.Join(dataDir, DBFile+"-shm"))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Guard against path traversal in a malicious archive.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("unsafe path in archive: %s", hdr.Name)
		}
		target := filepath.Join(dataDir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}
