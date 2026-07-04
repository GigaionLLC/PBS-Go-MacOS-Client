package restore

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// Extractor writes decoded entries to a destination directory. If Only is set,
// only that archive path (and its ancestor directories) is materialized.
type Extractor struct {
	Dest string
	Only string // e.g. "/sub/file.txt"; empty = extract everything
}

func perm(mode uint64) os.FileMode { return os.FileMode(mode & 0o777) }

func (e *Extractor) want(path string) bool {
	if e.Only == "" || path == "" {
		return true
	}
	// Extract the target itself and any directory on its path.
	return path == e.Only || strings.HasPrefix(e.Only, path+"/")
}

func (e *Extractor) dest(path string) string {
	return filepath.Join(e.Dest, filepath.FromSlash(path))
}

func setTimes(p string, m pxar.Meta) {
	t := time.Unix(m.MtimeSecs, int64(m.MtimeNanos))
	_ = os.Chtimes(p, t, t)
}

// OnDir creates the directory.
func (e *Extractor) OnDir(path string, m pxar.Meta) error {
	if !e.want(path) {
		return nil
	}
	d := e.dest(path)
	if err := os.MkdirAll(d, perm(m.Mode)|0o700); err != nil {
		return err
	}
	applyXattrs(d, m.Xattrs) // before setTimes: setxattr bumps ctime, not mtime
	setTimes(d, m)
	return nil
}

// OnFile writes the file content and restores mode/mtime.
func (e *Extractor) OnFile(path string, m pxar.Meta, content io.Reader) error {
	if !e.want(path) {
		return nil // content is drained by the decoder
	}
	d := e.dest(path)
	if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(d, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm(m.Mode))
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, content); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Chmod(d, perm(m.Mode))
	applyXattrs(d, m.Xattrs)
	setTimes(d, m)
	return nil
}

// OnSymlink recreates the symlink.
func (e *Extractor) OnSymlink(path string, m pxar.Meta, target string) error {
	if !e.want(path) {
		return nil
	}
	d := e.dest(path)
	if err := os.MkdirAll(filepath.Dir(d), 0o700); err != nil {
		return err
	}
	_ = os.Remove(d)
	if err := os.Symlink(target, d); err != nil {
		return err
	}
	applyXattrs(d, m.Xattrs) // NOFOLLOW binds to the link itself
	return nil
}

// ListEntry is one entry produced by Lister.
type ListEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "dir" | "file" | "symlink"
	Size uint64 `json:"size"`
	Mode uint64 `json:"mode"`
}

// Lister collects the archive's entries without writing to disk.
type Lister struct {
	Entries []ListEntry
}

// OnDir records a directory.
func (l *Lister) OnDir(path string, m pxar.Meta) error {
	l.Entries = append(l.Entries, ListEntry{Path: pathOrRoot(path), Type: "dir", Mode: m.Mode})
	return nil
}

// OnFile records a file (draining its content).
func (l *Lister) OnFile(path string, m pxar.Meta, content io.Reader) error {
	n, err := io.Copy(io.Discard, content)
	if err != nil {
		return err
	}
	l.Entries = append(l.Entries, ListEntry{Path: path, Type: "file", Size: uint64(n), Mode: m.Mode})
	return nil
}

// OnSymlink records a symlink.
func (l *Lister) OnSymlink(path string, m pxar.Meta, target string) error {
	l.Entries = append(l.Entries, ListEntry{Path: path, Type: "symlink", Mode: m.Mode})
	return nil
}

func pathOrRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

var (
	_ pxar.Visitor = (*Extractor)(nil)
	_ pxar.Visitor = (*Lister)(nil)
)
