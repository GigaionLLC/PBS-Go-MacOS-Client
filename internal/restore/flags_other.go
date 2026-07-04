//go:build !darwin

package restore

// applyFlags is a no-op off macOS.
func applyFlags(string, uint64) {}
