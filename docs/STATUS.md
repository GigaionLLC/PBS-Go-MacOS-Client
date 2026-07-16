# Project status & handoff notes

A living snapshot of where **pbmac** stands, the decisions behind it, and what's
left — so anyone (or a fresh session) can pick up without re-deriving context.
Deeper detail lives in [`DESIGN.md`](DESIGN.md) (architecture/protocol) and
[`CLI-JSON.md`](CLI-JSON.md) (the GUI↔CLI contract).

_Last updated: 2026-07-15 (post-v0.1.0 design review)._

## What this is

A native macOS (Apple Silicon) Proxmox Backup Server client: a Go **CLI**
(`pbmac`) plus a **SwiftUI app** that bundles the CLI. Backs up directories as
pxar archives with content-defined dedup and client-side AES-256-GCM encryption;
restores whole archives or single files. No daemon, no FUSE, no kernel extension.

## Done & validated

Core backup/restore is usable and **validated live against PBS 4.2** (see
`.github/workflows/live-validate.yml`, run via manual `workflow_dispatch`):

- Plain + encrypted + **incremental** backup→restore round-trips are byte-perfect.
- Single-file restore; `restore --list` browse.
- **Interop confirmed**: the official Rust `proxmox-backup-client` restores a
  pbmac-made archive byte-perfect. This is the key parity result.
- macOS xattrs (`com.apple.*`: quarantine, Finder info, tags, resource forks) and
  BSD file flags (`chflags`: immutable/Locked, hidden, append, nodump) preserved.
- APFS-snapshot backup (`backup --snapshot`, needs sudo + Full Disk Access).
- Wire/on-disk formats (buzhash chunker, DataBlob, dynamic index, manifest +
  HMAC signature, pxar v2) are byte-ported from Proxmox source and unit-tested;
  manifest signature matches PBS's gold vector.

The **GUI** (SwiftUI, `macos/`) compiles on Xcode 16 CI and its bundled `pbmac`
runs from inside the app bundle (verified in CI). It drives the CLI for browse /
restore / backup / connect, has an in-app Console, `pbmac://` deep links, and an
Install-Command-Line-Tool action.

## Key decisions (and why)

- **Go, pure / cgo-free, cross-compiles to darwin/arm64.** Hard invariant — it's
  the whole reason a native macOS binary is cheap. Anything needing cgo (ACLs) is
  deferred rather than breaking this.
- **Apple Silicon only (arm64).** v1 scope; Intel is a lipo/GOARCH follow-up.
- **GUI is native SwiftUI, not Tauri/Electron.** "Native as Finder" is macOS.
  The app **runs the bundled `pbmac`** for everything, so the GUI's command
  surface *is* the CLI's (not a reimplementation). The `gui/` HTML is only the
  reviewable design prototype.
- **`pbmac` is embedded in the .app** (an Xcode build phase compiles it into
  `Contents/Resources/pbmac`). One download; the terminal can share the same
  binary via Install-Command-Line-Tool.
- **Ad-hoc signed, not notarized.** Runs on Apple Silicon with right-click→Open;
  no Apple Developer account required. No App Sandbox / hardened runtime, so the
  app can exec the bundled CLI and reach the network without special entitlements.
- **No FUSE or macFUSE dependency.** pbmac will not require a third-party
  filesystem runtime, kernel extension, Recovery-mode security changes, or a
  separate filesystem installation. The official client's FUSE `mount` command
  is an intentional macOS platform exception, not a parity target for v1.
- **Credentials never in git.** Repo + fingerprint → `pbmac login` config
  (`~/Library/Application Support/pbmac/config.json`); API token → macOS Keychain;
  or `PBS_*` env / a gitignored `.env` (see `.env.example`). Test-server creds are
  local-only and must not be committed.

## Deferred (documented non-goals for v1)

- **NFSv4 ACLs** — can't be honestly represented in pxar's POSIX.1e ACL items and
  the real API needs cgo (conflicts with the cgo-free invariant). The metadata
  users rely on rides along as `com.apple.*` xattrs anyway. See DESIGN §7.
- **Finder-integrated, on-demand access** — not planned for the current release.
  The preferred native option, if pursued later, is a read-only Apple File
  Provider extension with metadata-only placeholders and per-file materialization.
  It would build on catalog-backed browsing and true random-access restore; it
  would not be a POSIX mount. Alternatives and prerequisites are recorded in
  DESIGN §6.1.
- **Intel (amd64) macOS**, whole-volume/bare-metal restore, a scheduler/daemon
  (leave to `launchd`), and PBS **namespaces** (`Client.Namespace` exists in the
  protocol layer but no CLI flag wires it yet).

## Known follow-ups / watch-list

Fixed in v0.1.0 after a parity/ship audit: restore path-traversal hardening,
batched dynamic-index append (large backups), manifest csum-panic guard, and
several GUI robustness fixes. Still open / worth attention:

- **Large-backup append is logic-reviewed but not *live*-tested.** The index is
  now sent in 1024-entry batches (`internal/backup/upload.go`); only single-batch
  (<1024 chunks, i.e. small backups) is exercised by live CI. Do a multi-GB
  backup on real hardware to exercise the multi-PUT path.
- **GUI runtime bits are compile-verified, not run in CI.** Sanity-check on a Mac:
  drag-out-to-Finder (file promises, `DragOut.swift`), the `pbmac://` URL scheme,
  and the Install-CLI symlink's admin fallback. The Restore button + Console are
  the guaranteed paths.
- **CLI usage/flag errors under `--json`** print plain-text flag usage, not the
  `{"error":…}` envelope (exit code is still non-zero). Low impact — the GUI
  tolerates non-JSON stderr — but a documented contract gap
  (`docs/CLI-JSON.md`). Deferred to avoid breaking `--help` handling.

## Build / test / release (pointers)

- Build CLI: `go build ./cmd/pbmac` (or `GOOS=darwin GOARCH=arm64 …`).
- Build app: `bash macos/build-app.sh` → `macos/dist/Proxmox Backup.app`
  (needs Mac + Xcode + Go + xcodegen).
- CI: `.github/workflows/ci.yml` (build+test+app compile on every push).
- Live validation: `live-validate.yml` (manual; needs the three `PBS_*` repo
  secrets; prunes its test snapshots afterward).
- Release: push a `vX.Y.Z` tag → `release.yml` builds + publishes the `.app` +
  standalone CLI + `SHA256SUMS.txt` as a GitHub Release. Version is stamped from
  the tag into both the bundle and the embedded binary.
