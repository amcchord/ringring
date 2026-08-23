import Foundation
import Security

enum CredentialStoreError: LocalizedError {
    case encoding
    case unexpectedStatus(OSStatus)

    var errorDescription: String? {
        switch self {
        case .encoding:
            return "RingRing couldn’t save this phone securely."
        case .unexpectedStatus:
            return "RingRing couldn’t access this phone’s secure setup."
        }
    }
}

struct CredentialStore: Sendable {
    private let service = "com.mcchord.ringring.sip-account"
    private let account = "primary"

    func save(_ value: ProvisionedPhone) throws {
        let data: Data
        do {
            data = try JSONEncoder().encode(value)
        } catch {
            throw CredentialStoreError.encoding
        }

        let query = baseQuery()
        let update: [String: Any] = [
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
        ]
        let updated = SecItemUpdate(query as CFDictionary, update as CFDictionary)
        if updated == errSecItemNotFound {
            var insert = query
            update.forEach { insert[$0.key] = $0.value }
            let status = SecItemAdd(insert as CFDictionary, nil)
            guard status == errSecSuccess else {
                throw CredentialStoreError.unexpectedStatus(status)
            }
        } else if updated != errSecSuccess {
            throw CredentialStoreError.unexpectedStatus(updated)
        }
    }

    func load() throws -> ProvisionedPhone? {
        var query = baseQuery()
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        if status == errSecItemNotFound {
            return nil
        }
        guard status == errSecSuccess, let data = result as? Data else {
            throw CredentialStoreError.unexpectedStatus(status)
        }
        let decoder = JSONDecoder()
        if let provisioned = try? decoder.decode(ProvisionedPhone.self, from: data) {
            return provisioned
        }
        return ProvisionedPhone.legacy(sip: try decoder.decode(SIPAccount.self, from: data))
    }

    func delete() throws {
        let status = SecItemDelete(baseQuery() as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CredentialStoreError.unexpectedStatus(status)
        }
    }

    private func baseQuery() -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
    }
}
