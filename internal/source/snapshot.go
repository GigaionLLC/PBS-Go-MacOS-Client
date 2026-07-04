package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// runner runs an external command and returns its stdout (stderr is folded into
// the error). It is an interface so the snapshot workflow can be unit-tested
// without a real macOS host; the darwin build supplies an exec-backed runner.
type runner interface {
	run(name string, args ...string) (stdout string, err error)
}

// volumeFunc resolves the base-volume mount point (and device) that contains a
// path. On darwin this wraps statfs(2); tests inject a fake.
type volumeFunc func(path string) (mountPoint, device string, err error)

// SnapshotSource backs up a crash-consistent APFS local snapshot: it creates the
// snapshot (tmutil), mounts it read-only (mount_apfs), and serves a
// *LiveDirectoryFS rooted at the requested subtree inside the mount. Close()
// unmounts and deletes the snapshot. It is created via OpenSnapshot (darwin only);
// this file holds the platform-neutral orchestration so it can be tested anywhere.
type SnapshotSource struct {
	fs       *LiveDirectoryFS
	base     string // base volume mount, e.g. "/System/Volumes/Data"
	snapDate string // "2026-07-04-120000"
	snapName string // "com.apple.TimeMachine.<date>.local"
	mount    string // temp mountpoint (empty until mounted)
	r        runner
	mu       sync.Mutex
	closed   bool
}

var snapshotDateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}-\d{6}`)

// parseSnapshotDate extracts the YYYY-MM-DD-HHMMSS token tmutil prints
// ("Created local snapshot with date: 2026-07-04-120000").
func parseSnapshotDate(out string) (string, error) {
	if m := snapshotDateRe.FindString(out); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("no snapshot date in %q", strings.TrimSpace(out))
}

// volumeRel returns abs's location relative to the root of its base volume,
// normalizing firmlinks: /Users/foo is surfaced at / via a firmlink but lives on
// the Data volume, so it is not textually under /System/Volumes/Data.
func volumeRel(base, abs string) string {
	if abs == base {
		return "" // backing up the volume root itself
	}
	if base != "/" && strings.HasPrefix(abs, base+"/") {
		return strings.TrimPrefix(abs, base+"/")
	}
	return strings.TrimPrefix(abs, "/")
}

// newSnapshotSource runs create+mount with an injected runner and volume
// resolver. On any error it tears down whatever was already set up.
func newSnapshotSource(path string, r runner, volOf volumeFunc) (*SnapshotSource, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		abs = resolved
	}
	base, _, err := volOf(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve volume for %s: %w", abs, err)
	}

	s := &SnapshotSource{base: base, r: r}
	ok := false
	defer func() {
		if !ok {
			_ = s.Close() // tear down whatever was set up on any failure
		}
	}()

	// Create the snapshot; fall back to the standalone `tmutil snapshot` verb if
	// `localsnapshot` produced nothing parseable (e.g. no TM-eligible volume).
	out, err := r.run("sudo", "tmutil", "localsnapshot")
	if err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}
	date, derr := parseSnapshotDate(out)
	if derr != nil {
		if out, err = r.run("tmutil", "snapshot"); err != nil {
			return nil, fmt.Errorf("create snapshot: %w", err)
		}
		if date, err = parseSnapshotDate(out); err != nil {
			return nil, err
		}
	}
	s.snapDate = date
	s.snapName = "com.apple.TimeMachine." + date + ".local"

	if s.mount, err = os.MkdirTemp("", "pbmac-snap-"); err != nil {
		return nil, err
	}
	if _, err = r.run("sudo", "/sbin/mount_apfs", "-o", "nobrowse,ro", "-s", s.snapName, base, s.mount); err != nil {
		return nil, fmt.Errorf("mount snapshot: %w", err)
	}

	if s.fs, err = NewLiveDirectoryFS(filepath.Join(s.mount, volumeRel(base, abs))); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

// FS returns the filesystem rooted at the snapshot copy of the requested path.
func (s *SnapshotSource) FS() *LiveDirectoryFS { return s.fs }

// Close unmounts the snapshot, deletes it, and removes the temp mountpoint. It is
// idempotent and best-effort, aggregating any errors.
func (s *SnapshotSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var errs []error
	if s.mount != "" {
		if _, err := s.r.run("diskutil", "unmount", s.mount); err != nil {
			if _, e2 := s.r.run("sudo", "umount", s.mount); e2 != nil {
				errs = append(errs, fmt.Errorf("unmount %s: %w", s.mount, err))
			}
		}
		if err := os.Remove(s.mount); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if s.snapDate != "" {
		if _, err := s.r.run("sudo", "tmutil", "deletelocalsnapshots", s.snapDate); err != nil {
			errs = append(errs, fmt.Errorf("delete snapshot %s: %w", s.snapDate, err))
		}
	}
	return errors.Join(errs...)
}
