import Foundation

struct ProvisioningDocument: Decodable, Equatable, Sendable {
    let version: Int
    let sip: SIPAccount
    let destinations: [DialDestination]

    private enum CodingKeys: String, CodingKey {
        case version
        case sip
        case destinations
    }

    init(version: Int, sip: SIPAccount, destinations: [DialDestination]) {
        self.version = version
        self.sip = sip
        self.destinations = destinations
    }

    init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        version = try values.decode(Int.self, forKey: .version)
        sip = try values.decode(SIPAccount.self, forKey: .sip)
        destinations = try values.decodeIfPresent([DialDestination].self, forKey: .destinations) ?? DialDestination.alwaysAvailable
    }

    func validated() throws -> ProvisionedPhone {
        guard version == 1 else {
            throw ProvisioningError.unsupportedVersion
        }
        try sip.validate()
        guard destinations.count <= 128 else {
            throw ProvisioningError.invalidDestination
        }
        var dialTargets = Set<String>()
        for destination in destinations {
            try destination.validate()
            guard dialTargets.insert(destination.dial).inserted else {
                throw ProvisioningError.invalidDestination
            }
        }
        return ProvisionedPhone(sip: sip, destinations: destinations)
    }
}

struct ProvisionedPhone: Codable, Equatable, Sendable {
    let sip: SIPAccount
    let destinations: [DialDestination]

    func validated() throws -> ProvisionedPhone {
        try sip.validate()
        guard destinations.count <= 128 else {
            throw ProvisioningError.invalidDestination
        }
        var dialTargets = Set<String>()
        for destination in destinations {
            try destination.validate()
            guard dialTargets.insert(destination.dial).inserted else {
                throw ProvisioningError.invalidDestination
            }
        }
        return self
    }

    static func legacy(sip: SIPAccount) -> ProvisionedPhone {
        ProvisionedPhone(sip: sip, destinations: DialDestination.alwaysAvailable)
    }
}

enum DialDestinationKind: String, Codable, Sendable {
    case person
    case service
    case call
}

struct DialDestination: Codable, Equatable, Identifiable, Sendable {
    let kind: DialDestinationKind
    let label: String
    let detail: String?
    let dial: String

    var id: String { dial }

    func validate() throws {
        guard Self.validText(label, maximum: 80, optional: false),
              Self.validText(detail ?? "", maximum: 160, optional: true),
              DialString.isCallable(dial) else {
            throw ProvisioningError.invalidDestination
        }
        if kind == .call,
           dial.range(of: #"^\*16[0-9]{2,5}$"#, options: .regularExpression) == nil {
            throw ProvisioningError.invalidDestination
        }
    }

    static let alwaysAvailable = [
        DialDestination(kind: .service, label: "Echo test", detail: "Hear your own voice come back.", dial: "*10"),
        DialDestination(kind: .service, label: "Pick another extension", detail: "Choose a new number by phone.", dial: "*15"),
    ]

    private static func validText(_ value: String, maximum: Int, optional: Bool) -> Bool {
        if value.isEmpty { return optional }
        guard value.unicodeScalars.count <= maximum,
              value.trimmingCharacters(in: .whitespacesAndNewlines) == value else {
            return false
        }
        return value.unicodeScalars.allSatisfy { !CharacterSet.controlCharacters.contains($0) }
    }
}

struct SIPAccount: Codable, Equatable, Sendable {
    let server: String
    let port: Int
    let transport: String
    let username: String
    let password: String
    let `extension`: String

