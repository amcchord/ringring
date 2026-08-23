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
            DialDestination(kind: .person, label: "Green phone", detail: nil, dial: "204"),
            DialDestination(kind: .service, label: "Local weather", detail: "Hear the host’s chosen forecast.", dial: "*12"),
        ])

        let phone = try document.validated()
        #expect(phone.destinations.map(\.label) == ["Green phone", "Local weather"])
        #expect(phone.destinations.map(\.dial) == ["204", "*12"])
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

    @Test(arguments: ["10", "203", "99999", "*98"])
    func callableDialStrings(value: String) {
        #expect(DialString.isCallable(value))
    }

    @Test(arguments: ["", "1", "123456", "#", "*9", "20#", "person"])
    func rejectedDialStrings(value: String) {
        #expect(!DialString.isCallable(value))
    }
}
