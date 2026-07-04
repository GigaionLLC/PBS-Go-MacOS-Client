//go:build darwin

package source

import "golang.org/x/sys/unix"

// readXattrs returns a file's extended attributes (name -> raw value) via the
// libSystem listxattr/getxattr wrappers in x/sys/unix. macOS com.apple.* names
// (quarantine, FinderInfo, ResourceFork, tags) are returned verbatim. Best-effort
// per attribute. The caller skips symlinks, so following-vs-NOFOLLOW is moot here.
//
// NOTE: NFSv4 ACLs are NOT captured — macOS exposes them only via the
// com.apple.system.Security pseudo-xattr, which getxattr rejects with EPERM, and
// the real ACL API needs cgo (see docs/DESIGN.md §7). Everything users typically
// rely on (resource forks, Finder info, tags, quarantine) is an ordinary xattr and
// is captured here.
func readXattrs(path string) (map[string][]byte, error) {
	xs := map[string][]byte{}
	sz, err := unix.Listxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if sz > 0 {
		nbuf := make([]byte, sz)
		if sz, err = unix.Listxattr(path, nbuf); err != nil {
			return nil, err
		}
		for _, name := range splitNUL(nbuf[:sz]) {
			if name == "" {
				continue
			}
			if v, err := getXattrValue(path, name); err == nil {
				xs[name] = v
			}
		}
	}
	if len(xs) == 0 {
		return nil, nil
	}
	return xs, nil
}

// getXattrValue reads one attribute (size query then fetch).
func getXattrValue(path, name string) ([]byte, error) {
	vsz, err := unix.Getxattr(path, name, nil)
	if err != nil {
		return nil, err
	}
	vbuf := make([]byte, vsz)
	if vsz > 0 {
		if _, err := unix.Getxattr(path, name, vbuf); err != nil {
			return nil, err
		}
	}
	return vbuf, nil
}

// splitNUL splits a NUL-separated name list into strings.
func splitNUL(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == 0 {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, string(b[start:]))
	}
	return out
}
