import SwiftUI
import AppKit

// Shows the equivalent `pbmac` command for an action with a one-click copy, so
// anything done in the GUI can be reproduced in a terminal or the console.
struct CommandChip: View {
    let command: String
    @State private var copied = false

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "terminal").font(.caption2).foregroundStyle(.secondary)
            Text(command)
                .font(.system(.caption, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1).truncationMode(.middle)
                .textSelection(.enabled)
            Spacer(minLength: 6)
            Button(action: copy) {
                Image(systemName: copied ? "checkmark" : "doc.on.doc")
                    .foregroundStyle(copied ? Color.green : Color.secondary)
            }
            .buttonStyle(.borderless)
            .help("Copy command")
        }
        .padding(.horizontal, 9).padding(.vertical, 6)
        .background(.quaternary.opacity(0.4), in: RoundedRectangle(cornerRadius: 7))
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(command, forType: .string)
        copied = true
    }
}
