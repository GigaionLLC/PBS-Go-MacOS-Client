package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	k, err := NewRandomKey()
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("the quick brown fox jumps over the lazy dog")
	sealed, err := k.Seal(plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(sealed.Ciphertext, plaintext) {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := k.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: %q != %q", got, plaintext)
	}
}

func TestOpenRejectsTamper(t *testing.T) {
	k, _ := NewRandomKey()
	sealed, _ := k.Seal([]byte("secret"))
	sealed.Ciphertext[0] ^= 0xFF // flip a bit
	if _, err := k.Open(sealed); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}

func TestWrongKeyFails(t *testing.T) {
	k1, _ := NewRandomKey()
	k2, _ := NewRandomKey()
	sealed, _ := k1.Seal([]byte("secret"))
	if _, err := k2.Open(sealed); err == nil {
		t.Fatal("expected failure decrypting with wrong key")
	}
}

func TestKeyedDigestDeterministic(t *testing.T) {
	k, _ := NewRandomKey()
	a := k.KeyedDigest([]byte("data"))
	b := k.KeyedDigest([]byte("data"))
	if a != b {
		t.Fatal("keyed digest not deterministic")
	}
}
