# TestFlight notes

## 0.1.0 (3) — One-tap party calls

What to test:

1. Install build 3 and confirm the main screen leads with large named call buttons instead of a keypad.
2. With fresh phone settings from a RingRing server containing this matching call-menu change, confirm **People** lists other active phones in the same party and **More to call** lists only the party's currently enabled services. The phone's own entry and every underlying extension/service code should stay hidden.
3. Tap a person and a service such as **Echo test**. Confirm the microphone prompt appears when needed, the correct private call starts, and the call screen keeps showing the friendly name.
4. Open **Dial manually** and confirm the numeric keypad remains available as a fallback.
5. Upgrade an already-configured build 1 install. Its SIP credential should continue to work, with **Echo test** and **Pick another extension** available until fresh settings provide the complete party snapshot.
6. Confirm another party's names never appear and a revoked/offline-only member is not included in a newly provisioned menu.

Known limits:

- The complete menu is a setup-time snapshot. Party changes require fresh phone settings in this build.
- Reliable incoming ringing after iOS suspends or force-quits the app still requires the planned PushKit/APNs bridge.
- The matching server-side provisioning change is in this repository but is not deployed as part of the TestFlight upload.
