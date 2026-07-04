# Handoff — pbmac (Proxmox Backup client for macOS)

This document lets a fresh session (in a new clone, with GitHub CI/CD for macOS
build testing) pick up exactly where we left off. Read this, then
[`docs/DESIGN.md`](DESIGN.md) for the architecture and protocol details.

## 1. What this is

A native macOS **arm64** CLI (`pbmac`), written in **Go**, that backs up and
restores files to/from a **Proxmox Backup Server (PBS)** — parity with what
Linux gets from the official `proxmox-backup-client`. A GUI is a planned,
separate component that drives the CLI via its `--json` output.

**Language decision (settled):** Go, not Rust. We spiked reusing the official
Rust crates; they aren't consumable as published dependencies (`pathpatterns`
0.3 and `pxar` at the pinned version aren't on crates.io; the workspace needs a
sibling `proxmox` checkout and has no Cargo.lock) and are Linux-coupled
(`nix`, `proxmox-sys`). Reuse would mean maintaining a patched fork of two
workspaces. So we **port the (small, stable) wire/on-disk formats to Go**,
faithfully, with source citations. Every format below was read from the actual
Proxmox source, not the prose docs. (If the goal ever shifts to upstreaming
macOS support into Proxmox's own client, that's the one scenario that flips the
decision back to Rust.)

## 2. Current status — functionally complete for backup & restore

Repo: **`github.com/GigaionLLC/PBS-Go-MacOS-Client`**, branch **`main`** — the
handoff code was imported here and now has macOS CI/CD (see §8). Earlier work
happened on `claude/proxmox-backup-macos-client-pj5060` in the origin clone.

**Done and unit-tested (all offline):**
- Repository spec parsing, config, CLI (`ping`, `list`, `backup`, `restore`,
  `login`, `version`).
- Content-defined **chunker** (buzhash + SHA-256).
- **DataBlob** codec — 4 variants, AES-256-GCM (16-byte IV), zstd, CRC32.
- **Dynamic index** (`.didx`) writer + reader + index checksum.
- **Manifest** (`index.json.blob`) marshal + parse.
- **pxar v2 encoder AND decoder** — proven consistent by an encode→decode
  round-trip test (files byte-for-byte, incl. empty/binary/nested/symlinks +
  metadata). SipHash-2-4 verified against its reference vector.
- **CryptConfig keyed digest** — `SHA256(data‖id_key)`,
  `id_key = PBKDF2(enc_key,"_id_key",10,32)`; fingerprint constant cross-checked
  against `sha256("Proxmox Backup Encryption Key Fingerprint")`.
- **Protocol** — fingerprint-pinned TLS, API-token auth, HTTP/2-upgrade writer
  and reader sessions (endpoints ported from source).
- **Full backup pipeline** wired: pxar→chunk→DataBlob→index→upload→manifest→
  finish (`backup`, with `--dry-run` for offline).
- **Full restore** wired: reader→index→fetch→decrypt→pxar-decode→extract; list
  + whole-archive + single-file (`restore`).
- **End-to-end encrypted backup→restore round-trip test** (in-memory, no server)
  — the strongest offline proof.
- **Polish:** PBS scrypt/PBKDF2 **keyfiles**, `.pxarexclude`/`--exclude`,
  macOS **Keychain** token storage, `--json` on data commands.

Run the suite: `go test ./...` (10 packages with tests, all passing).
Cross-compile: `GOOS=darwin GOARCH=arm64 go build -o pbmac ./cmd/pbmac`.

## 3. DONE since this handoff — manifest HMAC signing

**Manifest HMAC signing** for encrypted backups is now implemented and passes
the PBS gold test vector (`internal/manifest/signing.go` +
`internal/manifest/signing_test.go`, reproducing
`d7b446fb…35679e9`). The design/algorithm notes below are retained as the
implementation record.

- Algorithm (from `pbs-datastore/src/manifest.rs`):
  `signature = HMAC-SHA256(id_key, canonical_json(manifest))` where the manifest
  has its `unprotected` and `signature` fields removed before canonicalizing.
  Store as hex in `signature`; put `hex(fingerprint)` in
  `unprotected["key-fingerprint"]`.
