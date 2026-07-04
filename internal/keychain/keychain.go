// Package keychain stores secrets (the PBS API token) in the macOS Keychain via
// the `security` CLI, avoiding CGo so the client still cross-compiles from other
// hosts. On non-macOS platforms the operations report ErrUnsupported.
package keychain

import "errors"

// Service is the Keychain service name under which pbmac stores secrets; the
// account is the repository spec.
const Service = "pbmac"

var (
	// ErrUnsupported is returned on platforms without the macOS Keychain.
	ErrUnsupported = errors.New("keychain: only supported on macOS")
	// ErrNotFound is returned when no matching item exists.
	ErrNotFound = errors.New("keychain: item not found")
)
