//go:build darwin

package restore

import (
	"syscall"
	"unsafe"
)

// XATTR_NOFOLLOW: set on the symlink itself, not its target.
const xattrNoFollow = 0x0001

// applyXattrs sets each extended attribute on path (create-or-replace). It is
// best-effort: some system attributes (e.g. com.apple.* protected ones) may
// return EPERM, which is ignored so a restore never fails over an xattr.
func applyXattrs(path string, xs map[string][]byte) {
	for name, value := range xs {
		_ = setXattr(path, name, value)
	}
}

// setXattr wraps darwin setxattr(path, name, value, size, position, options).
func setXattr(path, name string, value []byte) error {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	np, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	var vp unsafe.Pointer
	if len(value) > 0 {
		vp = unsafe.Pointer(&value[0])
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_SETXATTR,
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(np)),
		uintptr(vp), uintptr(len(value)), 0, xattrNoFollow)
	if errno != 0 {
		return errno
	}
	return nil
}
