import Foundation

// Codable models mirroring the `pbmac --json` contract (see docs/CLI-JSON.md).
// Keys are kebab/snake-case on the wire; CodingKeys map them to Swift names.

struct Snapshot: Decodable, Identifiable, Hashable {
    let backupType: String
    let backupID: String
    let backupTime: Int
    let comment: String?
    let size: Int64?

    /// The `type/id/unixtime` spec pbmac expects as an argument.
    var id: String { "\(backupType)/\(backupID)/\(backupTime)" }
    var spec: String { id }

    enum CodingKeys: String, CodingKey {
        case backupType = "backup-type"
        case backupID = "backup-id"
        case backupTime = "backup-time"
        case comment, size
    }
}

struct Archive: Decodable, Identifiable, Hashable {
    let filename: String
    let cryptMode: String
    let size: Int64
    let csum: String

    var id: String { filename }
    var isEncrypted: Bool { cryptMode == "encrypt" }
    /// Display without the `.didx` index suffix, e.g. `root.pxar`.
    var displayName: String { filename.hasSuffix(".didx") ? String(filename.dropLast(5)) : filename }
    /// Name passed to `restore` (the archive, not its index).
    var restoreName: String { displayName }
    var isBrowsable: Bool { displayName.hasSuffix(".pxar") }

    enum CodingKeys: String, CodingKey {
        case filename, size, csum
        case cryptMode = "crypt-mode"
    }
}

struct FileEntry: Decodable, Identifiable, Hashable {
    let path: String
    let type: String   // dir | file | symlink
    let size: Int64?
    let mode: Int

    var id: String { path }
    var isDir: Bool { type == "dir" }
    var isSymlink: Bool { type == "symlink" }
    var name: String { (path as NSString).lastPathComponent }
}

struct PingResult: Decodable {
    let version: String
    let release: String?
    let repoid: String?
}

struct RestoreResult: Decodable {
    let snapshot: String
    let archive: String
    let target: String
    let filesRestored: Int
    let bytesWritten: Int64

    enum CodingKeys: String, CodingKey {
        case snapshot, archive, target
        case filesRestored = "files_restored"
        case bytesWritten = "bytes_written"
    }
}

struct BackupResult: Decodable {
    let archiveBytes: Int64
    let totalChunks: Int
    let uniqueChunks: Int
    let uniqueBytes: Int64
    let reusedChunks: Int
    let reusedBytes: Int64
    let encrypted: Bool
    let compressed: Bool
    let dedupRatio: Double
    let indexCsum: String
    let snapshot: String?

    enum CodingKeys: String, CodingKey {
        case archiveBytes = "archive_bytes"
        case totalChunks = "total_chunks"
        case uniqueChunks = "unique_chunks"
        case uniqueBytes = "unique_bytes"
        case reusedChunks = "reused_chunks"
        case reusedBytes = "reused_bytes"
        case encrypted, compressed
        case dedupRatio = "dedup_ratio"
        case indexCsum = "index_csum"
        case snapshot
    }
}

/// Error surfaced by pbmac under `--json` — a single `{"error": …}` object on stderr.
struct PBMacError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}
