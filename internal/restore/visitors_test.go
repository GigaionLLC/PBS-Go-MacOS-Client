package restore

import "testing"

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
