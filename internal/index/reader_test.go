package index

import (
	"crypto/sha256"
	"testing"
)

func TestDynamicIndexSerializeParseRoundTrip(t *testing.T) {
	w, err := NewDynamicIndexWriter(1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	type c struct {
		digest [32]byte
		size   uint64
	}
	chunks := []c{
		{sha256.Sum256([]byte("one")), 1000},
		{sha256.Sum256([]byte("two")), 2000},
		{sha256.Sum256([]byte("three")), 500},
	}
	for _, ch := range chunks {
		w.AddChunk(ch.digest, ch.size)
	}

	entries, err := ParseDynamicIndex(w.Serialize())
	if err != nil {
		t.Fatalf("ParseDynamicIndex: %v", err)
	}
	if len(entries) != len(chunks) {
		t.Fatalf("parsed %d entries, want %d", len(entries), len(chunks))
	}
	var end uint64
	for i, ch := range chunks {
		end += ch.size
		if entries[i].End != end {
			t.Errorf("entry %d end = %d, want %d", i, entries[i].End, end)
		}
		if entries[i].Digest != ch.digest {
			t.Errorf("entry %d digest mismatch", i)
		}
	}
}

func TestParseDynamicIndexRejectsBadMagic(t *testing.T) {
	buf := make([]byte, HeaderSize+EntrySize)
	if _, err := ParseDynamicIndex(buf); err == nil {
		t.Fatal("expected error on bad magic")
	}
}
