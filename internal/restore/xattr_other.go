//go:build !darwin

package restore

// applyXattrs is a no-op off macOS. The real implementation is in xattr_darwin.go.
func applyXattrs(string, map[string][]byte) {}
