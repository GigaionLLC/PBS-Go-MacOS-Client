import Foundation
import UniformTypeIdentifiers

// Finder-native "drag a file out to restore it": a browser row is dragged as a
// lazy file promise. When the user drops it (on the Desktop, a Finder window,
// etc.) the load handler restores just that path into a temp dir and hands the
// URL back, and the system copies it to the drop location.
//
// NOTE: verify on-device — file-promise plumbing can only be exercised on macOS.
// The Restore… button is the always-available equivalent.
enum DragOut {
    static func itemProvider(for node: TreeNode, model: AppModel) -> NSItemProvider {
        let provider = NSItemProvider()
        provider.suggestedName = node.name
        let type = node.isDir
            ? UTType.folder
            : (UTType(filenameExtension: (node.name as NSString).pathExtension) ?? .data)

        provider.registerFileRepresentation(
            forTypeIdentifier: type.identifier, fileOptions: [], visibility: .all
        ) { completion in
            let progress = Progress(totalUnitCount: 1)
            Task {
                do {
                    let url = try await restoreToTemp(node, model: model)
                    completion(url, false, nil)     // false: not in-place, let the system copy it
                } catch {
                    completion(nil, false, error)
                }
                progress.completedUnitCount = 1
            }
            return progress
        }
        return provider
    }

    @MainActor
    private static func restoreToTemp(_ node: TreeNode, model: AppModel) async throws -> URL {
        let fm = FileManager.default
        let base = URL(fileURLWithPath: NSTemporaryDirectory(), isDirectory: true)
        // Reclaim scratch dirs left by earlier drags (the system copies the item
        // out, so our temp copy would otherwise linger until the OS purges it).
        if let leftovers = try? fm.contentsOfDirectory(at: base, includingPropertiesForKeys: nil) {
            for url in leftovers where url.lastPathComponent.hasPrefix("pbmac-drag-") {
                try? fm.removeItem(at: url)
            }
        }
        let dir = base.appendingPathComponent("pbmac-drag-\(UUID().uuidString)", isDirectory: true)
        try fm.createDirectory(at: dir, withIntermediateDirectories: true)
        _ = try await model.restore(to: dir, filePath: node.path)

        // pbmac reconstructs the archive path under --target; locate the item.
        let relative = String(node.path.drop(while: { $0 == "/" }))
        let expected = dir.appendingPathComponent(relative)
        if FileManager.default.fileExists(atPath: expected.path) { return expected }
        if let found = firstMatch(named: node.name, under: dir) { return found }
        return expected
    }

    private static func firstMatch(named name: String, under dir: URL) -> URL? {
        guard let e = FileManager.default.enumerator(at: dir, includingPropertiesForKeys: nil) else { return nil }
        for case let url as URL in e where url.lastPathComponent == name { return url }
        return nil
    }
}
