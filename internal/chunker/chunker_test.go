package chunker

import (
	"bytes"
	"math/rand"
	"testing"
)

const (
	testMin = DefaultAvgSize >> 2 // 1 MiB
	testMax = DefaultAvgSize << 2 // 16 MiB
)

// chunkAll splits src and returns the chunks (metadata) and the reassembled bytes.
func chunkAll(t *testing.T, src []byte) ([]Chunk, []byte) {
	t.Helper()
	var chunks []Chunk
	var out []byte
	var total uint64
	err := New().Split(bytes.NewReader(src), func(ch Chunk, data []byte) error {
		if ch.Offset != total {
			t.Fatalf("offset gap: got %d want %d", ch.Offset, total)
		}
		if int(ch.Length) != len(data) {
			t.Fatalf("length mismatch: meta %d data %d", ch.Length, len(data))
		}
		total += uint64(len(data))
		chunks = append(chunks, ch)
		out = append(out, data...)
		return nil
	})
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	return chunks, out
}

func TestSplitReassembles(t *testing.T) {
	src := make([]byte, 40<<20)
	rand.New(rand.NewSource(1)).Read(src)
	_, out := chunkAll(t, src)
	if !bytes.Equal(out, src) {
		t.Fatal("reassembled data does not match source")
	}
}

func TestSplitBounds(t *testing.T) {
	src := make([]byte, 50<<20)
	rand.New(rand.NewSource(2)).Read(src)
	chunks, _ := chunkAll(t, src)
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	// Every chunk except the last (EOF-flushed) must be within [min, max]: a
	// boundary only fires once chunk_size >= min, and never past max.
	for i, ch := range chunks[:len(chunks)-1] {
		if int(ch.Length) < testMin || int(ch.Length) > testMax {
			t.Fatalf("chunk %d length %d out of [%d,%d]", i, ch.Length, testMin, testMax)
		}
	}
}

// TestDeterministic: the same input must always chunk identically (required for
// any dedup to work at all).
func TestDeterministic(t *testing.T) {
	src := make([]byte, 30<<20)
	rand.New(rand.NewSource(3)).Read(src)
	a, _ := chunkAll(t, src)
	b, _ := chunkAll(t, src)
	if len(a) != len(b) {
		t.Fatalf("chunk count differs across runs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Digest != b[i].Digest || a[i].Length != b[i].Length {
			t.Fatalf("chunk %d differs across runs", i)
		}
	}
}

// TestResyncAfterInsert is the dedup-quality check: inserting a few bytes near
// the start must only disturb the chunk(s) around the edit; the content-defined
// boundaries past it re-sync so the vast majority of chunks (by digest) are
// unchanged. A poor chunker (misaligned mask/table) fails this.
func TestResyncAfterInsert(t *testing.T) {
	src := make([]byte, 40<<20)
	rand.New(rand.NewSource(4)).Read(src)

	modified := make([]byte, 0, len(src)+8)
	modified = append(modified, src[:1000]...)
	modified = append(modified, []byte("INSERTED")...) // 8-byte insertion
	modified = append(modified, src[1000:]...)

	orig, _ := chunkAll(t, src)
	mod, _ := chunkAll(t, modified)

	set := make(map[[32]byte]bool, len(orig))
	for _, ch := range orig {
		set[ch.Digest] = true
	}
	shared := 0
	for _, ch := range mod {
		if set[ch.Digest] {
			shared++
		}
	}
	ratio := float64(shared) / float64(len(orig))
	t.Logf("chunks: orig=%d mod=%d shared=%d (%.0f%% re-synced)", len(orig), len(mod), shared, ratio*100)
	if ratio < 0.7 {
		t.Fatalf("dedup re-sync too weak after an 8-byte insert: only %.0f%% of chunks shared", ratio*100)
	}
}
