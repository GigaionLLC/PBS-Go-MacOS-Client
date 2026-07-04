package manifest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
	"golang.org/x/crypto/scrypt"
)

// u2028 is the raw UTF-8 of U+2028 LINE SEPARATOR, built from bytes so this
// source file stays pure ASCII. Go's json.Encoder escapes this codepoint even
// with SetEscapeHTML(false); serde_json (and thus our canonicalizer) does not.
var u2028 = string([]byte{0xe2, 0x80, 0xa8})

// goldSigner builds the CryptConfig from PBS's test vector key:
// scrypt(pw="test", salt="", N=65536, r=8, p=1, len=32).
func goldSigner(t *testing.T) *crypto.CryptConfig {
	t.Helper()
	dk, err := scrypt.Key([]byte("test"), []byte{}, 65536, 8, 1, crypto.KeySize)
	if err != nil {
		t.Fatalf("scrypt: %v", err)
	}
	var key crypto.Key
	copy(key[:], dk)
	cc, err := crypto.NewCryptConfig(key)
	if err != nil {
		t.Fatalf("NewCryptConfig: %v", err)
	}
	return cc
}

// TestManifestSignatureGoldVector reproduces test_manifest_signature from
// pbs-datastore/src/manifest.rs. Matching the expected hex proves our canonical
// JSON + CryptConfig keyed HMAC are byte-compatible with PBS.
func TestManifestSignatureGoldVector(t *testing.T) {
	const wantSig = "d7b446fb7db081662081d4b40fedd858a1d6307a5aff4ecff7d5bf4fd35679e9"

	// host/elsa/2020-06-26T13:56:05Z
	bt, err := time.Parse(time.RFC3339, "2020-06-26T13:56:05Z")
	if err != nil {
		t.Fatal(err)
	}
	m := New("host", "elsa", bt.Unix())

	var c1, c2 [32]byte
	for i := range c1 {
		c1[i], c2[i] = 1, 2
	}
	m.AddFile(FileInfo{Filename: "test1.img.fidx", Size: 200, Csum: c1, CryptMode: CryptEncrypt})
	m.AddFile(FileInfo{Filename: "abc.blob", Size: 200, Csum: c2, CryptMode: CryptNone})
	// This note lives in "unprotected" and must NOT affect the signature.
	m.Unprotected["note"] = "This is not protected by the signature."

	cc := goldSigner(t)
	if err := m.Sign(cc); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if m.Signature == nil {
		t.Fatal("Signature not set after Sign")
	}
	if *m.Signature != wantSig {
		t.Fatalf("signature mismatch:\n got  %s\n want %s", *m.Signature, wantSig)
	}

	// The fingerprint is recorded under unprotected["key-fingerprint"] as hex.
	fp := cc.Fingerprint()
	if got := m.Unprotected[FingerprintKey]; got != hex.EncodeToString(fp[:]) {
		t.Fatalf("key-fingerprint = %v", got)
	}
}

// TestSignatureIndependentOfUnprotected confirms the note under "unprotected"
// does not change the signature (the whole point of the gold vector's note).
func TestSignatureIndependentOfUnprotected(t *testing.T) {
	cc := goldSigner(t)

	mk := func(note string) *Manifest {
		m := New("host", "elsa", 1_593_179_765)
		var c [32]byte
		c[0] = 0xAA
		m.AddFile(FileInfo{Filename: "root.pxar.didx", Size: 100, Csum: c, CryptMode: CryptEncrypt})
		if note != "" {
			m.Unprotected["note"] = note
		}
		return m
	}
	a, b := mk("first note"), mk("a completely different note")
	if err := a.Sign(cc); err != nil {
		t.Fatal(err)
	}
	if err := b.Sign(cc); err != nil {
		t.Fatal(err)
	}
	if *a.Signature != *b.Signature {
		t.Fatalf("signature changed with unprotected content: %s vs %s", *a.Signature, *b.Signature)
	}
}

