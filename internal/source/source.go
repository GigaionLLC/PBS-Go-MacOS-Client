// Package source abstracts *where* the bytes to back up come from, so the
// backup pipeline never has to care whether it is reading files live or from a
// mounted APFS snapshot. v1 ships LiveDirectorySource; SnapshotSource (v2) will
// wrap tmutil/mount_apfs behind the same interface.
package source

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Entry is one filesystem object to be archived.
type Entry struct {
	// Path is the path relative to the source root, using '/' separators.
	Path string
	Info fs.FileInfo
}

// IsRegular reports whether the entry is a regular file (has content bytes).
func (e Entry) IsRegular() bool { return e.Info.Mode().IsRegular() }

// Source yields entries to archive and opens their content.
type Source interface {
	// Root returns a human-readable description of the source root.
	Root() string
	// Walk visits every entry under the root in a stable order.
	Walk(fn func(Entry) error) error
	// Open returns a reader for a regular-file entry's content.
	Open(e Entry) (io.ReadCloser, error)
	// Close releases any resources (e.g. an unmounted snapshot).
	Close() error
}

// LiveDirectorySource reads files directly from a directory on the live
// filesystem. No snapshot, no consistency guarantee beyond per-file reads.
type LiveDirectorySource struct {
	root string
}

// NewLiveDirectorySource returns a source rooted at dir. It verifies the path
// exists and is a directory.
func NewLiveDirectorySource(dir string) (*LiveDirectorySource, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "source", Path: abs, Err: os.ErrInvalid}
	}
	return &LiveDirectorySource{root: abs}, nil
}

// Root returns the absolute source directory.
func (s *LiveDirectorySource) Root() string { return s.root }

// Walk visits every entry under the root, in lexical order, skipping the root
// itself. Symlinks are reported but not followed.
func (s *LiveDirectorySource) Walk(fn func(Entry) error) error {
	return filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == s.root {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		return fn(Entry{Path: filepath.ToSlash(rel), Info: info})
	})
}

// Open opens a regular file entry for reading.
func (s *LiveDirectorySource) Open(e Entry) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.root, filepath.FromSlash(e.Path)))
}

// Close is a no-op for a live directory.
func (s *LiveDirectorySource) Close() error { return nil }
