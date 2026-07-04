# pbmac — Proxmox Backup for macOS

A native macOS (Apple Silicon / `arm64`) client for [Proxmox Backup
Server](https://www.proxmox.com/en/proxmox-backup-server) (PBS): **back up and
restore files** to/from a PBS datastore with client-side encryption, over the
reverse-engineered PBS wire protocol. Ships as a **CLI** (`pbmac`, written in Go)
and a **native SwiftUI app** that bundles the CLI inside it.

> **Status — v0.1.0, first testable release.** Backup/restore is usable and
> **validated live against PBS 4.2**: plain + encrypted + incremental round-trips
> are byte-perfect, and the **official `proxmox-backup-client` restores a
> pbmac-made archive byte-perfect** (interop confirmed on CI). Deferred: Intel
> Macs, NFSv4 ACLs (needs cgo), namespaces. See [`docs/STATUS.md`](docs/STATUS.md)
> for exactly what's done / validated / deferred, and
> [`docs/DESIGN.md`](docs/DESIGN.md) for architecture + protocol notes.

## Install (the app)

Grab the latest [**release**](https://github.com/GigaionLLC/PBS-Go-MacOS-Client/releases):
download `ProxmoxBackup-<ver>-macos-arm64.zip`, unzip, and move **Proxmox
Backup.app** to /Applications. `pbmac` is bundled inside — nothing else to install.

The app is **ad-hoc signed, not notarized**, so on first launch **right-click the
app → Open** (or `xattr -dr com.apple.quarantine "Proxmox Backup.app"`). Requires
Apple Silicon + macOS 14+. Grant **Full Disk Access** to back up broad user data
(Desktop/Documents/Photos).

In the app: **Connection & Keys** stores your repo/token/key (`pbmac login` +
`pbmac key create`); browse snapshots and restore via a Finder-style column
browser (or drag files out to Finder); back up a folder (drag it in). An in-app
**Console** (⌘K) runs any `pbmac` command against the same bundled binary, and
**Install Command-Line Tool** symlinks that binary onto your `PATH`.

## Use the CLI

```sh
# Save repository, pinned fingerprint, and API token (token -> macOS Keychain).
pbmac login --repo pbs.example.com:8007:store \
  --fingerprint AA:BB:.. --token 'user@pbs!tok:secret'

pbmac ping                 # connectivity/auth check
pbmac list                 # snapshots  (alias: snapshots)
pbmac archives host/mymac/1700000000          # files in a snapshot's manifest

# Generate a client-side encryption key (scrypt-wrapped if a passphrase is set).
export PBS_ENCRYPTION_PASSWORD=…
pbmac key create --keyfile key.json

# Back up a directory as an archive  (NAME.pxar:/abs/path). --dry-run works offline.
pbmac backup --keyfile key.json --exclude '*.log' --exclude 'node_modules/' \
  home.pxar:$HOME/Documents

# Restore: list contents, then a single file or the whole archive.
pbmac restore --list host/mymac/1700000000 home.pxar
pbmac restore --file /report.pdf --target ./out host/mymac/1700000000 home.pxar
pbmac restore --keyfile key.json --target ./out host/mymac/1700000000 home.pxar
```

Every data command accepts `--json` (stable contract in
[`docs/CLI-JSON.md`](docs/CLI-JSON.md)); flags may appear before or after
positional arguments. `PBS_REPOSITORY`, `PBS_API_TOKEN`, `PBS_FINGERPRINT`, and
`PBS_ENCRYPTION_PASSWORD` are read from the environment (flag > env > stored
config). For local testing, copy [`.env.example`](.env.example) to `.env`
(gitignored — **real creds never get committed**) and `set -a; source ./.env; set +a`.

## Build from source

```sh
# CLI (host build, or cross-compile the release target from any OS):
go build ./cmd/pbmac
GOOS=darwin GOARCH=arm64 go build -o pbmac ./cmd/pbmac

# The app (needs a Mac + Xcode + Go + xcodegen). Produces an ad-hoc-signed .app
# with pbmac embedded, in macos/dist/:
brew install xcodegen
bash macos/build-app.sh
```

See [`macos/README.md`](macos/README.md) (app internals) and
[`gui/README.md`](gui/README.md) (the design prototype).

## Layout

```
cmd/pbmac            CLI entry point
internal/            protocol, chunker, pxar, crypto, manifest, backup, restore,
                     source (darwin metadata: xattrs, chflags), cli, config, keychain
macos/               native SwiftUI app (bundles pbmac); build-app.sh, project.yml
gui/                 interactive HTML design prototype (reviewable on any OS)
docs/                DESIGN.md, CLI-JSON.md, STATUS.md
.github/workflows/   ci.yml (build+test+app compile), release.yml (tag -> Release),
                     live-validate.yml (manual: real PBS backup/restore + interop)
```

## Test & release

- **CI** (`ci.yml`, every push): `go vet` + `go test -race` on macOS arm64,
  darwin/arm64 cross-compile check, and a full macOS **app build** that embeds
  `pbmac` and runs it from inside the bundle.
- **Live validation** (`live-validate.yml`, manual `workflow_dispatch`): real
  backup/restore/encrypted/incremental round-trips, xattr + file-flag fidelity,
  APFS-snapshot backup, and official-client **interop**, against a PBS reachable
  over the WAN. Needs `PBS_REPOSITORY` / `PBS_API_TOKEN` / `PBS_FINGERPRINT` repo
  secrets.
- **Release**: push a `vX.Y.Z` tag (`release.yml`) → builds the ad-hoc-signed
  `.app` + standalone CLI + `SHA256SUMS.txt` and publishes a GitHub Release.

## License

See [`LICENSE`](LICENSE).
