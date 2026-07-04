package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/backup"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/config"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/crypto"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/exclude"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/keychain"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/manifest"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/protocol"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/repo"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/restore"
	"github.com/GigaionLLC/PBS-Go-MacOS-Client/internal/source"
)

// stringSlice is a repeatable string flag.
type stringSlice []string

func (s *stringSlice) String() string     { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error { *s = append(*s, v); return nil }

// parseArchiveSpec splits "name.pxar:/path/to/dir" into its parts.
func parseArchiveSpec(spec string) (archive, path string, err error) {
	i := strings.Index(spec, ":")
	if i <= 0 || i == len(spec)-1 {
		return "", "", fmt.Errorf("expected archive spec NAME.pxar:/path, got %q", spec)
	}
	return spec[:i], spec[i+1:], nil
}

// EnvKeyPassword holds the passphrase for a password-protected keyfile.
const envKeyPassword = "PBS_ENCRYPTION_PASSWORD"

// loadKey reads an encryption key: a PBS JSON keyfile (scrypt/PBKDF2-protected,
// passphrase from PBS_ENCRYPTION_PASSWORD) or a bare 32-byte / 64-hex key.
func loadKey(path string) (*crypto.Key, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if crypto.LooksLikeKeyFile(data) {
		k, err := crypto.LoadKeyFile(data, []byte(os.Getenv(envKeyPassword)))
		if err != nil {
			return nil, err
		}
		return &k, nil
	}
	var k crypto.Key
	switch {
	case len(data) == crypto.KeySize:
		copy(k[:], data)
	case len(strings.TrimSpace(string(data))) == crypto.KeySize*2:
		b, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, fmt.Errorf("keyfile is not valid hex: %w", err)
		}
		copy(k[:], b)
	default:
		return nil, fmt.Errorf("keyfile must be a PBS JSON keyfile, %d raw bytes, or %d hex chars", crypto.KeySize, crypto.KeySize*2)
	}
	return &k, nil
}

// emitJSON pretty-prints v to stdout as JSON and returns exit 0.
func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return 0
}

// backupJSON is the machine-readable backup result: the pipeline Result plus a
// hex index csum, the dedup ratio, and (for live backups) the created snapshot id.
type backupJSON struct {
	backup.Result
	DedupRatio float64 `json:"dedup_ratio"`
	IndexCsum  string  `json:"index_csum"`
	Snapshot   string  `json:"snapshot,omitempty"`
}

