import SwiftUI

@main
struct PBMacApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .frame(minWidth: 860, minHeight: 540)
                .task { await model.connect() }
        }
        .windowToolbarStyle(.unified)
        .commands {
            CommandGroup(after: .newItem) {
                Button("Refresh Snapshots") { Task { await model.loadSnapshots() } }
                    .keyboardShortcut("r")
            }
        }
    }
}
