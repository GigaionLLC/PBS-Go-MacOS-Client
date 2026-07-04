import SwiftUI

// An in-app pbmac terminal. Commands run against the exact bundled binary the
// rest of the app uses, so the GUI's command surface *is* the CLI's — not a copy.
struct ConsoleView: View {
    @Environment(AppModel.self) private var model
    @State private var input = ""

    private let examples = ["version", "ping", "list", "key create --kdf none --keyfile /tmp/k.json"]

    var body: some View {
        VStack(spacing: 0) {
            header
            Divider()
            scrollback
            Divider()
            inputBar
        }
    }

    private var header: some View {
        HStack {
            VStack(alignment: .leading, spacing: 1) {
                Text("Console").font(.headline)
                Text("Runs the bundled pbmac — the same binary every screen uses.")
                    .font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if !model.consoleLog.isEmpty {
                Button("Clear") { model.clearConsole() }.controlSize(.small)
            }
        }
        .padding(.horizontal, 16).padding(.vertical, 10)
    }

    private var scrollback: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if model.consoleLog.isEmpty {
                        emptyState
                    }
                    ForEach(model.consoleLog) { entry in
                        ConsoleRow(entry: entry).id(entry.id)
                    }
                }
                .padding(14)
            }
            .onChange(of: model.consoleLog.count) { _, _ in
                if let last = model.consoleLog.last {
                    withAnimation { proxy.scrollTo(last.id, anchor: .bottom) }
                }
            }
        }
    }

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("Type a pbmac command and press Return. Try:")
                .foregroundStyle(.secondary)
            ForEach(examples, id: \.self) { example in
                Button { input = example } label: {
                    Text("pbmac \(example)")
                        .font(.system(.callout, design: .monospaced))
                        .foregroundStyle(.tint)
                }
                .buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.vertical, 20)
    }

    private var inputBar: some View {
        HStack(spacing: 8) {
            Text("pbmac")
                .font(.system(.body, design: .monospaced))
                .foregroundStyle(.secondary)
            TextField("list  ·  backup --keyfile … root.pxar:/data  ·  key create …", text: $input)
                .textFieldStyle(.plain)
                .font(.system(.body, design: .monospaced))
                .onSubmit(run)
            Button("Run", action: run)
                .disabled(input.trimmingCharacters(in: .whitespaces).isEmpty)
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
    }

    private func run() {
        let line = input.trimmingCharacters(in: .whitespaces)
        guard !line.isEmpty else { return }
        input = ""
        Task { await model.runConsole(line) }
    }
}

private struct ConsoleRow: View {
    let entry: ConsoleEntry

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Image(systemName: entry.ok ? "chevron.right" : "xmark.octagon.fill")
                    .font(.caption)
                    .foregroundStyle(entry.ok ? Color.secondary : Color.red)
                Text("pbmac \(entry.command)")
                    .font(.system(.callout, design: .monospaced))
                    .textSelection(.enabled)
            }
            if !entry.output.isEmpty {
                Text(entry.output)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(entry.ok ? Color.secondary : Color.red)
                    .textSelection(.enabled)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