func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	// --repo is accepted for forward-compatibility with the live upload path
	// (M2+); the dry-run path does not contact a server.
	repoFlag := fs.String("repo", "", "repository spec (overrides env/config)")
	dryRun := fs.Bool("dry-run", false, "walk and chunk locally without uploading")
	encrypt := fs.Bool("encrypt", false, "encrypt chunks (dry-run uses an ephemeral key if no --keyfile)")
	keyfile := fs.String("keyfile", "", "path to a 32-byte (or 64-hex) encryption key")
	backupID := fs.String("id", "", "backup id (defaults to the hostname)")
	compress := fs.Bool("compress", false, "zstd-compress chunks")
	snapshot := fs.Bool("snapshot", false, "back up a crash-consistent APFS local snapshot (macOS; needs sudo + Full Disk Access)")
	var excludes stringSlice
	fs.Var(&excludes, "exclude", "exclude glob pattern (repeatable); .pxarexclude in the root is also read")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	if fs.NArg() != 1 {
		return fail("backup: expected exactly one NAME.pxar:/path argument")
	}
	archive, path, err := parseArchiveSpec(fs.Arg(0))
	if err != nil {
		return fail("%v", err)
	}

	srcFS, err := source.NewLiveDirectoryFS(path)
	if err != nil {
		return fail("source: %v", err)
	}
	// Optional: back up a crash-consistent APFS snapshot instead of the live tree.
	if *snapshot {
		ssFS, closer, err := source.OpenSnapshot(path)
		if err != nil {
			return fail("snapshot: %v", err)
		}
		defer closer.Close() // unmount + delete, even if the backup later fails
		srcFS = ssFS
	}

	// Resolve an optional encryption key.
	var key *crypto.Key
	if *keyfile != "" {
		if key, err = loadKey(*keyfile); err != nil {
			return fail("keyfile: %v", err)
		}
	} else if *encrypt {
		k, err := crypto.NewRandomKey()
		if err != nil {
			return fail("generate key: %v", err)
		}
		key = &k
	}

	var cc *crypto.CryptConfig
	if key != nil {
		if cc, err = crypto.NewCryptConfig(*key); err != nil {
			return fail("crypto: %v", err)
		}
	}

	// Build the exclude matcher from --exclude flags plus a .pxarexclude file
	// in the backup root, if present.
	excludeLines := []string(excludes)
	if b, err := os.ReadFile(filepath.Join(srcFS.Root(), ".pxarexclude")); err == nil {
		excludeLines = append(excludeLines, strings.Split(string(b), "\n")...)
	}

	opts := backup.Options{Crypt: cc, Compress: *compress, Exclude: exclude.New(excludeLines)}

	if *dryRun {
		res, _, err := backup.Run(srcFS, "/", backup.NullSink{}, opts)
		if err != nil {
			return fail("backup: %v", err)
		}
		if *outputJSON {
			return emitJSON(backupJSON{Result: res, DedupRatio: res.DedupRatio(), IndexCsum: fmt.Sprintf("%x", res.IndexCsum)})
		}
		fmt.Printf("dry-run backup of %s -> archive %q\n%s\n", srcFS.Root(), archive, backup.FormatResult(res))
		return 0
	}

	// Live backup: upload to the server.
	client, err := resolveClient(*repoFlag)
	if err != nil {
		return fail("%v", err)
	}
	id := *backupID
	if id == "" {
		if h, err := os.Hostname(); err == nil {
			id = h
		} else {
			id = "unknown"
		}
	}
	snap := protocol.Snapshot{Type: "host", ID: id, Time: time.Now().Unix()}
	res, err := backup.Upload(context.Background(), client, snap, archive, srcFS, "/", opts)
	if err != nil {
		return fail("backup: %v", err)
	}
	if *outputJSON {
		return emitJSON(backupJSON{
			Result:     res,
			DedupRatio: res.DedupRatio(),
			IndexCsum:  fmt.Sprintf("%x", res.IndexCsum),
			Snapshot:   fmt.Sprintf("%s/%s/%d", snap.Type, snap.ID, snap.Time),
		})
	}
	fmt.Printf("backed up %s -> %s/%s archive %q\n%s\n", srcFS.Root(), snap.Type, snap.ID, archive, backup.FormatResult(res))
	return 0
}

// resolveClient builds a protocol client from flags/env/config.
func resolveClient(repoFlag string) (*protocol.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	spec := cfg.ResolveRepository(repoFlag)
	if spec == "" {
		return nil, fmt.Errorf("no repository set (use --repo, %s, or `pbmac login`)", config.EnvRepository)
	}
	r, err := repo.Parse(spec)
	if err != nil {
		return nil, err
	}
	fp := os.Getenv(config.EnvFingerprint)
	if fp == "" {
		fp = cfg.Fingerprint
	}
	// Token precedence: env var, then the macOS Keychain (keyed by repo spec).
	token := os.Getenv(config.EnvAPIToken)
	if token == "" {
		if t, err := keychain.Retrieve(spec); err == nil {
			token = t
		}
	}
	return protocol.Dial(r, protocol.Credentials{
		APIToken:    token,
		Fingerprint: fp,
	})
}

func cmdPing(args []string) int {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	repoFlag := fs.String("repo", "", "repository spec (overrides env/config)")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	client, err := resolveClient(*repoFlag)
	if err != nil {
		return fail("%v", err)
	}
	v, err := client.GetVersion(context.Background())
	if err != nil {
		return fail("ping: %v", err)
	}
	if *outputJSON {
		return emitJSON(v)
	}
	fmt.Printf("ok: PBS %s-%s (repoid %s)\n", v.Version, v.Release, v.RepoID)
	return 0
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	repoFlag := fs.String("repo", "", "repository spec (overrides env/config)")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	client, err := resolveClient(*repoFlag)
	if err != nil {
		return fail("%v", err)
	}
	snaps, err := client.ListSnapshots(context.Background())
	if err != nil {
		return fail("list: %v", err)
	}
	if *outputJSON {
		if snaps == nil {
			snaps = []protocol.Snapshot{} // encode as [] not null
		}
		return emitJSON(snaps)
	}
	for _, s := range snaps {
		fmt.Printf("%s/%s\t%d\t%s\n", s.Type, s.ID, s.Time, s.Comment)
	}
	return 0
}

