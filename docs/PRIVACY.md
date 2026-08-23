# RingRing Family Phone privacy notice

Effective: August 23, 2026

RingRing Family Phone is a native endpoint for a private, self-hosted RingRing phone party. This notice covers the iOS app. The operator of the RingRing server you join is responsible for that server and its users.

## Data the app uses

When you scan or paste a one-time setup link, the app receives the RingRing server address, a generated SIP sign-in, the extension assigned by the party host, and a small menu of destinations that phone may call. The app stores this setup in the iOS device-only Keychain. It does not place the SIP password in `UserDefaults`, analytics, logs, or screenshots. While the app is active, it uses that sign-in over HTTPS to refresh same-party names, enabled services, and buttons for calls happening now; the server does not return a call history.

The camera is used only to scan the setup QR. The microphone and audio output are used for calls. RingRing does not upload a camera image or keep a QR image.

## Calls

Calls are carried through the RingRing server and are not end-to-end encrypted. The iOS app does not record calls, create transcripts, keep a call history, or connect to the public telephone network. RingRing cannot place emergency calls.

A party host may enable optional server-side voice services. Those services can contact their identified providers from the RingRing server; for example, the adults-only AI preview gives a spoken provider disclosure before sending call audio to that provider. The iOS app does not contain or receive the party's provider credentials.

To receive a call while backgrounded or locked, the app gives its RingRing server an Apple PushKit VoIP token for this iPhone. The server stores it encrypted and sends Apple only an opaque call identifier to wake the app; the notification does not contain party names, member names, extensions, SIP credentials, call audio, or a transcript. The later private SIP call supplies the caller identity. Disconnecting or revoking the phone removes the server registration, and Apple may process delivery metadata under Apple's own terms.

## Tracking and sharing

The app contains no advertising SDK, cross-app tracking, analytics tracker, or data broker integration. It uses Apple system services such as Keychain, PushKit, APNs, and CallKit to provide phone behavior. Ringtone choices and previews stay on the iPhone. Apple separately processes push delivery, App Store, and TestFlight activity under Apple's own terms.

## Retention and control

Disconnecting the phone removes its RingRing setup from the iPhone Keychain. A party host can revoke the phone on the RingRing server. Server records and backups follow the policy chosen by that self-hosted server's operator.

## Contact

For privacy questions or support, use the [RingRing support page](SUPPORT.md). Do not include setup links, QR codes, SIP credentials, family names, or call details in a public issue.
