import AVFoundation
import SwiftUI
import UIKit

struct RootView: View {
    @ObservedObject var model: AppModel
    @ObservedObject private var phone: PhoneService

    init(model: AppModel) {
        self.model = model
        phone = model.phone
    }

    var body: some View {
        ZStack {
            MemphisBackground()

            if model.account == nil {
                WelcomeView(model: model)
            } else if phone.callPhase == .idle {
                DialerView(model: model, phone: phone)
            } else {
                CallView(phone: phone, destinationLabel: model.callLabel(for: phone.remoteExtension))
            }

            if model.isProvisioning {
                Color.black.opacity(0.24).ignoresSafeArea()
                ProgressView("Setting up your phone…")
                    .font(.headline)
                    .padding(24)
                    .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
            }
        }
        .foregroundStyle(RingRingTheme.ink)
        .animation(.snappy, value: model.account)
        .animation(.snappy, value: phone.callPhase)
        .fullScreenCover(isPresented: $model.showingScanner) {
            ScannerScreen(model: model)
        }
        .sheet(isPresented: $model.showingSettings) {
            SettingsView(model: model, phone: model.phone)
        }
        .sheet(item: $model.pendingInvitation) { invitation in
            InvitationSetupView(model: model, invitation: invitation)
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
                .interactiveDismissDisabled(model.isProvisioning)
        }
        .task(id: model.account?.username) {
            guard model.account != nil else { return }
            while !Task.isCancelled, model.account != nil {
                await model.refreshMenu()
                try? await Task.sleep(for: .seconds(6))
            }
        }
        .alert(
            "Couldn’t finish that",
            isPresented: Binding(
                get: { model.errorMessage != nil },
                set: { if !$0 { model.errorMessage = nil } }
            ),
            actions: { Button("OK", role: .cancel) {} },
            message: { Text(model.errorMessage ?? "Please try again.") }
        )
    }
}

private struct WelcomeView: View {
    @ObservedObject var model: AppModel
    @State private var showingPaste = false
    @State private var pastedLink = ""

    var body: some View {
        ScrollView {
            VStack(spacing: 26) {
                Spacer(minLength: 24)

                ZStack {
                    RoundedRectangle(cornerRadius: 30, style: .continuous)
                        .fill(RingRingTheme.coral)
                        .frame(width: 116, height: 116)
                        .rotationEffect(.degrees(-5))
                    Image(systemName: "phone.fill")
                        .font(.system(size: 51, weight: .black))
                        .foregroundStyle(.white)
                        .rotationEffect(.degrees(-18))
                }
                .accessibilityHidden(true)

                VStack(spacing: 10) {
                    Text("RingRing")
                        .font(.subheadline.weight(.black))
                        .textCase(.uppercase)
                        .tracking(2.2)
                        .foregroundStyle(RingRingTheme.purple)
                    Text("Your party is calling.")
                        .font(.system(.largeTitle, design: .rounded, weight: .black))
                        .multilineTextAlignment(.center)
                    Text("Scan an invite and finish choosing this phone’s name and extension right here.")
                        .font(.title3)
                        .multilineTextAlignment(.center)
                        .foregroundStyle(.secondary)
                }

                RingRingCard {
                    VStack(spacing: 14) {
                        Button {
                            model.showingScanner = true
                        } label: {
                            Label("Scan invite or setup code", systemImage: "qrcode.viewfinder")
                        }
                        .buttonStyle(PrimaryButtonStyle())
                        .accessibilityHint("Opens the camera to scan a RingRing invitation or phone setup code")

                        Button {
                            pastedLink = UIPasteboard.general.string ?? ""
                            showingPaste = true
                        } label: {
                            Label("Paste invite or setup link", systemImage: "doc.on.clipboard")
                                .font(.headline)
                                .frame(maxWidth: .infinity, minHeight: 52)
                        }
                        .buttonStyle(.plain)
                    }
                }

                Label("Private party calls only — never the public phone network", systemImage: "lock.shield.fill")
                    .font(.footnote.weight(.semibold))
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal)
            }
            .padding(.horizontal, 24)
            .padding(.bottom, 32)
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
        }
        .alert("Paste a RingRing link", isPresented: $showingPaste) {
            TextField("ringring://join…", text: $pastedLink)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Join") { model.join(using: pastedLink) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Paste the private invitation or phone setup link from your RingRing host.")
        }
    }
}

