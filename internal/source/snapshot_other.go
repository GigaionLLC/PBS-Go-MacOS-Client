//go:build !darwin

package source

import (
	"errors"
	"io"
)

// ErrSnapshotUnsupported is returned by OpenSnapshot on non-macOS platforms.
var ErrSnapshotUnsupported = errors.New("APFS snapshot source is only supported on macOS")

// OpenSnapshot is unsupported off macOS. (The orchestration in snapshot.go is
// still compiled and unit-tested everywhere via newSnapshotSource + a fake runner.)
func OpenSnapshot(string) (*LiveDirectoryFS, io.Closer, error) {
	return nil, nil, ErrSnapshotUnsupported
}
