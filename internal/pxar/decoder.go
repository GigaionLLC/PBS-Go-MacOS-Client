package pxar

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// Visitor receives entries as the decoder walks a pxar archive. For files, the
// content reader is valid only for the duration of the OnFile call and must be
// consumed (the decoder drains any remainder). Paths are '/'-rooted; the root
// directory is passed as "".
type Visitor interface {
	OnDir(path string, m Meta) error
	OnFile(path string, m Meta, content io.Reader) error
	OnSymlink(path string, m Meta, target string) error
}

// Decoder parses a pxar v2 archive stream.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder returns a decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReaderSize(r, 128*1024)}
}

// itemHeader is a decoded 16-byte item header.
type itemHeader struct {
	htype   uint64
	content uint64 // content length (full_size - 16)
}

func (d *Decoder) readHeader() (itemHeader, error) {
	var h [HeaderSize]byte
	if _, err := io.ReadFull(d.r, h[:]); err != nil {
		return itemHeader{}, err
	}
	htype := binary.LittleEndian.Uint64(h[0:8])
	full := binary.LittleEndian.Uint64(h[8:16])
	if full < HeaderSize {
		return itemHeader{}, fmt.Errorf("pxar: item size %d < header", full)
	}
	return itemHeader{htype: htype, content: full - HeaderSize}, nil
}

func (d *Decoder) readContent(n uint64) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *Decoder) skip(n uint64) error {
	_, err := io.CopyN(io.Discard, d.r, int64(n))
	return err
}

// isMetadata reports item types that decorate an ENTRY and can be skipped by a
// consumer that doesn't restore them (xattrs, ACLs, fcaps, quota).
func isMetadata(htype uint64) bool {
	switch htype {
	case Xattr, Fcaps, 0x2ce8540a457d55b8, 0x136e3eceb04c03ab, 0x10868031e9582876,
		0xbbbb13415a6896f5, 0xc89357b40532cd1f, 0xf90a8a5816038ffe, 0xe07540e82f7d1cbb:
		return true
	}
	return false
}

// Walk decodes the whole archive, invoking v for each entry.
func (d *Decoder) Walk(v Visitor) error {
	h, err := d.readHeader()
	if err != nil {
		return err
	}
	if h.htype == FormatVersion || h.htype == Prelude {
		if err := d.skip(h.content); err != nil {
			return err
		}
		// A prelude may follow the version; skip any leading non-ENTRY items.
		for {
			h, err = d.readHeader()
			if err != nil {
				return err
			}
			if h.htype == Prelude {
				if err := d.skip(h.content); err != nil {
					return err
				}
				continue
			}
			break
		}
	}
	if h.htype != Entry {
		return fmt.Errorf("pxar: expected root ENTRY, got %#x", h.htype)
	}
	meta, err := d.parseStat(h.content)
	if err != nil {
		return err
	}
	xs, next, err := d.collectMeta()
	if err != nil {
		return err
	}
	meta.Xattrs = xs
	if err := v.OnDir("", meta); err != nil {
		return err
	}
	return d.walkDir("", next, v)
}

