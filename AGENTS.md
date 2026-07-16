# AGENTS.md — orientation for contributors (human or AI)

**pbmac** is a native macOS (Apple Silicon) Proxmox Backup Server client: a Go CLI
(`pbmac`) + a SwiftUI app that bundles it. Start here, then read the canonical docs:

- [`README.md`](README.md) — install / usage / build / layout.
- [`docs/STATUS.md`](docs/STATUS.md) — **current state, decisions, deferred items,
  known follow-ups.** Read this first to avoid re-deriving context.
- [`docs/DESIGN.md`](docs/DESIGN.md) — architecture + reverse-engineered PBS protocol.
- [`docs/CLI-JSON.md`](docs/CLI-JSON.md) — the frozen GUI↔CLI `--json` contract.

## Hard invariants — don't break these

- **Pure Go, cgo-free.** Everything must `GOOS=darwin GOARCH=arm64 go build` with no
  C toolchain. This is why the binary is cheap to ship natively; features needing
  cgo (e.g. ACLs) are deferred, not merged.
- **Wire/on-disk formats match PBS byte-for-byte.** Chunker, DataBlob, dynamic
  index, manifest+signature, pxar are ported from Proxmox source and unit-tested;
  the official `proxmox-backup-client` must keep restoring pbmac archives (the
  `interop` job in `live-validate.yml`). Don't change these without re-validating.
- **Credentials never in git.** `PBS_*` via env / gitignored `.env` (see
  `.env.example`); token in the macOS Keychain via `pbmac login`. Test-server creds
  are LOCAL ONLY.
- **The GUI runs the *bundled* `pbmac`** (embedded in the .app) — it does not
  reimplement the protocol. Keep the `--json` contract stable; the app depends on it.

## Verify your changes

- `go vet ./... && go test ./...` locally; CI (`ci.yml`) also builds the macOS app.
- Touching backup/restore/crypto/pxar? Re-run **`live-validate.yml`** (manual
  `workflow_dispatch`; needs the `PBS_*` repo secrets) — it does real round-trips +
  official-client interop against a live PBS.
- The Go side compiles/tests anywhere; the SwiftUI app only builds on a Mac (CI
  compiles it, but can't launch the GUI — see STATUS.md "Known follow-ups").

## Release

Push a `vX.Y.Z` tag → `release.yml` builds the ad-hoc-signed `.app` (pbmac
embedded) + standalone CLI + checksums and publishes a GitHub Release.
