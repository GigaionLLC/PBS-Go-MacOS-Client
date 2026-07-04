package pxar

import "testing"

func TestValidEntryName(t *testing.T) {
	valid := []string{"a", "a.txt", "..a", "a..", ".hidden", "with space", "résumé"}
	for _, n := range valid {
		if err := validEntryName(n); err != nil {
			t.Errorf("validEntryName(%q) = %v, want nil", n, err)
		}
	}
	// Names that could escape the target or corrupt the tree must be rejected.
	invalid := []string{"", ".", "..", "a/b", "/", "..\x00", "a\x00b"}
	for _, n := range invalid {
		if err := validEntryName(n); err == nil {
			t.Errorf("validEntryName(%q) = nil, want error", n)
		}
	}
}
