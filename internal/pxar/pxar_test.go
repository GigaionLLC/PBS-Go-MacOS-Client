package pxar

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

// Reference SipHash-2-4 test vector (key 00..0f, input 0..14).
func TestSipHash24Vector(t *testing.T) {
	key0 := uint64(0x0706050403020100)
	key1 := uint64(0x0f0e0d0c0b0a0908)
	in := make([]byte, 15)
	for i := range in {
		in[i] = byte(i)
	}
	if got := siphash24(key0, key1, in); got != 0xa129ca6149be45e5 {
		t.Fatalf("siphash24 = %#x, want 0xa129ca6149be45e5", got)
	}
}

// --- in-memory filesystem for the encoder ---

type memEntry struct {
	meta     Meta
	children []string
	data     []byte
	target   string
}

type memFS map[string]memEntry

func (m memFS) Stat(p string) (Meta, error)        { return m[p].meta, nil }
func (m memFS) ReadDir(p string) ([]string, error) { return m[p].children, nil }
func (m memFS) Readlink(p string) (string, error)  { return m[p].target, nil }
func (m memFS) Open(p string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m[p].data)), nil
}

func dir(children ...string) memEntry {
	return memEntry{meta: Meta{Mode: sIFDIR | 0o755}, children: children}
}
func reg(data string) memEntry {
	return memEntry{meta: Meta{Mode: sIFREG | 0o644, Size: uint64(len(data))}, data: []byte(data)}
}

type item struct {
	offset  uint64
	htype   uint64
	content []byte
}

func parse(t *testing.T, buf []byte) []item {
	t.Helper()
	var items []item
	var off uint64
	for int(off) < len(buf) {
		if int(off)+HeaderSize > len(buf) {
			t.Fatalf("truncated header at %d", off)
		}
		ht := binary.LittleEndian.Uint64(buf[off : off+8])
		size := binary.LittleEndian.Uint64(buf[off+8 : off+16])
		if size < HeaderSize || off+size > uint64(len(buf)) {
			t.Fatalf("bad item size %d at %d", size, off)
		}
		items = append(items, item{off, ht, buf[off+HeaderSize : off+size]})
		off += size
	}
	if off != uint64(len(buf)) {
		t.Fatalf("items do not cover buffer: %d vs %d", off, len(buf))
	}
	return items
}

func TestEncodeStructure(t *testing.T) {
	fs := memFS{
		"/":          dir("file.txt", "sub"),
		"/file.txt":  reg("hello"),
		"/sub":       dir("b.txt"),
		"/sub/b.txt": reg("world"),
	}

	var out bytes.Buffer
	if err := NewEncoder(&out).Encode(fs, "/"); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	items := parse(t, out.Bytes())

	// Expected htype sequence.
	want := []uint64{
		FormatVersion,  // v2 header
		Entry,          // root dir
		Filename,       // file.txt
		Entry, Payload, // its metadata + content
		Filename,       // sub
		Entry,          // sub dir
		Filename,       // b.txt
		Entry, Payload, // its metadata + content
		Goodbye, // sub's goodbye
		Goodbye, // root's goodbye
	}
	if len(items) != len(want) {
		t.Fatalf("item count = %d, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].htype != w {
			t.Fatalf("item %d htype = %#x, want %#x", i, items[i].htype, w)
		}
	}

	// FORMAT_VERSION content is the u64 version 2.
	if binary.LittleEndian.Uint64(items[0].content) != FormatVersionV2 {
		t.Fatal("wrong format version")
	}
	// Payloads carry the file bytes.
	if string(items[4].content) != "hello" || string(items[9].content) != "world" {
		t.Fatalf("payload mismatch: %q %q", items[4].content, items[9].content)
	}
	// FILENAME items are null-terminated.
	if string(items[2].content) != "file.txt\x00" {
		t.Fatalf("filename content = %q", items[2].content)
	}

	// Validate every GOODBYE table: child offsets must resolve to FILENAME
	// items and the tail marker to an ENTRY item.
	offToHtype := map[uint64]uint64{}
	for _, it := range items {
		offToHtype[it.offset] = it.htype
	}
	for _, it := range items {
		if it.htype != Goodbye {
			continue
		}
		n := len(it.content) / 24
		if n < 1 {
			t.Fatal("empty goodbye table")
		}
		goodbyeStart := it.offset
		sawTail := false
		for k := 0; k < n; k++ {
			rec := it.content[k*24 : k*24+24]
			hash := binary.LittleEndian.Uint64(rec[0:8])
			gofs := binary.LittleEndian.Uint64(rec[8:16])
			size := binary.LittleEndian.Uint64(rec[16:24])
			if hash == GoodbyeTailMark {
				sawTail = true
				if offToHtype[goodbyeStart-gofs] != Entry {
					t.Fatal("tail marker does not point to an ENTRY")
				}
				if size != uint64(len(it.content)+HeaderSize) {
					t.Fatalf("tail size = %d, want %d", size, len(it.content)+HeaderSize)
				}
				continue
			}
			if offToHtype[goodbyeStart-gofs] != Filename {
				t.Fatalf("goodbye child offset does not point to a FILENAME")
			}
		}
		if !sawTail {
			t.Fatal("goodbye table missing tail marker")
		}
	}
}
