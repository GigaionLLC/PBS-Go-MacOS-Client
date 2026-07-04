import SwiftUI

// Top-level split view: translucent sidebar (connection + snapshots + actions)
// on the left, the active pane on the right.
struct RootView: View {
    @Environment(AppModel.self) private var model
    @State private var pane: Pane = .browse

    enum Pane: Equatable { case browse, backup, setup }

    var body: some View {
        @Bindable var model = model
        NavigationSplitView {
            Sidebar(pane: $pane)
                .navigationSplitViewColumnWidth(min: 232, ideal: 258, max: 340)
        } detail: {
            switch pane {
            case .browse: DetailView()
            case .backup: BackupView(pane: $pane)
            case .setup:  SetupView(pane: $pane)
            }
        }
        .alert("Something went wrong",
               isPresented: Binding(get: { model.lastError != nil },
                                    set: { if !$0 { model.lastError = nil } })) {
            Button("OK", role: .cancel) { model.lastError = nil }
        } message: {
            Text(model.lastError ?? "")
        }
    }
}

struct Sidebar: View {
    @Environment(AppModel.self) private var model
    @Binding var pane: RootView.Pane

    var body: some View {
        @Bindable var model = model
        VStack(spacing: 0) {
            ConnectionHeader()
                .contentShape(Rectangle())
                .onTapGesture { pane = .setup }
                .padding(.horizontal, 10)
                .padding(.top, 10)

            List(selection: $model.selectedSnapshotID) {
                Section("Snapshots") {
                    if model.snapshots.isEmpty {
                        Text(model.connection.isConnected ? "No snapshots yet" : "Not connected")
                            .foregroundStyle(.secondary).font(.callout)
                    }
                    ForEach(model.snapshots) { snap in
                        SnapshotRow(snapshot: snap).tag(snap.id)
                    }
                }
            }
            .listStyle(.sidebar)
            .onChange(of: model.selectedSnapshotID) { _, id in
                pane = .browse
                Task { await model.selectSnapshot(id) }
            }

            Divider()
            VStack(spacing: 2) {
                SidebarButton(title: "Back Up a Folder…", systemImage: "arrow.up.circle.fill",
                              active: pane == .backup) { pane = .backup }
                SidebarButton(title: "Connection & Keys", systemImage: "key.fill",
                              active: pane == .setup) { pane = .setup }
            }
            .padding(8)
        }
    }
}

private struct SidebarButton: View {
    let title: String
    let systemImage: String
    let active: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Label(title, systemImage: systemImage)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 5).padding(.horizontal, 8)
                .background(active ? Color.accentColor.opacity(0.15) : .clear, in: RoundedRectangle(cornerRadius: 6))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

struct ConnectionHeader: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        HStack(spacing: 9) {
            Circle().fill(statusColor).frame(width: 9, height: 9)
                .shadow(color: statusColor.opacity(0.5), radius: 2)
            VStack(alignment: .leading, spacing: 1) {
                Text(statusText).font(.subheadline.weight(.semibold)).lineLimit(1)
                Text(model.repository.isEmpty ? "No repository set" : model.repository)
                    .font(.caption).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
            }
            Spacer(minLength: 4)
            Image(systemName: "chevron.right").font(.caption2.weight(.semibold)).foregroundStyle(.tertiary)
        }
        .padding(10)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 9))
    }

    private var statusColor: Color {
        switch model.connection {
        case .connected: return .green
        case .connecting: return .yellow
        case .failed: return .red
        case .unknown: return .secondary
        }
    }
    private var statusText: String {
        switch model.connection {
        case .connected(let v): return "Connected · PBS \(v)"
        case .connecting: return "Connecting…"
        case .failed: return "Disconnected"
        case .unknown: return "Not connected"
        }
    }
}

struct SnapshotRow: View {
    let snapshot: Snapshot

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon)
                .foregroundStyle(.tint).font(.title3).frame(width: 24)
            VStack(alignment: .leading, spacing: 1) {
                Text(snapshot.backupID).font(.body.weight(.medium)).lineLimit(1)
                Text(Fmt.relative(snapshot.backupTime)).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 4)
            if let size = snapshot.size, size > 0 {
                Text(Fmt.bytes(size)).font(.caption).foregroundStyle(.secondary).monospacedDigit()
            }
        }
        .padding(.vertical, 2)
    }

    private var icon: String {
        switch snapshot.backupType {
        case "host": return "desktopcomputer"
        case "vm": return "server.rack"
        case "ct": return "shippingbox.fill"
        default: return "externaldrive.fill"
        }
    }
}
