//go:build darwin

package restore

import (
	"golang.org/x/sys/unix"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// macOS chflags bits we restore. We apply the user-settable (UF_) variants so no
// elevated privileges are needed; the SF_ (system) immutable/append are not
// distinguishable through the pxar bit anyway.
const (
	ufNodump    = 0x00000001
	ufImmutable = 0x00000002
	ufAppend    = 0x00000004
	ufHidden    = 0x00008000
)

// applyFlags maps pxar entry flags back to macOS chflags and sets them via
// chflags (best-effort). Immutable/append lock the inode, so callers must apply
// flags only once the entry's content, xattrs, and times are final; for
// directories, callers mask off the locking bits (they would block child
// restore). No lchflags on darwin, so symlinks are not handled.
func applyFlags(path string, pxarFlags uint64) {
	var f uint32
	if pxarFlags&pxar.FlagImmutable != 0 {
		f |= ufImmutable
	}
	if pxarFlags&pxar.FlagAppend != 0 {
		f |= ufAppend
	}
	if pxarFlags&pxar.FlagHidden != 0 {
		f |= ufHidden
	}
	if pxarFlags&pxar.FlagNodump != 0 {
		f |= ufNodump
	}
	if f == 0 {
		return
	}
	_ = unix.Chflags(path, int(f))
}
