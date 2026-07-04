package cli

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
)

// restoreFlagSet mirrors cmdRestore's flags for reorder testing.
func restoreFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.String("repo", "", "")
	fs.Bool("list", false, "")
	fs.String("target", ".", "")
	fs.String("file", "", "")
	fs.String("keyfile", "", "")
	fs.Bool("json", false, "")
	return fs
}

func TestReorderArgsHoistsFlagsBeforePositionals(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			// The exact shape the GUI builds for restore: positionals first,
			// value flags after, --json appended last.
			name: "restore flags after positionals",
			in:   []string{"host/mymac/1700000000", "root.pxar", "--target", "/out", "--file", "/a.txt", "--json"},
			want: []string{"--target", "/out", "--file", "/a.txt", "--json", "host/mymac/1700000000", "root.pxar"},
		},
		{
			name: "archives with trailing --json",
			in:   []string{"host/mymac/1700000000", "--json"},
			want: []string{"--json", "host/mymac/1700000000"},
		},
		{
			name: "restore --list with trailing --json",
			in:   []string{"--list", "host/mymac/1", "root.pxar", "--json"},
			want: []string{"--list", "--json", "host/mymac/1", "root.pxar"},
		},
		{
			name: "already flags-first is unchanged",
			in:   []string{"--target", "/out", "host/mymac/1", "root.pxar"},
			want: []string{"--target", "/out", "host/mymac/1", "root.pxar"},
		},
		{
			name: "equals form is self-contained",
			in:   []string{"snap", "--target=/out", "arch", "--json"},
			want: []string{"--target=/out", "--json", "snap", "arch"},
		},
		{
			name: "double-dash terminator preserved",
			in:   []string{"--json", "--", "--weird-positional"},
			want: []string{"--json", "--", "--weird-positional"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reorderArgs(restoreFlagSet(), c.in)
			if len(got) != len(c.want) {
				t.Fatalf("length: got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("got %v, want %v", got, c.want)
				}
			}
		})
	}
}

// The whole point: after reordering, the FlagSet actually parses the flags and
// leaves exactly the positionals — the regression that broke every GUI command.
func TestReorderArgsThenParse(t *testing.T) {
	fs := restoreFlagSet()
	args := reorderArgs(fs, []string{"host/mymac/1", "root.pxar", "--target", "/out", "--json"})
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := fs.Lookup("target").Value.String(); got != "/out" {
		t.Errorf("target = %q, want /out", got)
	}
	if fs.Lookup("json").Value.String() != "true" {
		t.Error("--json was not parsed")
	}
	if fs.NArg() != 2 || fs.Arg(0) != "host/mymac/1" || fs.Arg(1) != "root.pxar" {
		t.Errorf("positionals = %v, want [host/mymac/1 root.pxar]", fs.Args())
	}
}

// A raw 32-byte key whose first byte is '{' must not be mistaken for a JSON keyfile.
func TestLoadKeyRawKeyBeginningWithBrace(t *testing.T) {
	var raw [crypto.KeySize]byte
	raw[0] = '{' // 0x7b — the LooksLikeKeyFile trigger
	for i := 1; i < len(raw); i++ {
		raw[i] = byte(i)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "raw.key")
	if err := os.WriteFile(p, raw[:], 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := loadKey(p)
	if err != nil {
		t.Fatalf("loadKey: %v", err)
	}
	if *k != crypto.Key(raw) {
		t.Fatal("raw key round-trip mismatch")
	}
}

// A real JSON keyfile still loads via the keyfile path.
func TestLoadKeyJSONKeyfile(t *testing.T) {
	var key crypto.Key
	for i := range key {
		key[i] = byte(i + 1)
	}
	blob, err := crypto.EncodeKeyFile(key, nil, "", time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	// sanity: it is JSON
	if !json.Valid(blob) {
		t.Fatal("EncodeKeyFile did not produce JSON")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "key.json")
	if err := os.WriteFile(p, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	k, err := loadKey(p)
	if err != nil {
		t.Fatalf("loadKey: %v", err)
	}
	if *k != key {
		t.Fatal("keyfile round-trip mismatch")
	}
}
