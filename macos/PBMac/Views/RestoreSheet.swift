import SwiftUI
import AppKit

// Restore sheet: confirm what (focused item or whole archive) and where, then
// run and report. Progress is indeterminate until pbmac gains --progress
// streaming (see docs/CLI-JSON.md).
struct RestoreSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss

    @State private var target: URL?
    @State private var wholeArchive = false
    @State private var phase: Phase = .form
    @State private var result: RestoreResult?

    enum Phase { case form, running, done }

    private var focused: TreeNode? { model.focusedNode }
    private var restoreSingle: Bool { focused != nil && !wholeArchive }

    private var restoreCommand: String {
        guard let snap = model.selectedSnapshot, let archive = model.selectedArchive else { return "" }
        var parts = ["pbmac restore"]
        if restoreSingle, let focused { parts.append("--file \(focused.path)") }
        parts.append("--target \(target?.path ?? "<destination>")")
        if archive.isEncrypted && model.hasKey { parts.append("--keyfile <key>") }
        parts.append(snap.spec)
        parts.append(archive.restoreName)
        return parts.joined(separator: " ")
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Restore").font(.title2.bold())
            switch phase {
            case .form: form
            case .running: running
            case .done: doneView
            }
        }
        .padding(22)
        .frame(width: 460)
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 14) {
            GroupBox {
                VStack(alignment: .leading, spacing: 8) {
                    Label {
                        Text(restoreSingle ? (focused?.path ?? "") : "Entire archive · \(model.selectedArchive?.displayName ?? "")")
                            .lineLimit(1).truncationMode(.middle)
                    } icon: {
                        Image(systemName: restoreSingle ? (focused?.systemImage ?? "doc") : "archivebox")
                    }
                    Toggle("Restore the entire archive", isOn: $wholeArchive)
                        .disabled(focused == nil)
                }
                .padding(6)
            }

            HStack(spacing: 8) {
                Text("To:").foregroundStyle(.secondary)
                Text(target?.path ?? "Choose a destination…")
                    .lineLimit(1).truncationMode(.middle)
                    .foregroundStyle(target == nil ? .secondary : .primary)
                Spacer()
                Button("Choose…", action: chooseTarget)
            }
            .padding(10)
            .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 8))

            if model.selectedArchive?.isEncrypted == true && !model.hasKey {
                Label("This archive is encrypted — set an encryption key in Connection & Keys first.",
                      systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.orange)
            }

            CommandChip(command: restoreCommand)

            HStack {
                Spacer()
                Button("Cancel") { dismiss() }.keyboardShortcut(.cancelAction)
                Button("Restore", action: run)
                    .keyboardShortcut(.defaultAction)
                    .buttonStyle(.borderedProminent)
                    .disabled(target == nil || (model.selectedArchive?.isEncrypted == true && !model.hasKey))
            }
        }
    }

    private var running: some View {
        VStack(spacing: 12) {
            ProgressView().controlSize(.large)
            Text("Restoring…").foregroundStyle(.secondary)
            Text("Live progress arrives with pbmac --progress.")
                .font(.caption2).foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 24)
    }

    private var doneView: some View {
        VStack(spacing: 12) {
            Image(systemName: "checkmark.circle.fill").font(.system(size: 42)).foregroundStyle(.green)
            Text("Restored \(result?.filesRestored ?? 0) item\((result?.filesRestored ?? 0) == 1 ? "" : "s")")
                .font(.headline)
            Text("\(Fmt.bytes(result?.bytesWritten)) written to \(result?.target ?? "")")
                .font(.caption).foregroundStyle(.secondary)
                .lineLimit(1).truncationMode(.middle)
            HStack {
                if let path = result?.target {
                    Button("Reveal in Finder") {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: path)])
                    }
                }
                Button("Done") { dismiss() }.buttonStyle(.borderedProminent)
            }
            .padding(.top, 4)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 8)
    }

    private func chooseTarget() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.canCreateDirectories = true
        panel.prompt = "Restore Here"
        panel.message = "Choose where to restore the selected files"
        if panel.runModal() == .OK { target = panel.url }
    }

    private func run() {
        guard let target else { return }
        phase = .running
        let path = restoreSingle ? focused?.path : nil
        Task {
            do {
                result = try await model.restore(to: target, filePath: path)
                phase = .done
            } catch {
                model.lastError = (error as? PBMacError)?.message ?? error.localizedDescription
                phase = .form
            }
        }
    }
}
