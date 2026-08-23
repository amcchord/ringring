import AVFoundation
import CallKit
import Foundation

@MainActor
protocol RingRingCallHandling: AnyObject {
    func startCall(to destination: String) throws
    func answerCall() throws
    func endCall() throws
    func setMuted(_ muted: Bool)
    func sendDTMF(_ digits: String)
    func activateAudioSession(_ active: Bool)
    func configureAudioSession()
}

@MainActor
final class CallKitCoordinator: NSObject {
    private weak var handler: RingRingCallHandling?
    private let provider: CXProvider
    private let controller = CXCallController()
    private var destinations: [UUID: String] = [:]

    private(set) var activeUUID: UUID?
    private(set) var endingFromApp = false
    private(set) var isIncoming = false

    init(handler: RingRingCallHandling) {
        self.handler = handler
        let configuration = CXProviderConfiguration()
        configuration.supportsVideo = false
        configuration.supportedHandleTypes = [.generic, .phoneNumber]
        configuration.maximumCallGroups = 1
        configuration.maximumCallsPerCallGroup = 1
        configuration.iconTemplateImageData = nil
        provider = CXProvider(configuration: configuration)
        super.init()
        provider.setDelegate(self, queue: .main)
    }

    func requestStart(to destination: String, displayName: String?) {
        let uuid = UUID()
        destinations[uuid] = destination
        let handle = CXHandle(type: displayName == nil ? .phoneNumber : .generic, value: displayName ?? destination)
        let action = CXStartCallAction(call: uuid, handle: handle)
        action.isVideo = false
        request(CXTransaction(action: action))
    }

    func reportIncoming(from caller: String, displayName: String?, completion: @escaping (Error?) -> Void) {
        guard activeUUID == nil else {
            completion(CallKitError.alreadyInCall)
            return
        }
        let uuid = UUID()
        activeUUID = uuid
        isIncoming = true
        let update = CXCallUpdate()
        update.remoteHandle = CXHandle(type: displayName == nil ? .phoneNumber : .generic, value: displayName ?? caller)
        update.localizedCallerName = displayName ?? "Extension \(caller)"
        update.hasVideo = false
        provider.reportNewIncomingCall(with: uuid, update: update) { [weak self] error in
            if error != nil {
                Task { @MainActor in
                    self?.activeUUID = nil
                    self?.isIncoming = false
                }
            }
            completion(error)
        }
    }

    func requestEnd() {
        guard let activeUUID else { return }
        endingFromApp = true
        request(CXTransaction(action: CXEndCallAction(call: activeUUID)))
    }

    func requestAnswer() {
        guard let activeUUID else { return }
        request(CXTransaction(action: CXAnswerCallAction(call: activeUUID)))
    }

    func requestMute(_ muted: Bool) {
        guard let activeUUID else { return }
        request(CXTransaction(action: CXSetMutedCallAction(call: activeUUID, muted: muted)))
    }

    func reportConnected() {
        guard let activeUUID, !isIncoming else { return }
        provider.reportOutgoingCall(with: activeUUID, connectedAt: Date())
    }

    func reportEnded(reason: CXCallEndedReason) {
        guard let activeUUID else { return }
        provider.reportCall(with: activeUUID, endedAt: Date(), reason: reason)
        finish(uuid: activeUUID)
    }

    private func request(_ transaction: CXTransaction) {
        controller.request(transaction) { [weak self] error in
            guard error != nil else { return }
            Task { @MainActor in
                self?.endingFromApp = false
            }
        }
    }

    private func finish(uuid: UUID) {
        destinations[uuid] = nil
        if activeUUID == uuid {
            activeUUID = nil
        }
        endingFromApp = false
        isIncoming = false
    }
}

extension CallKitCoordinator: CXProviderDelegate {
    nonisolated func providerDidReset(_ provider: CXProvider) {
        Task { @MainActor [weak self] in
            try? self?.handler?.endCall()
            self?.activeUUID = nil
            self?.isIncoming = false
            self?.destinations.removeAll()
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXStartCallAction) {
        Task { @MainActor [weak self] in
            guard let self, let destination = destinations[action.callUUID] else {
                action.fail()
                return
            }
            do {
                activeUUID = action.callUUID
                isIncoming = false
                configureAudioSession()
                try handler?.startCall(to: destination)
                provider.reportOutgoingCall(with: action.callUUID, startedConnectingAt: Date())
                action.fulfill()
            } catch {
                finish(uuid: action.callUUID)
                action.fail()
            }
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXAnswerCallAction) {
        Task { @MainActor [weak self] in
            do {
                self?.configureAudioSession()
                try self?.handler?.answerCall()
                action.fulfill()
            } catch {
                action.fail()
            }
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXEndCallAction) {
        Task { @MainActor [weak self] in
            do {
                try self?.handler?.endCall()
                self?.finish(uuid: action.callUUID)
                action.fulfill()
            } catch {
                action.fail()
            }
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXSetMutedCallAction) {
        Task { @MainActor [weak self] in
            self?.handler?.setMuted(action.isMuted)
            action.fulfill()
        }
    }

    nonisolated func provider(_ provider: CXProvider, perform action: CXPlayDTMFCallAction) {
        Task { @MainActor [weak self] in
            self?.handler?.sendDTMF(action.digits)
            action.fulfill()
        }
    }

    nonisolated func provider(_ provider: CXProvider, didActivate audioSession: AVAudioSession) {
        Task { @MainActor [weak self] in self?.handler?.activateAudioSession(true) }
    }

    nonisolated func provider(_ provider: CXProvider, didDeactivate audioSession: AVAudioSession) {
        Task { @MainActor [weak self] in self?.handler?.activateAudioSession(false) }
    }

    nonisolated func provider(_ provider: CXProvider, timedOutPerforming action: CXAction) {
        action.fail()
    }

    private func configureAudioSession() {
        handler?.configureAudioSession()
    }
}

private enum CallKitError: Error {
    case alreadyInCall
}
