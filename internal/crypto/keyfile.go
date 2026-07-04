package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/scrypt"
)

// KeyFile is the PBS on-disk encryption key format (pbs-key-config/src/lib.rs).
// When kdf is set, `data` is base64(iv[16] ‖ tag[16] ‖ AES-256-GCM(raw key))
// under a passphrase-derived key; when kdf is null, `data` is the raw 32-byte
// key. Timestamps/fingerprint/hint are present but not needed to load the key.
type KeyFile struct {
	Kdf  *kdf   `json:"kdf"`
	Data string `json:"data"`
	Hint string `json:"hint,omitempty"`
}

// kdf is serde's externally-tagged enum: {"Scrypt": {...}} or {"PBKDF2": {...}}.
type kdf struct {
	Scrypt *scryptParams `json:"Scrypt,omitempty"`
	PBKDF2 *pbkdf2Params `json:"PBKDF2,omitempty"`
}

type scryptParams struct {
	N    int    `json:"n"`
	R    int    `json:"r"`
	P    int    `json:"p"`
	Salt string `json:"salt"` // base64
}

type pbkdf2Params struct {
	Iter int    `json:"iter"`
	Salt string `json:"salt"` // base64
}

// LooksLikeKeyFile reports whether data is a JSON PBS keyfile (vs a raw key).
func LooksLikeKeyFile(data []byte) bool {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return true
		default:
			return false
		}
	}
	return false
}

// LoadKeyFile parses a PBS keyfile and returns the 32-byte encryption key. For
// passphrase-protected keys, passphrase must be supplied (empty for kdf=null).
func LoadKeyFile(data []byte, passphrase []byte) (Key, error) {
	var kf KeyFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return Key{}, fmt.Errorf("parse keyfile: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(kf.Data)
	if err != nil {
		return Key{}, fmt.Errorf("keyfile data is not base64: %w", err)
	}

	var key Key
	if kf.Kdf == nil {
		if len(raw) != KeySize {
			return Key{}, fmt.Errorf("unencrypted keyfile has %d-byte key, want %d", len(raw), KeySize)
		}
		copy(key[:], raw)
		return key, nil
	}

	// Derive the key-encryption key from the passphrase.
	var kek []byte
	switch {
	case kf.Kdf.Scrypt != nil:
		sp := kf.Kdf.Scrypt
		salt, err := base64.StdEncoding.DecodeString(sp.Salt)
		if err != nil {
			return Key{}, fmt.Errorf("keyfile salt not base64: %w", err)
		}
		if kek, err = scrypt.Key(passphrase, salt, sp.N, sp.R, sp.P, KeySize); err != nil {
			return Key{}, fmt.Errorf("scrypt: %w", err)
		}
	case kf.Kdf.PBKDF2 != nil:
		pp := kf.Kdf.PBKDF2
		salt, err := base64.StdEncoding.DecodeString(pp.Salt)
		if err != nil {
			return Key{}, fmt.Errorf("keyfile salt not base64: %w", err)
		}
		if kek, err = pbkdf2.Key(sha256.New, string(passphrase), salt, pp.Iter, KeySize); err != nil {
			return Key{}, fmt.Errorf("pbkdf2: %w", err)
		}
	default:
		return Key{}, fmt.Errorf("keyfile has an unknown KDF")
	}

	// data = iv[16] || tag[16] || ciphertext; AES-256-GCM with empty AAD.
	if len(raw) < 32 {
		return Key{}, fmt.Errorf("encrypted keyfile data too short (%d bytes)", len(raw))
	}
	iv, tag, ct := raw[0:16], raw[16:32], raw[32:]
	block, err := aes.NewCipher(kek)
	if err != nil {
		return Key{}, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return Key{}, err
	}
	sealed := make([]byte, 0, len(ct)+len(tag))
	sealed = append(append(sealed, ct...), tag...)
	plain, err := aead.Open(nil, iv, sealed, nil)
	if err != nil {
		return Key{}, fmt.Errorf("decrypt keyfile (wrong passphrase?): %w", err)
	}
	if len(plain) != KeySize {
		return Key{}, fmt.Errorf("decrypted key is %d bytes, want %d", len(plain), KeySize)
	}
	copy(key[:], plain)
	return key, nil
}
