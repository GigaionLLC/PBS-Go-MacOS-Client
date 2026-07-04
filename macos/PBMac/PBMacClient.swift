import Foundation

// Credentials/settings passed to pbmac as environment for a single invocation.
// pbmac itself resolves flag > env > stored config/Keychain, so anything left
// nil falls back to what `pbmac login` stored.
struct PBSEnv: Sendable {
    var repository: String?
    var token: String?
    var fingerprint: String?
    var encryptionPassword: String?
}

// Thin wrapper around the pbmac CLI. Every call runs the binary with `--json`,
// decodes stdout, and maps a non-zero exit's `{"error": …}` stderr envelope to a
// PBMacError. The process runs off the main thread; callers await the result.
struct PBMacClient: Sendable {
    let executableURL: URL

    /// Runs pbmac and decodes stdout as T.
    func run<T: Decodable>(_ args: [String], env: PBSEnv = PBSEnv(), as type: T.Type) async throws -> T {
        let data = try await runRaw(args + ["--json"], env: env)
        do {
            return try JSONDecoder().decode(T.self, from: data)
        } catch {
            let text = String(data: data, encoding: .utf8) ?? ""
            throw PBMacError(message: "unexpected output from pbmac: \(text.isEmpty ? error.localizedDescription : text)")
        }
    }

    /// Runs pbmac and returns raw stdout bytes, throwing PBMacError on failure.
    /// Result of running pbmac: both streams and the exit code, no interpretation.
    struct Execution: Sendable {
        let stdout: Data
        let stderr: Data
        let exitCode: Int32
        var ok: Bool { exitCode == 0 }
    }

    /// Runs pbmac and returns raw stdout, mapping a non-zero exit's stderr
    /// `{"error": …}` envelope (or plain text) to a PBMacError.
    func runRaw(_ args: [String], env: PBSEnv = PBSEnv()) async throws -> Data {
        let r = try await execute(args, env: env)
        if r.ok { return r.stdout }
        if let envelope = try? JSONDecoder().decode(ErrorEnvelope.self, from: r.stderr) {
            throw PBMacError(message: envelope.error)
        }
        let text = String(data: r.stderr, encoding: .utf8)?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        throw PBMacError(message: text.isEmpty ? "pbmac exited with status \(r.exitCode)" : text)
    }

    /// Runs pbmac exactly as given (no --json added) and returns combined output
    /// text + success — for the in-app console. Only a launch failure throws.
    func runConsole(_ args: [String], env: PBSEnv = PBSEnv()) async throws -> (text: String, ok: Bool) {
        let r = try await execute(args, env: env)
        var text = String(data: r.stdout, encoding: .utf8) ?? ""
        let errText = String(data: r.stderr, encoding: .utf8) ?? ""
        if !errText.isEmpty {
            if !text.isEmpty && !text.hasSuffix("\n") { text += "\n" }
            text += errText
        }
        return (text.trimmingCharacters(in: .newlines), r.ok)
    }

    /// Launches pbmac, drains both pipes (before waiting, to avoid a pipe-buffer
    /// deadlock), and returns both streams + the exit code.
    private func execute(_ args: [String], env: PBSEnv) async throws -> Execution {
        let exe = executableURL
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                let process = Process()
                process.executableURL = exe
                process.arguments = args

                var environment = ProcessInfo.processInfo.environment
                if let v = env.repository { environment["PBS_REPOSITORY"] = v }
                if let v = env.token { environment["PBS_API_TOKEN"] = v }
                if let v = env.fingerprint { environment["PBS_FINGERPRINT"] = v }
                if let v = env.encryptionPassword { environment["PBS_ENCRYPTION_PASSWORD"] = v }
                process.environment = environment

                let out = Pipe(), err = Pipe()
                process.standardOutput = out
                process.standardError = err

                do {
                    try process.run()
                } catch {
                    continuation.resume(throwing: PBMacError(
                        message: "cannot launch pbmac at \(exe.path): \(error.localizedDescription). Set PBMAC_BIN or install pbmac."))
                    return
                }

                let outData = out.fileHandleForReading.readDataToEndOfFile()
                let errData = err.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()
                continuation.resume(returning: Execution(stdout: outData, stderr: errData, exitCode: process.terminationStatus))
            }
        }
    }

    private struct ErrorEnvelope: Decodable { let error: String }

    /// Locates the pbmac binary: bundled sidecar → $PBMAC_BIN → common install paths.
    static func resolveExecutable() -> URL {
        let fm = FileManager.default
        if let bundled = Bundle.main.url(forResource: "pbmac", withExtension: nil) {
            return bundled
        }
        if let override = ProcessInfo.processInfo.environment["PBMAC_BIN"], fm.isExecutableFile(atPath: override) {
            return URL(fileURLWithPath: override)
        }
        for path in ["/opt/homebrew/bin/pbmac", "/usr/local/bin/pbmac"] where fm.isExecutableFile(atPath: path) {
            return URL(fileURLWithPath: path)
        }
        // Not found — Process.run will throw a helpful error when first used.
        return URL(fileURLWithPath: "/usr/local/bin/pbmac")
    }
}
