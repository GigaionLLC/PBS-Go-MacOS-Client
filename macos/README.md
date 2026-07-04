# Proxmox Backup — macOS app (SwiftUI)

A native macOS app for the `pbmac` client. It **shells out to the `pbmac` binary**
and consumes its `--json` output (the frozen contract in
[`../docs/CLI-JSON.md`](../docs/CLI-JSON.md)) — all protocol/trust-boundary code
stays in the CLI. SwiftUI + AppKit, Mac-only, for a true Finder feel.

Three jobs: **view** snapshots, **restore** (Finder-style column browser → sheet →
progress, or drag a file out to Finder), and **back up** (drag a folder in → live
dedup result). The [`../gui/`](../gui/) HTML prototype is the interactive design
spec; this is the real implementation.

## Build & run (needs a Mac)

The Xcode project is generated from [`project.yml`](project.yml) with
[XcodeGen](https://github.com/yonik/XcodeGen), so only this spec + the Swift
sources are tracked.

```sh
brew install xcodegen
cd macos
xcodegen               # writes PBMac.xcodeproj (gitignored)
open PBMac.xcodeproj   # ⌘R to run
```

**Point it at `pbmac`.** The app looks for the binary in this order: a copy
bundled in the app’s Resources (named `pbmac`) → `$PBMAC_BIN` → `/opt/homebrew/bin`
→ `/usr/local/bin`. For development, build the CLI and export the path:

```sh
go build -o /usr/local/bin/pbmac ./cmd/pbmac      # or: export PBMAC_BIN=/path/to/pbmac
```

For distribution, drop a `darwin/arm64` `pbmac` into the app target’s *Copy Bundle
Resources* and it’s found automatically. Xcode **Previews** work without any of
this — they run on the fixtures in `SampleData.swift`.

## Layout

| File | Role |
|---|---|
| `PBMacApp.swift` | `@main`, window, menu commands |
| `Models.swift` | Codable structs mirroring `pbmac --json` |
| `PBMacClient.swift` | runs the binary (`Process`), decodes stdout, maps `{"error"}` stderr |
| `AppModel.swift` | `@Observable` state + async commands (connect/list/archives/restore/backup/key) |
| `Tree.swift` | folds the flat `restore --list` paths into a browsable tree |
| `Views/RootView.swift` | split view, sidebar (connection + snapshots + actions) |
| `Views/DetailView.swift` | archive picker toolbar + browser + empty states |
| `Views/ColumnBrowser.swift` | Finder Miller columns |
| `Views/DragOut.swift` | drag a row out to Finder → restores on drop (file promise) |
| `Views/RestoreSheet.swift` | restore confirm → run → result |
| `Views/BackupView.swift` | drag-in source + options → dedup result |
| `Views/SetupView.swift` | Connection & Keys onboarding |

## Screen → command

| Screen | `pbmac` command |
|---|---|
| Connection status | `ping` |
| Snapshot list | `list` |
| Archive picker | `archives <snap>` |
| File browser | `restore --list <snap> <archive>` |
| Restore / drag-out | `restore <snap> <archive> --target <dir> [--file /p] [--keyfile K]` |
| Back up | `backup [--keyfile K|--encrypt] [--compress] [--exclude …] [--id X] <name.pxar:/path>` |
| Save & Connect | `login --repo … [--fingerprint …] [--token …]` |
| Generate key | `key create --keyfile … --kdf scrypt\|none` |

Credentials/passphrase are passed to the CLI via environment
(`PBS_REPOSITORY`, `PBS_API_TOKEN`, `PBS_FINGERPRINT`, `PBS_ENCRYPTION_PASSWORD`);
`login` persists the non-secret parts and stores the token in the Keychain.

## Verify on device

Some AppKit paths can only be exercised on macOS — sanity-check on first build:
- **Drag-out to Finder** (`DragOut.swift`): confirm a dragged file lands correctly.
  It restores to a temp dir and hands back the path pbmac wrote; if pbmac flattens
  vs. reconstructs the archive path differently than assumed, adjust the path
  resolution there. The **Restore…** button is the always-works equivalent.
- **Live progress** for backup/restore is indeterminate until pbmac adds the
  `--progress` NDJSON stream reserved in `CLI-JSON.md`.
