import SwiftUI
import AppKit
import Foundation
import UniformTypeIdentifiers

// Back Up pane: pick a folder (drag it in from Finder or choose it), set options,
// run, and show the dedup result. On success the snapshot list refreshes.
struct BackupView: View {
    @Environment(AppModel.self) private var model

    @State private var source: URL?
    @State private var archiveName = "root.pxar"
    @State private var backupID = ""
    @State private var encrypt = true
    @State private var compress = true
    @State private var excludes = ""
    @State private var dropTargeted = false
    @State private var phase: Phase = .form
    @State private var result: BackupResult?

    enum Phase { case form, running, done }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Back Up a Folder").font(.title2.bold())
                    Text("Snapshot a directory to your datastore. Only changed data is uploaded — unchanged chunks are reused from the previous snapshot.")
                        .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                }
                switch phase {
                case .form: form
                case .running: running
                case .done: doneView
                }
            }
            .padding(24)
            .frame(maxWidth: 620, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var form: some View {
        VStack(alignment: .leading, spacing: 16) {
            dropZone

            GroupBox {
                VStack(spacing: 0) {
                    LabeledField("Archive name") {
                        TextField("root.pxar", text: $archiveName).textFieldStyle(.roundedBorder).frame(maxWidth: 220)
                    }
                    Divider()
                    LabeledField("Backup ID") {
                        TextField("hostname (default)", text: $backupID)
                            .textFieldStyle(.roundedBorder).frame(maxWidth: 220)
                    }
                    Divider()
                    LabeledField("Encrypt", detail: model.hasKey ? "AES-256-GCM with your key" : "needs a key — set one in Connection & Keys") {
                        Toggle("", isOn: $encrypt).labelsHidden().toggleStyle(.switch)
                    }
                    Divider()
                    LabeledField("Compress", detail: "zstd chunk compression") {
                        Toggle("", isOn: $compress).labelsHidden().toggleStyle(.switch)
                    }
                    Divider()
                    LabeledField("Exclude") {
                        TextField("node_modules/, *.log", text: $excludes)
                            .textFieldStyle(.roundedBorder).frame(maxWidth: 260)
                    }
                }
                .padding(6)
            }

            if encrypt && !model.hasKey {
                Label("Encryption is on but no key is set — add one in Connection & Keys, or turn Encrypt off.",
                      systemImage: "exclamationmark.triangle")
                    .font(.caption).foregroundStyle(.orange)
            }

            HStack {
                Button("Back Up", action: run)
                    .buttonStyle(.borderedProminent)
                    .disabled(source == nil || archiveName.isEmpty || (encrypt && !model.hasKey))
                Spacer()
            }
            if let source {
                CommandChip(command: commandPreview(source: source))
            }
        }
    }

    private var dropZone: some View {
        VStack(spacing: 8) {
            Image(systemName: source == nil ? "folder.badge.plus" : "folder.fill")
                .font(.system(size: 30)).foregroundStyle(.tint)
            Text(source?.path ?? "Drop a folder here, or choose one")
                .font(source == nil ? .callout : .callout.weight(.medium))
                .foregroundStyle(source == nil ? .secondary : .primary)
                .lineLimit(1).truncationMode(.middle)
            Button(source == nil ? "Choose Folder…" : "Change…", action: chooseSource)
                .controlSize(.small)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 26)
        .background(
            RoundedRectangle(cornerRadius: 12)
                .fill(dropTargeted ? Color.accentColor.opacity(0.12) : Color(nsColor: .quaternaryLabelColor).opacity(0.25))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12)
                .strokeBorder(dropTargeted ? Color.accentColor : Color.secondary.opacity(0.5),
                              style: StrokeStyle(lineWidth: 1.5, dash: [6, 4]))
        )
        .dropDestination(for: URL.self) { urls, _ in
            guard let dir = urls.first(where: isDirectory) ?? urls.first else { return false }
            source = dir
            return true
        } isTargeted: { dropTargeted = $0 }
    }

    private var running: some View {
        VStack(spacing: 12) {
            ProgressView().controlSize(.large)
            Text("Backing up…").foregroundStyle(.secondary)
            Text("Live progress arrives with pbmac --progress.")
                .font(.caption2).foregroundStyle(.tertiary)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 24)
    }

    private var doneView: some View {
        VStack(alignment: .leading, spacing: 14) {
            Label("Backup complete", systemImage: "checkmark.circle.fill")
                .font(.headline).foregroundStyle(.green)
            if let r = result {
                HStack(spacing: 10) {
                    StatCard(title: "New", value: "\(r.uniqueChunks)", unit: "chunks")
                    StatCard(title: "Reused", value: "\(r.reusedChunks)", unit: "chunks")
                    StatCard(title: "Dedup", value: Fmt.percent(r.dedupRatio), unit: "")
                    StatCard(title: "Sent", value: Fmt.bytes(r.uniqueBytes), unit: "")
                }
                if let snap = r.snapshot {
                    Text("Snapshot \(snap)").font(.caption).foregroundStyle(.secondary)
                }
            }
            HStack {
                Button("Back Up Another") { phase = .form; result = nil }
                Button("View Snapshots") { model.pane = .browse }.buttonStyle(.borderedProminent)
            }
            .padding(.top, 2)
        }
    }

    // MARK: helpers

    private func isDirectory(_ url: URL) -> Bool {
        (try? url.resourceValues(forKeys: [.isDirectoryKey]))?.isDirectory ?? false
    }

    private func chooseSource() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.prompt = "Back Up"
        if panel.runModal() == .OK { source = panel.url }
    }

    private var excludeGlobs: [String] {
        excludes.split(whereSeparator: { $0 == "," || $0 == "\n" })
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
    }

    private func commandPreview(source: URL) -> String {
        var parts = ["pbmac backup"]
        if encrypt { parts.append(model.hasKey ? "--keyfile …" : "--encrypt") }
        if compress { parts.append("--compress") }
        if !backupID.isEmpty { parts.append("--id \(backupID)") }
        parts.append("\(archiveName):\(source.path)")
        return parts.joined(separator: " ")
    }

    private func run() {
        guard let source else { return }
        phase = .running
        Task {
            do {
                result = try await model.backup(
                    name: archiveName, source: source.path,
                    encrypt: encrypt, compress: compress,
                    excludes: excludeGlobs, id: backupID.nilIfEmpty)
                phase = .done
            } catch {
                model.lastError = (error as? PBMacError)?.message ?? error.localizedDescription
                phase = .form
            }
        }
    }
}

private struct LabeledField<Content: View>: View {
    let label: String
    var detail: String?
    @ViewBuilder let content: Content

    init(_ label: String, detail: String? = nil, @ViewBuilder content: () -> Content) {
        self.label = label
        self.detail = detail
        self.content = content()
    }

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 1) {
                Text(label).font(.body)
                if let detail { Text(detail).font(.caption).foregroundStyle(.secondary) }
            }
            Spacer()
            content
        }
        .padding(.vertical, 8)
    }
}

private struct StatCard: View {
    let title: String
    let value: String
    let unit: String

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
            HStack(alignment: .firstTextBaseline, spacing: 3) {
                Text(value).font(.title3.weight(.semibold)).monospacedDigit()
                if !unit.isEmpty { Text(unit).font(.caption2).foregroundStyle(.secondary) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 9))
    }
}
