// Package restore reassembles and extracts a pxar archive from a PBS snapshot:
// download the dynamic index, fetch + DataBlob-decode each chunk on demand,
// stream the reconstructed pxar through the decoder, and materialize files.
package restore

import (
	"context"
	"fmt"
	"io"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/index"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/manifest"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/protocol"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// ChunkStore is the subset of the reader session the restore path needs:
// download a named file (index/manifest) and fetch a chunk by digest.
// *protocol.RestoreReader satisfies it; tests provide an in-memory store.
type ChunkStore interface {
	Download(name string) ([]byte, error)
	Chunk(digest [32]byte) ([]byte, error)
}

// chunkStream presents the reconstructed pxar byte stream by fetching chunks by
// digest and DataBlob-decoding them on demand, caching by digest for dedup.
type chunkStream struct {
	store   ChunkStore
	entries []index.DynamicEntry
	key     *crypto.Key
	cache   map[[32]byte][]byte

	i       int
	buf     []byte
	off     int
	prevEnd uint64
}

func newChunkStream(store ChunkStore, entries []index.DynamicEntry, key *crypto.Key) *chunkStream {
	return &chunkStream{store: store, entries: entries, key: key, cache: map[[32]byte][]byte{}}
}

func (s *chunkStream) Read(p []byte) (int, error) {
	for s.off >= len(s.buf) {
		if s.i >= len(s.entries) {
			return 0, io.EOF
		}
		e := s.entries[s.i]
		s.i++
		data, ok := s.cache[e.Digest]
		if !ok {
			blob, err := s.store.Chunk(e.Digest)
			if err != nil {
				return 0, err
			}
			data, err = datablob.Decode(blob, s.key)
			if err != nil {
				return 0, fmt.Errorf("decode chunk %x: %w", e.Digest, err)
			}
			s.cache[e.Digest] = data
		}
		if want := e.End - s.prevEnd; uint64(len(data)) != want {
			return 0, fmt.Errorf("chunk %x size %d, index expects %d", e.Digest, len(data), want)
		}
		s.prevEnd = e.End
		s.buf, s.off = data, 0
	}
	n := copy(p, s.buf[s.off:])
	s.off += n
	return n, nil
}

// Archive opens the reader session and decodes the named archive (e.g.
// "root.pxar") through the given Visitor.
func Archive(ctx context.Context, c *protocol.Client, snap protocol.Snapshot, archiveName string, key *crypto.Key, v pxar.Visitor) error {
	r, err := c.BeginRestore(ctx, snap)
	if err != nil {
		return fmt.Errorf("begin restore: %w", err)
	}
	defer r.Close()
	return Extract(r, archiveName, key, v)
}

// Extract decodes an archive from any ChunkStore. It downloads the dynamic
// index, reassembles the pxar stream from its chunks, and walks it with v.
func Extract(store ChunkStore, archiveName string, key *crypto.Key, v pxar.Visitor) error {
	idxBytes, err := store.Download(archiveName + ".didx")
	if err != nil {
		return fmt.Errorf("download index: %w", err)
	}
	entries, err := index.ParseDynamicIndex(idxBytes)
	if err != nil {
		return err
	}
	stream := newChunkStream(store, entries, key)
	return pxar.NewDecoder(stream).Walk(v)
}

// ManifestFor downloads and decodes a snapshot's manifest (index.json.blob).
func ManifestFor(ctx context.Context, c *protocol.Client, snap protocol.Snapshot) (*manifest.Manifest, error) {
	r, err := c.BeginRestore(ctx, snap)
	if err != nil {
		return nil, fmt.Errorf("begin restore: %w", err)
	}
	defer r.Close()

	blob, err := r.Download(manifest.BlobName)
	if err != nil {
		return nil, fmt.Errorf("download manifest: %w", err)
	}
	// The manifest blob is stored unencrypted.
	data, err := datablob.Decode(blob, nil)
	if err != nil {
		return nil, fmt.Errorf("decode manifest blob: %w", err)
	}
	return manifest.Parse(data)
}