private struct ScannerScreen: View {
    @ObservedObject var model: AppModel

    var body: some View {
        ZStack {
            QRScannerView(
                onCode: { model.join(using: $0) },
                onError: {
                    model.errorMessage = $0
                    model.showingScanner = false
                }
            )
            .ignoresSafeArea()

            Color.black.opacity(0.18).ignoresSafeArea()

            VStack {
                HStack {
                    Spacer()
                    Button {
                        model.showingScanner = false
                    } label: {
                        Image(systemName: "xmark")
                            .font(.headline.weight(.black))
                            .frame(width: 48, height: 48)
                            .background(.ultraThinMaterial, in: Circle())
                    }
                    .foregroundStyle(.white)
                    .accessibilityLabel("Close scanner")
                }

                Spacer()

                RoundedRectangle(cornerRadius: 30, style: .continuous)
                    .stroke(RingRingTheme.yellow, style: StrokeStyle(lineWidth: 8, lineCap: .round))
                    .frame(width: 270, height: 270)
                    .shadow(color: .black.opacity(0.25), radius: 12)
                    .accessibilityHidden(true)

                Text("Point at the RingRing invite or setup code")
                    .font(.title3.weight(.bold))
                    .foregroundStyle(.white)
                    .padding(.top, 24)
                    .shadow(color: .black, radius: 5)

                Spacer()
            }
            .padding(24)
        }
    }
}

private struct InvitationSetupView: View {
    @ObservedObject var model: AppModel
    let invitation: PendingInvitation
    @Environment(\.dismiss) private var dismiss
    @State private var displayName = ""
    @State private var extensionValue: String
    @State private var adultExtension = false
    @FocusState private var focusedField: Field?

    private enum Field {
        case name
        case `extension`
    }

    init(model: AppModel, invitation: PendingInvitation) {
        self.model = model
        self.invitation = invitation
        _extensionValue = State(initialValue: invitation.preview.suggestedExtension)
    }

    private var currentInvitation: PendingInvitation {
        model.pendingInvitation ?? invitation
    }

    private var normalizedName: String {
        PhoneInvitationDetails.normalizedName(displayName)
    }

    private var canJoin: Bool {
        PhoneInvitationDetails.isText(normalizedName, maximum: 40) &&
            PhoneInvitationDetails.isExtension(extensionValue) &&
            !model.isProvisioning
    }

