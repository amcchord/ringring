import AVFoundation
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var account: SIPAccount?
    @Published private(set) var destinations: [DialDestination] = []
    @Published var isProvisioning = false
    @Published var errorMessage: String?
    @Published var showingScanner = false
    @Published var showingSettings = false

    let phone = PhoneService()

    private let store = CredentialStore()
    private let client = ProvisioningClient()

    init() {
#if DEBUG
        if ProcessInfo.processInfo.arguments.contains("--preview-call-menu") {
            account = SIPAccount(server: "preview.invalid", port: 5061, transport: "tls", username: "preview_phone", password: "preview-only", extension: "103")
            destinations = [
                DialDestination(kind: .person, label: "Kitchen phone", detail: nil, dial: "101"),
                DialDestination(kind: .person, label: "Workshop phone", detail: nil, dial: "102"),
                DialDestination(kind: .service, label: "Echo test", detail: "Hear your own voice come back.", dial: "*10"),
                DialDestination(kind: .service, label: "Local weather", detail: "Hear the host’s chosen forecast.", dial: "*12"),
                DialDestination(kind: .service, label: "Internet radio", detail: "Play the host’s chosen station.", dial: "*13"),
            ]
            phone.usePreviewReadyState()
            return
        }
#endif
        do {
            if let saved = try store.load() {
                let validated = try saved.validated()
                account = validated.sip
                destinations = validated.destinations
                phone.setCallDirectory(validated.destinations)
                try phone.configure(with: validated.sip)
            }
        } catch {
            errorMessage = "RingRing couldn’t restore this phone’s secure setup. Scan a fresh setup code."
        }
    }

    func join(using value: URL) {
        guard !isProvisioning else { return }
        isProvisioning = true
        errorMessage = nil
        Task {
            defer { isProvisioning = false }
            do {
                let provisioned = try await client.fetch(from: value)
                try store.save(provisioned)
                account = provisioned.sip
                destinations = provisioned.destinations
                phone.setCallDirectory(provisioned.destinations)
                try phone.configure(with: provisioned.sip)
                showingScanner = false
            } catch {
                errorMessage = (error as? LocalizedError)?.errorDescription ?? "RingRing couldn’t finish setup. Please try again."
            }
        }
    }

    func join(using text: String) {
        do {
            let url = try ProvisioningLink.provisioningURL(from: text)
            join(using: url)
        } catch {
            errorMessage = (error as? LocalizedError)?.errorDescription
        }
    }

    func disconnect() {
        do {
            try store.delete()
            phone.disconnect()
            account = nil
            destinations = []
            showingSettings = false
        } catch {
            errorMessage = "RingRing couldn’t remove this phone’s secure setup."
        }
    }

    func refresh() {
        phone.refresh()
    }

    func callLabel(for dialTarget: String) -> String? {
        destinations.first(where: { $0.dial == dialTarget })?.label
    }
}
