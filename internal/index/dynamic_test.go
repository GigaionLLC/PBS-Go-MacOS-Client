package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
)

func TestDynamicIndexCsumAndLayout(t *testing.T) {
	w, err := NewDynamicIndexWriter(1_700_000_000)
	if err != nil {
		t.Fatal(err)
	}
	chunks := []struct {
		digest [32]byte
		size   uint64
	}{
		{sha256.Sum256([]byte("a")), 100},
		{sha256.Sum256([]byte("b")), 250},
		{sha256.Sum256([]byte("c")), 50},
	}

	// Independently compute the expected checksum the way PBS does.
	want := sha256.New()
	var end uint64
	for i, c := range chunks {
		gotEnd := w.AddChunk(c.digest, c.size)
		end += c.size
		if gotEnd != end {
			t.Fatalf("chunk %d end = %d, want %d", i, gotEnd, end)
		}
		var off [8]byte
		binary.LittleEndian.PutUint64(off[:], end)
		want.Write(off[:])
		want.Write(c.digest[:])
	}

	var wantCsum [32]byte
	copy(wantCsum[:], want.Sum(nil))
	if w.Csum() != wantCsum {
		t.Fatal("index checksum does not match independent computation")
	}
	if w.ChunkCount() != 3 {
		t.Fatalf("chunk count = %d, want 3", w.ChunkCount())
	}
	if w.Size() != 400 {
		t.Fatalf("size = %d, want 400", w.Size())
	}

	// Verify the serialized file layout.
	file := w.Serialize()
	if len(file) != HeaderSize+3*EntrySize {
		t.Fatalf("file size = %d, want %d", len(file), HeaderSize+3*EntrySize)
	}
	if !bytes.Equal(file[0:8], datablob.DynamicIndexMagic[:]) {
		t.Fatal("wrong magic in serialized index")
	}
	if got := binary.LittleEndian.Uint64(file[24:32]); got != 1_700_000_000 {
		t.Fatalf("ctime = %d", got)
	}
	if !bytes.Equal(file[32:64], wantCsum[:]) {
		t.Fatal("serialized header csum mismatch")
	}
	// First entry: end offset 100, digest of "a".
	if got := binary.LittleEndian.Uint64(file[HeaderSize : HeaderSize+8]); got != 100 {
		t.Fatalf("first entry end = %d, want 100", got)
	}
	da := sha256.Sum256([]byte("a"))
	if !bytes.Equal(file[HeaderSize+8:HeaderSize+40], da[:]) {
		t.Fatal("first entry digest mismatch")
	}
}
