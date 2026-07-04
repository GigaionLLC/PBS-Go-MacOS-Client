//go:build darwin

package source

import "golang.org/x/sys/unix"

// aclXattrName is the pseudo-xattr macOS exposes the native (NFSv4/kauth) ACL
// under. getxattr/setxattr marshal the kauth_filesec blob to/from it (this is how
// copyfile(COPYFILE_ACL) moves ACLs). It is FILTERED OUT of listxattr, so it must
// be fetched by explicit name — carrying it verbatim through the xattr channel is
// how we get lossless macOS↔macOS ACL fidelity without cgo or a lossy POSIX map.
const aclXattrName = "com.apple.system.Security"

// readXattrs returns a file's extended attributes (name -> raw value) via the
// libSystem listxattr/getxattr wrappers in x/sys/unix: the listxattr-enumerated
// names (com.apple.* quarantine/FinderInfo/ResourceFork/tags, verbatim) plus the
// native ACL blob fetched explicitly. Best-effort per attribute. The caller skips
// symlinks, so following-vs-NOFOLLOW is moot here.
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

	// The ACL pseudo-xattr, present only on files/dirs that carry an ACL.
	if v, err := getXattrValue(path, aclXattrName); err == nil {
		xs[aclXattrName] = v
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
