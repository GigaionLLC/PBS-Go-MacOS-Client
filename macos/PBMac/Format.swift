import Foundation

// Small display-formatting helpers shared across views.
enum Fmt {
    static func bytes(_ n: Int64?) -> String {
        guard let n else { return "—" }
        return ByteCountFormatter.string(fromByteCount: n, countStyle: .file)
    }

    static func date(_ unix: Int) -> String {
        let f = DateFormatter()
        f.dateStyle = .medium
        f.timeStyle = .short
        return f.string(from: Date(timeIntervalSince1970: TimeInterval(unix)))
    }

    static func relative(_ unix: Int) -> String {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .abbreviated
        return f.localizedString(for: Date(timeIntervalSince1970: TimeInterval(unix)), relativeTo: Date())
    }

    /// "71%" from a 0…1 ratio.
    static func percent(_ ratio: Double) -> String {
        "\(Int((ratio * 100).rounded()))%"
    }
}