- **Canonical JSON** = compact, **object keys sorted recursively**, standard
  JSON string escaping but **no HTML escaping**, numbers in natural form. In Go:
  marshal → decode into `map[string]any` with `json.Decoder.UseNumber()` →
  `delete` the two fields → re-encode with `json.Encoder` + `SetEscapeHTML(false)`
  (the encoder sorts map keys and preserves `json.Number`); trim trailing `\n`.
- **Gold vector** (`test_manifest_signature` in `pbs-datastore/src/manifest.rs`):
  key = `scrypt(pw="test", salt=empty, n=65536, r=8, p=1)`; manifest
  `host/elsa/2020-06-26T13:56:05Z`; files
  `("test1.img.fidx", 200, [1u8;32], Encrypt)` and
  `("abc.blob", 200, [2u8;32], None)`; `unprotected["note"]="This is not
  protected by the signature."`. **Expected signature:**
  `d7b446fb7db081662081d4b40fedd858a1d6307a5aff4ecff7d5bf4fd35679e9`.
  Reproducing this hex in Go proves canonical-JSON + CryptConfig are
  byte-compatible with PBS.
- ✓ `manifest.Signer` interface (`AuthTag([]byte) [32]byte`, `Fingerprint()
  [32]byte`) added so `manifest` doesn't import `crypto`; `crypto.CryptConfig`
  satisfies it. `(*Manifest).Sign`, `JSONSigned`, and `Verify` implemented.
- ✓ `internal/backup/upload.go` now calls `m.JSONSigned(opts.Crypt)` when
  `opts.Crypt != nil` (which also sets `unprotected["key-fingerprint"]`);
  unencrypted backups still use `m.JSON()`.

## 4. Then — the remaining work, in priority order

1. **Live PBS validation** (the big one; needs a server). Ladder:
   `pbmac ping` → `pbmac list` → `pbmac backup --dry-run` → live `pbmac backup`
   → `pbmac restore`. Fix whatever the real server / real `pxar` decoder
   rejects. The inferred-but-unvalidated spots are: pxar decoder acceptance,
   and the writer endpoint params (high-confidence, ported from source).
   *Fixed during import (verified against `src/api2/backup/environment.rs`):* the
   `PUT /dynamic_index` `offset-list` must carry each chunk's **start** offset
   (0 for the first), not its end — the server validates the offset against its
   running stream position before adding the chunk's size. This was an
   upload-blocking bug; exercise it first against a live server.
