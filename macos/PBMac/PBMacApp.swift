import SwiftUI
import AppKit

@main
struct PBMacApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .frame(minWidth: 860, minHeight: 540)
                .task { await model.connect() }
                .onOpenURL { handleURL($0) }
        }
        .windowToolbarStyle(.unified)
        .commands {
            CommandGroup(after: .appInfo) {
                Button("Install Command-Line Tool…") { installCommandLineTool() }
            }
            CommandGroup(after: .newItem) {
                Button("Refresh Snapshots") { Task { await model.loadSnapshots() } }
                    .keyboardShortcut("r")
                Button("Console") { model.pane = .console }
                    .keyboardShortcut("k")
            }
        }
    }

    /// Handles `pbmac://` deep links so a terminal or script can drive the GUI:
    ///   pbmac://snapshots
    ///   pbmac://snapshot/host/mymac/1700000000
    ///   pbmac://backup   pbmac://setup
    ///   pbmac://console?cmd=list
    @MainActor
    private func handleURL(_ url: URL) {
        guard url.scheme == "pbmac" else { return }
        switch (url.host ?? "").lowercased() {
        case "", "snapshots", "list":
            model.pane = .browse
            Task { await model.loadSnapshots() }
        case "snapshot":
            let parts = url.pathComponents.filter { $0 != "/" }
            guard parts.count >= 3 else { return }
            let spec = parts.joined(separator: "/")   // type/id/time
            model.pane = .browse
            Task { await model.loadSnapshots(); await model.selectSnapshot(spec) }
        case "backup":
            model.pane = .backup
        case "setup", "keys", "connection":
            model.pane = .setup
        case "console", "run":
            model.pane = .console
            if let cmd = URLComponents(url: url, resolvingAgainstBaseURL: false)?
                .queryItems?.first(where: { $0.name == "cmd" })?.value {
                Task { await model.runConsole(cmd) }
            }
        default:
            break
        }
    }

    /// Symlinks the bundled pbmac onto the PATH so the terminal uses the identical
    /// binary. Falls back to showing the exact command when it can't write there.
    @MainActor
    private func installCommandLineTool() {
        let source = model.pbmacExecutableURL
        let dest = URL(fileURLWithPath: "/usr/local/bin/pbmac")
        let fm = FileManager.default
        let alert = NSAlert()
        do {
            try? fm.createDirectory(atPath: "/usr/local/bin", withIntermediateDirectories: true)
            try? fm.removeItem(at: dest)
            try fm.createSymbolicLink(at: dest, withDestinationURL: source)
            alert.messageText = "Command-line tool installed"
            alert.informativeText = "Run `pbmac` in Terminal — it points at the binary this app uses:\n\(source.path)"
        } catch {
            alert.alertStyle = .informational
            alert.messageText = "Finish install in Terminal"
            alert.informativeText = "This needs admin rights. Paste:\n\nsudo ln -sf \"\(source.path)\" /usr/local/bin/pbmac"
        }
        alert.runModal()
    }
}