    var body: some View {
        NavigationStack {
            ZStack {
                MemphisBackground()

                ScrollView {
                    VStack(spacing: 20) {
                        VStack(spacing: 9) {
                            ZStack {
                                RoundedRectangle(cornerRadius: 22, style: .continuous)
                                    .fill(RingRingTheme.yellow)
                                    .frame(width: 82, height: 82)
                                    .rotationEffect(.degrees(5))
                                Image(systemName: "phone.badge.plus.fill")
                                    .font(.system(size: 35, weight: .black))
                                    .foregroundStyle(RingRingTheme.ink)
                            }
                            .accessibilityHidden(true)

                            Text("Join \(currentInvitation.preview.partyName)")
                                .font(.system(.largeTitle, design: .rounded, weight: .black))
                                .multilineTextAlignment(.center)
                            Text("Pick how this phone appears to the rest of the party.")
                                .font(.body.weight(.medium))
                                .foregroundStyle(.secondary)
                                .multilineTextAlignment(.center)
                        }

                        RingRingCard {
                            VStack(alignment: .leading, spacing: 18) {
                                VStack(alignment: .leading, spacing: 7) {
                                    Text("Phone name")
                                        .font(.headline.weight(.black))
                                    TextField("Kitchen phone", text: $displayName)
                                        .textInputAutocapitalization(.words)
                                        .autocorrectionDisabled()
                                        .focused($focusedField, equals: .name)
                                        .submitLabel(.next)
                                        .onSubmit { focusedField = .extension }
                                        .padding(.horizontal, 15)
                                        .frame(minHeight: 52)
                                        .background(RingRingTheme.canvas, in: RoundedRectangle(cornerRadius: 15, style: .continuous))
                                    Text("Shown only to people in this private party.")
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }

                                VStack(alignment: .leading, spacing: 7) {
                                    Text("Extension")
                                        .font(.headline.weight(.black))
                                    TextField("101", text: $extensionValue)
                                        .keyboardType(.numberPad)
                                        .textContentType(.oneTimeCode)
                                        .focused($focusedField, equals: .extension)
                                        .font(.title3.monospacedDigit().weight(.bold))
                                        .padding(.horizontal, 15)
                                        .frame(minHeight: 52)
                                        .background(RingRingTheme.canvas, in: RoundedRectangle(cornerRadius: 15, style: .continuous))
                                        .onChange(of: extensionValue) { _, value in
                                            extensionValue = String(value.filter(\.isNumber).prefix(5))
                                        }
                                    Text("Use 2–5 digits. RingRing suggested \(currentInvitation.preview.suggestedExtension).")
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }

                                Toggle(isOn: $adultExtension) {
                                    VStack(alignment: .leading, spacing: 3) {
                                        Text("Adult extension (18+)")
                                            .font(.headline.weight(.black))
                                        Text("Allows any adult-only voice service your host has enabled. It does not enable public calls.")
                                            .font(.footnote)
                                            .foregroundStyle(.secondary)
                                    }
                                }
                                .tint(RingRingTheme.purple)
                                .frame(minHeight: 58)
                            }
                        }

                        if let error = model.invitationErrorMessage {
                            Label(error, systemImage: "exclamationmark.triangle.fill")
                                .font(.footnote.weight(.semibold))
                                .foregroundStyle(.red)
                                .multilineTextAlignment(.center)
                                .padding(.horizontal)
                        }

                        Button {
                            focusedField = nil
                            model.claimInvitation(displayName: displayName, extension: extensionValue, adultExtension: adultExtension)
                        } label: {
                            if model.isProvisioning {
                                ProgressView()
                                    .tint(.white)
                                    .accessibilityLabel("Joining party")
                            } else {
                                Label("Join and set up this iPhone", systemImage: "phone.fill")
                            }
                        }
                        .buttonStyle(PrimaryButtonStyle())
                        .disabled(!canJoin)
                        .opacity(canJoin || model.isProvisioning ? 1 : 0.45)

                        Label("Private party calls only — no public or emergency calling", systemImage: "lock.shield.fill")
                            .font(.footnote.weight(.semibold))
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    .padding(.horizontal, 22)
                    .padding(.top, 12)
                    .padding(.bottom, 32)
                    .frame(maxWidth: 560)
                    .frame(maxWidth: .infinity)
                }
            }
            .foregroundStyle(RingRingTheme.ink)
            .navigationTitle("Set up phone")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        model.cancelInvitation()
                        dismiss()
                    }
                    .disabled(model.isProvisioning)
                }
            }
            .onChange(of: model.pendingInvitation?.preview.suggestedExtension) { _, suggestion in
                if let suggestion {
                    extensionValue = suggestion
                }
            }
        }
    }
}

