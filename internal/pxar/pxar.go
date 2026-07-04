package pxar

import (
	"encoding/binary"
	"fmt"
	"io"
	"path"
	"sort"
)

// Meta is the OS-agnostic metadata the encoder needs for one entry. The caller
// (the source layer) fills it from the platform's stat; keeping it neutral lets
// pxar stay portable and testable off macOS.
type Meta struct {
	Mode       uint64 // full unix mode incl. type bits (S_IFDIR/S_IFREG/S_IFLNK)
	UID, GID   uint32
	MtimeSecs  int64
	MtimeNanos uint32
	Size       uint64 // regular-file content size
	Flags      uint64 // pxar entry file-attribute flags (see format.go Flag*); 0 if none
	// Xattrs holds extended attributes (name -> raw value); nil if none or the
	// platform doesn't support them. On macOS these carry com.apple.* attributes
	// (quarantine, Finder info, tags, resource forks) verbatim as PXAR_XATTR items.
	Xattrs map[string][]byte
}

func (m Meta) isDir() bool  { return m.Mode&sIFMT == sIFDIR }
func (m Meta) isReg() bool  { return m.Mode&sIFMT == sIFREG }
func (m Meta) isLink() bool { return m.Mode&sIFMT == sIFLNK }

// Filesystem is the tree the encoder walks. Paths use '/' separators and are
// interpreted by the implementation (rooted at the backup source).
type Filesystem interface {
	Stat(path string) (Meta, error)
	ReadDir(path string) ([]string, error) // child base names
	Open(path string) (io.ReadCloser, error)
	Readlink(path string) (string, error)
}

// Encoder writes a pxar v2 archive to an underlying writer (typically the pipe
// feeding the chunker).
type Encoder struct {
	w       *countingWriter
	exclude func(path string, isDir bool) bool
}

// NewEncoder returns an encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: &countingWriter{w: w}}
}

// SetExcluder installs a predicate; children for which it returns true are
// omitted from the archive (used for .pxarexclude / --exclude).
func (e *Encoder) SetExcluder(fn func(path string, isDir bool) bool) { e.exclude = fn }

// Encode writes the complete archive for the directory at root.
func (e *Encoder) Encode(fs Filesystem, root string) error {
	m, err := fs.Stat(root)
	if err != nil {
		return fmt.Errorf("stat root %s: %w", root, err)
	}
	if !m.isDir() {
		return fmt.Errorf("backup root %s is not a directory", root)
	}
	// v2 archives begin with a FORMAT_VERSION item.
	if err := e.writeItem(FormatVersion, u64le(FormatVersionV2)); err != nil {
		return err
	}
	return e.encodeDir(fs, root, m)
}

type goodbyeItem struct {
	hash   uint64
	offset uint64 // absolute child start while accumulating; relative at write
	size   uint64
}

func (e *Encoder) encodeDir(fs Filesystem, dirPath string, meta Meta) error {
	entryOffset := e.w.n
	if err := e.writeItem(Entry, encodeStat(meta)); err != nil {
		return err
	}
	if err := e.writeXattrs(meta.Xattrs); err != nil {
		return err
	}

	names, err := fs.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("readdir %s: %w", dirPath, err)
	}
	sort.Strings(names) // archive body is name-sorted

	var items []goodbyeItem
	for _, name := range names {
		child := path.Join(dirPath, name)
		cm, err := fs.Stat(child)
		if err != nil {
			return fmt.Errorf("stat %s: %w", child, err)
		}
		if !cm.isDir() && !cm.isReg() && !cm.isLink() {
			continue // skip sockets/fifos/devices in v1
		}
		if e.exclude != nil && e.exclude(child, cm.isDir()) {
			continue
		}
		childStart := e.w.n
		if err := e.writeItem(Filename, filenameContent(name)); err != nil {
			return err
		}
		switch {
		case cm.isDir():
			if err := e.encodeDir(fs, child, cm); err != nil {
				return err
			}
		case cm.isLink():
			if err := e.writeItem(Entry, encodeStat(cm)); err != nil {
				return err
			}
			if err := e.writeXattrs(cm.Xattrs); err != nil {
				return err
			}
			target, err := fs.Readlink(child)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", child, err)
			}
			if err := e.writeItem(Symlink, filenameContent(target)); err != nil {
				return err
			}
		case cm.isReg():
			if err := e.writeItem(Entry, encodeStat(cm)); err != nil {
				return err
			}
			if err := e.writeXattrs(cm.Xattrs); err != nil {
				return err
			}
			if err := e.writePayload(fs, child, cm.Size); err != nil {
				return err
			}
		}
		items = append(items, goodbyeItem{
			hash:   hashFilename([]byte(name)),
			offset: childStart,
			size:   e.w.n - childStart,
		})
	}

	return e.writeGoodbye(entryOffset, items)
}

