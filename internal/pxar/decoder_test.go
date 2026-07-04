package pxar

import (
	"bytes"
	"fmt"
	"io"
	"testing"
)

// collector is a Visitor that records the decoded tree.
type collector struct {
	dirs  map[string]Meta
	files map[string][]byte
	links map[string]string
	metas map[string]Meta
}

func newCollector() *collector {
	return &collector{
		dirs:  map[string]Meta{},
		files: map[string][]byte{},
		links: map[string]string{},
		metas: map[string]Meta{},
	}
}

func (c *collector) OnDir(p string, m Meta) error { c.dirs[p] = m; c.metas[p] = m; return nil }
func (c *collector) OnFile(p string, m Meta, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	c.files[p] = b
	c.metas[p] = m
	return nil
}
func (c *collector) OnSymlink(p string, m Meta, target string) error {
	c.links[p] = target
	c.metas[p] = m
	return nil
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	fs := memFS{
		"/":               dir("a.txt", "empty", "link", "sub"),
		"/a.txt":          reg("hello world"),
		"/empty":          reg(""),
		"/link":           {meta: Meta{Mode: sIFLNK | 0o777}, target: "a.txt"},
		"/sub":            dir("nested.bin", "deep"),
		"/sub/nested.bin": reg("binary\x00\x01\x02data"),
		"/sub/deep":       dir("leaf"),
		"/sub/deep/leaf":  reg("bottom"),
	}
	// Give entries distinct mtimes to check metadata survives.
	for k, e := range fs {
		e.meta.MtimeSecs = 1_700_000_000 + int64(len(k))
		e.meta.MtimeNanos = 123
		fs[k] = e
	}

	var buf bytes.Buffer
	if err := NewEncoder(&buf).Encode(fs, "/"); err != nil {
		t.Fatalf("encode: %v", err)
	}

	c := newCollector()
	if err := NewDecoder(&buf).Walk(c); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Every regular file must round-trip byte-for-byte.
	wantFiles := map[string]string{
		"/a.txt":          "hello world",
		"/empty":          "",
		"/sub/nested.bin": "binary\x00\x01\x02data",
		"/sub/deep/leaf":  "bottom",
	}
	if len(c.files) != len(wantFiles) {
		t.Fatalf("decoded %d files, want %d: %v", len(c.files), len(wantFiles), keys(c.files))
	}
	for p, want := range wantFiles {
		if got, ok := c.files[p]; !ok || string(got) != want {
			t.Errorf("file %s = %q (present=%v), want %q", p, got, ok, want)
		}
	}

	// Directories and symlink.
	for _, p := range []string{"", "/sub", "/sub/deep"} {
		if _, ok := c.dirs[p]; !ok {
			t.Errorf("missing directory %q", p)
		}
	}
	if c.links["/link"] != "a.txt" {
		t.Errorf("symlink target = %q, want a.txt", c.links["/link"])
	}

	// Metadata (mode + mtime) must survive for a sample file.
	m := c.metas["/a.txt"]
	if m.Mode&sIFMT != sIFREG || m.Mode&0o777 != 0o644 {
		t.Errorf("a.txt mode = %#o", m.Mode)
	}
	if m.MtimeNanos != 123 {
		t.Errorf("a.txt mtime nanos = %d, want 123", m.MtimeNanos)
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Ensure the collector satisfies Visitor.
var _ Visitor = (*collector)(nil)
var _ = fmt.Sprint
