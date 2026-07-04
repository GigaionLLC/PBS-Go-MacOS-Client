//go:build darwin

package source

import "golang.org/x/sys/unix"

// readXattrs returns a file's extended attributes (name -> raw value) via the
// libSystem listxattr/getxattr wrappers in x/sys/unix. macOS com.apple.* names
// (quarantine, FinderInfo, ResourceFork, tags) are returned verbatim. Best-effort
// per attribute. The caller skips symlinks, so following-vs-NOFOLLOW is moot here.
func readXattrs(path string) (map[string][]byte, error) {
	sz, err := unix.Listxattr(path, nil)
	if err != nil {
		return nil, err
	}
	if sz == 0 {
		return nil, nil
	}
	nbuf := make([]byte, sz)
	sz, err = unix.Listxattr(path, nbuf)
	if err != nil {
		return nil, err
	}
	names := splitNUL(nbuf[:sz])
	if len(names) == 0 {
		return nil, nil
	}
	xs := make(map[string][]byte, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		vsz, err := unix.Getxattr(path, name, nil)
		if err != nil {
			continue
		}
		vbuf := make([]byte, vsz)
		if vsz > 0 {
			if _, err := unix.Getxattr(path, name, vbuf); err != nil {
				continue
			}
		}
		xs[name] = vbuf
	}
	if len(xs) == 0 {
		return nil, nil
	}
	return xs, nil
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
