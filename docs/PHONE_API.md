# Phone API

RingRing exposes a small, versioned HTTP API so a trusted native application can join a private party and become a phone without scraping web pages or depending on the RingRing iOS implementation.

The contract is intentionally narrow: setup returns one SIP-over-TLS account and a safe initial button menu; that active device credential can later refresh the private menu, discover currently joinable same-party calls, and register an Apple VoIP push token. It does not expose a public directory, cross-party presence, host controls, call history, OpenAI credentials, or any route to the regular telephone network.

## Discover the contract

Every deployment serves its embedded OpenAPI 3.1 description at:

```text
GET /openapi.yaml
```

The source-of-truth file is [`internal/provisioning/phone-provisioning.openapi.yaml`](../internal/provisioning/phone-provisioning.openapi.yaml). The running app and the repository therefore ship the same contract. The public OpenAPI document permits cross-origin reads so documentation and client-generation tools can load it; credential responses deliberately do not enable browser CORS.

## General invitation flow

A general invitation is the 48-hour `/join/{token}` link and QR a host makes before choosing a member name or extension. A native app may finish those choices itself:

1. Accept only an HTTPS URL whose exact path is `/join/{token}` with a 43-character URL-safe token. Do not send the URL to a browser, link preview, analytics service, or QR service.
2. Derive the API URL on the same origin: `/api/v1/phone-invitations/{token}`.
3. `GET` that API URL with redirects, cookies, and caching disabled. This preview does **not** consume the invitation and returns only `version`, `party_name`, and `suggested_extension`.
4. Show the party name, collect a 1–40 character phone/member label, and offer the suggested 2–5 digit extension. Public emergency and crisis numbers are reserved and must not be offered.
5. `POST` one `application/json` claim to the same URL:

   ```json
   {
     "display_name": "Studio phone",
     "extension": "103",
     "device_label": "Phone app"
   }
   ```

6. A `200` atomically creates the member and device, consumes the invitation, and returns the same validated SIP account and call-menu document described below. Store both together in device-protected secret storage.
7. A `409` means another claim won the requested extension. The invitation remains usable; preview it again and offer the new suggestion. A `410` means the invitation can no longer be used, without disclosing why.

Invalid JSON, unknown fields, reserved extensions, and invalid labels do not consume an otherwise valid invitation. Credential-bearing responses do not permit browser CORS. The bearer token plus the non-simple JSON content type prevent an unrelated browser origin from claiming an invitation without a successful preflight; RingRing does not authorize that preflight and native clients must not attach cookies.

On `ringring.live`, the iOS app also declares `applinks:ringring.live`, while the server publishes `/.well-known/apple-app-site-association` for `/join/*`. This makes a private invitation tap open directly in an installed app. Self-hosted URLs remain supported through the app scanner and paste flow; adding universal-link taps for another hostname requires that app build to include that exact associated domain and the deployment to serve its matching association document.

## Preconfigured phone provisioning flow

1. A party host creates or rotates settings for one phone.
2. The private, no-store setup card shows a canonical phone API URL and the RingRing-app compatibility QR.
3. The phone app receives the URL through a direct paste, app-owned scanner, or a private handoff. It must not ask a browser, link preview, analytics service, or QR service to fetch it.
4. The app sends one `GET` with redirects disabled.
5. After a `200`, the app validates the entire document, stores the SIP password in device-protected secret storage, and registers to the exact server over TLS on port `5061` with ordinary certificate and hostname verification. That same device credential authenticates only the configured-phone endpoints described below.
6. The app renders `destinations` as friendly buttons. It may keep a clearly secondary manual dialer for compatible RingRing extensions and star codes.

The preferred endpoint is:

```text
GET /api/v1/phone-provisioning/{token}
```

Released RingRing iOS builds use this compatibility alias:

```text
GET /provision/ios/{token}
```

Both paths consume the same token. They are alternatives, not two requests a client should make.

## Invitation authentication and lifecycle

Both token types are 43-character URL-safe bearer values backed by 32 random bytes, and RingRing stores only their hashes. A general invitation expires after 48 hours and is consumed only by one successful web or native claim. A preconfigured phone token expires after 30 minutes and is consumed by its first successful XML or JSON fetch. Rotation replaces a phone token; phone revocation or deletion removes it.

