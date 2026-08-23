# TestFlight notes

## 0.1.0 (4) — Finish invitations in the app

What to test:

1. Install build 4 with no saved account. Make a normal **Invite one phone** link in the host web app, then scan its QR with **Scan invite or setup code**. Confirm the app shows the correct party name and a suggested extension without consuming the invitation.
2. Choose a neutral phone name and extension, leave **Adult extension (18+)** off unless intentionally testing an adult-only party line, and tap **Join and set up this iPhone**. Confirm the app becomes registered and shows named call buttons.
3. From Mail or Messages, tap a private `https://ringring.live/join/…` link. When RingRing is installed, confirm it opens the same native setup sheet. A used, expired, canceled, or malformed invitation should show one generic unavailable message.
4. Try to claim an extension that another invitation has just taken. Confirm RingRing offers a fresh suggestion and the original invitation remains usable.
5. Confirm the older preconfigured RingRing app QR and pasted `/api/v1/phone-provisioning/…` setup link still perform one-step setup.
6. Confirm **People** lists only other active phones in the same party and **More to call** lists only enabled services. Tap a person and **Echo test**, then verify the friendly name remains visible during the private call.

Known limits:

- The complete menu is a setup-time snapshot. Party changes require fresh phone settings in this build.
- Reliable incoming ringing after iOS suspends or force-quits the app still requires the planned PushKit/APNs bridge.
- Universal-link taps are enabled for `ringring.live`; other self-hosted domains continue to work through scan and paste unless their own signed build includes that associated domain.
