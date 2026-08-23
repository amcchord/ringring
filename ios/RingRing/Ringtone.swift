import AVFoundation
import Combine
import Foundation

enum RingRingRingtone: String, CaseIterable, Identifiable {
    case ringRingDouble
    case memphisBounce
    case confettiCall
    case softHello

    static let defaultsKey = "ringring.ringtone"

    var id: String { rawValue }

    var title: String {
        switch self {
        case .ringRingDouble: "Ring-ring double"
        case .memphisBounce: "Memphis bounce"
        case .confettiCall: "Confetti call"
        case .softHello: "Soft hello"
        }
    }

    var detail: String {
        switch self {
        case .ringRingDouble: "The cheerful classic"
        case .memphisBounce: "Bright, springy shapes"
        case .confettiCall: "A tiny party arriving"
        case .softHello: "Gentle and low-key"
        }
    }

    var filename: String {
        switch self {
        case .ringRingDouble: "ring1-ringring-double.wav"
        case .memphisBounce: "ring2-memphis-bounce.wav"
        case .confettiCall: "ring3-confetti-call.wav"
        case .softHello: "ring4-soft-hello.wav"
        }
    }

    static var saved: RingRingRingtone {
        guard let raw = UserDefaults.standard.string(forKey: defaultsKey), let value = RingRingRingtone(rawValue: raw) else {
            return .ringRingDouble
        }
        return value
    }
}

@MainActor
final class RingtonePreviewPlayer: ObservableObject {
    @Published private(set) var playing: RingRingRingtone?
    private var player: AVAudioPlayer?

    func play(_ ringtone: RingRingRingtone) {
        player?.stop()
        let name = String(ringtone.filename.dropLast(4))
        guard let url = Bundle.main.url(forResource: name, withExtension: "wav") else {
            playing = nil
            return
        }
        do {
            let player = try AVAudioPlayer(contentsOf: url)
            player.volume = 0.7
            player.prepareToPlay()
            player.play()
            self.player = player
            playing = ringtone
        } catch {
            playing = nil
        }
    }

    func stop() {
        player?.stop()
        player = nil
        playing = nil
    }
}
