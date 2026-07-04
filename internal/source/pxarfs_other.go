//go:build unix && !darwin

package source

import "syscall"

// statMtime extracts the modification time on Linux and other non-darwin unixes
// (Mtim). The `unix` constraint keeps this off Windows, which has no
// syscall.Stat_t (see meta_windows.go). Present so the project builds and tests
// off macOS during development.
func statMtime(st *syscall.Stat_t) (int64, uint32) {
	return int64(st.Mtim.Sec), uint32(st.Mtim.Nsec)
}