// walkDir decodes a directory's children starting from h — the first item after
// the directory's ENTRY and xattrs — until the GOODBYE.
func (d *Decoder) walkDir(dirPath string, h itemHeader, v Visitor) error {
	for {
		switch {
		case isMetadata(h.htype):
			// Defensive: xattrs are collected per-entry; nothing else expected here.
			if err := d.skip(h.content); err != nil {
				return err
			}
		case h.htype == Goodbye:
			return d.skip(h.content)
		case h.htype == Filename:
			name, err := d.readName(h.content)
			if err != nil {
				return err
			}
			if err := d.decodeChild(dirPath+"/"+name, v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pxar: unexpected item %#x in directory %q", h.htype, dirPath)
		}
		var err error
		if h, err = d.readHeader(); err != nil {
			return err
		}
	}
}

// decodeChild decodes one child (its ENTRY was preceded by the FILENAME).
func (d *Decoder) decodeChild(path string, v Visitor) error {
	h, err := d.readHeader()
	if err != nil {
		return err
	}
	if h.htype != Entry {
		return fmt.Errorf("pxar: expected ENTRY for %q, got %#x", path, h.htype)
	}
	meta, err := d.parseStat(h.content)
	if err != nil {
		return err
	}
	// Collect this entry's xattrs (skipping other metadata); next is the first
	// content item (SYMLINK / PAYLOAD, or the first child FILENAME / GOODBYE).
	xs, next, err := d.collectMeta()
	if err != nil {
		return err
	}
	meta.Xattrs = xs

	switch {
	case meta.isDir():
		if err := v.OnDir(path, meta); err != nil {
			return err
		}
		return d.walkDir(path, next, v)
	case meta.isLink():
		if next.htype != Symlink {
			return fmt.Errorf("pxar: expected SYMLINK for %q, got %#x", path, next.htype)
		}
		target, err := d.readName(next.content)
		if err != nil {
			return err
		}
		return v.OnSymlink(path, meta, target)
	case meta.isReg():
		if next.htype != Payload {
			return fmt.Errorf("pxar: expected PAYLOAD for %q, got %#x", path, next.htype)
		}
		lr := &io.LimitedReader{R: d.r, N: int64(next.content)}
		if err := v.OnFile(path, meta, lr); err != nil {
			return err
		}
		return d.skip(uint64(lr.N)) // drain any unconsumed payload
	default:
		return fmt.Errorf("pxar: unsupported mode %#o for %q", meta.Mode, path)
	}
}

// collectMeta reads the items that follow an ENTRY: XATTR items are collected
// into a name->value map, other metadata (ACLs, fcaps, quota) is skipped, and
// the first non-metadata header (the entry's content item) is returned.
func (d *Decoder) collectMeta() (map[string][]byte, itemHeader, error) {
	var xs map[string][]byte
	for {
		h, err := d.readHeader()
		if err != nil {
			return nil, itemHeader{}, err
		}
		if h.htype == Xattr {
			b, err := d.readContent(h.content)
			if err != nil {
				return nil, itemHeader{}, err
			}
			name, val := parseXattr(b)
			if xs == nil {
				xs = make(map[string][]byte)
			}
			xs[name] = val
			continue
		}
		if isMetadata(h.htype) {
			if err := d.skip(h.content); err != nil {
				return nil, itemHeader{}, err
			}
			continue
		}
		return xs, h, nil
	}
}

// parseXattr splits a PXAR_XATTR body (name + NUL + raw value) into name+value.
func parseXattr(b []byte) (string, []byte) {
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return string(b), nil
	}
	return string(b[:i]), append([]byte(nil), b[i+1:]...)
}

// readName reads a null-terminated name/target of the given content length.
func (d *Decoder) readName(n uint64) (string, error) {
	b, err := d.readContent(n)
	if err != nil {
		return "", err
	}
	// Strip the trailing null terminator.
	if len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return string(b), nil
}

// parseStat decodes a v2 ENTRY (Stat) payload.
func (d *Decoder) parseStat(n uint64) (Meta, error) {
	if n < 40 {
		return Meta{}, fmt.Errorf("pxar: ENTRY too small (%d bytes)", n)
	}
	b, err := d.readContent(n)
	if err != nil {
		return Meta{}, err
	}
	return Meta{
		Mode:       binary.LittleEndian.Uint64(b[0:8]),
		Flags:      binary.LittleEndian.Uint64(b[8:16]),
		UID:        binary.LittleEndian.Uint32(b[16:20]),
		GID:        binary.LittleEndian.Uint32(b[20:24]),
		MtimeSecs:  int64(binary.LittleEndian.Uint64(b[24:32])),
		MtimeNanos: binary.LittleEndian.Uint32(b[32:36]),
	}, nil
}
