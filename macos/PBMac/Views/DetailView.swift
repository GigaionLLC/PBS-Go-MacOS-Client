import SwiftUI

// The snapshot-browsing pane: a toolbar (archive picker + Restore) above the
// Finder-style column browser, with empty states when nothing is selected.
struct DetailView: View {
    @Environment(AppModel.self) private var model
    @State private var showRestore = false

    var body: some View {
        Group {
            if let snapshot = model.selectedSnapshot {
                VStack(spacing: 0) {
                    SnapshotToolbar(snapshot: snapshot, showRestore: $showRestore)
                    Divider()
                    content
                }
            } else {
                ContentUnavailableView(
                    "Select a Snapshot",
                    systemImage: "clock.arrow.circlepath",
                    description: Text("Choose a snapshot on the left to browse and restore its files."))
            }
        }
        .sheet(isPresented: $showRestore) { RestoreSheet() }
    }

    @ViewBuilder
    private var content: some View {
        if let tree = model.tree {
            ColumnBrowserView(root: tree).id(ObjectIdentifier(tree))
        } else if model.busy {
            ProgressView().controlSize(.large)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let archive = model.selectedArchive, !archive.isBrowsable {
            ContentUnavailableView(
                "Not Browsable",
                systemImage: "doc.questionmark",
                description: Text("“\(archive.displayName)” isn’t a file archive. Pick a .pxar archive to browse it."))
        } else {
            ContentUnavailableView(
                "Nothing to Show",
                systemImage: "tray",
                description: Text("This snapshot has no browsable archive."))
        }
    }
}

struct SnapshotToolbar: View {
    let snapshot: Snapshot
    @Environment(AppModel.self) private var model
    @Binding var showRestore: Bool

    var body: some View {
        HStack(spacing: 14) {
            VStack(alignment: .leading, spacing: 1) {
                Text(snapshot.backupID).font(.headline)
                Text(Fmt.date(snapshot.backupTime)).font(.caption).foregroundStyle(.secondary)
            }

            if !model.archives.isEmpty {
                // Custom binding: only a user-driven Picker change calls selectArchive.
                // Programmatic updates (selectSnapshot picking a default) flow through
                // `get` without re-triggering a load, avoiding a double restore --list.
                Picker("Archive", selection: Binding(
                    get: { model.selectedArchiveID },
                    set: { newID in Task { await model.selectArchive(newID) } }
                )) {
                    ForEach(model.archives) { archive in
                        Label(archive.displayName,
                              systemImage: archive.isEncrypted ? "lock.fill" : "doc")
                            .tag(Optional(archive.id))
                    }
                }
                .labelsHidden()
                .frame(maxWidth: 260)
            }

            Spacer(minLength: 8)

            if let focused = model.focusedNode {
                Label(focused.name, systemImage: focused.systemImage)
                    .font(.caption).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
            }

            Button {
                showRestore = true
            } label: {
                Label("Restore…", systemImage: "arrow.down.circle")
            }
            .disabled(model.tree == nil)
            .keyboardShortcut("r", modifiers: [.command, .shift])
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
    }
}