Because the token sits in the URL:

- never log, persist, instrument, preflight, crawl, or send it to a crash/analytics service;
- never place a real token in source code, shell history, screenshots, support tickets, or API documentation tools;
- disable redirects and reject user info, query strings, fragments, non-HTTPS origins, and unexpected paths;
- treat `410 Gone` as a request for fresh settings, without trying to determine why the link is unavailable;
- after any ambiguous network failure, let the person retry once in the same trusted app; if the server then returns `410`, request fresh settings;
- after `500`, request fresh settings because the server may already have atomically consumed the token before a later safe-encoding step failed.

The canonical API returns bounded `application/problem+json` documents for `410`, `429`, and `500`. A `429` includes `Retry-After: 60`. Error bodies never echo the token or distinguish malformed, unknown, expired, used, replaced, and revoked links.

## Successful response

The response is `application/json`, includes `Cache-Control: no-store, max-age=0`, and has this shape:

```json
{
  "version": 1,
  "sip": {
    "server": "ringring.example",
    "port": 5061,
    "transport": "tls",
    "username": "483726",
    "password": "927461830572",
    "extension": "101"
  },
  "destinations": [
    {
      "kind": "person",
      "label": "Studio phone",
      "dial": "102"
    },
    {
      "kind": "service",
      "label": "Echo test",
      "detail": "Hear your own voice come back.",
      "dial": "*10"
    }
  ]
}
```

All values above are fictitious. `username` is the SIP authentication identity; `extension` is the short party number shown to the person using the phone.

### Version 1 invariants

| Field | Contract |
| --- | --- |
| `version` | Exactly `1`. Reject unsupported versions. |
| `sip.server` | Validated RingRing DNS name or IP literal. |
| `sip.port` | Exactly `5061`. |
| `sip.transport` | Exactly `tls`. |
| `sip.username` | 1–128 ASCII letters, digits, `_`, or `-`. |
| `sip.password` | 1–256 bytes with no control characters. Sensitive. |
| `sip.extension` | 2–5 decimal digits inside this party. |
| `destinations` | At most 128 unique dial targets. An empty list is valid. |
| `kind` | `person`, `service`, or `call`. Setup documents contain the first two; refreshed state may add `call`. |
| `label` | 1–80 Unicode scalar values, trimmed, with no controls. |
| `detail` | Optional; at most 160 Unicode scalar values, trimmed, with no controls. |
| `dial` | A 2–5 digit party extension, an explicit two-digit RingRing star code, or—for `kind: call` only—the party conference join form `*16` plus a 2–5 digit extension. Never a PSTN number. |

The menu excludes the provisioned phone's own extension, every unrelated party, members without an active saved phone, and services that are not currently routable for this extension. It contains labels and hidden dial targets only—no party/host/device labels, email, provider identifiers, readiness, presence, timestamps, or call records.

The setup list is an initial snapshot. A configured client should replace it with validated responses from `GET /api/v1/phone/state`; the server-side party dialplan remains authoritative and still blocks revoked, disabled, or cross-party calls.

## Configured-phone state and background calls

Configured-phone requests use HTTP Basic authentication over HTTPS. The username is the device's globally unique SIP auth username and the password is that device's SIP password. Clients must build the `Authorization` header only for the exact RingRing origin, disable redirects, cookies, caching, and request logging, and never send the credential to another host. Revocation and credential rotation immediately invalidate these requests.

### Refresh the private menu

```text
GET /api/v1/phone/state
Authorization: Basic <device credential>
```

A successful response contains `version: 1`, the phone's own `extension`, and a current `destinations` array. Person and service entries follow the setup invariants. A `kind: call` entry names one currently active, joinable conference inside this device's party and uses the fixed `*16<extension>` dial form. Active-call state comes from Asterisk, is not stored as call history, and never crosses the party boundary. Clients should refresh while foregrounded, retain the last fully validated response through transient failures, and stop using the credential after `401`.

### Refresh a Grandstream phonebook

```text
GET /api/v1/phone/grandstream-phonebook.xml
Authorization: Basic <device credential>
```

