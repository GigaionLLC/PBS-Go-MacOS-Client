// Package index implements the PBS chunk-index formats. The dynamic index
// (.didx) is ported from pbs-datastore/src/dynamic_index.rs.
//
// Protocol note: during a backup the client does NOT upload the .didx file
// whole. It uploads chunks and appends (digest, end-offset) pairs via
// PUT dynamic_index; the server assembles the file. At dynamic_close the client
// must supply the index checksum it computed independently, which the server
// verifies against its assembled copy. DynamicIndexWriter computes exactly that
// checksum (and can also serialize the full file for local storage/tests).
//
// File layout:
//
//	DynamicIndexHeader (4096 bytes):
//	  magic[8] ++ uuid[16] ++ ctime(i64 LE)[8] ++ index_csum[32] ++ reserved[4032]
//	Entries (40 bytes each, appended after the header):
//	  end_offset(u64 LE)[8] ++ digest[32]
//
// The checksum is a running SHA-256 fed, per chunk in order, the 8-byte
// little-endian END offset followed by the 32-byte digest.
package index

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
)

// HeaderSize is the fixed .didx / .fidx header size (one page).
const HeaderSize = 4096

// EntrySize is the size of one dynamic index entry (end offset + digest).
const EntrySize = 40

// DynamicEntry is one appended chunk reference.
type DynamicEntry struct {
	End    uint64   // cumulative end offset of this chunk in the archive
	Digest [32]byte // plaintext chunk digest
}

// DynamicIndexWriter accumulates chunk references and computes the index
// checksum the server expects at dynamic_close.
type DynamicIndexWriter struct {
	uuid    [16]byte
	ctime   int64
	csum    hash.Hash
	entries []DynamicEntry
	end     uint64
}

// NewDynamicIndexWriter starts an index with a random UUID and the given
// creation time (unix seconds).
func NewDynamicIndexWriter(ctime int64) (*DynamicIndexWriter, error) {
	w := &DynamicIndexWriter{ctime: ctime, csum: sha256.New()}
	if _, err := io.ReadFull(rand.Reader, w.uuid[:]); err != nil {
		return nil, fmt.Errorf("generate index uuid: %w", err)
	}
	return w, nil
}

// AddChunk records a chunk of chunkSize bytes with the given plaintext digest,
// updating the running checksum. It returns the chunk's cumulative end offset
// (as needed for the PUT dynamic_index offset-list).
func (w *DynamicIndexWriter) AddChunk(digest [32]byte, chunkSize uint64) uint64 {
	w.end += chunkSize
	var off [8]byte
	binary.LittleEndian.PutUint64(off[:], w.end)
	w.csum.Write(off[:])
	w.csum.Write(digest[:])
	w.entries = append(w.entries, DynamicEntry{End: w.end, Digest: digest})
	return w.end
}

// Csum returns the current index checksum (pass this to dynamic_close).
func (w *DynamicIndexWriter) Csum() [32]byte {
	var out [32]byte
	copy(out[:], w.csum.Sum(nil))
	return out
}

// ChunkCount returns the number of chunks appended.
func (w *DynamicIndexWriter) ChunkCount() uint64 { return uint64(len(w.entries)) }

// Size returns the total archive size in bytes (the final end offset).
func (w *DynamicIndexWriter) Size() uint64 { return w.end }

// Entries returns the appended entries in order.
func (w *DynamicIndexWriter) Entries() []DynamicEntry { return w.entries }

// Serialize renders the complete .didx file (header + entries), with the final
// checksum written into the header. Useful for local storage and for tests that
// re-parse the file.
func (w *DynamicIndexWriter) Serialize() []byte {
	buf := make([]byte, HeaderSize+len(w.entries)*EntrySize)
	copy(buf[0:8], datablob.DynamicIndexMagic[:])
	copy(buf[8:24], w.uuid[:])
	binary.LittleEndian.PutUint64(buf[24:32], uint64(w.ctime))
	csum := w.Csum()
	copy(buf[32:64], csum[:])
	// buf[64:4096] stays zero (reserved).
	off := HeaderSize
	for _, e := range w.entries {
		binary.LittleEndian.PutUint64(buf[off:off+8], e.End)
		copy(buf[off+8:off+40], e.Digest[:])
		off += EntrySize
	}
	return buf
}