// parseSnapshot decodes a "type/id/unixtime" snapshot spec.
func parseSnapshot(s string) (protocol.Snapshot, error) {
	parts := strings.SplitN(s, "/", 3)
	if len(parts) != 3 {
		return protocol.Snapshot{}, fmt.Errorf("snapshot must be type/id/unixtime, got %q", s)
	}
	t, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return protocol.Snapshot{}, fmt.Errorf("bad backup-time %q", parts[2])
	}
	return protocol.Snapshot{Type: parts[0], ID: parts[1], Time: t}, nil
}

func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	repoFlag := fs.String("repo", "", "repository spec (overrides env/config)")
	list := fs.Bool("list", false, "list archive contents instead of restoring")
	target := fs.String("target", ".", "destination directory for restored files")
	file := fs.String("file", "", "restore only this single path from the archive")
	keyfile := fs.String("keyfile", "", "path to the decryption key (for encrypted backups)")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	if fs.NArg() != 2 {
		return fail("restore: expected SNAPSHOT and ARCHIVE (e.g. host/mymac/1700000000 root.pxar)")
	}
	snap, err := parseSnapshot(fs.Arg(0))
	if err != nil {
		return fail("%v", err)
	}
	archive := fs.Arg(1)

	client, err := resolveClient(*repoFlag)
	if err != nil {
		return fail("%v", err)
	}
	var key *crypto.Key
	if *keyfile != "" {
		if key, err = loadKey(*keyfile); err != nil {
			return fail("keyfile: %v", err)
		}
	}

	if *list {
		var l restore.Lister
		if err := restore.Archive(context.Background(), client, snap, archive, key, &l); err != nil {
			return fail("restore: %v", err)
		}
		if *outputJSON {
			if l.Entries == nil {
				l.Entries = []restore.ListEntry{} // encode as [] not null
			}
			return emitJSON(l.Entries)
		}
		for _, e := range l.Entries {
			fmt.Printf("%-7s %10d  %s\n", e.Type, e.Size, e.Path)
		}
		return 0
	}

	ex := &restore.Extractor{Dest: *target, Only: *file}
	if err := restore.Archive(context.Background(), client, snap, archive, key, ex); err != nil {
		return fail("restore: %v", err)
	}
	if *outputJSON {
		return emitJSON(map[string]any{
			"snapshot":       fmt.Sprintf("%s/%s/%d", snap.Type, snap.ID, snap.Time),
			"archive":        archive,
			"target":         *target,
			"files_restored": ex.Files,
			"bytes_written":  ex.Bytes,
		})
	}
	fmt.Printf("restored %s/%s archive %q to %s\n", snap.Type, snap.ID, archive, *target)
	return 0
}

func cmdLogin(args []string) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	repoFlag := fs.String("repo", "", "repository spec to store")
	fingerprint := fs.String("fingerprint", "", "server certificate SHA-256 to pin")
	token := fs.String("token", "", "API token USER@REALM!TOKENID:SECRET to store in the Keychain")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	if *repoFlag == "" {
		return fail("login: --repo is required")
	}
	if _, err := repo.Parse(*repoFlag); err != nil {
		return fail("login: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail("login: %v", err)
	}
	cfg.Repository = *repoFlag
	if *fingerprint != "" {
		cfg.Fingerprint = *fingerprint
	}
	if err := cfg.Save(); err != nil {
		return fail("login: %v", err)
	}
	p, _ := config.Path()
	tokenStored := false
	if *token != "" {
		if err := keychain.Store(*repoFlag, *token); err != nil {
			if *outputJSON {
				return fail("login: keychain: %v", err)
			}
			fmt.Printf("saved repository %q to %s\n", *repoFlag, p)
			fmt.Printf("note: could not store the token in the Keychain (%v);\n"+
				"      set it via the %s environment variable instead.\n", err, config.EnvAPIToken)
			return 0
		}
		tokenStored = true
	}
	if *outputJSON {
		return emitJSON(map[string]any{
			"repository":   *repoFlag,
			"config_path":  p,
			"fingerprint":  cfg.Fingerprint,
			"token_stored": tokenStored,
		})
	}
	fmt.Printf("saved repository %q to %s\n", *repoFlag, p)
	if tokenStored {
		fmt.Printf("stored API token in the macOS Keychain for %q\n", *repoFlag)
	} else {
		fmt.Printf("note: provide --token to store the API token in the Keychain, or set %s.\n", config.EnvAPIToken)
	}
	return 0
}

