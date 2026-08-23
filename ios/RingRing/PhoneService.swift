import CallKit
import Foundation
import linphonesw

enum RegistrationStatus: Equatable {
    case idle
    case connecting
    case ready
    case failed

    var label: String {
        switch self {
        case .idle: "Phone offline"
        case .connecting: "Connecting…"
        case .ready: "Ready for calls"
        case .failed: "Can’t connect"
        }
    }
}

enum CallPhase: Equatable {
    case idle
    case dialing
    case ringing
    case incoming
    case active
    case ended
}

@MainActor
final class PhoneService: NSObject, ObservableObject {
    @Published private(set) var registration: RegistrationStatus = .idle
    @Published private(set) var callPhase: CallPhase = .idle
    @Published private(set) var remoteExtension = ""
    @Published private(set) var isMuted = false
    @Published private(set) var isSpeakerOn = false
    @Published private(set) var connectedAt: Date?
    @Published private(set) var lastError: String?

    private var core: Core?
    private var delegate: CoreDelegateStub?
    private var account: SIPAccount?
    private var callNames: [String: String] = [:]
    private var currentCall: Call?
    private lazy var callKit = CallKitCoordinator(handler: self)

    override init() {
        super.init()
        prepareCore()
    }

    func configure(with account: SIPAccount) throws {
        try account.validate()
        guard let core else {
            throw PhoneError.engineUnavailable
        }
        resetRegistration()

        let identity = try Factory.Instance.createAddress(addr: "sip:\(account.username)@\(account.server)")
        let server = try Factory.Instance.createAddress(addr: "sip:\(account.server):\(account.port)")
        try server.setTransport(newValue: .Tls)

        let auth = try Factory.Instance.createAuthInfo(
            username: account.username,
            userid: nil,
            passwd: account.password,
            ha1: nil,
            realm: nil,
            domain: account.server
        )
        let params = try core.createAccountParams()
        try params.setIdentityaddress(newValue: identity)
        try params.setServeraddress(newValue: server)
        params.registerEnabled = true
        params.expires = 600

        let linphoneAccount = try core.createAccount(params: params)
        core.addAuthInfo(info: auth)
        try core.addAccount(account: linphoneAccount)
        core.defaultAccount = linphoneAccount

        self.account = account
        registration = .connecting
        lastError = nil
    }

    func refresh() {
        core?.refreshRegisters()
    }

    func setCallDirectory(_ destinations: [DialDestination]) {
        var names: [String: String] = [:]
        for destination in destinations {
            names[destination.dial] = destination.label
        }
        callNames = names
    }

#if DEBUG
    func usePreviewReadyState() {
        registration = .ready
    }

    func usePreviewActiveCall(to destination: String) {
        registration = .ready
        remoteExtension = destination
        callPhase = .active
        connectedAt = Date(timeIntervalSinceNow: -42)
    }
#endif

    func placeCall(to destination: String, named displayName: String? = nil) {
        guard DialString.isCallable(destination), registration == .ready else { return }
        lastError = nil
        remoteExtension = destination
        callPhase = .dialing
        callKit.requestStart(to: destination, displayName: displayName)
    }

    func hangUp() {
        callKit.requestEnd()
    }

    func answer() {
        callKit.requestAnswer()
    }

    func setMuteRequested(_ muted: Bool) {
        callKit.requestMute(muted)
    }

    func toggleSpeaker() {
        guard let core, let call = currentCall else { return }
        let wanted: AudioDevice.Kind = isSpeakerOn ? .Earpiece : .Speaker
        guard let device = core.audioDevices.first(where: { $0.type == wanted }) else { return }
        call.outputAudioDevice = device
        isSpeakerOn.toggle()
    }

    func sendDigit(_ digit: String) {
        sendDTMF(digit)
    }

    func disconnect() {
        if currentCall != nil {
            try? endCall()
        }
        resetRegistration()
        account = nil
        callNames = [:]
        registration = .idle
        resetCallState()
    }

    private func prepareCore() {
        do {
            LoggingService.Instance.logLevel = .Fatal
            let core = try Factory.Instance.createCore(configPath: "", factoryConfigPath: "", systemContext: nil)
            core.callkitEnabled = true
            core.pushNotificationEnabled = false
            core.videoCaptureEnabled = false
            core.videoDisplayEnabled = false

            let delegate = CoreDelegateStub(
                onCallStateChanged: { [weak self] _, call, state, _ in
                    Task { @MainActor [weak self] in self?.handle(call: call, state: state) }
                },
                onAccountRegistrationStateChanged: { [weak self] _, _, state, _ in
                    Task { @MainActor [weak self] in self?.handle(registration: state) }
                }
            )
            core.addDelegate(delegate: delegate)
            try core.start()
            self.core = core
            self.delegate = delegate
        } catch {
            lastError = PhoneError.engineUnavailable.localizedDescription
            registration = .failed
        }
    }

