package chunker

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestSplitReassembles(t *testing.T) {
	// Deterministic pseudo-random data larger than a few max chunks.
	src := make([]byte, 40<<20)
	rng := rand.New(rand.NewSource(1))
	rng.Read(src)

	var out []byte
	var total uint64
	c := New()
	err := c.Split(bytes.NewReader(src), func(ch Chunk, data []byte) error {
		if ch.Offset != total {
			t.Fatalf("offset gap: got %d want %d", ch.Offset, total)
		}
		if int(ch.Length) != len(data) {
			t.Fatalf("length mismatch: meta %d data %d", ch.Length, len(data))
		}
		total += uint64(len(data))
		out = append(out, data...)
		return nil
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if !bytes.Equal(out, src) {
		t.Fatal("reassembled data does not match source")
	}
}

func TestSplitBounds(t *testing.T) {
	src := make([]byte, 50<<20)
	rng := rand.New(rand.NewSource(2))
	rng.Read(src)

	c := New()
	var chunks []Chunk
	_ = c.Split(bytes.NewReader(src), func(ch Chunk, _ []byte) error {
		chunks = append(chunks, ch)
		return nil
	})
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Every chunk except the last must respect the max bound.
	for i, ch := range chunks[:len(chunks)-1] {
		if int(ch.Length) > defaultMaxSize {
			t.Fatalf("chunk %d exceeds max size: %d", i, ch.Length)
		}
	}
}
