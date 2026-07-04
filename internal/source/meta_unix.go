//go:build unix

package source

import (
	"fmt"
	"os"
	"syscall"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// metaFromInfo extracts POSIX owner/mode/mtime from a Unix stat. The mtime split
// (Mtimespec on darwin, Mtim elsewhere) lives in statMtime.
func metaFromInfo(fi os.FileInfo) (pxar.Meta, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return pxar.Meta{}, fmt.Errorf("no unix stat for %s", fi.Name())
	}
	secs, nanos := statMtime(st)
	return pxar.Meta{
		Mode:       uint64(st.Mode),
		UID:        uint32(st.Uid),
		GID:        uint32(st.Gid),
		MtimeSecs:  secs,
		MtimeNanos: nanos,
		Size:       uint64(fi.Size()),
	}, nil
}
