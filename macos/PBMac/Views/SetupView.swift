import SwiftUI
import AppKit

// Connection & Keys: server access (-> pbmac login) and the client encryption
// key (choose an existing keyfile or generate one via `pbmac key create`).
struct SetupView: View {
    @Environment(AppModel.self) private var model
    @State private var connecting = false

    var body: some View {
        @Bindable var model = model
        ScrollView {
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Connection & Keys").font(.title2.bold())
                    Text("Maps to `pbmac login` and your encryption key. The token and passphrase are held in the Keychain — never written to a plaintext file.")
                        .font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                }

                GroupBox("Server access") {
                    VStack(alignment: .leading, spacing: 12) {
                        LabeledInput("Repository", help: "host:port:datastore") {
                            TextField("fslave32.example.com:8007:store", text: $model.repository)
                                .textFieldStyle(.roundedBorder)
                        }
                        LabeledInput("API token", help: "Stored in the Keychain. Create one under Access Control → API Tokens.") {
                            SecureField("USER@REALM!TOKENID:SECRET", text: $model.token)
                                .textFieldStyle(.roundedBorder)
                        }
                        LabeledInput("Fingerprint", help: "SHA-256 of the server certificate, pinned for TLS. Auto-filled on first connect.") {
                            TextField("CB:28:5F:…", text: $model.fingerprint)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(8)
                }

                GroupBox("Encryption key") {
                    VStack(alignment: .leading, spacing: 12) {
                        LabeledInput("Key file", help: "Used to encrypt/decrypt backups client-side.") {
                            HStack {
                                Text(model.keyfilePath.isEmpty ? "None selected" : model.keyfilePath)
                                    .foregroundStyle(model.keyfilePath.isEmpty ? .secondary : .primary)
                                    .lineLimit(1).truncationMode(.middle)
                                    .frame(maxWidth: .infinity, alignment: .leading)
                                Button("Choose…", action: chooseKey)
                                Button("Generate…", action: generateKey)
                            }
                        }
                        LabeledInput("Passphrase", help: "Passed to pbmac as PBS_ENCRYPTION_PASSWORD. Leave blank for an unprotected key.") {
                            SecureField("unlocks a scrypt/PBKDF2-protected key", text: $model.passphrase)
                                .textFieldStyle(.roundedBorder)
                        }
                    }
                    .padding(8)
                }

                Label {
                    Text("Encryption happens on this Mac before anything is uploaded. **Keep a copy of the key somewhere safe — without it, encrypted snapshots can never be restored.**")
                        .font(.callout)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
                }
                .padding(12)
                .background(.orange.opacity(0.12), in: RoundedRectangle(cornerRadius: 10))

                HStack {
                    Button(action: connect) {
                        if connecting { ProgressView().controlSize(.small) } else { Text("Save & Connect") }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(model.repository.isEmpty || connecting)
                    Text("Runs `pbmac login`, then `ping`.").font(.caption).foregroundStyle(.secondary)
                }
            }
            .padding(24)
            .frame(maxWidth: 640, alignment: .leading)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private func connect() {
        connecting = true
        Task {
            await model.saveAndConnect()
            connecting = false
            if model.connection.isConnected { model.pane = .browse }
        }
    }

    private func chooseKey() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = true
        panel.canChooseDirectories = false
        panel.prompt = "Use Key"
        if panel.runModal() == .OK, let url = panel.url { model.setKeyfile(url.path) }
    }

    private func generateKey() {
        let panel = NSSavePanel()
        panel.nameFieldStringValue = "encryption-key.json"
        panel.prompt = "Generate Key"
        panel.message = "Choose where to save the new encryption key"
        guard panel.runModal() == .OK, let url = panel.url else { return }
        Task {
            do { try await model.createKey(at: url.path) }
            catch { model.lastError = (error as? PBMacError)?.message ?? error.localizedDescription }
        }
    }
}

private struct LabeledInput<Content: View>: View {
    let label: String
    var help: String?
    @ViewBuilder let content: Content

    init(_ label: String, help: String? = nil, @ViewBuilder content: () -> Content) {
        self.label = label
        self.help = help
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label).font(.subheadline.weight(.medium))
            content
            if let help { Text(help).font(.caption).foregroundStyle(.secondary) }
        }
    }
}
