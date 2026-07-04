package datablob

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
)

func TestRoundTripAllVariants(t *testing.T) {
	key, _ := crypto.NewRandomKey()
	// Compressible payload so the compressed path is actually taken.
	data := bytes.Repeat([]byte("proxmox backup chunk data 0123456789 "), 500)

	cases := []struct {
		name     string
		key      *crypto.Key
		compress bool
		magic    [8]byte
	}{
		{"uncompressed", nil, false, UncompressedBlobMagic},
		{"compressed", nil, true, CompressedBlobMagic},
		{"encrypted", &key, false, EncryptedBlobMagic},
		{"encrypted+compressed", &key, true, EncrComprBlobMagic},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			blob, err := Encode(data, c.key, c.compress)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			var gotMagic [8]byte
			copy(gotMagic[:], blob[0:8])
			if gotMagic != c.magic {
				t.Fatalf("magic = %v, want %v", gotMagic, c.magic)
			}
			if IsEncrypted(blob) != (c.key != nil) {
				t.Fatalf("IsEncrypted = %v", IsEncrypted(blob))
			}
			got, err := Decode(blob, c.key)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("round-trip mismatch (%d vs %d bytes)", len(got), len(data))
			}
		})
	}
}

// TestHeaderLayout pins the exact byte offsets against the Rust struct layout.
func TestHeaderLayout(t *testing.T) {
	key, _ := crypto.NewRandomKey()
	data := []byte("small payload that does not compress well")

	// Unencrypted: magic[0:8], crc[8:12] over payload[12:].
	blob, _ := Encode(data, nil, false)
	if len(blob) != headerSize+len(data) {
		t.Fatalf("unencrypted size = %d, want %d", len(blob), headerSize+len(data))
	}
	wantCRC := crc32.Checksum(blob[headerSize:], crcTable)
	if got := binary.LittleEndian.Uint32(blob[8:12]); got != wantCRC {
		t.Fatalf("unencrypted crc = %08x, want %08x", got, wantCRC)
	}

	// Encrypted: 44-byte header, crc over ciphertext[44:].
	eblob, _ := Encode(data, &key, false)
	if len(eblob) != encHeaderSize+len(data)+0 { // GCM ciphertext == plaintext length
		t.Fatalf("encrypted size = %d, want %d", len(eblob), encHeaderSize+len(data))
	}
	wantECRC := crc32.Checksum(eblob[encHeaderSize:], crcTable)
	if got := binary.LittleEndian.Uint32(eblob[8:12]); got != wantECRC {
		t.Fatalf("encrypted crc = %08x, want %08x", got, wantECRC)
	}
}

func TestDecodeRejectsCorruption(t *testing.T) {
	key, _ := crypto.NewRandomKey()
	data := []byte("integrity-protected payload")

	blob, _ := Encode(data, nil, false)
	blob[len(blob)-1] ^= 0xFF // corrupt the payload
	if _, err := Decode(blob, nil); err == nil {
		t.Fatal("expected crc mismatch on corrupted unencrypted blob")
	}

	eblob, _ := Encode(data, &key, false)
	eblob[len(eblob)-1] ^= 0xFF // corrupt the ciphertext
	if _, err := Decode(eblob, &key); err == nil {
		t.Fatal("expected failure on corrupted encrypted blob")
	}

	// Wrong key must fail authentication.
	other, _ := crypto.NewRandomKey()
	good, _ := Encode(data, &key, false)
	if _, err := Decode(good, &other); err == nil {
		t.Fatal("expected auth failure decrypting with wrong key")
	}
}
