// Package manifest implements the PBS backup manifest (index.json), ported from
// pbs-datastore/src/manifest.rs. The manifest lists every archive/blob in a
// snapshot with its size and checksum, and is uploaded as the blob named
// index.json.blob (a DataBlob).
package manifest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// BlobName is the fixed file name the manifest is stored under.
const BlobName = "index.json.blob"

// CryptMode mirrors pbs-api-types CryptMode.
type CryptMode string

const (
	CryptNone    CryptMode = "none"
	CryptEncrypt CryptMode = "encrypt"
	CryptSign    CryptMode = "sign-only"
)

// FileInfo describes one file (archive or blob) within the snapshot.
type FileInfo struct {
	Filename  string
	Size      uint64
	Csum      [32]byte
	CryptMode CryptMode
}

// fileInfoJSON is the wire form, with csum hex-encoded and PBS field names.
type fileInfoJSON struct {
	Filename  string    `json:"filename"`
	CryptMode CryptMode `json:"crypt-mode"`
	Size      uint64    `json:"size"`
	Csum      string    `json:"csum"`
}

// MarshalJSON renders a FileInfo with the PBS field names and hex csum.
func (f FileInfo) MarshalJSON() ([]byte, error) {
	mode := f.CryptMode
	if mode == "" {
		mode = CryptNone
	}
	return json.Marshal(fileInfoJSON{
		Filename:  f.Filename,
		CryptMode: mode,
		Size:      f.Size,
		Csum:      hex.EncodeToString(f.Csum[:]),
	})
}

// Manifest is a backup snapshot manifest.
type Manifest struct {
	BackupType  string         `json:"backup-type"`
	BackupID    string         `json:"backup-id"`
	BackupTime  int64          `json:"backup-time"`
	Files       []FileInfo     `json:"files"`
	Unprotected map[string]any `json:"unprotected"`
	Signature   *string        `json:"signature,omitempty"`
}

// New returns an empty manifest for the given snapshot coordinates.
func New(backupType, backupID string, backupTime int64) *Manifest {
	return &Manifest{
		BackupType:  backupType,
		BackupID:    backupID,
		BackupTime:  backupTime,
		Files:       []FileInfo{},
		Unprotected: map[string]any{},
	}
}

// AddFile records an archive/blob in the manifest.
func (m *Manifest) AddFile(f FileInfo) { m.Files = append(m.Files, f) }

// parseFileInfo is the wire form used when decoding.
type parseManifest struct {
	BackupType  string         `json:"backup-type"`
	BackupID    string         `json:"backup-id"`
	BackupTime  int64          `json:"backup-time"`
	Files       []fileInfoJSON `json:"files"`
	Unprotected map[string]any `json:"unprotected"`
	Signature   *string        `json:"signature"`
}

// Parse decodes a manifest from its JSON bytes (the decoded index.json.blob).
func Parse(data []byte) (*Manifest, error) {
	var pm parseManifest
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, err
	}
	m := New(pm.BackupType, pm.BackupID, pm.BackupTime)
	m.Unprotected = pm.Unprotected
	m.Signature = pm.Signature
	for _, f := range pm.Files {
		fi := FileInfo{Filename: f.Filename, Size: f.Size, CryptMode: f.CryptMode}
		// hex.Decode writes one byte per pair with no bound check, so a csum
		// longer than 64 hex chars would write past fi.Csum[32] and panic.
		if len(f.Csum) > hex.EncodedLen(len(fi.Csum)) {
			return nil, fmt.Errorf("bad csum for %s: %d hex chars (max %d)", f.Filename, len(f.Csum), hex.EncodedLen(len(fi.Csum)))
		}
		if _, err := hex.Decode(fi.Csum[:], []byte(f.Csum)); err != nil {
			return nil, fmt.Errorf("bad csum for %s: %w", f.Filename, err)
		}
		m.Files = append(m.Files, fi)
	}
	return m, nil
}

// JSON renders the manifest as bytes ready to be wrapped in a DataBlob and
// uploaded under BlobName. For encrypted backups the manifest is signed first —
// use JSONSigned (see signing.go), which HMACs the canonical JSON and records
// the key fingerprint. Unencrypted manifests need no signature.
func (m *Manifest) JSON() ([]byte, error) {
	return json.Marshal(m)
}
