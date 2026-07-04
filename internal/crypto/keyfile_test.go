package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"testing"

	"golang.org/x/crypto/scrypt"
)

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestLoadUnencryptedKeyFile(t *testing.T) {
	var key Key
	for i := range key {
		key[i] = byte(i + 1)
	}
	data, _ := json.Marshal(KeyFile{Data: b64(key[:])}) // kdf nil
	got, err := LoadKeyFile(data, nil)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if got != key {
		t.Fatal("unencrypted key mismatch")
	}
}

func TestLoadScryptKeyFile(t *testing.T) {
	var key Key
	for i := range key {
		key[i] = byte(0xA0 + i)
	}
	pass := []byte("correct horse battery staple")
	salt := []byte("0123456789abcdef0123456789abcdef") // 32 bytes

	// Build an encrypted keyfile exactly as PBS does: scrypt KEK, AES-256-GCM
	// with a 16-byte IV, data = iv || tag || ciphertext.
	const n, r, p = 65536, 8, 1
	kek, err := scrypt.Key(pass, salt, n, r, p, KeySize)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(kek)
	aead, _ := cipher.NewGCMWithNonceSize(block, 16)
	iv := []byte("sixteen byte iv!")
	sealed := aead.Seal(nil, iv, key[:], nil)
	ct, tag := sealed[:len(sealed)-16], sealed[len(sealed)-16:]
	data := append(append(append([]byte{}, iv...), tag...), ct...)

	kf := KeyFile{
		Kdf:  &kdf{Scrypt: &scryptParams{N: n, R: r, P: p, Salt: b64(salt)}},
		Data: b64(data),
	}
	blob, _ := json.Marshal(kf)

	got, err := LoadKeyFile(blob, pass)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if got != key {
		t.Fatal("decrypted key mismatch")
	}

	// Wrong passphrase must fail authentication.
	if _, err := LoadKeyFile(blob, []byte("wrong")); err == nil {
		t.Fatal("expected failure with wrong passphrase")
	}
}

func TestLooksLikeKeyFile(t *testing.T) {
	if !LooksLikeKeyFile([]byte("  \n{\"kdf\":null}")) {
		t.Error("JSON keyfile not detected")
	}
	if LooksLikeKeyFile([]byte("rawbytes")) {
		t.Error("raw data misdetected as keyfile")
	}
}
