//go:build windows

package source

import (
	"os"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// POSIX file-type bits (S_IFMT), defined here so this file needs no syscall
// package (Windows has no syscall.Stat_t).
const (
	modeDir     = 0o040000 // S_IFDIR
	modeRegular = 0o100000 // S_IFREG
	modeSymlink = 0o120000 // S_IFLNK
)

// metaFromInfo synthesizes pxar metadata on Windows. This path exists only so
// the client builds and its tests run during local development on Windows; the
// real backup target is darwin, where metaFromInfo (meta_unix.go) reads true
// POSIX owner/mode/mtime. Here UID/GID are 0 and the mode carries just the Go
// permission bits plus the file-type bit, which is enough for pxar encoding.
func metaFromInfo(fi os.FileInfo) (pxar.Meta, error) {
	m := fi.Mode()
	mode := uint64(m.Perm())
	switch {
	case m&os.ModeSymlink != 0:
		mode |= modeSymlink
	case m.IsDir():
		mode |= modeDir
	default:
		mode |= modeRegular
	}
	mt := fi.ModTime()
	return pxar.Meta{
		Mode:       mode,
		MtimeSecs:  mt.Unix(),
		MtimeNanos: uint32(mt.Nanosecond()),
		Size:       uint64(fi.Size()),
	}, nil
}
