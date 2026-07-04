package manifest

import (
	"strings"
	"testing"
)

// A csum longer than 64 hex chars must be a clean error, not a panic
// (hex.Decode into the fixed [32]byte has no bound check).
func TestParseRejectsOverlongCsum(t *testing.T) {
	long := strings.Repeat("a", 65)
	data := []byte(`{"backup-type":"host","backup-id":"x","backup-time":1,"files":[` +
		`{"filename":"root.pxar.didx","crypt-mode":"none","size":1,"csum":"` + long + `"}]}`)
	if _, err := Parse(data); err == nil {
		t.Fatal("expected an error for an over-long csum, got nil")
	}
}

// A well-formed 64-char csum still parses.
func TestParseAcceptsValidCsum(t *testing.T) {
	csum := strings.Repeat("ab", 32) // 64 hex chars
	data := []byte(`{"backup-type":"host","backup-id":"x","backup-time":1,"files":[` +
		`{"filename":"root.pxar.didx","crypt-mode":"none","size":1,"csum":"` + csum + `"}]}`)
	m, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(m.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(m.Files))
	}
}
