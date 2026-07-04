//go:build darwin

package source

import (
	"os"
	"syscall"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// macOS chflags bits with a pxar equivalent (sys/stat.h).
const (
	ufNodump    = 0x00000001
	ufImmutable = 0x00000002
	ufAppend    = 0x00000004
	ufHidden    = 0x00008000
	sfImmutable = 0x00020000
	sfAppend    = 0x00040000
)

// fileFlags maps a file's macOS st_flags to the pxar entry-flags subset we carry.
// The user (UF_) and system (SF_) immutable/append variants collapse onto the
// single pxar bit (pxar inherits Linux's model, which has one of each). System-
// managed flags (SIP, dataless, firmlink, compressed) have no equivalent and are
// dropped.
func fileFlags(fi os.FileInfo) uint64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	f := uint32(st.Flags)
	var out uint64
	if f&(ufImmutable|sfImmutable) != 0 {
		out |= pxar.FlagImmutable
	}
	if f&(ufAppend|sfAppend) != 0 {
		out |= pxar.FlagAppend
	}
	if f&ufHidden != 0 {
		out |= pxar.FlagHidden
	}
	if f&ufNodump != 0 {
		out |= pxar.FlagNodump
	}
	return out
}
