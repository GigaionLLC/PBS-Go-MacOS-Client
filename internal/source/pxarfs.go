package source

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// LiveDirectoryFS implements pxar.Filesystem over a directory on the live
// filesystem, rooted at a real path. Virtual paths are '/'-rooted (the encoder
// is invoked with root "/"). Symlinks are captured as symlinks (Lstat), not
// followed. The platform-specific mtime extraction lives in statMtime.
type LiveDirectoryFS struct {
	root string
}

// NewLiveDirectoryFS roots a pxar filesystem at dir.
func NewLiveDirectoryFS(dir string) (*LiveDirectoryFS, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	return &LiveDirectoryFS{root: abs}, nil
}

// Root returns the absolute root directory.
func (f *LiveDirectoryFS) Root() string { return f.root }

func (f *LiveDirectoryFS) real(p string) string {
	return filepath.Join(f.root, filepath.FromSlash(p))
}

// Stat returns pxar metadata for the entry at virtual path p. The extraction of
// POSIX owner/mode/mtime from the os.FileInfo is platform-specific (metaFromInfo
// lives in meta_unix.go / meta_windows.go).
func (f *LiveDirectoryFS) Stat(p string) (pxar.Meta, error) {
	real := f.real(p)
	fi, err := os.Lstat(real)
	if err != nil {
		return pxar.Meta{}, err
	}
	m, err := metaFromInfo(fi)
	if err != nil {
		return pxar.Meta{}, fmt.Errorf("stat %s: %w", p, err)
	}
	// Extended attributes (macOS com.apple.*; no-op off darwin).
	xs, err := readXattrs(real)
	if err != nil {
		return pxar.Meta{}, fmt.Errorf("xattr %s: %w", p, err)
	}
	m.Xattrs = xs
	return m, nil
}

// ReadDir returns the child base names of directory p.
func (f *LiveDirectoryFS) ReadDir(p string) ([]string, error) {
	ents, err := os.ReadDir(f.real(p))
	if err != nil {
		return nil, err
	}
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name()
	}
	return names, nil
}

// Open opens a regular file for reading.
func (f *LiveDirectoryFS) Open(p string) (io.ReadCloser, error) { return os.Open(f.real(p)) }

// Readlink returns a symlink's target.
func (f *LiveDirectoryFS) Readlink(p string) (string, error) { return os.Readlink(f.real(p)) }
