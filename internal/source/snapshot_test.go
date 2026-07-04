package source

import (
	"os"
	"strings"
	"testing"
)

func TestParseSnapshotDate(t *testing.T) {
	got, err := parseSnapshotDate("Created local snapshot with date: 2026-07-04-120000\n")
	if err != nil || got != "2026-07-04-120000" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseSnapshotDate("nothing here"); err == nil {
		t.Fatal("expected error for missing date")
	}
}

func TestVolumeRel(t *testing.T) {
	cases := []struct{ base, abs, want string }{
		{"/System/Volumes/Data", "/System/Volumes/Data/Users/foo", "Users/foo"},
		{"/System/Volumes/Data", "/Users/foo/Documents", "Users/foo/Documents"}, // firmlink
		{"/System/Volumes/Data", "/System/Volumes/Data", ""},                    // volume root
		{"/", "/etc/hosts", "etc/hosts"},
		{"/", "/", ""},
	}
	for _, c := range cases {
		if got := volumeRel(c.base, c.abs); got != c.want {
			t.Errorf("volumeRel(%q,%q) = %q, want %q", c.base, c.abs, got, c.want)
		}
	}
}

// fakeRunner records every command and canned-answers the snapshot-create call.
type fakeRunner struct{ calls [][]string }

func (f *fakeRunner) run(name string, args ...string) (string, error) {
	full := append([]string{name}, args...)
	f.calls = append(f.calls, full)
	line := strings.Join(full, " ")
	if strings.Contains(line, "snapshot") && !strings.Contains(line, "deletelocalsnapshots") {
		return "Created local snapshot with date: 2026-07-04-120000\n", nil
	}
	return "", nil
}

func (f *fakeRunner) sawCmd(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(strings.Join(c, " "), substr) {
			return true
		}
	}
	return false
}

// TestSnapshotSourceCommands verifies the create/mount/cleanup command vectors.
// The final FS-root step only resolves on darwin (it depends on filepath.Abs and
// the fake mount having the subtree), so the constructor is expected to error off
// darwin — but by then the snapshot has been created, mounted, and (via the
// constructor's teardown) unmounted+deleted, which is exactly what we assert.
func TestSnapshotSourceCommands(t *testing.T) {
	fr := &fakeRunner{}
	volOf := func(string) (string, string, error) { return "/System/Volumes/Data", "/dev/fake", nil }

	_, _ = newSnapshotSource("/Users/foo/Documents", fr, volOf)

	if !fr.sawCmd("sudo tmutil localsnapshot") {
		t.Error("did not create a snapshot")
	}
	if !fr.sawCmd("mount_apfs -o nobrowse,ro -s com.apple.TimeMachine.2026-07-04-120000.local /System/Volumes/Data") {
		t.Errorf("mount_apfs not invoked as expected: %v", fr.calls)
	}
	// The constructor's teardown must delete the snapshot even though FS rooting failed.
	if !fr.sawCmd("tmutil deletelocalsnapshots 2026-07-04-120000") {
		t.Errorf("snapshot not cleaned up: %v", fr.calls)
	}
}

// TestSnapshotClose exercises the happy-path teardown directly (cross-platform):
// unmount, delete, remove the temp mount, and idempotency.
func TestSnapshotClose(t *testing.T) {
	fr := &fakeRunner{}
	mount := t.TempDir()
	s := &SnapshotSource{r: fr, mount: mount, snapDate: "2026-07-04-120000"}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !fr.sawCmd("unmount") {
		t.Error("did not unmount")
	}
	if !fr.sawCmd("tmutil deletelocalsnapshots 2026-07-04-120000") {
		t.Error("did not delete the snapshot")
	}
	if _, err := os.Stat(mount); !os.IsNotExist(err) {
		t.Errorf("temp mountpoint not removed: %v", err)
	}

	n := len(fr.calls)
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if len(fr.calls) != n {
		t.Error("second Close re-ran teardown")
	}
}
