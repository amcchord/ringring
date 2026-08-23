import AVFoundation
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var account: SIPAccount?
    @Published private(set) var destinations: [DialDestination] = []
    @Published var isProvisioning = false
    @Published var errorMessage: String?
    @Published var invitationErrorMessage: String?
    @Published var showingScanner = false
    @Published var showingSettings = false
    @Published var pendingInvitation: PendingInvitation?

    let phone = PhoneService()

    private let store = CredentialStore()
    private let client = ProvisioningClient()
    private let invitationClient = InvitationClient()

    init() {
#if DEBUG
        let arguments = ProcessInfo.processInfo.arguments
        if arguments.contains("--preview-invitation") {
            let token = String(repeating: "p", count: 43)
            let sourceURL = URL(string: "https://ringring.live/join/\(token)")!
            let link = try! PhoneInvitationLink(sourceURL: sourceURL)
            pendingInvitation = PendingInvitation(
                link: link,
                preview: PhoneInvitationPreview(version: 1, partyName: "Color Club", suggestedExtension: "103")
            )
            return
        }
        let previewsConfiguredPhone = arguments.contains("--preview-call-menu") || arguments.contains("--preview-active-call")
        if previewsConfiguredPhone {
            account = SIPAccount(server: "ringring.live", port: 5061, transport: "tls", username: "preview_phone", password: "preview-only", extension: "103")
            destinations = [
                DialDestination(kind: .person, label: "Kitchen phone", detail: nil, dial: "101"),
                DialDestination(kind: .person, label: "Workshop phone", detail: nil, dial: "102"),
                DialDestination(kind: .service, label: "Echo test", detail: "Hear your own voice come back.", dial: "*10"),
                DialDestination(kind: .service, label: "Local weather", detail: "Hear the host’s chosen forecast.", dial: "*12"),
                DialDestination(kind: .service, label: "Internet radio", detail: "Play the host’s chosen station.", dial: "*13"),
            ]
            phone.setCallDirectory(destinations)
            if arguments.contains("--preview-active-call") {
                phone.usePreviewActiveCall(to: "*10")
            } else {
                phone.usePreviewReadyState()
            }
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
        guard account == nil else {
            errorMessage = "This iPhone is already connected. Disconnect it in Phone settings before opening another invitation."
            return
        }
        do {
            try open(SetupLink.parse(value))
        } catch {
            errorMessage = (error as? LocalizedError)?.errorDescription
        }
    }

    func join(using text: String) {
        guard account == nil else {
            errorMessage = "This iPhone is already connected. Disconnect it in Phone settings before opening another invitation."
            return
        }
        do {
            try open(SetupLink.parse(text))
        } catch {
            errorMessage = (error as? LocalizedError)?.errorDescription
        }
    }

    func claimInvitation(displayName: String, extension extensionValue: String, adultExtension: Bool) {
        guard !isProvisioning, let invitation = pendingInvitation else { return }
        let normalizedName = PhoneInvitationDetails.normalizedName(displayName)
        let normalizedExtension = extensionValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard PhoneInvitationDetails.isText(normalizedName, maximum: 40),
              PhoneInvitationDetails.isExtension(normalizedExtension) else {
            invitationErrorMessage = InvitationError.invalidDetails.errorDescription
            return
        }
        isProvisioning = true
        invitationErrorMessage = nil
        Task {
            defer { isProvisioning = false }
            do {
                let provisioned = try await invitationClient.claim(PhoneInvitationClaim(
                    displayName: normalizedName,
                    extension: normalizedExtension,
                    adultExtension: adultExtension,
                    deviceLabel: "iPhone app"
                ), using: invitation.link)
                try install(provisioned)
                pendingInvitation = nil
            } catch InvitationError.extensionTaken {
                do {
                    let refreshed = try await invitationClient.preview(invitation.link)
                    pendingInvitation = PendingInvitation(link: invitation.link, preview: refreshed)
                    invitationErrorMessage = InvitationError.extensionTaken.errorDescription
                } catch {
                    pendingInvitation = nil
                    errorMessage = (error as? LocalizedError)?.errorDescription ?? InvitationError.serverRejected.errorDescription
                }
            } catch {
                invitationErrorMessage = (error as? LocalizedError)?.errorDescription ?? "RingRing couldn’t finish setup. Please try again."
            }
        }
    }

    func cancelInvitation() {
        guard !isProvisioning else { return }
        pendingInvitation = nil
        invitationErrorMessage = nil
    }

    private func open(_ setupLink: SetupLink) throws {
        guard !isProvisioning else { return }
        switch setupLink {
        case .provisioning(let url):
            provision(using: url)
        case .invitation(let link):
            preview(link)
        }
    }

    private func provision(using value: URL) {
        isProvisioning = true
        errorMessage = nil
        showingScanner = false
        Task {
            defer { isProvisioning = false }
            do {
                let provisioned = try await client.fetch(from: value)
                try install(provisioned)
            } catch {
                errorMessage = (error as? LocalizedError)?.errorDescription ?? "RingRing couldn’t finish setup. Please try again."
            }
        }
    }

    private func preview(_ link: PhoneInvitationLink) {
        isProvisioning = true
        errorMessage = nil
        invitationErrorMessage = nil
        showingScanner = false
        Task {
            defer { isProvisioning = false }
            do {
                let preview = try await invitationClient.preview(link)
                pendingInvitation = PendingInvitation(link: link, preview: preview)
            } catch {
                errorMessage = (error as? LocalizedError)?.errorDescription ?? "RingRing couldn’t open that invitation. Please try again."
            }
        }
    }

    private func install(_ provisioned: ProvisionedPhone) throws {
        try store.save(provisioned)
        account = provisioned.sip
        destinations = provisioned.destinations
        phone.setCallDirectory(provisioned.destinations)
        try phone.configure(with: provisioned.sip)
    }

    func disconnect() {
        do {
            try store.delete()
            phone.disconnect()
            account = nil
            destinations = []
            pendingInvitation = nil
            invitationErrorMessage = nil
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
