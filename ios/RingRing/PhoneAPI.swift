import Foundation

struct PhoneStateDocument: Decodable, Equatable, Sendable {
    let version: Int
    let `extension`: String
    let destinations: [DialDestination]

    func validated() throws -> PhoneStateDocument {
        guard version == 1,
              `extension`.range(of: #"^[0-9]{2,5}$"#, options: .regularExpression) != nil,
              destinations.count <= 128 else {
            throw PhoneAPIError.invalidResponse
        }
        var targets = Set<String>()
        for destination in destinations {
            do {
                try destination.validate()
            } catch {
                throw PhoneAPIError.invalidResponse
            }
            guard targets.insert(destination.dial).inserted else {
                throw PhoneAPIError.invalidResponse
            }
        }
        return self
    }
}

enum ApplePushEnvironment: String, Encodable, Sendable {
    case development
    case production

    static var current: ApplePushEnvironment {
#if DEBUG
        .development
#else
        .production
#endif
    }
}

private struct PushRegistrationRequest: Encodable {
    let token: String
    let environment: ApplePushEnvironment
}

final class PhoneAPIClient: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    private static let maximumResponseSize = 256 * 1024

    func fetchState(for account: SIPAccount) async throws -> PhoneStateDocument {
        let request = try makeRequest(account: account, path: "/api/v1/phone/state", method: "GET")
        let (data, response) = try await perform(request)
        guard response.statusCode == 200, data.count <= Self.maximumResponseSize,
              response.mimeType == "application/json" else {
            throw map(response)
        }
        do {
            return try JSONDecoder().decode(PhoneStateDocument.self, from: data).validated()
        } catch let error as PhoneAPIError {
            throw error
        } catch {
            throw PhoneAPIError.invalidResponse
        }
    }

    func registerPushToken(_ token: String, environment: ApplePushEnvironment, for account: SIPAccount) async throws {
        guard token.range(of: #"^[a-f0-9]{64}$"#, options: .regularExpression) != nil else {
            throw PhoneAPIError.invalidPushToken
        }
        var request = try makeRequest(account: account, path: "/api/v1/phone/push", method: "PUT")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(PushRegistrationRequest(token: token, environment: environment))
        let (_, response) = try await perform(request)
        guard response.statusCode == 204 else {
            throw map(response)
        }
    }

    func deletePushToken(for account: SIPAccount) async throws {
        let request = try makeRequest(account: account, path: "/api/v1/phone/push", method: "DELETE")
        let (_, response) = try await perform(request)
        guard response.statusCode == 204 else {
            throw map(response)
        }
    }

    private func makeRequest(account: SIPAccount, path: String, method: String) throws -> URLRequest {
        try account.validate()
        var components = URLComponents()
#if DEBUG
        let localHost = ["localhost", "127.0.0.1", "::1"].contains(account.server.lowercased())
        components.scheme = localHost ? "http" : "https"
#else
        components.scheme = "https"
#endif
        components.host = account.server
        components.path = path
        guard let url = components.url else {
            throw PhoneAPIError.invalidResponse
        }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("Basic " + Data("\(account.username):\(account.password)".utf8).base64EncodedString(), forHTTPHeaderField: "Authorization")
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        return request
    }

    private func perform(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.requestCachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        configuration.timeoutIntervalForRequest = 8
        configuration.timeoutIntervalForResource = 12
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.httpShouldSetCookies = false
        let session = URLSession(configuration: configuration, delegate: self, delegateQueue: nil)
        defer { session.finishTasksAndInvalidate() }
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, data.count <= Self.maximumResponseSize else {
            throw PhoneAPIError.invalidResponse
        }
        return (data, http)
    }

    private func map(_ response: HTTPURLResponse) -> PhoneAPIError {
        switch response.statusCode {
        case 401: .credentialsRevoked
        case 429: .tooManyRequests
        case 503: .pushUnavailable
        default: .serverRejected
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

enum PhoneAPIError: LocalizedError, Equatable {
    case invalidResponse
    case invalidPushToken
    case credentialsRevoked
    case tooManyRequests
    case pushUnavailable
    case serverRejected

    var errorDescription: String? {
        switch self {
        case .credentialsRevoked:
            "This phone was disconnected by its host. Scan a fresh invitation to reconnect."
        case .pushUnavailable:
            "Background ringing is not ready on this RingRing server yet."
        case .tooManyRequests:
            "RingRing is refreshing too quickly. Wait a moment and try again."
        case .invalidPushToken, .invalidResponse, .serverRejected:
            "RingRing couldn’t refresh this phone right now."
        }
    }
}
