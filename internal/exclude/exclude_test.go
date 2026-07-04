package exclude

import "testing"

func TestExcluded(t *testing.T) {
	m := New([]string{
		"# comment",
		"*.log",         // basename glob, any depth
		"node_modules/", // dir-only, any depth (floating)
		"/build",        // anchored to root
		"src/*.tmp",     // anchored (internal slash)
		"**/cache",      // any depth via **
		"!keep.log",     // re-include
	})

	cases := []struct {
		path string
		dir  bool
		want bool
	}{
		{"/app.log", false, true},
		{"/sub/deep/app.log", false, true},
		{"/keep.log", false, false},      // negated
		{"/node_modules", true, true},    // dir-only, matches dir
		{"/node_modules", false, false},  // not a dir -> pattern skipped
		{"/x/node_modules", true, true},  // floating, any depth
		{"/build", true, true},           // anchored root
		{"/sub/build", true, false},      // anchored -> not matched deeper
		{"/src/a.tmp", false, true},      // anchored internal slash
		{"/other/a.tmp", false, false},   // not under src
		{"/a/b/cache", true, true},       // **/cache
		{"/cache", true, true},           // ** matches zero segments
		{"/readme.md", false, false},     // nothing matches
	}
	for _, c := range cases {
		if got := m.Excluded(c.path, c.dir); got != c.want {
			t.Errorf("Excluded(%q, dir=%v) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}

func TestEmpty(t *testing.T) {
	if !New(nil).Empty() {
		t.Error("nil patterns should be empty")
	}
	if New([]string{"*.log"}).Empty() {
		t.Error("should not be empty")
	}
}
