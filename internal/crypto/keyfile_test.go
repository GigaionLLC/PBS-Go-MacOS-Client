package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

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

func TestEncodeKeyFileUnencryptedRoundTrip(t *testing.T) {
	var key Key
	for i := range key {
		key[i] = byte(0x11 * (i%15 + 1))
	}
	blob, err := EncodeKeyFile(key, nil, "", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("EncodeKeyFile: %v", err)
	}
	if !LooksLikeKeyFile(blob) {
		t.Fatal("encoded keyfile not detected as a keyfile")
	}
	// kdf must be explicitly null and created/modified present for interop.
	var probe map[string]any
	if err := json.Unmarshal(blob, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe["kdf"] != nil {
		t.Errorf("kdf = %v, want null for unencrypted key", probe["kdf"])
	}
	if probe["created"] == "" || probe["modified"] == "" {
		t.Error("created/modified missing")
	}
	got, err := LoadKeyFile(blob, nil)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if got != key {
		t.Fatal("unencrypted round-trip mismatch")
	}
}

func TestEncodeKeyFileScryptRoundTrip(t *testing.T) {
	key, err := NewRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	pass := []byte("correct horse battery staple")
	blob, err := EncodeKeyFile(key, pass, "my hint", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatalf("EncodeKeyFile: %v", err)
	}

	var kf KeyFile
	if err := json.Unmarshal(blob, &kf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if kf.Kdf == nil || kf.Kdf.Scrypt == nil {
		t.Fatal("expected a Scrypt kdf")
	}
	if kf.Hint != "my hint" {
		t.Errorf("hint = %q, want %q", kf.Hint, "my hint")
	}

	got, err := LoadKeyFile(blob, pass)
	if err != nil {
		t.Fatalf("LoadKeyFile: %v", err)
	}
	if got != key {
		t.Fatal("scrypt round-trip mismatch")
	}
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
