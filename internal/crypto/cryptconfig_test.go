package crypto

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

// The fingerprint input constant must equal sha256 of the documented string.
func TestFingerprintInputConstant(t *testing.T) {
	want := sha256.Sum256([]byte("Proxmox Backup Encryption Key Fingerprint"))
	if want != fingerprintInput {
		t.Fatalf("fingerprintInput does not match sha256 of the PBS string")
	}
}

func TestComputeDigestMatchesFormula(t *testing.T) {
	var k Key
	for i := range k {
		k[i] = byte(i)
	}
	cc, err := NewCryptConfig(k)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("chunk contents")

	// Independently: SHA256(data || id_key).
	h := sha256.New()
	h.Write(data)
	h.Write(cc.idKey[:])
	var want [32]byte
	copy(want[:], h.Sum(nil))

	if cc.ComputeDigest(data) != want {
		t.Fatal("ComputeDigest != SHA256(data || id_key)")
	}
}

// Different keys must produce different digests for the same data (the whole
// point of the id_key namespace).
func TestDigestNamespacedByKey(t *testing.T) {
	var k1, k2 Key
	k1[0], k2[0] = 1, 2
	cc1, _ := NewCryptConfig(k1)
	cc2, _ := NewCryptConfig(k2)
	data := []byte("same data")
	if cc1.ComputeDigest(data) == cc2.ComputeDigest(data) {
		t.Fatal("digests should differ across keys")
	}
	// And a plain SHA-256 must differ from the keyed digest.
	plain := sha256.Sum256(data)
	keyed := cc1.ComputeDigest(data)
	if bytes.Equal(plain[:], keyed[:]) {
		t.Fatal("keyed digest should differ from plain sha256")
	}
}
