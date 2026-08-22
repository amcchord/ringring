# Architecture

## System shape

RingRing deliberately starts as a small, self-hostable system:

```text
Internet
  |
  +-- HTTPS 443 -------------------- Caddy ---- RingRing web app ---- SQLite
  |
  +-- SIP 5060/UDP ----------------- Asterisk
  |                                     |  ^
  +-- RTP 10000-10199/UDP --------------+  |
                                            |
                            generated config + private AMI reload
                                            |
                                       RingRing app
```

Caddy, the app, and Asterisk run as separate containers. Only Caddy's HTTPS port and Asterisk's SIP/RTP ports are public. The database, Asterisk Manager Interface (AMI), app origin, FastAGI service, and AI AudioSocket bridge stay on private container networks.

## Domain model

- **User**: an adult host with a RingRing-native account; optional federated login can be added without changing party ownership.
- **Party**: the hard tenant and dialing boundary. It owns an extension range and optional voice-service settings.
- **Host**: a user's administrative role in a party.
- **Invitation**: a single-use, expiring, hashed bearer token that permits one member/device enrollment.
- **Member**: a person represented by a display label and one extension within a party. An account is not required.
- **Device**: a SIP registration identity and encrypted secret associated with a member.
- **Provisioning link**: a single-use, 30-minute, hashed bearer token that lets Linphone fetch only one device's account configuration.
- **Service line**: a party-enabled special extension such as time, weather, radio, or OpenAI voice.

## Call isolation

Every registered endpoint has a globally unique, random SIP username and an Asterisk endpoint context derived from its party ID. The generated dialplan emits only that party's members and enabled service extensions into the context. There is no route from party contexts to trunks or a global outbound context.

Extension `101` can therefore exist in many parties without collision. A device cannot choose its context or construct a cross-party endpoint name.

## NAT and phone compatibility

Most devices will register outbound from home networks. PJSIP endpoints use symmetric RTP, forced response ports, rewritten contacts, server-relayed media, keepalives, and conservative codecs (`ulaw`, `alaw`, and optional `g722`). The setup UI will give exact registrar, username, secret, transport, and extension values instead of exposing Asterisk terminology.

UDP SIP is supported for older adapters. SIP TLS is planned for capable devices but is not exposed by the first deployment. Every device has a random registration identity; PJSIP unidentified-request limits and a checked-in Fail2Ban policy provide layered registration protection.

## Configuration flow

The database is authoritative. When a device or party setting changes, the app:

1. Validates the full desired configuration.
2. Renders party-scoped PJSIP and dialplan fragments into a temporary file.
3. Atomically replaces the generated files on a shared volume.
4. Requests a reload over private AMI.
5. Logs reload failures while preserving the database as the recoverable source of truth. One-time credentials already issued to a person stay visible even if a reload needs to be retried.

On startup, the app regenerates all telephony configuration from the database.

## One-time Linphone setup

