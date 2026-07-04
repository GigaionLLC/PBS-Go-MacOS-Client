# pbmac CLI — JSON contract

Every data command accepts `--json`, switching stdout to a single pretty-printed
JSON value. This is the stable contract a GUI (or any script) drives; see
[`DESIGN.md`](DESIGN.md) §6. Human-readable output (no `--json`) is not a contract
and may change.

**Errors.** On failure with `--json`, a single object is written to **stderr** and
the exit code is non-zero:

```json
{ "error": "snapshot must be type/id/unixtime, got \"foo\"" }
```

Exit codes: `0` success, `1` runtime failure, `2` usage/flag error.

**Specs.** A *snapshot* is `backup-type/backup-id/backup-time` (e.g.
`host/mymac/1700000000`). A *backup archive spec* is `NAME.pxar:/abs/path`.

---

## Commands

### `version --json`
```json
{ "name": "pbmac", "version": "0.1.0" }
```

### `ping [--repo R] --json`
Connectivity + auth check (server version).
```json
{ "version": "4.2", "release": "2", "repoid": "79bee2…" }
```

### `list [--repo R] --json`  (alias `snapshots`)
Array (never `null`); `comment`/`size` omitted when absent.
```json
[ { "backup-type": "host", "backup-id": "mymac", "backup-time": 1700000000,
    "comment": "", "size": 0 } ]
```

### `archives <snapshot> [--repo R] --json`
The snapshot manifest's file list — populates an archive picker.
```json
[ { "filename": "root.pxar.didx", "crypt-mode": "encrypt", "size": 40664,
    "csum": "abac…" } ]
```

### `restore --list <snapshot> <archive> [--keyfile K] --json`
Entries in an archive (never `null`). `mode` is the raw unix mode (decimal).
```json
[ { "path": "/a.txt", "type": "file", "size": 27, "mode": 33188 } ]
```
`type` ∈ `dir | file | symlink`.

### `restore <snapshot> <archive> --target <dir> [--file /p] [--keyfile K] --json`
```json
{ "snapshot": "host/mymac/1700000000", "archive": "root.pxar",
  "target": "./out", "files_restored": 3, "bytes_written": 40027 }
```

### `backup [--dry-run] [--encrypt|--keyfile K] [--compress] [--exclude G …] [--snapshot] [--id X] [--repo R] <NAME.pxar:/path> --json`
```json
{ "archive_bytes": 40000, "total_chunks": 7, "unique_chunks": 7,
  "unique_bytes": 40000, "reused_chunks": 0, "reused_bytes": 0,
  "encrypted": false, "compressed": false, "dedup_ratio": 0.0,
  "index_csum": "abac…", "snapshot": "host/mymac/1700000000" }
```
`snapshot` is present only for a live (non-dry-run) backup — it identifies the
snapshot just created. `reused_*` reflect incremental dedup against the previous
snapshot.

### `login --repo R [--fingerprint F] [--token 'U@R!T:S'] --json`
```json
{ "repository": "host:8007:store", "config_path": "~/…/pbmac.json",
  "fingerprint": "CB:28:…", "token_stored": true }
```
A Keychain-store failure is an `{"error": …}` (exit 1) under `--json`.

---

## Not yet in the contract (planned)

- **Progress streaming** for long backups/restores: a `--progress` flag emitting
  newline-delimited JSON events (`{"event":"progress","phase":"upload",
  "done_bytes":N,"total_bytes":M}`) followed by a terminal `{"event":"result",…}`.
  The schema is reserved; the current `--json` emits only the final object.
