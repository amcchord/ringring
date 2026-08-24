import Foundation

enum SetupLink: Equatable {
    case provisioning(URL)
    case invitation(PhoneInvitationLink)

    static func parse(_ candidate: URL) throws -> SetupLink {
        if candidate.scheme?.lowercased() == "ringring" {
            return .provisioning(try ProvisioningLink.provisioningURL(from: candidate))
        }
        if candidate.path.hasPrefix("/join/") {
            return .invitation(try PhoneInvitationLink(sourceURL: candidate))
        }
        return .provisioning(try ProvisioningLink.provisioningURL(from: candidate))
    }

    static func parse(_ candidate: String) throws -> SetupLink {
        guard let url = URL(string: candidate.trimmingCharacters(in: .whitespacesAndNewlines)) else {
            throw ProvisioningError.invalidLink
        }
        return try parse(url)
    }
}

struct PhoneInvitationLink: Equatable, Sendable {
    let sourceURL: URL
    let apiURL: URL

    init(sourceURL: URL) throws {
        guard var components = URLComponents(url: sourceURL, resolvingAgainstBaseURL: false),
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
        let pathParts = components.path.split(separator: "/", omittingEmptySubsequences: true)
        guard allowedScheme,
              pathParts.count == 2,
              pathParts[0] == "join",
              PhoneInvitationDetails.isToken(String(pathParts[1])) else {
            throw ProvisioningError.invalidLink
        }
        components.path = "/api/v1/phone-invitations/\(pathParts[1])"
        guard let apiURL = components.url else {
            throw ProvisioningError.invalidLink
        }
        self.sourceURL = sourceURL
        self.apiURL = apiURL
    }
}

struct PhoneInvitationPreview: Decodable, Equatable, Sendable {
    let version: Int
    let partyName: String
    let suggestedExtension: String

    private enum CodingKeys: String, CodingKey {
        case version
        case partyName = "party_name"
        case suggestedExtension = "suggested_extension"
    }

    func validated() throws -> PhoneInvitationPreview {
        guard version == 1,
              PhoneInvitationDetails.isText(partyName, maximum: 80),
              PhoneInvitationDetails.isExtension(suggestedExtension) else {
            throw InvitationError.invalidResponse
        }
        return self
    }
}

struct PendingInvitation: Identifiable, Equatable, Sendable {
    let link: PhoneInvitationLink
    let preview: PhoneInvitationPreview

    var id: String { link.apiURL.absoluteString }
}

struct PhoneInvitationClaim: Encodable, Equatable, Sendable {
    let displayName: String
    let `extension`: String
    let deviceLabel: String

    private enum CodingKeys: String, CodingKey {
        case displayName = "display_name"
        case `extension`
        case deviceLabel = "device_label"
    }
}

enum PhoneInvitationDetails {
    private static let reservedExtensions = Set(["000", "111", "112", "911", "988", "999"])

    static func normalizedName(_ value: String) -> String {
        value.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
    }

    static func isText(_ value: String, maximum: Int) -> Bool {
        !value.isEmpty &&
            value.unicodeScalars.count <= maximum &&
            value.trimmingCharacters(in: .whitespacesAndNewlines) == value &&
            value.unicodeScalars.allSatisfy { !CharacterSet.controlCharacters.contains($0) }
    }

    static func isExtension(_ value: String) -> Bool {
        (2...5).contains(value.count) &&
            value.allSatisfy(\.isNumber) &&
            value.unicodeScalars.allSatisfy { $0.value >= 48 && $0.value <= 57 } &&
            !reservedExtensions.contains(value)
    }

    static func isToken(_ value: String) -> Bool {
        value.range(of: #"^[A-Za-z0-9_-]{43}$"#, options: .regularExpression) != nil
    }
}

enum InvitationError: LocalizedError, Equatable {
    case expired
    case extensionTaken
    case invalidDetails
    case retryLater
    case responseTooLarge
    case invalidResponse
    case serverRejected

    var errorDescription: String? {
        switch self {
        case .expired:
            return "That invitation was used, expired, or canceled. Ask your host for a fresh one."
        case .extensionTaken:
            return "That extension was just taken. RingRing found another one you can use."
        case .invalidDetails:
            return "Check the phone name and extension, then try again."
        case .retryLater:
            return "Too many setup attempts. Wait a moment, then try again."
        case .responseTooLarge, .invalidResponse, .serverRejected:
            return "RingRing couldn’t safely open that invitation. Ask your host for a fresh one."
        }
    }
}

final class InvitationClient: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private static let maximumResponseSize = 256 * 1024

    func preview(_ link: PhoneInvitationLink) async throws -> PhoneInvitationPreview {
        var request = URLRequest(url: link.apiURL)
        request.httpMethod = "GET"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let data = try await responseData(for: request)
        do {
            return try JSONDecoder().decode(PhoneInvitationPreview.self, from: data).validated()
        } catch let error as InvitationError {
            throw error
        } catch {
            throw InvitationError.invalidResponse
        }
    }

    func claim(_ claim: PhoneInvitationClaim, using link: PhoneInvitationLink) async throws -> ProvisionedPhone {
        var request = URLRequest(url: link.apiURL)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(claim)
        let data = try await responseData(for: request)
        do {
            return try JSONDecoder().decode(ProvisioningDocument.self, from: data).validated()
        } catch let error as ProvisioningError {
            throw error
        } catch {
            throw InvitationError.invalidResponse
        }
    }

    private func responseData(for request: URLRequest) async throws -> Data {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        configuration.timeoutIntervalForRequest = 15
        configuration.timeoutIntervalForResource = 20
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.httpShouldSetCookies = false

        let session = URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
        defer { session.finishTasksAndInvalidate() }
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw InvitationError.invalidResponse
        }
        switch http.statusCode {
        case 200:
            break
        case 404, 410:
            throw InvitationError.expired
        case 400:
            throw InvitationError.invalidDetails
        case 409:
            throw InvitationError.extensionTaken
        case 429:
            throw InvitationError.retryLater
        default:
            throw InvitationError.serverRejected
        }
        guard data.count <= Self.maximumResponseSize else {
            throw InvitationError.responseTooLarge
        }
        guard http.mimeType == "application/json" else {
            throw InvitationError.invalidResponse
        }
        return data
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