// cmdKey dispatches the `key` command group. Today it has one subcommand,
// `create`, which generates a client-side encryption key for encrypted backups.
func cmdKey(args []string) int {
	if len(args) == 0 {
		return fail("key: expected a subcommand (create)")
	}
	switch args[0] {
	case "create":
		return cmdKeyCreate(args[1:])
	default:
		return fail("key: unknown subcommand %q (expected create)", args[0])
	}
}

// defaultKeyfilePath returns the standard keyfile location, next to config.json.
func defaultKeyfilePath() (string, error) {
	p, err := config.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "encryption-key.json"), nil
}

// cmdKeyCreate generates a random AES-256 key and writes it as a PBS keyfile.
// With --kdf scrypt (default) the key is wrapped under the passphrase in
// PBS_ENCRYPTION_PASSWORD; --kdf none writes an unprotected key. Never
// overwrites an existing file unless --force.
func cmdKeyCreate(args []string) int {
	fs := flag.NewFlagSet("key create", flag.ContinueOnError)
	out := fs.String("keyfile", "", "output path (default: alongside config.json as encryption-key.json)")
	kdfName := fs.String("kdf", "scrypt", "key protection: scrypt (passphrase-protected) or none")
	hint := fs.String("hint", "", "non-secret hint stored in the keyfile (e.g. which passphrase)")
	force := fs.Bool("force", false, "overwrite an existing keyfile")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON

	path := *out
	if path == "" {
		p, err := defaultKeyfilePath()
		if err != nil {
			return fail("key create: %v", err)
		}
		path = p
	}

	var passphrase []byte
	switch *kdfName {
	case "scrypt":
		passphrase = []byte(os.Getenv(envKeyPassword))
		if len(passphrase) == 0 {
			return fail("key create: --kdf scrypt needs a passphrase — set %s (or use --kdf none for an unprotected key)", envKeyPassword)
		}
	case "none":
		// unprotected raw key
	default:
		return fail("key create: unknown --kdf %q (expected scrypt or none)", *kdfName)
	}

	if _, err := os.Stat(path); err == nil && !*force {
		return fail("key create: %s already exists (use --force to overwrite)", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fail("key create: %v", err)
	}

	key, err := crypto.NewRandomKey()
	if err != nil {
		return fail("key create: %v", err)
	}
	blob, err := crypto.EncodeKeyFile(key, passphrase, *hint, time.Now())
	if err != nil {
		return fail("key create: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fail("key create: %v", err)
	}
	// O_EXCL closes the TOCTOU gap with the Stat check above unless --force.
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if *force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fail("key create: %v", err)
	}
	if _, err := f.Write(blob); err != nil {
		_ = f.Close()
		return fail("key create: %v", err)
	}
	if err := f.Close(); err != nil {
		return fail("key create: %v", err)
	}

	encrypted := *kdfName == "scrypt"
	if *outputJSON {
		return emitJSON(map[string]any{
			"path":      path,
			"kdf":       *kdfName,
			"encrypted": encrypted,
		})
	}
	if encrypted {
		fmt.Printf("created passphrase-protected encryption key at %s\n", path)
		fmt.Println("keep the passphrase safe — without it this key, and every backup made with it, is unrecoverable.")
	} else {
		fmt.Printf("created unprotected encryption key at %s\n", path)
		fmt.Println("keep this file safe — without it, backups made with it cannot be restored.")
	}
	return 0
}

// cmdArchives lists the archives/files recorded in a snapshot's manifest — the
// data a GUI needs to populate an archive picker. `pbmac archives <type/id/time>`.
func cmdArchives(args []string) int {
	fs := flag.NewFlagSet("archives", flag.ContinueOnError)
	repoFlag := fs.String("repo", "", "repository spec (overrides env/config)")
	outputJSON := fs.Bool("json", false, "emit JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	jsonMode = *outputJSON
	if fs.NArg() != 1 {
		return fail("archives: expected a SNAPSHOT (e.g. host/mymac/1700000000)")
	}
	snap, err := parseSnapshot(fs.Arg(0))
	if err != nil {
		return fail("%v", err)
	}
	client, err := resolveClient(*repoFlag)
	if err != nil {
		return fail("%v", err)
	}
	m, err := restore.ManifestFor(context.Background(), client, snap)
	if err != nil {
		return fail("archives: %v", err)
	}
	if *outputJSON {
		files := m.Files
		if files == nil {
			files = []manifest.FileInfo{}
		}
		return emitJSON(files)
	}
	for _, f := range m.Files {
		fmt.Printf("%-24s %12d  %s\n", f.Filename, f.Size, f.CryptMode)
	}
	return 0
}
