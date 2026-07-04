// Package datablob implements the PBS "DataBlob" chunk/blob framing, ported
// byte-for-byte from the official source (pbs-datastore/src/data_blob.rs and
// file_formats.rs). Every chunk and blob uploaded to or downloaded from a PBS
// datastore is a DataBlob.
//
// Layout (all little-endian):
//
//	DataBlobHeader          = magic[8] ++ crc[4]                    (12 bytes)
//	EncryptedDataBlobHeader = DataBlobHeader ++ iv[16] ++ tag[16]   (44 bytes)
//
// Four variants, selected by the magic:
//
//	uncompressed      : header(12) ++ data
//	compressed        : header(12) ++ zstd(data)
//	encrypted         : encheader(44) ++ aes256gcm(data)
//	encrypted+zstd    : encheader(44) ++ aes256gcm(zstd(data))
//
// The CRC is CRC-32 (IEEE) over the bytes *after* the header (i.e. over the
// ciphertext for encrypted variants). Encryption is AES-256-GCM with a random
// 16-byte IV, a 16-byte tag, and empty AAD. Compression is zstd (level 1).
package datablob

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/klauspost/compress/zstd"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
)

// Magic numbers from pbs-datastore/src/file_formats.rs.
var (
	UncompressedBlobMagic = [8]byte{66, 171, 56, 7, 190, 131, 112, 161}
	CompressedBlobMagic   = [8]byte{49, 185, 88, 66, 111, 182, 163, 127}
	EncryptedBlobMagic    = [8]byte{123, 103, 133, 190, 34, 45, 76, 240}
	EncrComprBlobMagic    = [8]byte{230, 89, 27, 191, 11, 191, 216, 11}

	// Index-file magics, exported for the index format work (not used here).
	FixedIndexMagic   = [8]byte{47, 127, 65, 237, 145, 253, 15, 205}
	DynamicIndexMagic = [8]byte{28, 145, 78, 165, 25, 186, 179, 205}
	CatalogMagic      = [8]byte{145, 253, 96, 249, 196, 103, 88, 213}
)

const (
	headerSize    = 12 // magic[8] + crc[4]
	encHeaderSize = 44 // header + iv[16] + tag[16]
	ivSize        = 16
	tagSize       = 16
)

// crc32 uses the IEEE polynomial, matching Rust's crc32fast.
var crcTable = crc32.IEEETable

// zstd one-shot encoder/decoder are safe for concurrent use.
var (
	zstdEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(1)))
	zstdDec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
)

// aead builds the AES-256-GCM AEAD with PBS's 16-byte nonce size.
func aead(key crypto.Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, ivSize)
}

// Encode frames data as a DataBlob. If key is non-nil the payload is encrypted;
// if compress is set the payload is zstd-compressed when that actually shrinks
// it (matching the upstream "only keep compression if smaller" behavior).
func Encode(data []byte, key *crypto.Key, compress bool) ([]byte, error) {
	payload := data
	compressed := false
	if compress {
		c := zstdEnc.EncodeAll(data, nil)
		if len(c) < len(data) {
			payload, compressed = c, true
		}
	}

	if key == nil {
		magic := UncompressedBlobMagic
		if compressed {
			magic = CompressedBlobMagic
		}
		out := make([]byte, headerSize, headerSize+len(payload))
		copy(out[0:8], magic[:])
		binary.LittleEndian.PutUint32(out[8:12], crc32.Checksum(payload, crcTable))
		return append(out, payload...), nil
	}

	// Encrypted variants.
	ae, err := aead(*key)
	if err != nil {
		return nil, err
	}
	var iv [ivSize]byte
	if _, err := io.ReadFull(rand.Reader, iv[:]); err != nil {
		return nil, fmt.Errorf("generate iv: %w", err)
	}
	sealed := ae.Seal(nil, iv[:], payload, nil) // ciphertext ++ tag
	if len(sealed) < tagSize {
		return nil, fmt.Errorf("aead output too short")
	}
	ct := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]

	magic := EncryptedBlobMagic
	if compressed {
		magic = EncrComprBlobMagic
	}
	out := make([]byte, encHeaderSize, encHeaderSize+len(ct))
	copy(out[0:8], magic[:])
	binary.LittleEndian.PutUint32(out[8:12], crc32.Checksum(ct, crcTable))
	copy(out[12:28], iv[:])
	copy(out[28:44], tag)
	return append(out, ct...), nil
}

// Decode reverses Encode: it verifies the CRC, then decrypts and/or
// decompresses as indicated by the magic. key may be nil for plaintext blobs.
func Decode(blob []byte, key *crypto.Key) ([]byte, error) {
	if len(blob) < headerSize {
		return nil, fmt.Errorf("datablob too short: %d bytes", len(blob))
	}
	var magic [8]byte
	copy(magic[:], blob[0:8])
	wantCRC := binary.LittleEndian.Uint32(blob[8:12])

	switch magic {
	case UncompressedBlobMagic, CompressedBlobMagic:
		payload := blob[headerSize:]
		if got := crc32.Checksum(payload, crcTable); got != wantCRC {
			return nil, fmt.Errorf("crc mismatch: got %08x want %08x", got, wantCRC)
		}
		if magic == CompressedBlobMagic {
			return zstdDec.DecodeAll(payload, nil)
		}
		return append([]byte(nil), payload...), nil

	case EncryptedBlobMagic, EncrComprBlobMagic:
		if key == nil {
			return nil, fmt.Errorf("blob is encrypted but no key was provided")
		}
		if len(blob) < encHeaderSize {
			return nil, fmt.Errorf("encrypted datablob too short: %d bytes", len(blob))
		}
		iv := blob[12:28]
		tag := blob[28:44]
		ct := blob[encHeaderSize:]
		if got := crc32.Checksum(ct, crcTable); got != wantCRC {
			return nil, fmt.Errorf("crc mismatch: got %08x want %08x", got, wantCRC)
		}
		ae, err := aead(*key)
		if err != nil {
			return nil, err
		}
		sealed := make([]byte, 0, len(ct)+len(tag))
		sealed = append(sealed, ct...)
		sealed = append(sealed, tag...)
		payload, err := ae.Open(nil, iv, sealed, nil)
		if err != nil {
			return nil, fmt.Errorf("decrypt/authenticate blob: %w", err)
		}
		if magic == EncrComprBlobMagic {
			return zstdDec.DecodeAll(payload, nil)
		}
		return payload, nil

	default:
		return nil, fmt.Errorf("unknown datablob magic %v", magic)
	}
}

// IsEncrypted reports whether a blob uses one of the encrypted magics.
func IsEncrypted(blob []byte) bool {
	if len(blob) < 8 {
		return false
	}
	var magic [8]byte
	copy(magic[:], blob[0:8])
	return magic == EncryptedBlobMagic || magic == EncrComprBlobMagic
}
