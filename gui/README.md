# pbmac GUI

A native-feeling macOS front-end for the `pbmac` Proxmox Backup client. It is a
**separate component**: it never speaks the PBS protocol itself — it shells out to
the `pbmac` binary and consumes its `--json` output (the frozen contract in
[`../docs/CLI-JSON.md`](../docs/CLI-JSON.md)). This keeps all trust-boundary and
protocol code in the CLI.

Three jobs, designed to be obvious: **view** snapshots, **restore** files
(Finder-style column browser → select → destination → progress), and **back up** a
folder (source + options → live dedup progress).

## Status

- **`index.html`** — the complete front-end (HTML/CSS/JS, no build step, no
  dependencies). Open it in any browser to click through the full UX; it runs on
  embedded fixtures whose shapes exactly match `pbmac --json`. It is theme-aware
  (light/dark) and uses the macOS system font (SF Pro on a Mac).
- The **native shell** (Tauri) that execs the real `pbmac` is the remaining
  step — see *Wiring the shell* below. It's split out because it can only be
  built/notarized/exercised on macOS, whereas the front-end above is verifiable
  anywhere.

## Why Tauri (and not SwiftUI/Electron)

The GUI's whole job is "spawn a CLI, stream JSON, render lists/trees/forms," so
deep native APIs matter little. Tauri renders in the macOS **system WebView**
(feels close to native, tiny footprint) and its **sidecar** mechanism is built to
bundle+exec a CLI like `pbmac` (including signing/notarization). It also lets the
UI be developed and reviewed on any OS against fixtures, with only a thin Rust
glue file that's validated on a Mac. Electron is the fallback (heavier, less
native); a truly native rewrite would be **SwiftUI** — best feel, but a macOS-only
build and a second language for a Go team, so it's not the default.

## Screen → command map

| Screen | `pbmac` command | JSON consumed |
|---|---|---|
| Connection status | `pbmac ping --json` | `{version,release,repoid}` |
| Snapshot list (sidebar) | `pbmac list --json` | `[{backup-type,backup-id,backup-time,size,…}]` |
| Archive picker | `pbmac archives <snap> --json` | `[{filename,size,csum,crypt-mode}]` |
| File browser (columns) | `pbmac restore --list <snap> <archive> --json` | `[{path,type,size,mode}]` |
| Restore | `pbmac restore <snap> <archive> --target <dir> [--file /p] [--keyfile k] --json` | `{files_restored,bytes_written,…}` |
| Back up | `pbmac backup [--encrypt] [--compress] [--exclude …] --id <id> <name.pxar:/path> --json` | `backup.Result` (`unique_chunks`, `reused_chunks`, `dedup_ratio`, …) |
| Errors | any command, on failure | `{"error": "..."}` on stderr, exit 1 |

Snapshot spec is `backup-type/backup-id/backup-time`; the browser builds its tree
from the `restore --list` `path`s.

## Wiring the shell (Tauri)

1. Scaffold on a Mac: `npm create tauri-app@latest` → framework "Vanilla", then
   drop `index.html` in as the front-end (`frontendDist`/`devUrl` → this folder).
2. Bundle the CLI as a **sidecar**: build `pbmac` for `aarch64-apple-darwin`, place
   it at `src-tauri/binaries/pbmac-aarch64-apple-darwin`, and add to
   `tauri.conf.json` → `bundle.externalBin: ["binaries/pbmac"]`.
3. Add one Rust command that runs the sidecar and returns its stdout (JSON):

   ```rust
   #[tauri::command]
   async fn pbmac(app: tauri::AppHandle, args: Vec<String>) -> Result<String, String> {
       use tauri_plugin_shell::ShellExt;
       let out = app.shell().sidecar("pbmac").map_err(|e| e.to_string())?
           .args(args).output().await.map_err(|e| e.to_string())?;
       if out.status.success() { Ok(String::from_utf8_lossy(&out.stdout).into()) }
       else { Err(String::from_utf8_lossy(&out.stderr).into()) } // {"error": …}
   }
   ```

4. In `index.html`, replace the fixture calls with a single shim (the UI already
   funnels every data read through one place):

   ```js
   async function pbmac(args){                 // returns parsed JSON
     if (window.__TAURI__) return JSON.parse(await window.__TAURI__.core.invoke('pbmac', {args:[...args,'--json']}));
     return FIXTURES[args[0]];                 // fallback for browser dev
   }
   ```
   Credentials come from the environment (`PBS_REPOSITORY`, `PBS_API_TOKEN`,
   `PBS_FINGERPRINT`) or a `login` screen calling `pbmac login`.

5. `npm run tauri build` → a signed/notarized `.app`. Long backups/restores should
   use the planned `--progress` NDJSON stream (see `CLI-JSON.md`) so the progress
   bars are live rather than simulated.

## Develop

```sh
open gui/index.html          # or: python3 -m http.server -d gui
```
No install needed for the front-end. The native app needs Node + Rust + Xcode on
macOS (per the steps above).
