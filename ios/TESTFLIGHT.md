# TestFlight notes

## 0.1.0 (5) — Background calls, live joins, and ringtones

What to test on a physical iPhone:

1. Install build 5, open it once, and confirm **Settings → Background calls** says the iPhone is ready. If it offers **Try again**, tap it once on a stable network.
2. Put RingRing in the background, wait at least one minute, then call its extension from another party phone. Confirm the normal iPhone incoming-call screen appears and answering connects two-way audio.
3. Lock the iPhone and repeat. Answer directly from the lock screen; confirm the friendly caller label appears when SIP finishes reconnecting and that audio works in both directions.
4. In **Settings → Ringtone**, preview and select each of the four RingRing sounds. Background the app and confirm the selected sound is used for the next incoming call.
5. Start a party call between two other phones. Confirm **Happening now** appears in the app within a few seconds, tap its friendly join button, and verify all participants share the same call. Hang up and confirm the live button disappears.
6. Rename or add a party phone and confirm the main call menu refreshes without rescanning setup. Briefly disable the network and confirm the last safe menu remains usable rather than being replaced by an empty one.
7. Recheck a direct person button, **Echo test**, manual dialing, mute, speaker, keypad tones, incoming decline, and disconnect/reset.

Known limits:

- PushKit and CallKit delivery require a real iPhone and the production server; Simulator only verifies UI and deterministic app logic.
- iOS can suppress background launch after the person explicitly force-quits the app. Relaunch RingRing once before testing.
- Live-call buttons are a current snapshot, not call history or public presence, and appear only to authenticated phones in the same party.
- Universal-link taps are enabled for `ringring.live`; other self-hosted domains continue to work through scan and paste unless their own signed build includes that associated domain.

## 0.1.0 (4) — Finish invitations in the app

What to test:

1. Install build 4 with no saved account. Make a normal **Invite one phone** link in the host web app, then scan its QR with **Scan invite or setup code**. Confirm the app shows the correct party name and a suggested extension without consuming the invitation.
2. Choose a neutral phone name and extension, then tap **Join and set up this iPhone**. Confirm the app becomes registered and shows named call buttons.
3. From Mail or Messages, tap a private `https://ringring.live/join/…` link. When RingRing is installed, confirm it opens the same native setup sheet. A used, expired, canceled, or malformed invitation should show one generic unavailable message.
4. Try to claim an extension that another invitation has just taken. Confirm RingRing offers a fresh suggestion and the original invitation remains usable.
5. Confirm the older preconfigured RingRing app QR and pasted `/api/v1/phone-provisioning/…` setup link still perform one-step setup.
6. Confirm **People** lists only other active phones in the same party and **More to call** lists only enabled services. Tap a person and **Echo test**, then verify the friendly name remains visible during the private call.

Known limits:

- The complete menu is a setup-time snapshot. Party changes require fresh phone settings in this build.
- Reliable incoming ringing after iOS suspends or force-quits the app still requires the planned PushKit/APNs bridge.
- Universal-link taps are enabled for `ringring.live`; other self-hosted domains continue to work through scan and paste unless their own signed build includes that associated domain.
