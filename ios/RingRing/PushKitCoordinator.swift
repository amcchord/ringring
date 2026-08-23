import Foundation
import PushKit

@MainActor
final class PushKitCoordinator: NSObject {
    var onToken: ((String) -> Void)?
    var onTokenInvalidated: (() -> Void)?

    private weak var phone: PhoneService?
    private var registry: PKPushRegistry?

    init(phone: PhoneService) {
        self.phone = phone
        super.init()
    }

    func start() {
        guard registry == nil else { return }
        let registry = PKPushRegistry(queue: .main)
        registry.delegate = self
        registry.desiredPushTypes = [.voIP]
        self.registry = registry
    }
}

extension PushKitCoordinator: PKPushRegistryDelegate {
    nonisolated func pushRegistry(_ registry: PKPushRegistry, didUpdate pushCredentials: PKPushCredentials, for type: PKPushType) {
        guard type == .voIP else { return }
        let token = pushCredentials.token.map { String(format: "%02x", $0) }.joined()
        Task { @MainActor [weak self] in
            self?.onToken?(token)
        }
    }

    nonisolated func pushRegistry(_ registry: PKPushRegistry, didInvalidatePushTokenFor type: PKPushType) {
        guard type == .voIP else { return }
        Task { @MainActor [weak self] in
            self?.onTokenInvalidated?()
        }
    }

    nonisolated func pushRegistry(
        _ registry: PKPushRegistry,
        didReceiveIncomingPushWith payload: PKPushPayload,
        for type: PKPushType,
        completion: @escaping () -> Void
    ) {
        guard type == .voIP else {
            completion()
            return
        }
        let callID = payload.dictionaryPayload["call_id"] as? String
        let uuid = callID.flatMap(UUID.init(uuidString:)) ?? UUID()
        Task { @MainActor [weak self] in
            guard let phone = self?.phone else {
                completion()
                return
            }
            phone.receiveIncomingPush(callID: uuid, completion: completion)
        }
    }
}