    private func resetRegistration() {
        core?.clearAccounts()
        core?.clearAllAuthInfo()
    }

    private func handle(registration state: RegistrationState) {
        switch state {
        case .Ok:
            registration = .ready
            lastError = nil
        case .Progress, .Refreshing:
            registration = .connecting
        case .Failed:
            registration = .failed
            lastError = "RingRing couldn’t connect this phone. Check the network or ask your host for a fresh setup."
        case .Cleared, .None:
            registration = account == nil ? .idle : .connecting
        }
    }

    private func handle(call: Call, state: Call.State) {
        currentCall = call
        switch state {
        case .IncomingReceived, .PushIncomingReceived:
            guard callKit.activeUUID == nil else { return }
            let caller = call.remoteAddress?.username ?? "RingRing"
            remoteExtension = caller
            callPhase = .incoming
            callKit.reportIncoming(from: caller, displayName: callNames[caller]) { [weak self] error in
                guard error != nil else { return }
                Task { @MainActor in
                    try? call.terminate()
                    self?.lastError = "This incoming call couldn’t be shown."
                    self?.resetCallState()
                }
            }
        case .OutgoingInit, .OutgoingProgress:
            callPhase = .dialing
        case .OutgoingRinging, .OutgoingEarlyMedia:
            callPhase = .ringing
        case .Connected, .StreamsRunning:
            callPhase = .active
            if connectedAt == nil {
                connectedAt = Date()
                callKit.reportConnected()
            }
        case .Error:
            lastError = "The call couldn’t connect."
            callKit.reportEnded(reason: .failed)
            finishCall()
        case .End:
            if callKit.activeUUID != nil {
                callKit.reportEnded(reason: callKit.endingFromApp ? .remoteEnded : .remoteEnded)
            }
            finishCall()
        case .Released:
            finishCall()
        default:
            break
        }
    }

    private func finishCall() {
        callPhase = .ended
        currentCall = nil
        isMuted = false
        isSpeakerOn = false
        connectedAt = nil
        Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(650))
            guard self?.currentCall == nil else { return }
            self?.resetCallState()
        }
    }

    private func resetCallState() {
        callPhase = .idle
        remoteExtension = ""
        isMuted = false
        isSpeakerOn = false
        connectedAt = nil
    }
}

extension PhoneService: RingRingCallHandling {
    func startCall(to destination: String) throws {
        guard let core, let account else { throw PhoneError.notConfigured }
        core.configureAudioSession()
        let remote = try Factory.Instance.createAddress(addr: "sip:\(destination)@\(account.server)")
        let params = try core.createCallParams(call: nil)
        params.mediaEncryption = .None
        params.videoEnabled = false
        guard let call = core.inviteAddressWithParams(addr: remote, params: params) else {
            throw PhoneError.callFailed
        }
        currentCall = call
    }

    func answerCall() throws {
        guard let currentCall else { throw PhoneError.noCall }
        core?.configureAudioSession()
        try currentCall.accept()
    }

    func endCall() throws {
        if let currentCall, currentCall.state != .End, currentCall.state != .Released {
            try currentCall.terminate()
        }
        resetCallState()
    }

    func setMuted(_ muted: Bool) {
        core?.micEnabled = !muted
        isMuted = muted
    }

    func sendDTMF(_ digits: String) {
        try? currentCall?.sendDtmfs(dtmfs: digits)
    }

    func activateAudioSession(_ active: Bool) {
        core?.activateAudioSession(activated: active)
    }

    func configureAudioSession() {
        core?.configureAudioSession()
    }
}

enum DialString {
    static func isCallable(_ value: String) -> Bool {
        value.range(of: #"^(?:[0-9]{2,5}|\*[0-9]{2})$"#, options: .regularExpression) != nil
    }
}

private enum PhoneError: LocalizedError {
    case engineUnavailable
    case notConfigured
    case noCall
    case callFailed

    var errorDescription: String? {
        switch self {
        case .engineUnavailable: "The phone engine couldn’t start."
        case .notConfigured: "This phone isn’t set up yet."
        case .noCall: "There isn’t a call to answer."
        case .callFailed: "The call couldn’t start."
        }
    }
}
