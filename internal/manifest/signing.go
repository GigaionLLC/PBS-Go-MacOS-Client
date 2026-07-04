package manifest

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Signer is the subset of crypto.CryptConfig the manifest needs to sign itself.
// It is declared here (rather than importing internal/crypto) so the manifest
// package stays free of crypto dependencies; *crypto.CryptConfig satisfies it.
type Signer interface {
	// AuthTag returns HMAC-SHA256(id_key, data) — PBS's compute_auth_tag.
	AuthTag(data []byte) [32]byte
	// Fingerprint identifies the signing key without revealing it.
	Fingerprint() [32]byte
}

// FingerprintKey is the unprotected field under which the key fingerprint is
// recorded, matching PBS (pbs-datastore/src/manifest.rs).
const FingerprintKey = "key-fingerprint"

// canonicalJSON renders raw (a marshaled manifest) in PBS's canonical form and
// strips the fields excluded from the signature. It is a faithful port of
// pbs-datastore/src/manifest.rs write_canonical_json: object keys sorted
// recursively (byte-wise), arrays kept in order, numbers written verbatim, and
// strings escaped exactly as serde_json does — only ", \, and control bytes
// < 0x20 are escaped; every other byte (including "/", "<", "&", and the UTF-8
// of U+2028/U+2029) is copied raw. The top-level "unprotected" and "signature"
// fields are removed before hashing.
//
// A hand-rolled serializer (not json.Encoder) is required for byte-exact PBS
// compatibility: Go's encoder unconditionally escapes U+2028/U+2029 even with
// SetEscapeHTML(false), which serde_json does not — so a filename containing
// either character would otherwise produce a signature PBS cannot reproduce.
func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonical decode: %w", err)
	}
	if obj, ok := v.(map[string]any); ok {
		delete(obj, "unprotected")
		delete(obj, "signature")
	}
	var buf bytes.Buffer
	if err := writeCanonicalValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeCanonicalValue serializes a decoded JSON value (json.Number, string,
// bool, []any, map[string]any) in canonical form. Null is rejected, matching
// PBS ("canonical json does not allow null values").
func writeCanonicalValue(buf *bytes.Buffer, v any) error {
	switch val := v.(type) {
	case nil:
		return fmt.Errorf("canonical json does not allow null values")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(val.String())
	case string:
		writeCanonicalString(buf, val)
	case []any:
		buf.WriteByte('[')
		for i, e := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonicalValue(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonicalString(buf, k)
			buf.WriteByte(':')
			if err := writeCanonicalValue(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical json: unsupported value type %T", v)
	}
	return nil
}

// writeCanonicalString escapes s exactly as serde_json does: ", \, and the short
// forms \b \t \n \f \r; other control bytes as \u00xx (lowercase hex); every
// other byte — all UTF-8 continuation bytes and U+2028/U+2029 included — copied
// verbatim. Operating on bytes (not runes) keeps even invalid UTF-8 byte-identical
// to what PBS would hash.
func writeCanonicalString(buf *bytes.Buffer, s string) {
	const hexdigits = "0123456789abcdef"
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if c < 0x20 {
				buf.WriteString(`\u00`)
				buf.WriteByte(hexdigits[c>>4])
				buf.WriteByte(hexdigits[c&0x0f])
			} else {
				buf.WriteByte(c)
			}
		}
	}
	buf.WriteByte('"')
}

// computeSignature returns HMAC-SHA256(id_key, canonical_json(manifest)). The
// manifest's own Signature/Unprotected fields are ignored (canonicalJSON strips
// them), so it is safe to call before or after Sign.
func (m *Manifest) computeSignature(s Signer) ([32]byte, error) {
	raw, err := m.JSON()
	if err != nil {
		return [32]byte{}, err
	}
	canon, err := canonicalJSON(raw)
	if err != nil {
		return [32]byte{}, err
	}
	return s.AuthTag(canon), nil
}

// Sign computes the manifest signature with s and records it: the hex signature
// in the Signature field, and hex(fingerprint) under unprotected["key-fingerprint"].
// This mirrors PBS's to_value(Some(crypt_config)).
func (m *Manifest) Sign(s Signer) error {
	sig, err := m.computeSignature(s)
	if err != nil {
		return err
	}
	hexSig := hex.EncodeToString(sig[:])
	m.Signature = &hexSig
	if m.Unprotected == nil {
		m.Unprotected = map[string]any{}
	}
	fp := s.Fingerprint()
	m.Unprotected[FingerprintKey] = hex.EncodeToString(fp[:])
	return nil
}

// JSONSigned signs the manifest with s and returns the full signed JSON blob
// (ready to be DataBlob-wrapped and uploaded as index.json.blob). Use this in
// place of JSON() whenever a backup is encrypted; the manifest blob itself is
// stored unencrypted — it is signed, not encrypted.
func (m *Manifest) JSONSigned(s Signer) ([]byte, error) {
	if err := m.Sign(s); err != nil {
		return nil, err
	}
	return m.JSON()
}

// Verify recomputes the manifest's signature with s and reports whether it
// matches the stored Signature. It returns an error if the manifest is unsigned
// or the stored signature is malformed.
func (m *Manifest) Verify(s Signer) (bool, error) {
	if m.Signature == nil {
		return false, fmt.Errorf("manifest is not signed")
	}
	want, err := hex.DecodeString(*m.Signature)
	if err != nil {
		return false, fmt.Errorf("bad signature hex: %w", err)
	}
	got, err := m.computeSignature(s)
	if err != nil {
		return false, err
	}
	return hmac.Equal(want, got[:]), nil
}
