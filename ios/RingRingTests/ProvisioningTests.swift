import Foundation
import Testing
@testable import RingRing

struct ProvisioningTests {
    private let token = "abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"

    @Test func parsesRingRingDeepLink() throws {
        let nested = "https://family.example/provision/ios/\(token)"
        let escaped = try #require(nested.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed))
        let link = try #require(URL(string: "ringring://join?provision=\(escaped)"))

        #expect(try ProvisioningLink.provisioningURL(from: link).absoluteString == nested)
    }

    @Test func acceptsDirectSecureProvisioningLink() throws {
        let link = try #require(URL(string: "https://family.example/provision/ios/\(token)"))
        #expect(try ProvisioningLink.provisioningURL(from: link) == link)

        let openAPIPath = try #require(URL(string: "https://family.example/api/v1/phone-provisioning/\(token)"))
        #expect(try ProvisioningLink.provisioningURL(from: openAPIPath) == openAPIPath)
    }

    @Test func turnsGeneralInvitationIntoNativeInvitationEndpoint() throws {
        let invite = try #require(URL(string: "https://family.example/join/\(token)"))
        let parsed = try SetupLink.parse(invite)
        guard case .invitation(let link) = parsed else {
            Issue.record("general invitation was parsed as a provisioning link")
            return
        }
        #expect(link.sourceURL == invite)
        #expect(link.apiURL.absoluteString == "https://family.example/api/v1/phone-invitations/\(token)")
    }

    @Test func rejectsUnsafeGeneralInvitationLinks() throws {
        let queried = try #require(URL(string: "https://family.example/join/\(token)?preview=true"))
        let credentialed = try #require(URL(string: "https://name:secret@family.example/join/\(token)"))
        let short = try #require(URL(string: "https://family.example/join/short"))

        #expect(throws: ProvisioningError.invalidLink) { try SetupLink.parse(queried) }
        #expect(throws: ProvisioningError.invalidLink) { try SetupLink.parse(credentialed) }
        #expect(throws: ProvisioningError.invalidLink) { try SetupLink.parse(short) }
    }

    @Test func validatesInvitationPreviewAndExtensionSafetyRules() throws {
        let preview = PhoneInvitationPreview(version: 1, partyName: "Color Club", suggestedExtension: "103")
        #expect(try preview.validated() == preview)
        #expect(PhoneInvitationDetails.isExtension("10"))
        #expect(PhoneInvitationDetails.isExtension("203"))
        #expect(!PhoneInvitationDetails.isExtension("911"))
        #expect(!PhoneInvitationDetails.isExtension("988"))
        #expect(!PhoneInvitationDetails.isExtension("123456"))
        #expect(!PhoneInvitationDetails.isExtension("２０３"))

        let unsafe = PhoneInvitationPreview(version: 1, partyName: "Color\nClub", suggestedExtension: "103")
        #expect(throws: InvitationError.invalidResponse) { try unsafe.validated() }
    }

    @Test func invitationClaimUsesDocumentedJSONFields() throws {
		let claim = PhoneInvitationClaim(displayName: "Studio phone", extension: "103", deviceLabel: "iPhone app")
        let encoded = try JSONEncoder().encode(claim)
        let object = try #require(JSONSerialization.jsonObject(with: encoded) as? [String: Any])
        #expect(object["display_name"] as? String == "Studio phone")
        #expect(object["extension"] as? String == "103")
		#expect(object["device_label"] as? String == "iPhone app")
		#expect(object.count == 3)
    }

    @Test func rejectsCredentialsQueriesAndUnrelatedPaths() throws {
        let credentialed = try #require(URL(string: "https://name:secret@family.example/provision/ios/\(token)"))
        let queried = try #require(URL(string: "https://family.example/provision/ios/\(token)?copy=true"))
        let unrelated = try #require(URL(string: "https://family.example/api/v2/phone-provisioning/\(token)"))

        #expect(throws: ProvisioningError.invalidLink) { try ProvisioningLink.provisioningURL(from: credentialed) }
        #expect(throws: ProvisioningError.invalidLink) { try ProvisioningLink.provisioningURL(from: queried) }
        #expect(throws: ProvisioningError.invalidLink) { try ProvisioningLink.provisioningURL(from: unrelated) }
    }

    @Test func validatesExpectedTLSAccount() throws {
        let account = SIPAccount(
            server: "ring.example",
            port: 5061,
            transport: "tls",
            username: "device_01",
            password: "a-long-random-secret",
            extension: "203"
        )
        #expect(throws: Never.self) { try account.validate() }
    }

