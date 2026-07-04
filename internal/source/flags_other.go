//go:build !darwin

package source

import "os"

// fileFlags is a no-op off macOS: only darwin has BSD chflags/st_flags.
func fileFlags(os.FileInfo) uint64 { return 0 }
