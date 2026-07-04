//go:build darwin

package source

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
)

// OpenSnapshot creates an APFS local snapshot of the volume containing path,
// mounts it read-only, and returns a filesystem rooted at path's snapshot copy
// plus a Closer that unmounts and deletes the snapshot (defer it once the backup
// finishes). Requires sudo (mount_apfs/tmutil) and — on patched macOS — Full Disk
// Access for the reading process (a snapshot is not a TCC bypass).
func OpenSnapshot(path string) (*LiveDirectoryFS, io.Closer, error) {
	s, err := newSnapshotSource(path, execRunner{}, statfsVolume)
	if err != nil {
		return nil, nil, err
	}
	return s.fs, s, nil
}

type execRunner struct{}

func (execRunner) run(name string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// statfsVolume returns the base-volume mount point and device for path.
func statfsVolume(path string) (mountPoint, device string, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return "", "", err
	}
	return int8CStr(st.Mntonname[:]), int8CStr(st.Mntfromname[:]), nil
}

// int8CStr converts a NUL-terminated darwin C char array (int8) to a Go string.
func int8CStr(b []int8) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(b[i])
	}
	return string(out)
}
