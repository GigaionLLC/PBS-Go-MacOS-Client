# pbmac — Proxmox Backup client for macOS

A native macOS (Apple Silicon / `arm64`) client for [Proxmox Backup
Server](https://www.proxmox.com/en/proxmox-backup-server) (PBS), written in Go.
The goal is parity with what Linux gets from the official
`proxmox-backup-client`: **back up and restore files** to/from a PBS datastore,
with client-side encryption, over the reverse-engineered PBS wire protocol.

> **Status: functionally complete for backup & restore, pending live-server
> validation.** Every wire/on-disk format is ported byte-for-byte from the
> Proxmox source and unit-tested, including a full encrypted backup→restore
> round-trip proven offline and the manifest HMAC signature matched against
> PBS's gold test vector. What remains is validating uploads against a real PBS
> instance. See [`docs/DESIGN.md`](docs/DESIGN.md).

## Features

- **Backup** a directory tree to a PBS datastore: pxar v2 archive → content-
  defined chunking → `DataBlob` framing → dynamic index → manifest.
- **Restore**: list snapshots, list archive contents, restore a whole archive
  or a single file — no NBD, no macFUSE.
- **Client-side encryption**: AES-256-GCM with PBS's keyed chunk digest
  (`CryptConfig`), and PBS `scrypt`/`PBKDF2` keyfiles.
- **Excludes**: `.pxarexclude` files and repeatable `--exclude` globs.
- **Credentials**: fingerprint-pinned TLS, API-token auth, macOS Keychain
  storage.
- **Scriptable**: `--json` output on data commands so a GUI can drive the CLI.

## Components

1. **CLI client (`pbmac`)** — the primary deliverable. Native `arm64` binary,
   no daemon, no kernel extension. Emits machine-readable JSON so a GUI can
   drive it.
2. **GUI (future, separate)** — a point-and-click app for browsing snapshots
   and restoring files, implemented as a thin front-end that shells out to
   `pbmac` and consumes its JSON output.

## Building

```sh
# Native (host) build for local development
go build ./cmd/pbmac

# Cross-compile the release target from any host
GOOS=darwin GOARCH=arm64 go build -o pbmac ./cmd/pbmac
```

CI (`.github/workflows/ci.yml`) builds and runs `go test -race ./...` on a
`macos-14` (Apple Silicon) runner and cross-compiles the darwin/arm64 target on
Linux. Tagged `v*` pushes produce a `pbmac-darwin-arm64` release build
(`.github/workflows/release.yml`).

## Usage

```sh
# Save the repository, pinned fingerprint, and API token (Keychain on macOS).
pbmac login --repo pbs.example.com:store \
  --fingerprint AA:BB:.. --token 'user@pbs!tok:secret'

# Check connectivity/auth, then list snapshots.
pbmac ping
pbmac list

# Dry-run a backup (offline): pxar-encode, chunk, report dedup + index csum.
pbmac backup --dry-run home.pxar:$HOME/Documents

# Real backup, encrypted, excluding caches.
export PBS_ENCRYPTION_PASSWORD=…            # for a passphrase-protected keyfile
pbmac backup --keyfile key.json --exclude '*.log' --exclude 'node_modules/' \
  home.pxar:$HOME/Documents

# Restore: list contents, then restore one file or the whole archive.
pbmac restore host/mymac/1700000000 home.pxar --list
pbmac restore host/mymac/1700000000 home.pxar --file /report.pdf --target ./out
pbmac restore host/mymac/1700000000 home.pxar --keyfile key.json --target ./out
```

`--json` is available on `ping`, `list`, `backup`, and `restore --list` for
scripting and for a GUI front-end.

## Design & protocol notes

See [`docs/DESIGN.md`](docs/DESIGN.md) for the architecture, the PBS protocol
reverse-engineering notes, the restore design, the encryption scheme, the
macOS/APFS considerations, and the roadmap.

## License

See [`LICENSE`](LICENSE).