The one-time setup screen keeps the universal registrar, username, password, extension, and transport fields for ATAs, desk phones, and arbitrary SIP apps. It also renders a Linphone-specific QR according to [Linphone's remote-provisioning format](https://wiki.linphone.org/xwiki/wiki/public/view/Linphone/Remote%20Provisioning/). No external QR or provisioning service receives the credentials.

The QR contains an HTTPS URL, not the SIP password. Its 32-byte random token is stored only as a SHA-256 digest, expires after 30 minutes, and is consumed transactionally by the first `GET`. `HEAD` does not consume it. The response decrypts only that device's SIP secret and returns a transient Linphone XML document with the generated username, extension, RingRing registrar, UDP port, and password; it contains no member name, party name, host data, or integration key. A second fetch receives a generic gone response. Rotation atomically replaces any prior link, while revocation and cascading device deletion remove it.

Setup pages and provisioning responses are `no-store`, `no-referrer`, and `noindex`; token-bearing request paths are masked in application logs and separately rate limited. The UI warns people to use Linphone's scanner instead of a normal camera/browser because an ordinary fetch would consume the link without configuring the app. A desktop `sip-linphone` handler and a copyable remote-provisioning URL are secondary paths. This configures the account only and does not assert mobile push or background-ringing support.

The optional `make linphone-smoke` interoperability test packages the checksum-pinned official Linphone Python engine separately from RingRing. On a disposable internal Docker network, it serves one XML document generated by the same production function, requires Linphone to fetch it exactly once and create exactly one account, then independently asks Asterisk to confirm that contact is registered and reachable. The harness uses fixed smoke-only credentials, no production database or environment, and no host ports; it removes its containers, network, and generated state on exit. This proves the provisioning format and SIP registration path, not mobile camera UX, push delivery, background ringing, or real NAT traversal.

The authenticated party page reads current PJSIP contact state through the same private AMI boundary. Asterisk emits one `ContactList` event per registered contact and a completion event; the app immediately reduces those events to a generated SIP username and normalized reachability state. Contact URIs, source addresses, ports, call IDs, and user-agent strings never enter the template or database. Results are not cached or exposed on public or invitation pages. A failed or slow AMI query produces an explicit “status unavailable” hint while leaving host controls usable.

Deletion follows the same source-of-truth rule. Removing a member cascades through its devices; removing a party cascades through invitations, services, members, devices, and encrypted party credentials. The app then reconciles the smaller desired configuration with Asterisk. If the private reload fails, the deletion remains authoritative in SQLite, the host receives an operator-retry warning, and the next successful reconciliation or app startup removes the stale generated route.

A party with an OpenAI project has an additional external boundary: the app retrieves and archives that project before deleting local party state. An archived response is accepted on retry, so a completed external archive followed by a failed local delete can safely be retried. An archive error fails closed and leaves the party, its members, and encrypted runtime key intact. A host account is deleted only after a transaction confirms that it owns no parties.

## Recovery boundary

Durable recovery state consists of the complete SQLite app-state directory plus both root-readable deployment environment files. The environment files are part of the recovery boundary because the application master key decrypts SIP and party service credentials; a database copy without that key is intentionally insufficient. Generated Asterisk configuration and synthesized voice cache files are derived state and are regenerated after restore.

The checked-in backup workflow stops the only SQLite writer long enough for a consistent WAL-mode snapshot, restarts it, and verifies the copy without network access. The restore drill runs an extracted copy in a separate network-disabled container with no published ports, no organization admin key, and no AMI secret. It checks database structure, credential decryptability, application readiness, and telephony regeneration without mounting or replacing production state.

## Host authentication

Native host accounts use a case-insensitive username and an Argon2id password hash with a unique salt. Production signup additionally requires a deployment-level family access code, shared out of band with trusted hosts, so a public splash page cannot be used to create paid OpenAI resources anonymously. Eight random recovery codes are shown once, stored only as hashes, and rotated together after a successful password reset. Recovery invalidates every existing session.

Google OpenID Connect remains an optional integration, not a prerequisite for operating or joining RingRing.

## Special service lines

Every generated party context includes `*10`, an Asterisk-local echo test. It answers, beeps, and returns media and DTMF to the same authenticated caller until they press `#` or hang up. This proves a phone's microphone, speaker, negotiated codec, and both RTP directions without another member or external service.

Each party controls its other generated routes. `*11` uses Asterisk's local time playback, `*12` calls a private FastAGI service that combines Open-Meteo data with the party's OpenAI text-to-speech key, `*13` streams the fixed Groove Salad MP3 endpoint through Asterisk, and `*14` connects the answered channel to the app's private AI AudioSocket listener. The public radio stream uses HTTP because Asterisk's `MP3Player`/`mpg123` path in the reference image cannot open HTTPS; it carries no credentials or caller data. A disabled optional service has no dialplan route.

Weather audio is cached by party and settings timestamp, and disabling the line takes effect before a cached file can be served. The fixed radio URL is code-controlled; accepting arbitrary host URLs is intentionally deferred to avoid SSRF and dialplan-injection risk.

## OpenAI isolation

When `OPENAI_ADMIN_KEY` is configured, party creation provisions an OpenAI project, sets a monthly hard spend limit, creates a service account, encrypts the returned key once, and discards it from logs and responses. Runtime calls use that party key. The organization admin key is never used for model calls.

Party deletion uses that administrator connection only to archive the party's project. Archival occurs before the local encrypted key and project identifiers are removed, and a local delete is refused if archival cannot be confirmed.

For `*14`, FastAGI validates the party setting and issues a short-lived, one-use ticket keyed by a random call UUID. Asterisk then opens an [AudioSocket](https://docs.asterisk.org/Configuration/Channel-Drivers/AudioSocket/) connection containing that UUID. The app claims the ticket, rechecks the party, decrypts only that party's key, converts Asterisk's 8 kHz signed-linear audio to G.711 μ-law, and bridges it to OpenAI's server-side [Realtime WebSocket](https://developers.openai.com/api/docs/guides/realtime-websocket). The connection sends a stable privacy-preserving safety identifier but no member name, SIP username, or extension. Neither the organization admin key nor another party's key enters the call path.

RingRing omits input transcription, disables Realtime tracing, discards output transcript events, exposes no model tools, and stores no call content. It limits each call to three minutes by default and reserves only two simultaneous AI calls per deployment. The host must explicitly enable the line and acknowledge the under-18 rules; callers hear a generated disclosure before live audio begins.

## Growth seams

- A storage interface permits a later move from SQLite to PostgreSQL.
- A telephony reconciler isolates Asterisk configuration and reload mechanics.
- Service-line handlers are independent adapters.
- Static assets and templates are embedded for a single app artifact, while Caddy can serve them directly later if needed.
