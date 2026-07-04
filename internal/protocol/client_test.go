package protocol

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestParseFingerprint(t *testing.T) {
	valid := "AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:" +
		"AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99"
	got, err := parseFingerprint(valid)
	if err != nil {
		t.Fatalf("valid fingerprint rejected: %v", err)
	}
	if len(got) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(got))
	}

	// Same value without colons and lowercased must parse identically.
	got2, err := parseFingerprint(strings.ToLower(strings.ReplaceAll(valid, ":", "")))
	if err != nil {
		t.Fatalf("colonless fingerprint rejected: %v", err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(got2) {
		t.Fatal("colon/lowercase variants parsed differently")
	}

	// Empty means "use system trust store": nil, no error.
	if b, err := parseFingerprint("  "); err != nil || b != nil {
		t.Fatalf("empty fingerprint: got (%v, %v), want (nil, nil)", b, err)
	}

	for _, bad := range []string{"zz:zz", "AABBCC", valid + ":00"} {
		if _, err := parseFingerprint(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
