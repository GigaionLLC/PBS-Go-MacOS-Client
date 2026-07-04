package restore_test

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/backup"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/restore"
)

// --- in-memory pxar filesystem ---

const (
	modeDir  = 0o040000 | 0o755
	modeReg  = 0o100000 | 0o644
	modeLink = 0o120000 | 0o777
)

type tfile struct {
	mode     uint64
	data     []byte
	children []string
	target   string
}
type tfs map[string]tfile

func (f tfs) Stat(p string) (pxar.Meta, error) {
	e := f[p]
	return pxar.Meta{Mode: e.mode, Size: uint64(len(e.data)), MtimeSecs: 1_700_000_000}, nil
}
func (f tfs) ReadDir(p string) ([]string, error)  { return f[p].children, nil }
func (f tfs) Readlink(p string) (string, error)   { return f[p].target, nil }
func (f tfs) Open(p string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f[p].data)), nil
}

// --- in-memory chunk sink and store ---

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

// --- collecting visitor ---

type collector struct {
	files map[string][]byte
	links map[string]string
	dirs  map[string]bool
}

func newCollector() *collector {
	return &collector{files: map[string][]byte{}, links: map[string]string{}, dirs: map[string]bool{}}
}
func (c *collector) OnDir(p string, _ pxar.Meta) error { c.dirs[p] = true; return nil }
func (c *collector) OnFile(p string, _ pxar.Meta, r io.Reader) error {
	b, err := io.ReadAll(r)
	c.files[p] = b
	return err
}
func (c *collector) OnSymlink(p string, _ pxar.Meta, t string) error { c.links[p] = t; return nil }

func TestBackupRestoreRoundTrip(t *testing.T) {
	// Random data several times the max chunk size so the stream spans many
	// content-defined chunks; dup.txt is identical, so once the chunker
	// re-syncs inside it, those chunks share digests and dedup.
	big := make([]byte, 12<<20) // 12 MiB, spans several chunks
	rng := rand.New(rand.NewSource(42))
	rng.Read(big)
	fs := tfs{
		"/":         {mode: modeDir, children: []string{"big.txt", "dup.txt", "link", "sub"}},
		"/big.txt":  {mode: modeReg, data: big},
		"/dup.txt":  {mode: modeReg, data: big}, // identical -> should dedup
		"/link":     {mode: modeLink, target: "big.txt"},
		"/sub":      {mode: modeDir, children: []string{"small"}},
		"/sub/small": {mode: modeReg, data: []byte("small file")},
	}

	for _, encrypt := range []bool{false, true} {
		name := "plain"
		if encrypt {
			name = "encrypted"
		}
		t.Run(name, func(t *testing.T) {
			var cc *crypto.CryptConfig
			var key *crypto.Key
			if encrypt {
				k, _ := crypto.NewRandomKey()
				cc, _ = crypto.NewCryptConfig(k)
				ek := cc.EncKey()
				key = &ek
			}

			// Back up into an in-memory store.
			sink := memSink{}
			res, idx, err := backup.Run(fs, "/", sink, backup.Options{Crypt: cc})
			if err != nil {
				t.Fatalf("backup: %v", err)
			}

			store := memStore{
				files:  map[string][]byte{"root.pxar.didx": idx.Serialize()},
				chunks: sink,
			}

			// Restore and compare.
			col := newCollector()
			if err := restore.Extract(store, "root.pxar", key, col); err != nil {
				t.Fatalf("restore: %v", err)
			}
			want := map[string]string{
				"/big.txt":   string(big),
				"/dup.txt":   string(big),
				"/sub/small": "small file",
			}
			if len(col.files) != len(want) {
				t.Fatalf("restored %d files, want %d", len(col.files), len(want))
			}
			for p, w := range want {
				if string(col.files[p]) != w {
					t.Errorf("file %s mismatch (%d vs %d bytes)", p, len(col.files[p]), len(w))
				}
			}
			if col.links["/link"] != "big.txt" {
				t.Errorf("symlink = %q", col.links["/link"])
			}
			if !col.dirs["/sub"] {
				t.Error("missing /sub dir")
			}
			// Dedup efficiency depends on content-defined chunk alignment
			// (a chunker-tuning matter), so this is informational, not a
			// correctness assertion.
			t.Logf("%s: archive=%d bytes, chunks total=%d unique=%d (dedup %.0f%%)",
				name, res.ArchiveBytes, res.TotalChunks, res.UniqueChunks, res.DedupRatio()*100)
		})
	}
}