// TestJSONSignedAndVerify checks the full signed-blob round-trip: JSONSigned
// produces a manifest that parses back and Verify-ies under the same key, and
// fails to verify under a different key.
func TestJSONSignedAndVerify(t *testing.T) {
	cc := goldSigner(t)

	m := New("host", "mymac", 1_700_000_000)
	var c [32]byte
	c[0] = 0x11
	m.AddFile(FileInfo{Filename: "root.pxar.didx", Size: 8192, Csum: c, CryptMode: CryptEncrypt})

	blob, err := m.JSONSigned(cc)
	if err != nil {
		t.Fatalf("JSONSigned: %v", err)
	}

	// The signed blob must carry signature + key-fingerprint.
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["signature"]; !ok {
		t.Error("signed blob missing signature")
	}
	unp, _ := raw["unprotected"].(map[string]any)
	if _, ok := unp[FingerprintKey]; !ok {
		t.Error("signed blob missing unprotected.key-fingerprint")
	}

	parsed, err := Parse(blob)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ok, err := parsed.Verify(cc)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify returned false for a correctly signed manifest")
	}

	// A different key must not verify.
	var otherKey crypto.Key
	otherKey[0] = 0x99
	other, _ := crypto.NewCryptConfig(otherKey)
	ok, err = parsed.Verify(other)
	if err != nil {
		t.Fatalf("Verify (wrong key): %v", err)
	}
	if ok {
		t.Fatal("Verify accepted a manifest under the wrong key")
	}
}

// TestVerifyUnsigned reports a clear error for an unsigned manifest.
func TestVerifyUnsigned(t *testing.T) {
	m := New("host", "mymac", 1)
	if _, err := m.Verify(goldSigner(t)); err == nil {
		t.Fatal("expected error verifying an unsigned manifest")
	}
}

// TestCanonicalStringEscaping locks the string escaper to serde_json's exact
// behavior (the crux of PBS byte-compatibility): only ", \, and control bytes
// are escaped; "/", "<", "&", DEL (0x7f), and non-ASCII are copied raw.
func TestCanonicalStringEscaping(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "root.pxar.didx", `"root.pxar.didx"`},
		{"no-html-escaping", "a/b<c>d&e", `"a/b<c>d&e"`},
		{"quote-and-backslash", "q\"b\\", `"q\"b\\"`},
		{"short-controls", "a\tb\nc\rd\be\ff", `"a\tb\nc\rd\be\ff"`},
		{"other-controls", "\x00\x1f", "\"\\u0000\\u001f\""},
		{"del-is-raw", "x\x7fy", "\"x\x7fy\""},
		{"u2028-raw", "l" + u2028 + "p", "\"l" + u2028 + "p\""},
		{"literal-backslash-u", "\\u2028", `"\\u2028"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var b bytes.Buffer
			writeCanonicalString(&b, c.in)
			if b.String() != c.want {
				t.Errorf("escape(%q) = %q, want %q", c.in, b.String(), c.want)
			}
		})
	}
}

// TestCanonicalU2028NotEscaped is a focused regression guard: a filename with
// U+2028 must canonicalize with the raw UTF-8 bytes, and the canonical form must
// NOT contain the 6-byte ASCII escape " " that Go's json.Encoder would emit.
func TestCanonicalU2028NotEscaped(t *testing.T) {
	m := New("host", "elsa", 1_593_179_765)
	var c [32]byte
	c[0] = 0xAA
	m.AddFile(FileInfo{Filename: "re" + u2028 + "port.pdf", Size: 1, Csum: c, CryptMode: CryptEncrypt})

	raw, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	canon, err := canonicalJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(canon, []byte("re"+u2028+"port.pdf")) {
		t.Errorf("canonical form did not preserve raw U+2028:\n%q", canon)
	}
	if strings.Contains(string(canon), "\\u2028") {
		t.Errorf("canonical form escaped U+2028 (incompatible with PBS):\n%q", canon)
	}
}

// TestCanonicalRejectsNull matches PBS: null is not allowed in canonical JSON.
func TestCanonicalRejectsNull(t *testing.T) {
	if _, err := canonicalJSON([]byte(`{"backup-id":null}`)); err == nil {
		t.Fatal("expected canonicalJSON to reject a null value")
	}
}
