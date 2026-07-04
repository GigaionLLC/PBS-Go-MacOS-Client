import SwiftUI

// A node in the browsable file tree built from `restore --list` output. The flat
// list of archive paths is folded into a tree the Finder-style column browser
// walks. Reference identity keeps it cheap to pass around SwiftUI views.
final class TreeNode: Identifiable, Hashable {
    let name: String
    let path: String          // absolute path within the archive, e.g. /Documents/a.txt
    let isDir: Bool
    let entry: FileEntry?     // nil for synthesized intermediate directories
    private(set) var children: [TreeNode] = []

    init(name: String, path: String, isDir: Bool, entry: FileEntry?) {
        self.name = name
        self.path = path
        self.isDir = isDir
        self.entry = entry
    }

    var id: String { path }
    static func == (lhs: TreeNode, rhs: TreeNode) -> Bool { lhs.path == rhs.path }
    func hash(into hasher: inout Hasher) { hasher.combine(path) }

    var isSymlink: Bool { entry?.isSymlink ?? false }
    var size: Int64? { entry?.size }

    var systemImage: String {
        if isDir { return "folder.fill" }
        if isSymlink { return "arrow.up.forward.square" }
        switch (name as NSString).pathExtension.lowercased() {
        case "png", "jpg", "jpeg", "gif", "heic", "tiff", "webp": return "photo"
        case "pdf": return "doc.richtext"
        case "zip", "gz", "tar", "dmg", "pxar": return "archivebox"
        case "mp4", "mov", "m4v", "avi": return "film"
        case "mp3", "m4a", "wav", "aac", "flac": return "music.note"
        default: return "doc"
        }
    }

    var iconColor: Color { isDir ? .accentColor : .secondary }

    private func addChild(_ node: TreeNode) { children.append(node) }

    private func sortRecursively() {
        children.sort { a, b in
            if a.isDir != b.isDir { return a.isDir }               // directories first
            return a.name.localizedStandardCompare(b.name) == .orderedAscending
        }
        children.forEach { $0.sortRecursively() }
    }

    /// Builds a root node (path "/") from a flat list of archive entries.
    static func build(from entries: [FileEntry]) -> TreeNode {
        let root = TreeNode(name: "/", path: "/", isDir: true, entry: nil)
        var dirs: [String: TreeNode] = ["/": root]

        // Parent directories sort before their children, so a single pass suffices.
        for entry in entries.sorted(by: { $0.path < $1.path }) {
            let comps = entry.path.split(separator: "/").map(String.init)
            guard !comps.isEmpty else { continue }
            var parent = root
            var acc = ""
            for (i, comp) in comps.enumerated() {
                acc += "/" + comp
                let isLast = i == comps.count - 1
                if isLast {
                    if entry.isDir {
                        if dirs[acc] == nil {
                            let node = TreeNode(name: comp, path: acc, isDir: true, entry: entry)
                            parent.addChild(node)
                            dirs[acc] = node
                        }
                    } else {
                        parent.addChild(TreeNode(name: comp, path: acc, isDir: false, entry: entry))
                    }
                } else if let existing = dirs[acc] {
                    parent = existing
                } else {
                    let node = TreeNode(name: comp, path: acc, isDir: true, entry: nil)
                    parent.addChild(node)
                    dirs[acc] = node
                    parent = node
                }
            }
        }
        root.sortRecursively()
        return root
    }

    /// Total number of file leaves under this node (for "N items" summaries).
    var leafCount: Int {
        isDir ? children.reduce(0) { $0 + $1.leafCount } : 1
    }
}
