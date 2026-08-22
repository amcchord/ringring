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

Caddy, the app, and Asterisk run as separate containers. Only Caddy's HTTPS port and Asterisk's SIP/RTP ports are public. The database, Asterisk Manager Interface (AMI), and app origin stay on private container networks.

## Domain model

- **User**: an adult host with a RingRing-native account; optional federated login can be added without changing party ownership.
- **Party**: the hard tenant and dialing boundary. It owns an extension range and optional voice-service settings.
- **Host**: a user's administrative role in a party.
- **Invitation**: a single-use, expiring, hashed bearer token that permits one member/device enrollment.
- **Member**: a person represented by a display label and one extension within a party. An account is not required.
- **Device**: a SIP registration identity and encrypted secret associated with a member.
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

## Host authentication

Native host accounts use a case-insensitive username and an Argon2id password hash with a unique salt. Production signup additionally requires a deployment-level family access code, shared out of band with trusted hosts, so a public splash page cannot be used to create paid OpenAI resources anonymously. Eight random recovery codes are shown once, stored only as hashes, and rotated together after a successful password reset. Recovery invalidates every existing session.

Google OpenID Connect remains an optional integration, not a prerequisite for operating or joining RingRing.

## Special service lines

Each party controls its own generated routes. `*11` uses Asterisk's local time playback, `*12` calls a private FastAGI service that combines Open-Meteo data with the party's OpenAI text-to-speech key, and `*13` streams the fixed Groove Salad MP3 endpoint through Asterisk. The public stream uses HTTP because Asterisk's `MP3Player`/`mpg123` path in the reference image cannot open HTTPS; it carries no credentials or caller data. A disabled service has no dialplan route.

Weather audio is cached by party and settings timestamp, and disabling the line takes effect before a cached file can be served. The fixed radio URL is code-controlled; accepting arbitrary host URLs is intentionally deferred to avoid SSRF and dialplan-injection risk.

## OpenAI isolation

When `OPENAI_ADMIN_KEY` is configured, party creation provisions an OpenAI project, sets a monthly hard spend limit, creates a service account, encrypts the returned key once, and discards it from logs and responses. Runtime calls use that party key. The organization admin key is never used for model calls.

The first OpenAI voice line may bridge Asterisk audio to the Realtime API through the app. OpenAI's Realtime SIP endpoint is also a candidate, but it will not replace the core local SIP registrar or weaken party isolation.

## Growth seams

- A storage interface permits a later move from SQLite to PostgreSQL.
- A telephony reconciler isolates Asterisk configuration and reload mechanics.
- Service-line handlers are independent adapters.
- Static assets and templates are embedded for a single app artifact, while Caddy can serve them directly later if needed.