2. **GUI** — separate component; the CLI's `--json` output is the contract.
3. Optional: chunker tuning (dedup alignment is weak at current params — a
   correctness-neutral quality issue; see the round-trip test's dedup log).

## 5. Architecture / package map

```
cmd/pbmac              entry point
internal/cli           subcommands, flag parsing, JSON output
internal/repo          repository spec parsing
internal/config        config file + env resolution
internal/keychain      macOS Keychain via `security` (build-tagged)
internal/source        LiveDirectoryFS (pxar.Filesystem over live FS; darwin/linux mtime split)
internal/pxar          pxar v2 encoder + decoder + format constants + SipHash
internal/chunker       content-defined chunking
internal/datablob      DataBlob chunk/blob framing (magics, crypto, zstd, CRC)
internal/crypto        AES-256-GCM primitive, CryptConfig (keyed digest), keyfile loader
internal/index         dynamic index (.didx) writer + parser
internal/manifest      index.json manifest marshal/parse (signing goes here next)
internal/exclude       .pxarexclude / --exclude matcher
internal/protocol      TLS pinning, auth, HTTP/2 writer + reader sessions
internal/backup        pipeline (Run) + live upload (Upload)
internal/restore       reassembly + extract/list visitors + ChunkStore interface
```

## 6. Source-of-truth references (how we verify formats)

The official source is the ground truth. Two repos:
- **`github.com/proxmox/proxmox-backup`** — clones fine here. Key files we used:
  `pbs-datastore/src/{data_blob,dynamic_index,manifest}.rs`,
  `pbs-datastore/src/file_formats.rs`, `pbs-tools/src/crypt_config.rs`,
  `pbs-key-config/src/lib.rs`, `src/api2/{backup,reader}/mod.rs`,
  `src/api2/backup/upload_chunk.rs`, `pbs-client/src/backup_writer.rs`.
- **`github.com/proxmox/pxar`** (old standalone) — the pxar format:
  `src/format/mod.rs`, `src/encoder/mod.rs`. (The monorepo `proxmox/proxmox`
  clone was blocked by the proxy here; the standalone pxar repo's raw files
  fetched fine.)

In the previous environment these were cloned under `/tmp/claude-0/`
(`proxmox-backup`, and a pxar spike). Those are throwaway; re-clone/`grep` as
needed. **Rule:** when adding/adjusting any endpoint or format, mirror the
corresponding Rust `#[api(...)]` handler or struct exactly, and cite the file
in the code comment (we already do this throughout).

## 7. Dependencies (all pure-Go, cross-compile cleanly)

`golang.org/x/net` (http2 upgrade), `github.com/klauspost/compress` (zstd),
`golang.org/x/crypto` (scrypt). Toolchain bumped to **Go 1.25** (x/net needed
≥1.25; we also use stdlib `crypto/pbkdf2`, new in 1.24).

## 8. CI/CD for macOS build testing — implemented

Two GitHub Actions workflows now exist:
- `.github/workflows/ci.yml` — on push/PR: a **macos-14** (Apple Silicon) job
  runs `go vet`, `go build ./...`, `go test -race ./...`, builds the native
  `pbmac`, and smoke-tests `pbmac version` + offline `backup --dry-run` (plain
  and `--encrypt`); a **ubuntu-latest** job cross-compiles `GOOS=darwin
  GOARCH=arm64` as a fast build-tag regression check.
- `.github/workflows/release.yml` — on a `v*` tag (or manual dispatch): builds a
  version-stamped `pbmac-darwin-arm64` + SHA-256, uploads it as an artifact, and
  (on a tag) publishes a GitHub Release. NOTE: the binary is **not yet code-
  signed/notarized**, so macOS Gatekeeper quarantines it — `xattr -d
  com.apple.quarantine pbmac-darwin-arm64` to run a test build; add signing/
  notarization before any real distribution.

The original guidance below still describes what the mac runner exercises:

- **Runner:** `runs-on: macos-14` (Apple Silicon, arm64 — our real target).
- **Steps:** `actions/setup-go` (go 1.25) → `go build ./...` →
  `go vet ./...` → `go test ./...` → `go build -o pbmac ./cmd/pbmac` (native
  arm64 on the runner) → smoke test `./pbmac version` and
  `./pbmac backup --dry-run repo.pxar:$PWD`.
- **Also cross-compile check** on a `ubuntu-latest` job:
  `GOOS=darwin GOARCH=arm64 go build ./cmd/pbmac` (fast, catches build-tag
  regressions cheaply).
- **macOS-specific things that only run on the mac runner:**
  - `internal/keychain` uses the `security` CLI — its darwin path only compiles/
    runs on macOS. A keychain test would need a temporary keychain
    (`security create-keychain`), so keep it opt-in / skipped by default.
  - `internal/source` mtime uses `syscall.Stat_t.Mtimespec` on darwin — the
    macOS runner is where this path is actually exercised. Add a test that backs
    up a temp dir and restores it, asserting file bytes + mode + mtime survive
    (the encode→decode round-trip already covers the format; this covers the
    real darwin stat mapping).
  - Consider a `lipo`/universal-binary step later if Intel support is ever
    wanted (currently arm64-only by decision).
- **Do NOT** run `playwright install` or fetch browsers; not relevant here.
- No secrets are needed for build/test. Live PBS validation (needs a server +
  token) should be a separate, manually-triggered job, not part of CI.

## 9. Conventions / gotchas

- Commit style: descriptive body; end with the Co-Authored-By / Claude-Session
  trailers (see existing commits). Never put the model identifier in commits.
- Don't open a PR unless asked.
- `--dry-run` is the offline proof path; keep it working without a server.
- Encrypted vs plain digest: encrypted uses `CryptConfig.ComputeDigest`
  (keyed), plain uses SHA-256 — the pipeline already branches on `opts.Crypt`.
- The manifest blob is uploaded **unencrypted** (it's signed, not encrypted).
- pxar paths are `/`-rooted virtual paths (root = `""` in the decoder, `/` in
  the encoder call); `LiveDirectoryFS` maps them to real paths.