This compatibility endpoint returns Grandstream `AddressBook` XML rather than JSON. It includes other active members in the device's party and the `*` services currently enabled for that extension. It omits the phone's own extension and live-call buttons because Grandstream may retain a downloaded address book between refreshes. The WP826 setup file configures this endpoint over HTTPS, refreshes every five minutes, and lets RingRing remove stale managed entries; unrelated manual contacts therefore do not belong in the local address book of a RingRing-managed handset.

### Register an Apple VoIP token

```text
PUT /api/v1/phone/push
Authorization: Basic <device credential>
Content-Type: application/json

{"token":"<64 lowercase hex characters>","environment":"production"}
```

The `32`-byte PushKit VoIP token is encrypted with the deployment master key and bound to this device. The server accepts only the configured APNs environment; TestFlight/App Store clients use `production`. Re-register whenever PushKit supplies a new token. Remove it with authenticated `DELETE /api/v1/phone/push` when disconnecting the phone. Credential rotation, phone revocation, device deletion, PushKit invalidation, and a permanent APNs rejection also remove the registration.

For an incoming extension call, RingRing authenticates the caller from Asterisk's endpoint, selects registrations only for the target extension in the same party, and sends a content-minimized VoIP notification containing only `aps: {}` and an opaque call UUID. Names, extensions, SIP identities, passwords, IP addresses, and call audio are not sent to Apple. The server briefly gives the app time to report the UUID through CallKit and refresh SIP, then continues the ordinary party-scoped dial. APNs is a wake signal, not call signaling or authorization; the later authenticated SIP invitation remains authoritative.

`PUT` returns `503` when the deployment has not configured APNs. `401` is deliberately generic, and all responses are `no-store` with no browser CORS. Clients must call the PushKit completion handler promptly after reporting to CallKit. A physical iPhone is required to verify foreground, background, and lock-screen behavior; the operating system may suppress wake after an explicit force-quit.

## Client security checklist

- Accept only the exact `/join/`, canonical invitation API, canonical provisioning API, or documented compatibility path with a 43-character token.
- Require HTTPS outside a loopback-only development build.
- Disable redirects, HTTP credential fields, cookies, caches, and automatic request logging.
- Require a `200` and JSON content type; cap the body at 256 KiB before decoding.
- Decode required fields into explicit types, ignore unknown optional fields, validate every invariant above, and reject duplicate `dial` values.
- Persist the account and menu atomically so a crash cannot leave a credential without its validated metadata.
- Keep the password in device-protected secret storage; never put it in preferences, logs, metrics, CallKit labels, notifications, or clipboard history.
- Register only with the supplied TLS server/port and keep certificate validation enabled.
- Show friendly labels for known calls. Keep the technical target internal to the SIP engine.
- Do not infer that a listed person is online, and do not add PSTN, emergency, trunk, arbitrary URI, transfer, or cross-party routing.
- Provide an explicit disconnect/reset action that removes the local credential and menu.
- Use the device credential only for the documented state and push endpoints on its exact HTTPS origin; never expose it to browser code.
- Treat APNs as an opaque wake hint, report a generic CallKit call immediately, and reveal a friendly caller label only after the SIP invitation arrives.
- For general invitations, treat extension suggestions as advisory, handle `409` without discarding the token, and never claim until the person confirms the displayed party and choices.

## Compatibility and evolution

New optional response fields may be added within version 1; the OpenAPI schemas therefore leave unknown properties available for forward compatibility. Clients should ignore unknown fields but continue to validate every required field and every optional field they use. A change that alters the meaning or safety boundary of an existing field requires a new integer `version` and a new documented client decision; clients must not guess how to consume an unsupported version.

`/provision/ios/{token}` remains available for released RingRing apps but is deprecated for new integrations. It returns the same successful JSON document and shares token consumption, caching, privacy, and rate limits with the canonical endpoint. Its legacy error response is HTML, which is why new clients should use the canonical API.

The API now includes the server half of RingRing's PushKit/APNs wake architecture. Other platforms can implement an equivalent device-authenticated wake registration without weakening the same-party SIP authorization boundary; such an endpoint needs its own versioned schema, encrypted token storage, revocation behavior, minimized payload, and explicit privacy documentation.
