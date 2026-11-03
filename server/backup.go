package server

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Version is the app/API version. Bump on any client/server contract change so
// a stale client can detect a mismatch (audit Q42).
// A release build sets it from the git tag via -ldflags
// "-X salt/server.Version=..."; a build from source stays "dev", the same
// default the frontend takes when SALT_VERSION is unset (vite.config.ts). It
// used to be "1.6.8", a number from before the reset, so `make build` produced
// a server calling itself 1.6.8 beside a frontend calling itself dev, and the
// reload banner fired on every page load.
// The frontend is stamped from the same string (SALT_VERSION → vite define),
// because two hand-kept numbers drift and the mismatch banner then fires
// forever.
//
// It is also the LAST thing to trust when asking what is running on a box: it
// is a string, and a mislabelled image proves nothing. Verify a deploy by
// behaviour — a route that answers, a marker in the bundle.
var Version = "dev"

// Backup writes a consistent gzip'd tar of the workspace: the SQLite DB
// (snapshotted with VACUUM INTO so WAL contents are included) plus all uploads.
// Safe to run against a live instance (WAL allows a concurrent reader).
//
// A file under files/ that cannot be opened is left out and everything else
// still goes in; Backup then returns a *BackupIncomplete naming it. It used to
// stop at the first such file, drop every file after it, and report success,
// so a cron job checking the exit status kept a backup that was missing data
// (#109). Any other failure, a full disk say, removes the half-written archive
// rather than leaving a file that looks like a backup.
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
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	fail := func(err error) error {
		tw.Close()
		gz.Close()
		out.Close()
		os.Remove(dest)
		return err
	}

	if err := tarFile(tw, snap, DBFile); err != nil {
		return fail(err)
	}
	var skipped []string
	filesDir := filepath.Join(dataDir, "files")
	walkErr := filepath.Walk(filesDir, func(path string, info os.FileInfo, err error) error {
		rel, _ := filepath.Rel(dataDir, path)
		rel = filepath.ToSlash(rel)
		if err != nil {
			// No uploads yet is not a gap; an unreadable directory is.
			if path == filesDir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			skipped = append(skipped, rel)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if err := tarFile(tw, path, rel); err != nil {
			if errors.Is(err, errUnreadable) {
				skipped = append(skipped, rel)
				return nil
			}
			return err
		}
		return nil
	})
	if walkErr != nil {
		return fail(walkErr)
	}
	// Closing is part of writing: the end of the archive reaches the disk here,
	// so a full disk shows up in these errors and nowhere else.
	if err := tw.Close(); err != nil {
		return fail(err)
	}
	if err := gz.Close(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(dest)
		return err
	}
	if len(skipped) > 0 {
		return &BackupIncomplete{Skipped: skipped}
	}
	return nil
}

// BackupIncomplete is the archive that was written without some files,
// because they could not be read. Everything else is in it.
type BackupIncomplete struct {
	Skipped []string
}

func (e *BackupIncomplete) Error() string {
	const show = 10
	names, more := e.Skipped, ""
	if len(names) > show {
		names, more = names[:show], fmt.Sprintf(" and %d more", len(e.Skipped)-show)
	}
	return fmt.Sprintf("%d file(s) could not be read and are not in the archive: %s%s",
		len(e.Skipped), strings.Join(names, ", "), more)
}

// errUnreadable marks a file that could not be opened. Nothing has been written
// for it yet, so it can be left out without damaging the archive.
var errUnreadable = errors.New("unreadable")

func tarFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%w: %v", errUnreadable, err)
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
	// Exactly the size the header promised: a file that grows while it is
	// copied would otherwise fail the archive, one that shrinks corrupts it.
	_, err = io.CopyN(tw, f, info.Size())
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