private struct DialerView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var phone: PhoneService
    @State private var showingManualDialer = false

    private var people: [DialDestination] {
        model.destinations.filter { $0.kind == .person }
    }

    private var services: [DialDestination] {
        model.destinations.filter { $0.kind == .service }
    }

    private var liveCalls: [DialDestination] {
        model.destinations.filter { $0.kind == .call }
    }

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                HStack {
                    StatusPill(status: phone.registration)
                    Spacer()
                    Button {
                        model.showingSettings = true
                    } label: {
                        Image(systemName: "gearshape.fill")
                            .font(.title3)
                            .frame(width: 48, height: 48)
                            .background(.white.opacity(0.88), in: Circle())
                    }
                    .accessibilityLabel("Phone settings")
                }

                VStack(spacing: 7) {
                    Text("Who should we call?")
                        .font(.system(.largeTitle, design: .rounded, weight: .black))
                        .multilineTextAlignment(.center)
                    Text("Tap a name and RingRing handles the number.")
                        .font(.body.weight(.medium))
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 8)

                if !liveCalls.isEmpty {
                    CallMenuSection(title: "Happening now", destinations: liveCalls, enabled: phone.registration == .ready) {
                        requestMicrophoneThenCall($0)
                    }
                }

                if !people.isEmpty {
                    CallMenuSection(title: "People", destinations: people, enabled: phone.registration == .ready) {
                        requestMicrophoneThenCall($0)
                    }
                }

                if !services.isEmpty {
                    CallMenuSection(title: "More to call", destinations: services, enabled: phone.registration == .ready) {
                        requestMicrophoneThenCall($0)
                    }
                }

                if model.destinations.isEmpty {
                    RingRingCard {
                        VStack(spacing: 10) {
                            Image(systemName: "person.crop.circle.badge.questionmark")
                                .font(.system(size: 38, weight: .bold))
                                .foregroundStyle(RingRingTheme.purple)
                            Text("No call buttons yet")
                                .font(.title3.weight(.black))
                            Text("Ask your host for fresh phone settings to add your party menu.")
                                .font(.subheadline)
                                .foregroundStyle(.secondary)
                                .multilineTextAlignment(.center)
                        }
                        .frame(maxWidth: .infinity)
                    }
                }

                Button {
                    showingManualDialer = true
                } label: {
                    Label("Dial manually", systemImage: "circle.grid.3x3.fill")
                        .font(.headline)
                        .frame(maxWidth: .infinity, minHeight: 52)
                }
                .buttonStyle(.plain)
                .accessibilityHint("Opens the number keypad")

                if let error = phone.lastError {
                    Label(error, systemImage: "exclamationmark.triangle.fill")
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(.red)
                        .multilineTextAlignment(.center)
                }
            }
            .padding(20)
            .frame(maxWidth: 520)
            .frame(maxWidth: .infinity)
        }
        .safeAreaPadding(.bottom, 24)
        .sheet(isPresented: $showingManualDialer) {
            ManualDialerView(model: model, phone: phone)
                .presentationDetents([.large])
                .presentationDragIndicator(.visible)
        }
    }

    private func requestMicrophoneThenCall(_ destination: DialDestination) {
        AVAudioApplication.requestRecordPermission { allowed in
            Task { @MainActor in
                guard allowed else {
                    model.errorMessage = "Microphone access is required to make a call. Turn it on in Settings."
                    return
                }
                phone.placeCall(to: destination.dial, named: destination.label)
            }
        }
    }
}

private struct CallMenuSection: View {
    let title: String
    let destinations: [DialDestination]
    let enabled: Bool
    let call: (DialDestination) -> Void

    private let colors = [RingRingTheme.coral, RingRingTheme.blue, RingRingTheme.yellow, RingRingTheme.mint]

    var body: some View {
        VStack(alignment: .leading, spacing: 11) {
            Text(title.uppercased())
                .font(.caption.weight(.black))
                .tracking(1.6)
                .foregroundStyle(RingRingTheme.purple)
                .padding(.leading, 7)

            ForEach(Array(destinations.enumerated()), id: \.element.id) { index, destination in
                Button {
                    call(destination)
                } label: {
                    HStack(spacing: 14) {
                        ZStack {
                            RoundedRectangle(cornerRadius: 17, style: .continuous)
                                .fill(colors[index % colors.count])
                                .rotationEffect(.degrees(index.isMultiple(of: 2) ? -3 : 3))
                            Image(systemName: symbol(for: destination))
                                .font(.title2.weight(.black))
                                .foregroundStyle(RingRingTheme.ink)
                        }
                        .frame(width: 56, height: 56)
                        .accessibilityHidden(true)

                        VStack(alignment: .leading, spacing: 3) {
                            Text(destination.label)
                                .font(.title3.weight(.black))
                                .foregroundStyle(RingRingTheme.ink)
                                .multilineTextAlignment(.leading)
                            if let detail = destination.detail, !detail.isEmpty {
                                Text(detail)
                                    .font(.subheadline)
                                    .foregroundStyle(.secondary)
                                    .multilineTextAlignment(.leading)
                            }
                        }

                        Spacer(minLength: 8)

                        Image(systemName: "phone.fill")
                            .font(.headline.weight(.black))
                            .foregroundStyle(.white)
                            .frame(width: 44, height: 44)
                            .background(RingRingTheme.purple, in: Circle())
                            .accessibilityHidden(true)
                    }
                    .padding(13)
                    .frame(maxWidth: .infinity, minHeight: 82)
                    .background {
                        RoundedRectangle(cornerRadius: 24, style: .continuous)
                            .fill(.white.opacity(0.95))
                            .shadow(color: RingRingTheme.cardShadow, radius: 0, x: 5, y: 6)
                    }
                    .overlay {
                        RoundedRectangle(cornerRadius: 24, style: .continuous)
                            .stroke(RingRingTheme.ink.opacity(0.12), lineWidth: 1)
                    }
                }
                .buttonStyle(.plain)
                .disabled(!enabled)
                .opacity(enabled ? 1 : 0.52)
                .accessibilityLabel(destination.kind == .call ? destination.label : "Call \(destination.label)")
                .accessibilityHint(destination.detail ?? "Starts a private party call")
            }
        }
    }

