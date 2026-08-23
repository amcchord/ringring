# RingRing for iOS

RingRing is a native SwiftUI SIP endpoint for the private RingRing family phone network. It scans either a general party invitation or a preconfigured phone QR, completes extension setup in the app when needed, stores the resulting SIP credential and party call-menu snapshot in the iOS Keychain, registers over SIP TLS, places and receives party calls, and presents calls through CallKit.

## Requirements

- Xcode 26 or newer
- XcodeGen (`brew install xcodegen`)
- An iPhone running iOS 17 or newer for microphone, camera, and real SIP testing
- A reachable RingRing deployment with trusted SIP TLS on port 5061

## Generate and test

```sh
cd ios
xcodegen generate
xcodebuild -project RingRing.xcodeproj -scheme RingRing \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test
```

## App Store presentation

The English App Store copy lives under `fastlane/metadata/en-US`. Generate the three privacy-safe 6.9-inch iPhone screenshots with:

```sh
./scripts/capture-app-store-screenshots.sh
```

The script boots an iPhone 17 Pro Max simulator, uses debug-only generic fixtures, captures the real SwiftUI screens at 1320×2868, and validates every output size. Generated screenshots remain gitignored; upload them with `node ../scripts/appstore-connect.mjs sync`. That command obtains the tracked shared Team credential directly from AustinLand, keeps the private key in memory, and never prints or writes it.

App Store Connect gets the app icon from the uploaded build's `AppIcon` asset. The canonical 1024px source is `RingRing/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png`; changing it therefore requires a new build, not a metadata-only upload.

The simulator can exercise onboarding and UI tests, but a physical iPhone is required to prove registration, two-way audio, route switching, incoming ringing, and CallKit behavior. After connecting, tap **Echo test** for RingRing's two-way audio check; the app keeps the underlying `*10` code out of the main call menu.

## Provisioning contract

The app accepts the general HTTPS `/join/<token>` link shown in a host's 48-hour invitation. It previews only the party name and a safe available extension, then asks for the phone name, extension, and adult-service permission in a native Memphis-style form. The final JSON claim consumes the invitation and returns the new SIP account plus call menu atomically. If another invitee takes the suggested extension first, the app refreshes the suggestion without losing the invitation.

Preconfigured phones still accept `ringring://join?provision=https://…/<setup-path>/<token>` deep links or the nested HTTPS provisioning URL itself. The preferred path is `/api/v1/phone-provisioning/<token>`; `/provision/ios/<token>` remains accepted for setup cards issued to released builds. Those tokens expire after 30 minutes and are consumed by the first successful GET through either route. Both flows return a versioned JSON response containing one SIP account and a bounded snapshot of currently dialable names and enabled party services. It omits the phone's own member, members without an active phone, party/host/device labels, email, provider identifiers, and every non-calling field. The app validates every label and hidden dial target, rejects duplicate or oversized menus, and keeps the payload in the device-only Keychain rather than `UserDefaults`, logs, or source control.

The reference build supports `applinks:ringring.live`, so tapping a `https://ringring.live/join/…` invitation opens the installed app directly. The scanner and paste controls support any correctly configured HTTPS RingRing deployment; a self-hosted operator who also wants universal-link taps must add that hostname to their own signed app build and publish the matching Apple association file.

The vendor-neutral client contract, error semantics, examples, and implementation checklist live in [the phone API guide](../docs/PHONE_API.md). Every running server also exposes its embedded OpenAPI description at `/openapi.yaml`.

The named menu is the default phone screen. Tapping a button places the corresponding party-scoped call without showing or asking for its extension. A manual keypad remains behind **Dial manually** for compatibility. The initial menu is a setup-time snapshot; party changes require fresh phone settings until an authenticated directory-refresh channel is added.

## Incoming-call limitation in this first TestFlight build

Calls arrive while the app's SIP core remains active, including during an active call and ordinary short background periods. Reliable ringing after iOS fully suspends or terminates the app requires a RingRing-controlled PushKit/APNs wake path and Asterisk call holding. That server feature is deliberately not faked in this build and is documented as follow-up work before a public release.

## Linphone SDK license

The app currently links Linphone SDK 5.5.15 under the GNU Affero General Public License v3.0. RingRing's complete app source is available in this directory and the exact package version/checksums are pinned by Swift Package Manager. See [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Before public App Store release, confirm the intended AGPL distribution path with counsel or obtain Belledonne Communications' proprietary Linphone SDK license. TestFlight distribution should not be treated as resolving that licensing decision.
