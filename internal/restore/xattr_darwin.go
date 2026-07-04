//go:build darwin

package restore

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// applyXattrs sets each extended attribute on path (create-or-replace, flags=0).
// Best-effort: some protected com.apple.* attributes may return EPERM, which is
// ignored so a restore never fails over an xattr.
func applyXattrs(path string, xs map[string][]byte) {
	for name, value := range xs {
		err := unix.Setxattr(path, name, value, 0)
		if os.Getenv("PBMAC_DEBUG_XATTR") != "" && name == "com.apple.system.Security" {
			fmt.Fprintf(os.Stderr, "pbmac-debug: setxattr %s len=%d on %s -> %v\n", name, len(value), path, err)
		}
	}
}