    private func symbol(for destination: DialDestination) -> String {
        if destination.kind == .call { return "person.3.fill" }
        guard destination.kind == .service else { return "person.fill" }
        return switch destination.dial {
        case "*10": "waveform"
        case "*11": "clock.fill"
        case "*12": "cloud.sun.fill"
        case "*13": "radio.fill"
        case "*14": "sparkles"
        case "*15": "number.circle.fill"
        default: "star.fill"
        }
    }
}

private struct ManualDialerView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var phone: PhoneService
    @Environment(\.dismiss) private var dismiss
    @State private var digits = ""

    private let keys = [
        ("1", ""), ("2", "ABC"), ("3", "DEF"),
        ("4", "GHI"), ("5", "JKL"), ("6", "MNO"),
        ("7", "PQRS"), ("8", "TUV"), ("9", "WXYZ"),
        ("*", ""), ("0", "+"), ("#", ""),
    ]
    private let columns = Array(repeating: GridItem(.flexible(), spacing: 15), count: 3)

    var body: some View {
        NavigationStack {
            VStack(spacing: 18) {
                HStack {
                    Text(digits.isEmpty ? "Enter a number" : digits)
                        .font(.system(size: digits.isEmpty ? 22 : 38, weight: .bold, design: .rounded))
                        .foregroundStyle(digits.isEmpty ? .secondary : RingRingTheme.ink)
                        .monospacedDigit()
                        .lineLimit(1)
                        .minimumScaleFactor(0.7)
                    Spacer()
                    Button {
                        if !digits.isEmpty { digits.removeLast() }
                    } label: {
                        Image(systemName: "delete.left.fill")
                            .font(.title2)
                            .frame(width: 48, height: 48)
                    }
                    .disabled(digits.isEmpty)
                    .accessibilityLabel("Delete last digit")
                }
                .frame(minHeight: 54)

                LazyVGrid(columns: columns, spacing: 13) {
                    ForEach(Array(keys.enumerated()), id: \.offset) { _, key in
                        DialKey(number: key.0, letters: key.1) {
                            guard digits.count < 5 else { return }
                            digits.append(key.0)
                        }
                    }
                }

                Button {
                    requestMicrophoneThenCall()
                } label: {
                    Label(digits.isEmpty ? "Call" : "Call number", systemImage: "phone.fill")
                }
                .buttonStyle(PrimaryButtonStyle(color: RingRingTheme.mint))
                .foregroundStyle(RingRingTheme.ink)
                .disabled(!DialString.isCallable(digits) || phone.registration != .ready)
                .opacity(DialString.isCallable(digits) && phone.registration == .ready ? 1 : 0.42)
            }
            .padding(24)
            .foregroundStyle(RingRingTheme.ink)
            .navigationTitle("Dial manually")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
        }
    }

    private func requestMicrophoneThenCall() {
        AVAudioApplication.requestRecordPermission { allowed in
            Task { @MainActor in
                guard allowed else {
                    model.errorMessage = "Microphone access is required to make a call. Turn it on in Settings."
                    return
                }
                dismiss()
                phone.placeCall(to: digits)
            }
        }
    }
}

private struct DialKey: View {
    let number: String
    let letters: String
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 0) {
                Text(number)
                    .font(.system(size: 30, weight: .bold, design: .rounded))
                Text(letters.isEmpty ? " " : letters)
                    .font(.system(size: 9, weight: .bold))
                    .tracking(1.4)
            }
            .foregroundStyle(RingRingTheme.ink)
            .frame(maxWidth: .infinity, minHeight: 60)
            .background(RingRingTheme.canvas, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(letters.isEmpty ? number : "\(number), \(letters)")
    }
}

private struct StatusPill: View {
    let status: RegistrationStatus

