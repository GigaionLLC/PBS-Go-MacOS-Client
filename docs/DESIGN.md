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
- Machine-readable (`--json`) output so a GUI can drive the CLI (see §6 and
  [`CLI-JSON.md`](CLI-JSON.md)).

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
internal/chunker     content-defined chunking (PBS-exact buzhash) + SHA-256 digests
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

Dedup (implemented): the client downloads the previous snapshot's index
(`GET /previous`, which also registers those chunks in the server's known-chunks
set) and skips re-uploading any chunk whose digest is already present, so repeat
backups only send changed data. The content-defined chunker is a byte-exact port
of PBS's buzhash (`pbs-datastore/src/chunker.rs`), so boundaries align with the
server's and unchanged regions re-sync after edits.

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
- **Keyfile format (implemented):** the PBS on-disk keyfile is both read and
  written — scrypt/PBKDF2 passphrase-wrapped, or a raw 32-byte / 64-hex key
  (`internal/crypto/keyfile.go`: `LoadKeyFile`/`EncodeKeyFile`). `pbmac key create`
  generates one (passphrase from `PBS_ENCRYPTION_PASSWORD`); `--keyfile` accepts
  any of these forms. Written keys carry `created`/`modified` so the official
  client reads them too.

## 6. GUI ↔ CLI contract

The GUI is a separate component. It never speaks the PBS protocol itself; it
shells out to `pbmac` and parses `--json`. The stable per-command JSON shapes
(and the `{"error": …}` envelope) are frozen in [`CLI-JSON.md`](CLI-JSON.md), so
the GUI can be built and iterated against fixtures on any OS. This keeps the trust
boundary and the protocol code in one place (the CLI).

**Stack: native SwiftUI (`macos/`).** "Native as Finder" is inherently macOS, so
the app is a Mac-only SwiftUI + AppKit build: Finder-style Miller-column browser,
`NSOpenPanel`, drag to/from Finder. It runs the **bundled** `pbmac` for every
operation (embedded in the app bundle, so it ships as one download), which makes
the GUI's command surface *identical* to the CLI's rather than a reimplementation.
An in-app **Console** runs arbitrary `pbmac` commands, each action shows its
equivalent command with copy, `pbmac://` deep links drive it from a script, and an
"Install Command-Line Tool" menu symlinks the bundled binary onto `PATH`. Screens:
Connection & Keys (`login` + `key create`), snapshot browser (`list`), archive
picker (`archives`), file restore picker (`restore --list`), restore run, backup
config+run. The design prototype (reviewable on any OS) lives in `gui/`.

## 7. macOS & APFS considerations

- **APFS snapshots are optional**, used only to get a *consistent*, permission-
  stable read of files mid-backup. They are **not** required for v1 directory
  backup.
- The low-level `fs_snapshot_create()` syscall needs the
  `com.apple.developer.vfs.snapshot` entitlement, granted by Apple only to
  vetted backup vendors — not available to us. So the **`SnapshotSource`
  (implemented, `backup --snapshot`)** shells out to Apple's entitled binaries:
  `tmutil localsnapshot` → `mount_apfs -o nobrowse,ro -s` → back up the subtree
  from the read-only snapshot mount → `tmutil deletelocalsnapshots` on close
  (`internal/source/snapshot*.go`). Needs `sudo` and — since CVE-2020-9771 — the
  reading process still needs Full Disk Access (a snapshot is not a TCC bypass).
  The create/mount/cleanup orchestration is unit-tested via an injected runner.
- **TCC / Full Disk Access:** reading broad user data (Desktop, Documents,
  Photos library) triggers macOS privacy prompts; the app needs Full Disk
  Access for wide backups. Document this in user setup.
- **pxar fidelity:** standard POSIX metadata (mode, mtime, symlinks) and
  **extended attributes are preserved** — captured on darwin via
  `listxattr`/`getxattr` and restored via `setxattr` (`internal/source` +
  `internal/restore`, build-tagged), carried as `PXAR_XATTR` items. Because
  macOS keeps Finder info, tags, quarantine, and even resource forks *as*
  `com.apple.*` xattrs, those ride along.
- **BSD file flags (`chflags`) — implemented** for the bits with a pxar
  equivalent, carried in the ENTRY `flags` field: `UF/SF_IMMUTABLE` → immutable
  (Finder "Locked"), `UF/SF_APPEND` → append, `UF_HIDDEN` → hidden, `UF_NODUMP` →
  nodump (`internal/source/flags_darwin.go` + `internal/restore/flags_darwin.go`,
  mapped against `pbs-client/src/pxar/flags.rs`). macOS↔macOS round-trips; the
  official Linux client honors immutable/append on restore. System-managed macOS
  flags (SIP/dataless/firmlink/compressed) have no equivalent and are dropped;
  symlinks are skipped (no `lchflags` on darwin).
- **ACLs — deferred (no cgo-free path).** macOS uses NFSv4-style ACLs, which
  cannot be honestly represented in pxar's POSIX.1e `PXAR_ACL_*` items (no deny
  ACEs, no GUIDs, rwx-only). The tempting cgo-free route — the
  `com.apple.system.Security` pseudo-xattr — does **not** work: `getxattr` of it
  returns `EPERM` (verified on CI), so the kernel doesn't expose the ACL to
  userspace that way. The real API (`acl_get_file`/`acl_set_file`, or `getattrlist
  ATTR_CMN_EXTENDED_SECURITY`) needs cgo or a hand-rolled libSystem trampoline,
  which conflicts with the pure-Go cross-compile invariant (§2). So ACL fidelity
  is deferred — a future opt-in cgo build could add it. Note the metadata users
  actually rely on (resource forks, Finder info, tags, quarantine) travels as
  ordinary `com.apple.*` xattrs and *is* preserved.

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
| M6 (v2) | `SnapshotSource` via tmutil ✓ (`backup --snapshot`); macOS metadata fidelity ✓ (xattrs, file flags; ACLs deferred — need cgo); GUI front-end: native SwiftUI app (`macos/`) that bundles pbmac ✓ | mixed |
| v0.1.0 (shipped) | First testable release: ad-hoc-signed `.app` (pbmac embedded) + standalone CLI, published by `release.yml`. Live round-trips + official-client interop confirmed; parity/ship audit fixes applied. See [`STATUS.md`](STATUS.md). | validated |

**Formats status:** all wire/on-disk formats are ported byte-for-byte from the
Proxmox source and unit-tested offline. The two things unit tests *cannot* prove
without a live server / real decoder are (a) the **pxar encoder** decoding
cleanly in the real `pxar`/PBS decoder (goodbye-table offsets, v2 version
handling — structurally self-checked here), and (b) the **encrypted keyed
digest** (`CryptConfig`) for dedup. Both are the natural first things to
exercise once a PBS target is available.

**Highest-risk items — validated live (PBS 4.2):** encryption wire-compat (§5)
and pxar encoder→decoder acceptance were the two biggest risks; both plain and
encrypted backup→restore round-trips are byte-perfect against a real server, and
the signed manifest is accepted (reported `sign-only`). **Interop is confirmed:**
the official Rust `proxmox-backup-client` restores a pbmac-made archive
byte-perfect (`.github/workflows/live-validate.yml` → `interop` job). The live
suite also covers single-file restore, incremental dedup (0 uploads on an
unchanged re-backup), xattr and BSD-file-flag fidelity, and APFS-snapshot backup.
