import SwiftUI

// Finder-style Miller columns. `chain` is the list of opened directories
// (root-first); a new column appears to the right for each. Tapping a directory
// drills in; tapping a file focuses it. Rows are draggable out to Finder
// (see DragOut), which restores them on drop.
struct ColumnBrowserView: View {
    @Environment(AppModel.self) private var model
    let root: TreeNode
    @State private var chain: [TreeNode] = []

    private var columns: [[TreeNode]] {
        var cols = [root.children]
        for dir in chain { cols.append(dir.children) }
        return cols
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: 0) {
                    ForEach(Array(columns.enumerated()), id: \.offset) { index, items in
                        column(items, index: index).id(index)
                        Divider()
                    }
                }
                .frame(maxHeight: .infinity, alignment: .top)
            }
            .onChange(of: chain.count) { _, count in
                withAnimation(.easeOut(duration: 0.2)) { proxy.scrollTo(count, anchor: .trailing) }
            }
        }
    }

    // A column is a vertically-scrolling stack (not a List) so it sizes cleanly
    // inside the horizontal scroller and fills the available height.
    private func column(_ items: [TreeNode], index: Int) -> some View {
        let selectedID = selection(inColumn: index)
        return ScrollView(.vertical) {
            LazyVStack(spacing: 1) {
                if items.isEmpty {
                    Text("Empty folder")
                        .foregroundStyle(.tertiary).font(.callout)
                        .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                }
                ForEach(items) { node in
                    ColumnRow(node: node, selected: node.id == selectedID)
                        .contentShape(Rectangle())
                        .onTapGesture { open(node, column: index) }
                        .onDrag { DragOut.itemProvider(for: node, model: model) }
                }
            }
            .padding(.vertical, 4).padding(.horizontal, 6)
        }
        .frame(width: 252)
        .frame(maxHeight: .infinity)
    }

    private func selection(inColumn index: Int) -> TreeNode.ID? {
        if index < chain.count { return chain[index].id }
        if let focused = model.focusedNode, columns.indices.contains(index),
           columns[index].contains(where: { $0.id == focused.id }) {
            return focused.id
        }
        return nil
    }

    private func open(_ node: TreeNode, column index: Int) {
        chain = Array(chain.prefix(index))
        model.focusedNode = node
        if node.isDir { chain.append(node) }
    }
}

struct ColumnRow: View {
    let node: TreeNode
    let selected: Bool

    var body: some View {
        HStack(spacing: 7) {
            Image(systemName: node.systemImage)
                .foregroundStyle(selected ? Color.white : node.iconColor)
                .frame(width: 17)
            Text(node.name)
                .lineLimit(1).truncationMode(.middle)
                .foregroundStyle(selected ? Color.white : Color.primary)
            Spacer(minLength: 4)
            if node.isDir {
                Image(systemName: "chevron.right")
                    .font(.caption2)
                    .foregroundStyle(selected ? Color.white.opacity(0.8) : Color.secondary)
            } else if let size = node.size {
                Text(Fmt.bytes(size))
                    .font(.caption).monospacedDigit()
                    .foregroundStyle(selected ? Color.white.opacity(0.85) : Color.secondary)
            }
        }
        .padding(.vertical, 3).padding(.horizontal, 6)
        .background(selected ? Color.accentColor : Color.clear, in: RoundedRectangle(cornerRadius: 5))
    }
}
