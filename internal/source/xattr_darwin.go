//go:build darwin

package source

import (
	"syscall"
	"unsafe"
)

// XATTR_NOFOLLOW: operate on a symlink itself, not its target.
const xattrNoFollow = 0x0001

// readXattrs returns a path's extended attributes (name -> raw value). It uses
// XATTR_NOFOLLOW so a symlink's own xattrs are read, never the target's. macOS
// com.apple.* names (quarantine, FinderInfo, ResourceFork, tags) are returned
// verbatim. Best-effort per attribute; a vanished attr is skipped.
func readXattrs(path string) (map[string][]byte, error) {
	names, err := listXattr(path)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	xs := make(map[string][]byte, len(names))
	for _, name := range names {
		v, err := getXattr(path, name)
		if err != nil {
			continue
		}
		xs[name] = v
	}
	if len(xs) == 0 {
		return nil, nil
	}
	return xs, nil
}

// listXattr wraps darwin listxattr(path, namebuf, size, options).
func listXattr(path string) ([]string, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	sz, _, errno := syscall.Syscall6(syscall.SYS_LISTXATTR,
		uintptr(unsafe.Pointer(p)), 0, 0, xattrNoFollow, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	if sz == 0 {
		return nil, nil
	}
	buf := make([]byte, sz)
	n, _, errno := syscall.Syscall6(syscall.SYS_LISTXATTR,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), xattrNoFollow, 0, 0)
	if errno != 0 {
		return nil, errno
	}
	var names []string
	for _, part := range splitNUL(buf[:n]) {
		if part != "" {
			names = append(names, part)
		}
	}
	return names, nil
}

// getXattr wraps darwin getxattr(path, name, value, size, position, options).
func getXattr(path, name string) ([]byte, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	np, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	sz, _, errno := syscall.Syscall6(syscall.SYS_GETXATTR,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(np)), 0, 0, 0, xattrNoFollow)
	if errno != 0 {
		return nil, errno
	}
	if sz == 0 {
		return []byte{}, nil
	}
	buf := make([]byte, sz)
	n, _, errno := syscall.Syscall6(syscall.SYS_GETXATTR,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(np)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, xattrNoFollow)
	if errno != 0 {
		return nil, errno
	}
	return buf[:n], nil
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
