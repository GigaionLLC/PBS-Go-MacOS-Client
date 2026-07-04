// Package pxar encodes (backup) and decodes (restore) the PBS "pxar" archive
// format, ported from the proxmox `pxar` crate (src/format/mod.rs and
// src/encoder). This file holds the format constants and the filename hash.
//
// Wire structure: the archive is a flat sequence of items, each a 16-byte
// Header { htype: u64 LE, full_size: u64 LE } followed by full_size-16 content
// bytes. A directory is: ENTRY, then for each child a FILENAME item followed by
// that child's items (recursively), then a GOODBYE table. This encoder targets
// pxar format v2 (a leading FORMAT_VERSION item + StatxTimestamp mtimes).
package pxar

import "encoding/binary"

// Item header-type magics (pxar src/format/mod.rs).
const (
	FormatVersion   uint64 = 0x730f6c75df16a40d
	Prelude         uint64 = 0xe309d79d9f7b771b
	Entry           uint64 = 0xd5956474e588acef
	EntryV1         uint64 = 0x11da850a1c1cceff
	Filename        uint64 = 0x16701121063917b3
	Symlink         uint64 = 0x27f971e7dbf5dc5f
	Device          uint64 = 0x9fc9e906586d5ce9
	Xattr           uint64 = 0x0dab0229b57dcd03
	Fcaps           uint64 = 0x2da9dd9db5f7fb67
	Hardlink        uint64 = 0x51269c8422bd7275
	Payload         uint64 = 0x28147a1b0b7c1a25
	Goodbye         uint64 = 0x2fec4fa642d5731d
	GoodbyeTailMark uint64 = 0xef5eed5b753e1555
)

// FormatVersionV2 is the version value serialized in the FORMAT_VERSION item.
const FormatVersionV2 uint64 = 2

// SipHash-2-4 keys used for goodbye-table filename hashes (pxar src/format).
const (
	hashKey1 uint64 = 0x83ac3f1cfbb450db
	hashKey2 uint64 = 0xaa4f1b6879369fbd
)

// Unix mode bits used to classify entries (portable subset).
const (
	sIFMT  uint64 = 0o170000
	sIFDIR uint64 = 0o040000
	sIFREG uint64 = 0o100000
	sIFLNK uint64 = 0o120000
)

// HeaderSize is the fixed pxar item header size (htype + full_size).
const HeaderSize = 16

// hashFilename returns the SipHash-2-4 of a name using the pxar keys.
func hashFilename(name []byte) uint64 { return siphash24(hashKey1, hashKey2, name) }

// siphash24 implements SipHash-2-4 (2 compression rounds, 4 finalization).
func siphash24(k0, k1 uint64, data []byte) uint64 {
	v0 := k0 ^ 0x736f6d6570736575
	v1 := k1 ^ 0x646f72616e646f6d
	v2 := k0 ^ 0x6c7967656e657261
	v3 := k1 ^ 0x7465646279746573

	round := func() {
		v0 += v1
		v1 = rotl(v1, 13)
		v1 ^= v0
		v0 = rotl(v0, 32)
		v2 += v3
		v3 = rotl(v3, 16)
		v3 ^= v2
		v0 += v3
		v3 = rotl(v3, 21)
		v3 ^= v0
		v2 += v1
		v1 = rotl(v1, 17)
		v1 ^= v2
		v2 = rotl(v2, 32)
	}

	n := len(data)
	end := n - (n % 8)
	for i := 0; i < end; i += 8 {
		m := binary.LittleEndian.Uint64(data[i : i+8])
		v3 ^= m
		round()
		round()
		v0 ^= m
	}

	// Last block: remaining bytes plus the length in the top byte.
	var b uint64 = uint64(n) << 56
	for i, s := end, uint(0); i < n; i, s = i+1, s+8 {
		b |= uint64(data[i]) << s
	}
	v3 ^= b
	round()
	round()
	v0 ^= b

	v2 ^= 0xff
	round()
	round()
	round()
	round()
	return v0 ^ v1 ^ v2 ^ v3
}

func rotl(x uint64, b uint) uint64 { return (x << b) | (x >> (64 - b)) }
