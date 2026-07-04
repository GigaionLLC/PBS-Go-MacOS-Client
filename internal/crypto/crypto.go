// Package crypto provides the client-side chunk encryption primitive used for
// encrypted backups. v1 ships the raw AES-256-GCM seal/open operation over a
// 32-byte key. The PBS-specific DataBlob framing and the keyed (HMAC-SHA256)
// chunk digest used for encrypted dedup are tracked TODOs — see docs/DESIGN.md
// §5 — and must be validated against the PBS source before the crypto path is
// considered wire-complete.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// NonceSize is the AES-GCM nonce length in bytes.
const NonceSize = 12

// Key is a 32-byte symmetric encryption key.
type Key [KeySize]byte

// NewRandomKey returns a cryptographically random key.
func NewRandomKey() (Key, error) {
	var k Key
	if _, err := io.ReadFull(rand.Reader, k[:]); err != nil {
		return k, fmt.Errorf("generate key: %w", err)
	}
	return k, nil
}

// Sealed is an encrypted chunk: the random nonce prepended to the AES-GCM
// ciphertext (which already includes the authentication tag).
type Sealed struct {
	Nonce      [NonceSize]byte
	Ciphertext []byte // includes the 16-byte GCM tag
}

func (k Key) aead() (cipher.AEAD, error) {
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext with AES-256-GCM under a fresh random nonce.
func (k Key) Seal(plaintext []byte) (*Sealed, error) {
	aead, err := k.aead()
	if err != nil {
		return nil, err
	}
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ct := aead.Seal(nil, nonce[:], plaintext, nil)
	return &Sealed{Nonce: nonce, Ciphertext: ct}, nil
}

// Open decrypts a Sealed chunk, verifying the GCM tag.
func (k Key) Open(s *Sealed) ([]byte, error) {
	if s == nil {
		return nil, errors.New("nil sealed chunk")
	}
	aead, err := k.aead()
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, s.Nonce[:], s.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt/authenticate chunk: %w", err)
	}
	return pt, nil
}

// KeyedDigest computes the HMAC-SHA256 of data under the key. PBS uses a keyed
// digest for encrypted chunks so dedup is scoped to a key. This is provided now
// so the backup path can adopt it once the exact PBS keying is confirmed.
func (k Key) KeyedDigest(data []byte) [32]byte {
	mac := hmac.New(sha256.New, k[:])
	mac.Write(data)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}
