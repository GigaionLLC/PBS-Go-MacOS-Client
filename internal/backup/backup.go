// Package backup orchestrates a backup: a pxar archive of the source tree is
// streamed through the content-defined chunker; each chunk is DataBlob-encoded
// (and optionally encrypted), deduplicated, handed to a ChunkSink, and recorded
// in the dynamic index. This mirrors how PBS actually chunks the pxar stream
// (not individual files). The ChunkSink abstraction lets the same pipeline run
// offline (dry-run stats) or upload to a live server (M1+).
package backup

import (
	"fmt"
	"io"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/chunker"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/datablob"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/exclude"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/index"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/pxar"
)

// ChunkSink receives each unique DataBlob-encoded chunk for storage. rawSize is
// the plaintext chunk size; blob is the encoded (and possibly encrypted) bytes.
type ChunkSink interface {
	Put(digest [32]byte, rawSize uint32, blob []byte) error
}

// NullSink discards chunks; used for dry-run.
type NullSink struct{}

// Put implements ChunkSink.
func (NullSink) Put([32]byte, uint32, []byte) error { return nil }

// Result summarizes a backup pass.
type Result struct {
	ArchiveBytes uint64   `json:"archive_bytes"` // size of the pxar stream
	TotalChunks  int      `json:"total_chunks"`
	UniqueChunks int      `json:"unique_chunks"`
	UniqueBytes  uint64   `json:"unique_bytes"`
	IndexCsum    [32]byte `json:"-"`
	Encrypted    bool     `json:"encrypted"`
	Compressed   bool     `json:"compressed"`
}

// DedupRatio is the fraction of chunk bytes eliminated by dedup (0..1).
func (r Result) DedupRatio() float64 {
	if r.ArchiveBytes == 0 {
		return 0
	}
	return 1 - float64(r.UniqueBytes)/float64(r.ArchiveBytes)
}

// Options controls the pipeline.
type Options struct {
	Crypt    *crypto.CryptConfig // non-nil enables AES-256-GCM + keyed digest
	Compress bool                // zstd-compress chunks
	Ctime    int64               // index creation time (backup-time)
	Exclude  *exclude.Matcher    // optional .pxarexclude/--exclude patterns
}

// Run streams the pxar archive of fs rooted at root through the full pipeline,
// storing unique chunks via sink and building the dynamic index. It returns the
// pass summary and the completed index writer (for its csum/chunk-count/size,
// needed by dynamic_close).
func Run(fs pxar.Filesystem, root string, sink ChunkSink, opts Options) (Result, *index.DynamicIndexWriter, error) {
	idx, err := index.NewDynamicIndexWriter(opts.Ctime)
	if err != nil {
		return Result{}, nil, err
	}

	pr, pw := io.Pipe()
	enc := pxar.NewEncoder(pw)
	if opts.Exclude != nil && !opts.Exclude.Empty() {
		enc.SetExcluder(opts.Exclude.Excluded)
	}
	// Encode the pxar archive on a goroutine; the chunker consumes the pipe.
	go func() {
		pw.CloseWithError(enc.Encode(fs, root))
	}()

	res := Result{Encrypted: opts.Crypt != nil, Compressed: opts.Compress}
	var encKey *crypto.Key
	if opts.Crypt != nil {
		k := opts.Crypt.EncKey()
		encKey = &k
	}
	seen := make(map[[32]byte]struct{})
	ch := chunker.New()

	err = ch.Split(pr, func(c chunker.Chunk, data []byte) error {
		res.ArchiveBytes += uint64(c.Length)
		res.TotalChunks++
		// PBS keys the chunk digest for encrypted backups (SHA256(data||id_key));
		// c.Digest is the plain SHA-256 used for the unencrypted path.
		digest := c.Digest
		if opts.Crypt != nil {
			digest = opts.Crypt.ComputeDigest(data)
		}
		// Every chunk (including duplicates) is recorded in the index in order.
		idx.AddChunk(digest, uint64(c.Length))

		if _, dup := seen[digest]; dup {
			return nil
		}
		seen[digest] = struct{}{}
		res.UniqueChunks++
		res.UniqueBytes += uint64(c.Length)

		blob, err := datablob.Encode(data, encKey, opts.Compress)
		if err != nil {
			return fmt.Errorf("encode chunk: %w", err)
		}
		return sink.Put(digest, c.Length, blob)
	})
	if err != nil {
		pr.CloseWithError(err)
		return Result{}, nil, err
	}
	res.IndexCsum = idx.Csum()
	return res, idx, nil
}

// FormatResult renders a Result as a short human-readable report.
func FormatResult(r Result) string {
	enc, comp := "off", "off"
	if r.Encrypted {
		enc = "on"
	}
	if r.Compressed {
		comp = "on"
	}
	return fmt.Sprintf(
		"pxar archive: %s\nchunks: %d (unique %d, %s)\ndedup: %.1f%%  encryption: %s  compression: %s\nindex csum: %x",
		humanBytes(r.ArchiveBytes), r.TotalChunks, r.UniqueChunks, humanBytes(r.UniqueBytes),
		r.DedupRatio()*100, enc, comp, r.IndexCsum,
	)
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
