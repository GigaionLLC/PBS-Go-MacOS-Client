package restore

import (
	"path/filepath"
	"testing"
)

func TestExtractorSafeDest(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "target")
	e := &Extractor{Dest: dest}

	// In-target paths resolve under Dest.
	for _, p := range []string{"/", "/a.txt", "/sub/b.txt"} {
		got, err := e.safeDest(p)
		if err != nil {
			t.Errorf("safeDest(%q) errored: %v", p, err)
			continue
		}
		if rel, _ := filepath.Rel(dest, got); rel == ".." || len(rel) >= 2 && rel[:2] == ".." {
			t.Errorf("safeDest(%q) = %q escaped Dest", p, got)
		}
	}
	// Traversal paths must be refused (defense in depth beyond the decoder).
	for _, p := range []string{"/../../etc/x", "/../evil", "/a/../../b"} {
		if _, err := e.safeDest(p); err == nil {
			t.Errorf("safeDest(%q) = nil error, want rejection", p)
		}
	}
}

func TestExtractorWant(t *testing.T) {
	cases := []struct {
		only string
		path string
		want bool
	}{
		// Empty Only extracts everything.
		{"", "/anything", true},
		// Single-file target: itself and its ancestors, nothing else.
		{"/a/b/c.txt", "/a/b/c.txt", true},
		{"/a/b/c.txt", "/a", true},
		{"/a/b/c.txt", "/a/b", true},
		{"/a/b/c.txt", "/a/b/other.txt", false},
		{"/a/b/c.txt", "/a/z", false},
		// Directory target: itself, its ancestors, AND its whole subtree.
		{"/a/b", "/a/b", true},
		{"/a/b", "/a", true},
		{"/a/b", "/a/b/c.txt", true},
		{"/a/b", "/a/b/deep/nested.txt", true},
		{"/a/b", "/a/bcd", false}, // sibling with a shared prefix must not match
		{"/a/b", "/a/c", false},
	}
	for _, c := range cases {
		e := &Extractor{Only: c.only}
		if got := e.want(c.path); got != c.want {
			t.Errorf("Only=%q path=%q: want()=%v, want %v", c.only, c.path, got, c.want)
		}
	}
}
