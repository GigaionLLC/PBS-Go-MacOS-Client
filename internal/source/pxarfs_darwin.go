//go:build darwin

package source

import "syscall"

// statMtime extracts the modification time on macOS (Mtimespec).
func statMtime(st *syscall.Stat_t) (int64, uint32) {
	return int64(st.Mtimespec.Sec), uint32(st.Mtimespec.Nsec)
}
