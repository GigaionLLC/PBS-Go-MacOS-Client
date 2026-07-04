// Package chunker splits a byte stream into content-defined chunks using a
// buzhash rolling hash, mirroring the *approach* PBS uses for dynamically sized
// chunks. Boundaries fall where the rolling hash has enough low zero bits,
// bounded by a min and max chunk size so pathological inputs stay in range.
//
// NOTE: matching PBS's exact chunk boundaries (its specific buzhash table and
// mask) is required only for cross-client dedup alignment, not for a correct
// standalone backup — the server accepts whatever chunks the client uploads via
// the dynamic index. Aligning the constants with upstream is a tracked TODO.
package chunker

import (
	"crypto/sha256"
	"io"
)

const (
	// windowSize is the rolling-hash window in bytes.
	windowSize = 64
	// Default size bounds for dynamically sized chunks.
	defaultMinSize = 1 << 20  // 1 MiB
	defaultAvgBits = 22       // ~4 MiB average (mask = 2^22-1)
	defaultMaxSize = 16 << 20 // 16 MiB
)

// buzTable holds 256 pseudo-random 64-bit values. It is generated
// deterministically at init so builds are reproducible without a huge literal.
var buzTable [256]uint64

func init() {
	var s uint64 = 0x2545F4914F6CDD1D // fixed seed (xorshift64)
	for i := range buzTable {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		buzTable[i] = s
	}
}

func rotl(v uint64, r uint) uint64 { return (v << r) | (v >> (64 - r)) }

// Chunk describes one content-defined chunk: its SHA-256 digest, byte offset in
// the overall stream, and length.
type Chunk struct {
	Digest [32]byte
	Offset uint64
	Length uint32
}

// Chunker produces chunks from a reader.
type Chunker struct {
	minSize int
	maxSize int
	mask    uint64
}

// New returns a Chunker with default PBS-like size bounds.
func New() *Chunker {
	return &Chunker{
		minSize: defaultMinSize,
		maxSize: defaultMaxSize,
		mask:    (1 << defaultAvgBits) - 1,
	}
}

// Split reads r to EOF and invokes fn for each chunk in order, handing fn the
// chunk metadata and the chunk's raw bytes. The byte slice passed to fn is only
// valid for the duration of the call. Split returns the first error from r or fn.
func (c *Chunker) Split(r io.Reader, fn func(Chunk, []byte) error) error {
	br := &byteReader{r: r}
	var (
		offset uint64
		buf    []byte
		hash   uint64
		window [windowSize]byte
		wpos   int
		filled bool
	)

	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		ch := Chunk{
			Digest: sha256.Sum256(buf),
			Offset: offset,
			Length: uint32(len(buf)),
		}
		if err := fn(ch, buf); err != nil {
			return err
		}
		offset += uint64(len(buf))
		buf = buf[:0]
		hash = 0
		wpos = 0
		filled = false
		window = [windowSize]byte{}
		return nil
	}

	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		buf = append(buf, b)

		// Update rolling hash: add incoming, remove outgoing (once window full).
		out := window[wpos]
		window[wpos] = b
		wpos = (wpos + 1) % windowSize
		if wpos == 0 {
			filled = true
		}
		hash = rotl(hash, 1) ^ buzTable[b]
		if filled {
			hash ^= rotl(buzTable[out], windowSize%64)
		}

		if len(buf) >= c.maxSize {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if len(buf) >= c.minSize && (hash&c.mask) == 0 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// byteReader adapts an io.Reader to cheap single-byte reads with buffering.
type byteReader struct {
	r   io.Reader
	buf [64 * 1024]byte
	n   int
	pos int
}

func (b *byteReader) ReadByte() (byte, error) {
	if b.pos >= b.n {
		n, err := b.r.Read(b.buf[:])
		if n == 0 {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		b.n = n
		b.pos = 0
	}
	c := b.buf[b.pos]
	b.pos++
	return c, nil
}
