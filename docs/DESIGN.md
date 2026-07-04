# pbmac — Design & Protocol Notes

This document captures the architecture, the reverse-engineered Proxmox Backup
Server (PBS) protocol, the encryption scheme, the macOS-specific concerns, and
the roadmap. It is the source of truth for *why* the code is shaped the way it
is.

## 1. Goals & non-goals

**Goals (v1, CLI):**
- Native macOS `arm64` binary. No Docker, no daemon, no kernel extension.
- Back up chosen directories to a PBS datastore as a `pxar` archive with
  content-defined dedup.
- Restore: list snapshots, list archive contents, restore a whole archive or a
  single file — without mounting anything (no macFUSE).
- Client-side **AES-256-GCM** encryption, wire-compatible with PBS so backups
  restore with official tooling too.
- Machine-readable (`--output json`) output so a future GUI can drive the CLI.

**Non-goals (for now):**
- Intel (`amd64`) macOS — arm64 only for v1.
- Whole-volume / bare-metal restore.
- A long-running agent or scheduler (leave scheduling to `launchd`).
- The GUI itself — it is a separate component that shells out to `pbmac`.

## 2. Why Go (and not the official Rust client)

- The official `proxmox-backup-client` is Rust, officially Linux-only, and
  painful to cross-compile to macOS (Apple SDK toolchain licensing + Linux
  syscalls in its pxar/xattr/ACL paths).
- Go cross-compiles to `darwin/arm64` with `GOOS`/`GOARCH` and no C toolchain
  for pure-Go packages — the single biggest lever for a native macOS binary.
