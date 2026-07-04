//go:build !darwin

package source

// readXattrs is a no-op off macOS (Windows/Linux dev): pxar archives produced
// there simply carry no extended attributes. The real implementation is in
// xattr_darwin.go.
func readXattrs(string) (map[string][]byte, error) { return nil, nil }
