# pbmac GUI — design prototype

The interactive **design spec** for the macOS app: a dependency-free `index.html`
you can open in any browser (or on a phone) to click through the whole UX —
snapshot browsing, the Finder-style restore flow, the backup form with live dedup
stats, and the Connection & Keys setup screen. It runs on fixtures whose shapes
match `pbmac --json` (see [`../docs/CLI-JSON.md`](../docs/CLI-JSON.md)).

**The real app is native SwiftUI, in [`../macos/`](../macos/).** This HTML is kept
as the reference the SwiftUI views are built against — it's the thing that renders
without a Mac, so design iteration happens here first.

Why SwiftUI (not Tauri/Electron): "native as Finder" is inherently macOS, so the
app is a Mac-only SwiftUI + AppKit build — true Miller columns, `NSOpenPanel`, and
drag to/from Finder. It still drives the same `pbmac --json` contract underneath.

## Develop

```sh
open gui/index.html          # or: python3 -m http.server -d gui
```

It's theme-aware (light/dark), uses the macOS system font, and is responsive down
to phone width for review. No build step, no dependencies.

## Screen → command map

Both this prototype and the SwiftUI app drive these `pbmac` commands:

| Screen | `pbmac` command |
|---|---|
| Connection status | `ping` |
| Snapshot list | `list` |
| Archive picker | `archives <snap>` |
| File browser (columns) | `restore --list <snap> <archive>` |
| Restore / drag-out to Finder | `restore <snap> <archive> --target <dir> [--file /p] [--keyfile K]` |
| Back up (drag-in source) | `backup [--keyfile K\|--encrypt] [--compress] [--exclude …] [--id X] <name.pxar:/path>` |
| Save & Connect | `login --repo … [--fingerprint …] [--token …]` |
| Generate key | `key create --keyfile … --kdf scrypt\|none` |
| Errors | any command → `{"error": …}` on stderr, exit 1 |

Credentials and the encryption passphrase are passed via environment
(`PBS_REPOSITORY`, `PBS_API_TOKEN`, `PBS_FINGERPRINT`, `PBS_ENCRYPTION_PASSWORD`);
`login` persists the non-secret parts and stores the token in the Keychain.
