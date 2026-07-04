package index

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
)

// ParseDynamicIndex parses a .didx file (header + 40-byte entries) into the
// ordered list of chunk references. Each entry's byte range in the archive is
// [previous End, End); the digest identifies the chunk to fetch.
func ParseDynamicIndex(data []byte) ([]DynamicEntry, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("dynamic index too small: %d bytes", len(data))
	}
	if !bytes.Equal(data[0:8], datablob.DynamicIndexMagic[:]) {
		return nil, fmt.Errorf("not a dynamic index (bad magic)")
	}
	body := data[HeaderSize:]
	if len(body)%EntrySize != 0 {
		return nil, fmt.Errorf("dynamic index body not a multiple of %d: %d bytes", EntrySize, len(body))
	}
	n := len(body) / EntrySize
	entries := make([]DynamicEntry, n)
	for i := 0; i < n; i++ {
		rec := body[i*EntrySize:]
		entries[i].End = binary.LittleEndian.Uint64(rec[0:8])
		copy(entries[i].Digest[:], rec[8:40])
	}
	return entries, nil
}