    func validate() throws {
        guard Self.matches(server, #"^(?=.{1,253}$)(?:[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:]+\])$"#),
              !server.contains("..") else {
            throw ProvisioningError.invalidAccount
        }
        guard port == 5061, transport.lowercased() == "tls" else {
            throw ProvisioningError.insecureTransport
        }
        guard Self.matches(username, #"^[A-Za-z0-9_-]{1,128}$"#),
              (1...256).contains(password.utf8.count),
              password.unicodeScalars.allSatisfy({ !CharacterSet.controlCharacters.contains($0) }),
              Self.matches(`extension`, #"^[0-9]{2,5}$"#) else {
            throw ProvisioningError.invalidAccount
        }
    }

    private static func matches(_ value: String, _ pattern: String) -> Bool {
        value.range(of: pattern, options: .regularExpression) != nil
    }
}

enum ProvisioningLink {
    static func provisioningURL(from candidate: URL) throws -> URL {
        if candidate.scheme?.lowercased() == "ringring" {
            guard candidate.host?.lowercased() == "join",
                  let components = URLComponents(url: candidate, resolvingAgainstBaseURL: false),
                  let encoded = components.queryItems?.first(where: { $0.name == "provision" })?.value,
                  let nested = URL(string: encoded) else {
                throw ProvisioningError.invalidLink
            }
            return try validateProvisioningURL(nested)
        }

        return try validateProvisioningURL(candidate)
    }

    static func provisioningURL(from candidate: String) throws -> URL {
        guard let url = URL(string: candidate.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            throw ProvisioningError.invalidLink
        }
        return try provisioningURL(from: url)
    }

    private static func validateProvisioningURL(_ url: URL) throws -> URL {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              components.host != nil,
              components.user == nil,
              components.password == nil,
              components.query == nil,
              components.fragment == nil else {
            throw ProvisioningError.invalidLink
        }

        let secure = components.scheme?.lowercased() == "https"
#if DEBUG
        let localHost = ["localhost", "127.0.0.1", "::1"].contains(components.host?.lowercased() ?? "")
        let allowedScheme = secure || (components.scheme?.lowercased() == "http" && localHost)
#else
        let allowedScheme = secure
#endif
        let supportedPath = components.path.range(of: #"^/api/v1/phone-provisioning/[A-Za-z0-9_-]{43}$"#, options: .regularExpression) != nil ||
            components.path.range(of: #"^/provision/ios/[A-Za-z0-9_-]{43}$"#, options: .regularExpression) != nil
        guard allowedScheme, supportedPath else {
            throw ProvisioningError.invalidLink
        }
        return url
    }
}

enum ProvisioningError: LocalizedError, Equatable {
    case invalidLink
    case expired
    case serverRejected
    case responseTooLarge
    case invalidResponse
    case unsupportedVersion
    case insecureTransport
    case invalidAccount
    case invalidDestination

    var errorDescription: String? {
        switch self {
        case .invalidLink:
            return "That isn’t a RingRing setup code. Ask your host for a fresh one."
        case .expired:
            return "That setup code has expired or was already used. Ask your host for a new one."
        case .serverRejected:
            return "RingRing couldn’t complete this setup. Please try a fresh code."
        case .responseTooLarge, .invalidResponse, .unsupportedVersion, .insecureTransport, .invalidAccount, .invalidDestination:
            return "That setup code isn’t valid. Ask your host for a fresh one."
        }
    }
}

final class ProvisioningClient: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private static let maximumResponseSize = 256 * 1024

    func fetch(from candidate: URL) async throws -> ProvisionedPhone {
        let url = try ProvisioningLink.provisioningURL(from: candidate)
        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil

        let session = URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
        defer { session.finishTasksAndInvalidate() }

        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData

        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw ProvisioningError.invalidResponse
        }
        if http.statusCode == 404 || http.statusCode == 410 {
            throw ProvisioningError.expired
        }
        guard http.statusCode == 200 else {
            throw ProvisioningError.serverRejected
        }
        guard data.count <= Self.maximumResponseSize else {
            throw ProvisioningError.responseTooLarge
        }
        if let mimeType = http.mimeType, mimeType != "application/json" {
            throw ProvisioningError.invalidResponse
        }

        do {
            return try JSONDecoder().decode(ProvisioningDocument.self, from: data).validated()
        } catch let error as ProvisioningError {
            throw error
        } catch {
            throw ProvisioningError.invalidResponse
        }
    }

    func urlSession(
        _ session: URLSession,
        task: URLSessionTask,
        willPerformHTTPRedirection response: HTTPURLResponse,
        newRequest request: URLRequest,
        completionHandler: @escaping (URLRequest?) -> Void
    ) {
        completionHandler(nil)
    }
}
