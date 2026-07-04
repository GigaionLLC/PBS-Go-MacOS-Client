package crypto

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"fmt"
)

// fingerprintInput is sha256(b"Proxmox Backup Encryption Key Fingerprint"),
// the constant PBS feeds into compute_digest to derive a key fingerprint
// (pbs-tools/src/crypt_config.rs).
var fingerprintInput = [32]byte{
	110, 208, 239, 119, 71, 31, 255, 77, 85, 199, 168, 254, 74, 157, 182, 33,
	97, 64, 127, 19, 76, 114, 93, 223, 48, 153, 45, 37, 236, 69, 237, 38,
}

// CryptConfig mirrors PBS's CryptConfig: it holds the raw AES-256-GCM key and a
// derived id_key that namespaces chunk digests so encrypted dedup is scoped to
// the key (pbs-tools/src/crypt_config.rs).
type CryptConfig struct {
	encKey Key
	idKey  [32]byte
}

// NewCryptConfig derives the id_key from the encryption key via
// PBKDF2-HMAC-SHA256(enc_key, "_id_key", 10, 32), exactly as PBS does.
func NewCryptConfig(encKey Key) (*CryptConfig, error) {
	dk, err := pbkdf2.Key(sha256.New, string(encKey[:]), []byte("_id_key"), 10, KeySize)
	if err != nil {
		return nil, fmt.Errorf("derive id_key: %w", err)
	}
	cc := &CryptConfig{encKey: encKey}
	copy(cc.idKey[:], dk)
	return cc, nil
}

// EncKey returns the raw AES-256-GCM key (used for DataBlob encryption).
func (c *CryptConfig) EncKey() Key { return c.encKey }

// ComputeDigest returns SHA256(data || id_key) — the chunk digest for encrypted
// backups. The id_key is appended (not prepended) to resist length-extension.
func (c *CryptConfig) ComputeDigest(data []byte) [32]byte {
	h := sha256.New()
	h.Write(data)
	h.Write(c.idKey[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// AuthTag returns HMAC-SHA256(id_key, data) — used to sign the backup manifest.
func (c *CryptConfig) AuthTag(data []byte) [32]byte {
	mac := hmac.New(sha256.New, c.idKey[:])
	mac.Write(data)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// Fingerprint identifies the key without revealing it.
func (c *CryptConfig) Fingerprint() [32]byte {
	return c.ComputeDigest(fingerprintInput[:])
}