    var body: some View {
        HStack(spacing: 7) {
            Circle()
                .fill(color)
                .frame(width: 10, height: 10)
            Text(status.label)
                .font(.footnote.weight(.bold))
        }
        .padding(.horizontal, 13)
        .frame(minHeight: 44)
        .background(.white.opacity(0.88), in: Capsule())
        .accessibilityElement(children: .combine)
    }

    private var color: Color {
        switch status {
        case .ready: RingRingTheme.mint
        case .connecting: RingRingTheme.yellow
        case .failed: RingRingTheme.coral
        case .idle: .gray
        }
    }
}

private struct CallView: View {
    @ObservedObject var phone: PhoneService
    let destinationLabel: String?
    @State private var showingKeypad = false

    var body: some View {
        VStack(spacing: 26) {
            Spacer()

            ZStack {
                Circle()
                    .fill(RingRingTheme.blue)
                    .frame(width: 142, height: 142)
                Image(systemName: "phone.fill")
                    .font(.system(size: 55, weight: .black))
                    .foregroundStyle(.white)
                    .rotationEffect(.degrees(phone.callPhase == .incoming ? 18 : -18))
            }
            .accessibilityHidden(true)

            VStack(spacing: 8) {
                Text(phaseLabel)
                    .font(.subheadline.weight(.black))
                    .tracking(1.5)
                    .foregroundStyle(RingRingTheme.purple)
                Text(destinationLabel ?? (phone.remoteExtension.isEmpty ? "RingRing call" : "Extension \(phone.remoteExtension)"))
                    .font(.system(.largeTitle, design: .rounded, weight: .black))
                    .multilineTextAlignment(.center)
                if let connectedAt = phone.connectedAt {
                    TimelineView(.periodic(from: connectedAt, by: 1)) { timeline in
                        Text(duration(from: connectedAt, to: timeline.date))
                            .font(.title3.monospacedDigit().weight(.semibold))
                            .foregroundStyle(.secondary)
                    }
                }
            }

            if phone.callPhase == .incoming {
                HStack(spacing: 34) {
                    RoundCallButton(title: "Decline", symbol: "phone.down.fill", color: RingRingTheme.coral) {
                        phone.hangUp()
                    }
                    RoundCallButton(title: "Answer", symbol: "phone.fill", color: RingRingTheme.mint) {
                        phone.answer()
                    }
                }
            } else {
                HStack(spacing: 18) {
                    RoundCallButton(title: phone.isMuted ? "Unmute" : "Mute", symbol: phone.isMuted ? "mic.slash.fill" : "mic.fill", color: phone.isMuted ? RingRingTheme.yellow : .white) {
                        phone.setMuteRequested(!phone.isMuted)
                    }
                    RoundCallButton(title: "Keypad", symbol: "circle.grid.3x3.fill", color: .white) {
                        showingKeypad = true
                    }
                    RoundCallButton(title: "Speaker", symbol: "speaker.wave.2.fill", color: phone.isSpeakerOn ? RingRingTheme.yellow : .white) {
                        phone.toggleSpeaker()
                    }
                }

                RoundCallButton(title: "Hang up", symbol: "phone.down.fill", color: RingRingTheme.coral) {
                    phone.hangUp()
                }
            }

            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity)
        .sheet(isPresented: $showingKeypad) {
            CallKeypadView(phone: phone)
                .presentationDetents([.medium])
                .presentationDragIndicator(.visible)
        }
    }

    private var phaseLabel: String {
        switch phone.callPhase {
        case .incoming: "INCOMING CALL"
        case .dialing: "CALLING"
        case .ringing: "RINGING"
        case .active: "CONNECTED"
        case .ended: "CALL ENDED"
        case .idle: "READY"
        }
    }

    private func duration(from start: Date, to now: Date) -> String {
        let seconds = max(0, Int(now.timeIntervalSince(start)))
        return String(format: "%02d:%02d", seconds / 60, seconds % 60)
    }
}

private struct RoundCallButton: View {
    let title: String
    let symbol: String
    let color: Color
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 8) {
                ZStack {
                    Circle()
                        .fill(color)
                        .shadow(color: RingRingTheme.cardShadow, radius: 0, x: 4, y: 5)
                    Image(systemName: symbol)
                        .font(.title2.weight(.bold))
                }
                .frame(width: 66, height: 66)
                Text(title)
                    .font(.footnote.weight(.bold))
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
    }
}