// writeGoodbye emits the directory's GOODBYE table: one item per child (offset
// measured back from the GOODBYE start to the child's FILENAME), sorted by hash
// for binary search, terminated by a tail marker pointing back to the ENTRY.
func (e *Encoder) writeGoodbye(entryOffset uint64, items []goodbyeItem) error {
	goodbyeStart := e.w.n
	for i := range items {
		items[i].offset = goodbyeStart - items[i].offset
	}
	sort.Slice(items, func(i, j int) bool { return items[i].hash < items[j].hash })

	tailSize := uint64(HeaderSize + (len(items)+1)*24)
	content := make([]byte, 0, (len(items)+1)*24)
	var scratch [24]byte
	for _, it := range items {
		binary.LittleEndian.PutUint64(scratch[0:8], it.hash)
		binary.LittleEndian.PutUint64(scratch[8:16], it.offset)
		binary.LittleEndian.PutUint64(scratch[16:24], it.size)
		content = append(content, scratch[:]...)
	}
	// Tail marker: offset back to this directory's ENTRY, size of the GOODBYE.
	binary.LittleEndian.PutUint64(scratch[0:8], GoodbyeTailMark)
	binary.LittleEndian.PutUint64(scratch[8:16], goodbyeStart-entryOffset)
	binary.LittleEndian.PutUint64(scratch[16:24], tailSize)
	content = append(content, scratch[:]...)

	return e.writeItem(Goodbye, content)
}

// writeXattrs emits one PXAR_XATTR item per attribute, immediately after the
// entry's ENTRY item and before its FILENAME/PAYLOAD/SYMLINK. Names are sorted
// for a stable archive; each item's content is name + NUL + raw value.
func (e *Encoder) writeXattrs(xs map[string][]byte) error {
	if len(xs) == 0 {
		return nil
	}
	names := make([]string, 0, len(xs))
	for name := range xs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := e.writeItem(Xattr, encodeXattr(name, xs[name])); err != nil {
			return err
		}
	}
	return nil
}

// encodeXattr builds a PXAR_XATTR item body: name + one NUL + raw value.
func encodeXattr(name string, value []byte) []byte {
	b := make([]byte, 0, len(name)+1+len(value))
	b = append(b, name...)
	b = append(b, 0)
	b = append(b, value...)
	return b
}

// writeItem writes a header (htype + full_size) followed by content.
func (e *Encoder) writeItem(htype uint64, content []byte) error {
	var h [HeaderSize]byte
	binary.LittleEndian.PutUint64(h[0:8], htype)
	binary.LittleEndian.PutUint64(h[8:16], uint64(HeaderSize+len(content)))
	if _, err := e.w.Write(h[:]); err != nil {
		return err
	}
	if len(content) > 0 {
		if _, err := e.w.Write(content); err != nil {
			return err
		}
	}
	return nil
}

// writePayload streams a regular file's content as a PXAR_PAYLOAD item without
// buffering it, trusting size from stat.
func (e *Encoder) writePayload(fs Filesystem, path string, size uint64) error {
	var h [HeaderSize]byte
	binary.LittleEndian.PutUint64(h[0:8], Payload)
	binary.LittleEndian.PutUint64(h[8:16], HeaderSize+size)
	if _, err := e.w.Write(h[:]); err != nil {
		return err
	}
	r, err := fs.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer r.Close()
	n, err := io.CopyN(e.w, r, int64(size))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if uint64(n) != size {
		return fmt.Errorf("short read on %s: %d of %d bytes", path, n, size)
	}
	return nil
}

// encodeStat serializes a v2 Entry (Stat) payload: mode, flags, uid, gid, and a
// StatxTimestamp (secs i64, nanos u32, zero u32).
func encodeStat(m Meta) []byte {
	buf := make([]byte, 40)
	binary.LittleEndian.PutUint64(buf[0:8], m.Mode)
	binary.LittleEndian.PutUint64(buf[8:16], m.Flags)
	binary.LittleEndian.PutUint32(buf[16:20], m.UID)
	binary.LittleEndian.PutUint32(buf[20:24], m.GID)
	binary.LittleEndian.PutUint64(buf[24:32], uint64(m.MtimeSecs))
	binary.LittleEndian.PutUint32(buf[32:36], m.MtimeNanos)
	binary.LittleEndian.PutUint32(buf[36:40], 0) // _zero padding
	return buf
}

// filenameContent returns a null-terminated name/target payload.
func filenameContent(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

func u64le(v uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, v)
	return b
}

// countingWriter tracks how many bytes have been written, for goodbye offsets.
type countingWriter struct {
	w io.Writer
	n uint64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	k, err := c.w.Write(p)
	c.n += uint64(k)
	return k, err
}
