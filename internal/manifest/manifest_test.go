package manifest

import (
	"encoding/json"
	"testing"
)

func TestManifestJSONFieldNames(t *testing.T) {
	m := New("host", "mymac", 1_700_000_000)
	var csum [32]byte
	csum[0] = 0xAB
	m.AddFile(FileInfo{Filename: "root.pxar.didx", Size: 4096, Csum: csum})

	data, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"backup-type", "backup-id", "backup-time", "files", "unprotected"} {
		if _, ok := got[k]; !ok {
			t.Errorf("manifest missing field %q", k)
		}
	}
	files := got["files"].([]any)
	f0 := files[0].(map[string]any)
	if f0["filename"] != "root.pxar.didx" {
		t.Errorf("filename = %v", f0["filename"])
	}
	if f0["crypt-mode"] != string(CryptNone) {
		t.Errorf("crypt-mode = %v, want none", f0["crypt-mode"])
	}
	if f0["size"].(float64) != 4096 {
		t.Errorf("size = %v", f0["size"])
	}
	// csum must be 64 hex chars, first byte AB.
	cs := f0["csum"].(string)
	if len(cs) != 64 || cs[:2] != "ab" {
		t.Errorf("csum = %q", cs)
	}
}

func TestBlobName(t *testing.T) {
	if BlobName != "index.json.blob" {
		t.Fatalf("BlobName = %q", BlobName)
	}
}

func TestManifestParseRoundTrip(t *testing.T) {
	m := New("host", "mymac", 1_700_000_000)
	var c1, c2 [32]byte
	c1[0], c2[31] = 0x11, 0x22
	m.AddFile(FileInfo{Filename: "root.pxar.didx", Size: 8192, Csum: c1, CryptMode: CryptEncrypt})
	m.AddFile(FileInfo{Filename: "catalog.pcat1.didx", Size: 4096, Csum: c2})

	data, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.BackupType != "host" || got.BackupID != "mymac" || got.BackupTime != 1_700_000_000 {
		t.Fatalf("header mismatch: %+v", got)
	}
	if len(got.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(got.Files))
	}
	if got.Files[0].Filename != "root.pxar.didx" || got.Files[0].Size != 8192 ||
		got.Files[0].Csum != c1 || got.Files[0].CryptMode != CryptEncrypt {
		t.Errorf("file[0] mismatch: %+v", got.Files[0])
	}
	if got.Files[1].CryptMode != CryptNone {
		t.Errorf("file[1] crypt-mode = %q, want none", got.Files[1].CryptMode)
	}
}
