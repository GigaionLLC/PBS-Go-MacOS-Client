package source_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/backup"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/restore"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/source"
)

// POSIX S_IFMT type bits we assert on the mode metaFromInfo produces.
const (
	sIFMT  = 0o170000
	sIFREG = 0o100000
	sIFDIR = 0o040000
)

// writeTree lays down a small directory tree and returns the temp root.
func writeTree(t *testing.T) (root string, files map[string][]byte) {
	t.Helper()
	root = t.TempDir()
	files = map[string][]byte{
		"/a.txt":     []byte("hello pbmac"),
		"/sub/b.txt": []byte("nested bytes\x00\x01\x02"),
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for vp, data := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(vp)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, files
}

// TestLiveDirectoryFSStat checks that metaFromInfo (meta_unix.go / meta_windows.go)
// yields sane pxar metadata over a real filesystem: correct size, the right
// S_IFMT type bit for files vs dirs, and a populated mtime.
func TestLiveDirectoryFSStat(t *testing.T) {
	root, files := writeTree(t)
	fs, err := source.NewLiveDirectoryFS(root)
	if err != nil {
		t.Fatal(err)
	}

	fm, err := fs.Stat("/a.txt")
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if fm.Size != uint64(len(files["/a.txt"])) {
		t.Errorf("file size = %d, want %d", fm.Size, len(files["/a.txt"]))
	}
	if fm.Mode&sIFMT != sIFREG {
		t.Errorf("file mode type = %#o, want S_IFREG %#o", fm.Mode&sIFMT, sIFREG)
	}
	if fm.MtimeSecs <= 0 {
		t.Errorf("file mtime not populated: %d", fm.MtimeSecs)
	}

	dm, err := fs.Stat("/sub")
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if dm.Mode&sIFMT != sIFDIR {
		t.Errorf("dir mode type = %#o, want S_IFDIR %#o", dm.Mode&sIFMT, sIFDIR)
	}
}

// --- minimal in-memory chunk store / visitor for the round-trip ---

type memSink map[[32]byte][]byte

func (s memSink) Put(d [32]byte, _ uint32, blob []byte) error {
	s[d] = append([]byte(nil), blob...)
	return nil
}

type memStore struct {
	files  map[string][]byte
	chunks memSink
}

func (m memStore) Download(name string) ([]byte, error) {
	b, ok := m.files[name]
	if !ok {
		return nil, fmt.Errorf("no file %q", name)
	}
	return b, nil
}
func (m memStore) Chunk(d [32]byte) ([]byte, error) {
	b, ok := m.chunks[d]
	if !ok {
		return nil, fmt.Errorf("missing chunk %x", d)
	}
	return b, nil
}

type collector struct {
	files map[string][]byte
	dirs  map[string]bool
}

func (c *collector) OnDir(p string, _ pxar.Meta) error { c.dirs[p] = true; return nil }
func (c *collector) OnFile(p string, _ pxar.Meta, r io.Reader) error {
	b, err := io.ReadAll(r)
	c.files[p] = b
	return err
}
func (c *collector) OnSymlink(string, pxar.Meta, string) error { return nil }

// TestSourceBackupRestoreRoundTrip drives the real LiveDirectoryFS (not an
// in-memory stub) through the offline backup pipeline and back out through
// restore, asserting the file bytes survive. This complements the format-level
// round-trip in internal/restore by exercising the on-disk source adapter, and
// it runs on every platform (including Windows for local development).
func TestSourceBackupRestoreRoundTrip(t *testing.T) {
	root, files := writeTree(t)
	fs, err := source.NewLiveDirectoryFS(root)
	if err != nil {
		t.Fatal(err)
	}

	sink := memSink{}
	_, idx, err := backup.Run(fs, "/", sink, backup.Options{})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	store := memStore{
		files:  map[string][]byte{"root.pxar.didx": idx.Serialize()},
		chunks: sink,
	}

	col := &collector{files: map[string][]byte{}, dirs: map[string]bool{}}
	if err := restore.Extract(store, "root.pxar", nil, col); err != nil {
		t.Fatalf("restore: %v", err)
	}

	for vp, want := range files {
		got, ok := col.files[vp]
		if !ok {
			t.Errorf("restored tree missing %s", vp)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("file %s mismatch: got %d bytes, want %d", vp, len(got), len(want))
		}
	}
	if !col.dirs["/sub"] {
		t.Error("restored tree missing /sub directory")
	}
}