    @Test func validatesNamedPartyCallMenu() throws {
        let account = SIPAccount(
            server: "ring.example",
            port: 5061,
            transport: "tls",
            username: "device_01",
            password: "a-long-random-secret",
            extension: "203"
        )
        let document = ProvisioningDocument(version: 1, sip: account, destinations: [
            DialDestination(kind: .call, label: "Join Green + Gold", detail: "2 phones are talking now.", dial: "*16204"),
            DialDestination(kind: .person, label: "Green phone", detail: nil, dial: "204"),
            DialDestination(kind: .service, label: "Local weather", detail: "Hear the host’s chosen forecast.", dial: "*12"),
        ])

        let phone = try document.validated()
        #expect(phone.destinations.map(\.label) == ["Join Green + Gold", "Green phone", "Local weather"])
        #expect(phone.destinations.map(\.dial) == ["*16204", "204", "*12"])
    }

    @Test func olderProvisioningPayloadGetsSafeDefaultButtons() throws {
        let payload = """
        {
          "version": 1,
          "sip": {
            "server": "ring.example",
            "port": 5061,
            "transport": "tls",
            "username": "device_01",
            "password": "a-long-random-secret",
            "extension": "203"
          }
        }
        """

        let document = try JSONDecoder().decode(ProvisioningDocument.self, from: Data(payload.utf8))
        #expect(try document.validated().destinations == DialDestination.alwaysAvailable)
    }

    @Test func rejectsMalformedOrDuplicateMenuChoices() {
        let account = SIPAccount(server: "ring.example", port: 5061, transport: "tls", username: "device_01", password: "secret", extension: "203")
        let malformed = ProvisioningDocument(version: 1, sip: account, destinations: [
            DialDestination(kind: .person, label: "Outside\nparty", detail: nil, dial: "204"),
        ])
        let duplicate = ProvisioningDocument(version: 1, sip: account, destinations: [
            DialDestination(kind: .person, label: "Green phone", detail: nil, dial: "204"),
            DialDestination(kind: .service, label: "Wrong duplicate", detail: nil, dial: "204"),
        ])

        #expect(throws: ProvisioningError.invalidDestination) { try malformed.validated() }
        #expect(throws: ProvisioningError.invalidDestination) { try duplicate.validated() }
    }

    @Test func destinationLimitsCountUnicodeScalars() {
        let account = SIPAccount(server: "ring.example", port: 5061, transport: "tls", username: "device_01", password: "secret", extension: "203")
        let tooManyScalars = String(repeating: "e\u{301}", count: 41)
        let document = ProvisioningDocument(version: 1, sip: account, destinations: [
            DialDestination(kind: .person, label: tooManyScalars, detail: nil, dial: "204"),
        ])

        #expect(throws: ProvisioningError.invalidDestination) { try document.validated() }
    }

    @Test func rejectsInsecureOrMalformedAccounts() {
        let insecure = SIPAccount(server: "ring.example", port: 5060, transport: "udp", username: "device_01", password: "secret", extension: "203")
        let invalidIdentity = SIPAccount(server: "ring.example", port: 5061, transport: "tls", username: "a person", password: "secret", extension: "203")
        let invalidExtension = SIPAccount(server: "ring.example", port: 5061, transport: "tls", username: "device_01", password: "secret", extension: "911911")

        #expect(throws: ProvisioningError.insecureTransport) { try insecure.validate() }
        #expect(throws: ProvisioningError.invalidAccount) { try invalidIdentity.validate() }
        #expect(throws: ProvisioningError.invalidAccount) { try invalidExtension.validate() }
    }

    @Test(arguments: ["10", "203", "99999", "*98", "*16102", "*1699999"])
    func callableDialStrings(value: String) {
        #expect(DialString.isCallable(value))
    }

    @Test(arguments: ["", "1", "123456", "#", "*9", "*161", "*16123456", "20#", "person"])
    func rejectedDialStrings(value: String) {
        #expect(!DialString.isCallable(value))
    }

    @Test func validatesRefreshedPhoneStateAndLiveCalls() throws {
        let payload = """
        {
          "version": 1,
          "extension": "203",
          "destinations": [
            {"kind":"call","label":"Join Green + Gold","detail":"2 phones are talking now.","dial":"*16204"},
            {"kind":"person","label":"Green phone","dial":"204"}
          ]
        }
        """
        let state = try JSONDecoder().decode(PhoneStateDocument.self, from: Data(payload.utf8))
        #expect(try state.validated().destinations.first?.kind == .call)

        let invalid = PhoneStateDocument(version: 1, extension: "203", destinations: [
            DialDestination(kind: .call, label: "Wrong join", detail: nil, dial: "*10")
        ])
        #expect(throws: PhoneAPIError.invalidResponse) { try invalid.validated() }
    }
}