- The hard, portable parts (protocol, chunking, pxar) already exist in prior
  art ([tizbac/proxmoxbackupclient_go](https://github.com/tizbac/proxmoxbackupclient_go)),
  which is plain Go. We reuse the *shape* of that work and add what it lacks:
  encryption, a macOS-friendly restore path, and a clean source abstraction.

## 3. Architecture

```
cmd/pbmac            CLI entry point / argument dispatch
internal/cli         subcommand handlers (backup, restore, list, login, version)
internal/repo        PBS repository spec parsing ([[user@]host[:port]:]datastore)
internal/config      on-disk config + credential handling (Keychain later)
internal/source      Source abstraction: where bytes to back up come from
                       - LiveDirectorySource   (v1: read files live)
                       - SnapshotSource        (v2: tmutil-backed APFS snapshot)
internal/chunker     content-defined chunking (rolling hash) + SHA-256 digests
internal/pxar        pxar archive encode (backup) / decode (restore)   [stub]
internal/crypto      AES-256-GCM chunk encryption + PBS keyfile format
internal/protocol    PBS HTTP/2 client: writer (upload) + reader (download) [stub]
internal/backup      orchestration: source -> pxar -> chunk -> encrypt -> upload
internal/restore     orchestration: download index+chunks -> decrypt -> pxar -> fs
```

The **Source** abstraction is deliberately front-and-center so the APFS
snapshot decision never blocks the backup pipeline (see §7).

## 4. PBS wire protocol (grounded in the official source)

**Maintenance principle:** we track the *actual Proxmox source*, not just the
prose docs, so the client stays easy to update as PBS gains features. The
endpoint paths, HTTP methods, and query-parameter names below are taken from:

- `src/api2/backup/mod.rs` — the backup (writer) router.
- `src/api2/backup/upload_chunk.rs` — chunk/blob upload parameters.
- `src/api2/reader/mod.rs` — the reader (restore) router.
- `pbs-client/src/backup_writer.rs` — the official client's operation ordering.

When implementing a new endpoint, mirror the corresponding Rust
`#[api(...)]`-annotated handler so parameter names/types match exactly.

Reference docs: <https://pbs.proxmox.com/docs/backup-protocol.html>. All API
calls go to port **8007** over TLS. Auth is an **API token**
(`Authorization: PBSAPIToken=USER@REALM!TOKENID:SECRET`) or a ticket; the server
cert is pinned by **SHA-256 fingerprint** (self-signed is the norm).

**Confirmed against source** — the session upgrades require a **`store`** query
parameter (the datastore) and accept an optional **`ns`** (namespace). Sub-route
params: `dynamic_index` POST `archive-name`; `dynamic_chunk` POST
`wid`/`digest`/`size`/`encoded-size`; `dynamic_index` PUT `wid`/`digest-list`/
`offset-list`; `dynamic_close` POST `wid`/`chunk-count`/`size`/`csum`; `blob`
POST `file-name`/`encoded-size`; `finish` POST (no params). Reader: `download`
GET `file-name`; `chunk` GET `digest`.

### 4.1 Backup (writer) session

1. `GET /api2/json/backup` — upgrade to HTTP/2 with protocol
   `proxmox-backup-protocol-v1`; server replies `101 Switching Protocols`.
2. Over the upgraded connection, issue REST calls:
   - `POST /dynamic_index` (file archives) → returns a writer id (`wid`).
   - `POST /dynamic_chunk` — upload each unique chunk (digest + data).
   - `PUT /dynamic_index` — append chunk digests+offsets to the index.
   - `POST /dynamic_close` — finalize the index.
   - (`fixed_*` equivalents exist for block images; not needed for file backup.)
3. Upload the `index.json.blob` / manifest and `PUT /finish`.

Dedup: before uploading a chunk, the client may check whether the server
already has that digest (previous-snapshot "known chunks") and skip re-upload.

### 4.2 Restore (reader) session

1. `GET /api2/json/reader` — upgrade to HTTP/2 with protocol
   `proxmox-backup-reader-protocol-v1`; server replies `101`.
2. Over the connection:
   - `GET /download?file-name=index.json.blob` — the snapshot manifest.
   - `GET /download?file-name=<archive>.didx` — the dynamic index (chunk list).
   - `GET /chunk?digest=<hex>` — fetch each needed chunk.
3. Reconstruct the `pxar` byte stream from the index + chunks, decrypt if
   needed, then extract.

**macOS restore strategy:** catalog-driven. Download the `.pxar` catalog to
*list* contents cheaply; for a single-file restore, resolve only the chunks
covering that file's byte range and fetch just those. No NBD, no FUSE. This is
also what the GUI drives: `restore --list` to browse, `restore --file` to pull.

## 5. Encryption (must match PBS to stay wire-compatible)

- **Cipher:** AES-256-GCM per chunk; random 96-bit nonce; GCM tag stored with
  the chunk. PBS wraps this in a `DataBlob` with a small magic/header.
- **Key:** a raw 32-byte key, or a passphrase-derived key via **scrypt**
  (matching PBS's `keyfile` JSON format), optionally RSA-wrapped as a master
  key for recovery.
- **Keyed digest (implemented):** for *encrypted* backups the chunk digest is
  `SHA256(data ‖ id_key)`, where `id_key = PBKDF2-HMAC-SHA256(enc_key, "_id_key",
  10, 32)` — ported exactly from `pbs-tools/src/crypt_config.rs` as
  `crypto.CryptConfig`. This namespaces dedup to a key. The pipeline uses this
  digest for the index, dedup, and chunk naming whenever a key is set. The key
  **fingerprint** (`compute_digest(FINGERPRINT_INPUT)`) is recorded in the
  manifest's `unprotected["key-fingerprint"]`.
- **Manifest signature (implemented):** for encrypted backups PBS signs the
  manifest with `HMAC-SHA256(id_key, canonical_json)` (`CryptConfig.AuthTag`),
  over the canonical JSON with the `unprotected`/`signature` fields stripped.
  Implemented in `internal/manifest/signing.go` (`Sign`/`JSONSigned`/`Verify`)
  and validated byte-for-byte against PBS's gold test vector.
- **Keyfile format (follow-up):** the on-disk passphrase-protected keyfile
  (scrypt-wrapped) is not yet parsed; keys are supplied raw via `--keyfile`.

## 6. GUI ↔ CLI contract

The GUI is a separate component. It never speaks the PBS protocol itself; it
shells out to `pbmac` and parses `--output json`. Every subcommand that returns
data (snapshot lists, archive listings, progress) must have a stable JSON shape.
This keeps the trust boundary and the protocol code in one place (the CLI) and
lets the GUI be any tech (SwiftUI, Tauri, Electron).

## 7. macOS & APFS considerations

- **APFS snapshots are optional**, used only to get a *consistent*, permission-
  stable read of files mid-backup. They are **not** required for v1 directory
  backup.
- The low-level `fs_snapshot_create()` syscall needs the
  `com.apple.developer.vfs.snapshot` entitlement, granted by Apple only to
  vetted backup vendors after review — **not** available to us today. So the
  `SnapshotSource` (v2) shells out to `tmutil localsnapshot` +
  `mount_apfs -s` (Apple's own entitled binaries) instead.
- **TCC / Full Disk Access:** reading broad user data (Desktop, Documents,
  Photos library) triggers macOS privacy prompts; the app needs Full Disk
  Access for wide backups. Document this in user setup.
- **pxar fidelity:** macOS file metadata (resource forks, Finder flags,
  `com.apple.*` xattrs) differs from Linux. v1 preserves standard POSIX
  metadata + xattrs; exotic Finder metadata is a documented known-gap.

## 8. Roadmap

| Milestone | Deliverable | Server needed? |
|-----------|-------------|----------------|
| M0 (done) | Scaffold: CLI surface, repo parsing, chunker, AES-256-GCM primitive, dir source, `backup --dry-run` | no |
| M0.5 (done) | Formats ported from source & unit-tested: protocol client (`ping`/`list`, HTTP/2 writer+reader), **DataBlob** codec, **dynamic index** (.didx), **manifest** (index.json), **pxar v2 encoder** | no |
| M1 (done, code) | Backup wired end-to-end: pxar → chunk → DataBlob → `dynamic_index` create → chunk upload → append → `dynamic_close` → manifest → `finish` | validate |
| M2 (done, code) | Full directory backup pipeline (`backup`), verified offline via `--dry-run`; live path fails cleanly without a server | validate |
| M4 (done, code) | Restore in `pbmac`: reader protocol, index parse, chunk reassembly, **pxar decoder**, list + whole-archive + single-file. pxar encoder↔decoder proven consistent by a round-trip test | validate |
| M3 (done, code) | Encrypted dedup: `CryptConfig` keyed digest ✓, scrypt/PBKDF2 keyfile ✓, manifest HMAC signature ✓ (gold-vector matched) | validate |
| M5 (done) | Keychain credential storage ✓, `.pxarexclude`/`--exclude` ✓, `--json` on data commands ✓ | no |
| M6 (v2) | `SnapshotSource` via tmutil; GUI front-end | mixed |

**Formats status:** all wire/on-disk formats are ported byte-for-byte from the
Proxmox source and unit-tested offline. The two things unit tests *cannot* prove
without a live server / real decoder are (a) the **pxar encoder** decoding
cleanly in the real `pxar`/PBS decoder (goodbye-table offsets, v2 version
handling — structurally self-checked here), and (b) the **encrypted keyed
digest** (`CryptConfig`) for dedup. Both are the natural first things to
exercise once a PBS target is available.

**Highest-risk item:** encryption wire-compat (§5) and pxar decoder acceptance.
Budget real time validating against a live PBS, not just unit tests.
