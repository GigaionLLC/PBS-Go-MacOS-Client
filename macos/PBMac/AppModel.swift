import SwiftUI
import Observation

enum ConnectionState: Equatable {
    case unknown, connecting
    case connected(String)   // server version
    case failed(String)      // message

    var isConnected: Bool { if case .connected = self { return true } else { return false } }
}

// Single source of truth for the UI. Holds connection/credentials, the loaded
// snapshot→archive→tree data, and the async commands that drive pbmac. Runs on
// the main actor; the client does its process work off-main.
@MainActor
@Observable
final class AppModel {
    private let client: PBMacClient
    private let defaults = UserDefaults.standard

    // Settings. Non-secret fields persist via explicit calls (see persist* /
    // setKeyfile); token and passphrase are kept in memory only.
    var repository: String
    var fingerprint: String
    var keyfilePath: String
    var token: String = ""          // in-memory; prefer `pbmac login` (Keychain)
    var passphrase: String = ""     // in-memory; passed as PBS_ENCRYPTION_PASSWORD

    // Connection.
    var connection: ConnectionState = .unknown

    // Data.
    var snapshots: [Snapshot] = []
    var selectedSnapshotID: Snapshot.ID?
    var archives: [Archive] = []
    var selectedArchiveID: Archive.ID?
    var tree: TreeNode?
    var focusedNode: TreeNode?

    // Status.
    var busy = false
    var lastError: String?

    var selectedSnapshot: Snapshot? { snapshots.first { $0.id == selectedSnapshotID } }
    var selectedArchive: Archive? { archives.first { $0.id == selectedArchiveID } }
    var hasKey: Bool { !keyfilePath.isEmpty }

    init(client: PBMacClient = PBMacClient(executableURL: PBMacClient.resolveExecutable())) {
        self.client = client
        let environment = ProcessInfo.processInfo.environment
        repository = defaults.string(forKey: "repository") ?? environment["PBS_REPOSITORY"] ?? ""
        fingerprint = defaults.string(forKey: "fingerprint") ?? environment["PBS_FINGERPRINT"] ?? ""
        keyfilePath = defaults.string(forKey: "keyfilePath") ?? ""
        token = environment["PBS_API_TOKEN"] ?? ""
        passphrase = environment["PBS_ENCRYPTION_PASSWORD"] ?? ""
    }

    private var env: PBSEnv {
        PBSEnv(repository: repository.nilIfEmpty,
               token: token.nilIfEmpty,
               fingerprint: fingerprint.nilIfEmpty,
               encryptionPassword: passphrase.nilIfEmpty)
    }

    private func report(_ error: Error) {
        lastError = (error as? PBMacError)?.message ?? error.localizedDescription
    }

    private func persistServerSettings() {
        defaults.set(repository, forKey: "repository")
        defaults.set(fingerprint, forKey: "fingerprint")
    }

    /// Selects an encryption keyfile and remembers its path.
    func setKeyfile(_ path: String) {
        keyfilePath = path
        defaults.set(path, forKey: "keyfilePath")
    }

    // MARK: Connection

    func connect() async {
        connection = .connecting
        do {
            let ping: PingResult = try await client.run(["ping"], env: env, as: PingResult.self)
            connection = .connected(ping.version)
            await loadSnapshots()
        } catch {
            connection = .failed((error as? PBMacError)?.message ?? error.localizedDescription)
        }
    }

    /// Persists repo/fingerprint/token via `pbmac login`, then connects.
    func saveAndConnect() async {
        do {
            var args = ["login", "--repo", repository]
            if let fp = fingerprint.nilIfEmpty { args += ["--fingerprint", fp] }
            if let tk = token.nilIfEmpty { args += ["--token", tk] }
            struct LoginResult: Decodable { let fingerprint: String? }
            let result: LoginResult = try await client.run(args, env: env, as: LoginResult.self)
            if let fp = result.fingerprint, !fp.isEmpty { fingerprint = fp }
            persistServerSettings()
        } catch {
            connection = .failed((error as? PBMacError)?.message ?? error.localizedDescription)
            return
        }
        await connect()
    }

    // MARK: Snapshots / archives / tree

    func loadSnapshots() async {
        busy = true; defer { busy = false }
        do {
            var snaps = try await client.run(["list"], env: env, as: [Snapshot].self)
            snaps.sort { $0.backupTime > $1.backupTime }
            snapshots = snaps
        } catch { report(error) }
    }

    func selectSnapshot(_ id: Snapshot.ID?) async {
        selectedSnapshotID = id
        archives = []; selectedArchiveID = nil; tree = nil; focusedNode = nil
        guard let snap = selectedSnapshot else { return }
        busy = true; defer { busy = false }
        do {
            archives = try await client.run(["archives", snap.spec], env: env, as: [Archive].self)
            selectedArchiveID = archives.first(where: { $0.isBrowsable })?.id ?? archives.first?.id
            await loadTree()
        } catch { report(error) }
    }

    func selectArchive(_ id: Archive.ID?) async {
        selectedArchiveID = id
        tree = nil; focusedNode = nil
        await loadTree()
    }

    private func loadTree() async {
        guard let snap = selectedSnapshot, let archive = selectedArchive, archive.isBrowsable else { return }
        busy = true; defer { busy = false }
        do {
            let entries = try await client.run(
                ["restore", "--list", snap.spec, archive.restoreName], env: env, as: [FileEntry].self)
            tree = TreeNode.build(from: entries)
        } catch { report(error) }
    }

    // MARK: Restore / backup / key

    /// Restores `filePath` (nil = whole archive) into `target`. Returns the result.
    func restore(to target: URL, filePath: String?) async throws -> RestoreResult {
        guard let snap = selectedSnapshot, let archive = selectedArchive else {
            throw PBMacError(message: "select a snapshot and archive first")
        }
        var args = ["restore", snap.spec, archive.restoreName, "--target", target.path]
        if let filePath { args += ["--file", filePath] }
        if archive.isEncrypted, let key = keyfilePath.nilIfEmpty { args += ["--keyfile", key] }
        return try await client.run(args, env: env, as: RestoreResult.self)
    }

    func backup(name: String, source: String, encrypt: Bool, compress: Bool,
                excludes: [String], id: String?) async throws -> BackupResult {
        var args = ["backup"]
        if encrypt {
            // Refuse to encrypt without a persisted key: a per-backup ephemeral
            // key would be discarded, making the snapshot unrecoverable.
            guard let key = keyfilePath.nilIfEmpty else {
                throw PBMacError(message: "Encryption is on but no key is set. Add or generate a key in Connection & Keys, or turn Encrypt off.")
            }
            args += ["--keyfile", key]
        }
        if compress { args.append("--compress") }
        for glob in excludes where !glob.isEmpty { args += ["--exclude", glob] }
        if let id, !id.isEmpty { args += ["--id", id] }
        args.append("\(name):\(source)")
        let result = try await client.run(args, env: env, as: BackupResult.self)
        await loadSnapshots()
        return result
    }

    /// Generates an encryption key via `pbmac key create` and remembers its path.
    /// A non-empty passphrase produces a scrypt-protected key.
    func createKey(at path: String) async throws {
        struct KeyResult: Decodable { let path: String }
        var args = ["key", "create", "--keyfile", path, "--force"]
        args += ["--kdf", passphrase.isEmpty ? "none" : "scrypt"]
        let result = try await client.run(args, env: env, as: KeyResult.self)
        setKeyfile(result.path)
    }
}