private struct CallKeypadView: View {
    @ObservedObject var phone: PhoneService
    @Environment(\.dismiss) private var dismiss
    private let keys = ["1", "2", "3", "4", "5", "6", "7", "8", "9", "*", "0", "#"]
    private let columns = Array(repeating: GridItem(.flexible(), spacing: 12), count: 3)

    var body: some View {
        VStack(spacing: 14) {
            HStack {
                Text("Call keypad")
                    .font(.title2.weight(.black))
                Spacer()
                Button("Done") { dismiss() }
                    .font(.headline)
            }

            LazyVGrid(columns: columns, spacing: 10) {
                ForEach(keys, id: \.self) { key in
                    Button {
                        phone.sendDigit(key)
                    } label: {
                        Text(key)
                            .font(.title2.monospacedDigit().weight(.bold))
                            .frame(maxWidth: .infinity, minHeight: 52)
                            .background(RingRingTheme.canvas, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Send \(key)")
                }
            }
        }
        .padding(24)
        .foregroundStyle(RingRingTheme.ink)
    }
}

private struct SettingsView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var phone: PhoneService
    @Environment(\.dismiss) private var dismiss
    @State private var confirmingDisconnect = false
    @StateObject private var ringtonePreview = RingtonePreviewPlayer()
    @AppStorage(RingRingRingtone.defaultsKey) private var ringtoneRaw = RingRingRingtone.ringRingDouble.rawValue

    var body: some View {
        NavigationStack {
            List {
                Section("This phone") {
                    LabeledContent("Extension", value: model.account?.extension ?? "—")
                    LabeledContent("Server", value: model.account?.server ?? "—")
                    LabeledContent("Status", value: phone.registration.label)
                }

                Section("Privacy") {
                    Label("SIP sign-in is protected with TLS", systemImage: "lock.fill")
                    Label("Calls stay inside your RingRing party", systemImage: "person.3.fill")
                    Label("No public phone or emergency calling", systemImage: "phone.down.fill")
                    Text("Call audio is server-mediated and is not end-to-end encrypted in this test build.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                Section("Background calls") {
                    LabeledContent("Incoming calls", value: model.backgroundCalls.label)
                    Text("RingRing uses Apple’s VoIP wake-up service and the system call screen so calls can ring while the app is in the background or the iPhone is locked.")
                        .font(.footnote)
                    if model.backgroundCalls == .unavailable {
                        Button("Try background setup again") { model.refresh() }
                    }
                }

                Section("Ringtone") {
                    ForEach(RingRingRingtone.allCases) { ringtone in
                        Button {
                            ringtoneRaw = ringtone.rawValue
                            phone.setRingtone(ringtone)
                            ringtonePreview.play(ringtone)
                        } label: {
                            HStack(spacing: 12) {
                                Image(systemName: ringtonePreview.playing == ringtone ? "speaker.wave.2.fill" : "music.note")
                                    .foregroundStyle(RingRingTheme.purple)
                                    .frame(width: 26)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(ringtone.title)
                                        .foregroundStyle(RingRingTheme.ink)
                                    Text(ringtone.detail)
                                        .font(.footnote)
                                        .foregroundStyle(.secondary)
                                }
                                Spacer()
                                if ringtoneRaw == ringtone.rawValue {
                                    Image(systemName: "checkmark.circle.fill")
                                        .foregroundStyle(RingRingTheme.mint)
                                }
                            }
                            .frame(minHeight: 48)
                        }
                    }
                }

                Section("Open source") {
                    Link("Linphone SDK licensing", destination: URL(string: "https://www.linphone.org/en/legal/")!)
                }

                Section {
                    Button("Disconnect this phone", role: .destructive) {
                        confirmingDisconnect = true
                    }
                } footer: {
                    Text("This removes the SIP setup from this iPhone. Your host can issue a new code later.")
                }
            }
            .navigationTitle("Phone settings")
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .confirmationDialog("Disconnect this phone?", isPresented: $confirmingDisconnect, titleVisibility: .visible) {
                Button("Disconnect", role: .destructive) { model.disconnect() }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("This removes its private calling credentials from the Keychain.")
            }
            .onDisappear { ringtonePreview.stop() }
        }
    }
}
