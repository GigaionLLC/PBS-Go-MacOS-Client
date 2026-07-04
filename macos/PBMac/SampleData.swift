#if DEBUG
import Foundation

// Fixtures for Xcode Previews. Shapes match `pbmac --json` (see docs/CLI-JSON.md),
// so previews exercise the same decoding path as the live app.
enum SampleData {
    static let snapshots: [Snapshot] = decode("""
    [{"backup-type":"host","backup-id":"mymac","backup-time":1719950400,"comment":"nightly","size":48910234},
     {"backup-type":"host","backup-id":"mymac","backup-time":1719864000,"size":48120990},
     {"backup-type":"host","backup-id":"studio","backup-time":1719777600,"size":41200110}]
    """)

    static let archives: [Archive] = decode("""
    [{"filename":"root.pxar.didx","crypt-mode":"encrypt","size":48910234,"csum":"abac12"},
     {"filename":"catalog.pcat1.didx","crypt-mode":"encrypt","size":10240,"csum":"beef34"}]
    """)

    static let entries: [FileEntry] = decode("""
    [{"path":"/Documents","type":"dir","mode":16877},
     {"path":"/Documents/report.pdf","type":"file","size":420112,"mode":33188},
     {"path":"/Documents/budget.xlsx","type":"file","size":88210,"mode":33188},
     {"path":"/Documents/Drafts","type":"dir","mode":16877},
     {"path":"/Documents/Drafts/notes.md","type":"file","size":12044,"mode":33188},
     {"path":"/Documents/Drafts/pitch.key","type":"file","size":9910233,"mode":33188},
     {"path":"/Pictures","type":"dir","mode":16877},
     {"path":"/Pictures/sunset.jpg","type":"file","size":3401221,"mode":33188},
     {"path":"/Pictures/team.png","type":"file","size":1802113,"mode":33188},
     {"path":"/.zshrc","type":"file","size":540,"mode":33188}]
    """)

    static func decode<T: Decodable>(_ json: String) -> T {
        try! JSONDecoder().decode(T.self, from: Data(json.utf8))
    }
}

@MainActor
extension AppModel {
    static var sample: AppModel {
        let model = AppModel()
        model.repository = "fslave32:8007:store"
        model.connection = .connected("4.2")
        model.snapshots = SampleData.snapshots
        model.selectedSnapshotID = SampleData.snapshots.first?.id
        model.archives = SampleData.archives
        model.selectedArchiveID = SampleData.archives.first?.id
        model.tree = TreeNode.build(from: SampleData.entries)
        return model
    }
}

#Preview("App") {
    RootView()
        .environment(AppModel.sample)
        .frame(width: 1000, height: 620)
}
#endif
