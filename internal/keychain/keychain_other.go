//go:build !darwin

package keychain

// Store is unsupported off macOS.
func Store(account, secret string) error { return ErrUnsupported }

// Retrieve is unsupported off macOS.
func Retrieve(account string) (string, error) { return "", ErrUnsupported }

// Delete is unsupported off macOS.
func Delete(account string) error { return ErrUnsupported }
