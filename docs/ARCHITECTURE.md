# Architecture

## System shape

RingRing deliberately starts as a small, self-hostable system:

```text
Internet
  |
  +-- HTTPS 443 -------------------- Caddy ---- RingRing web app ---- SQLite
  |
  +-- SIP 5060/UDP, 5061/TCP ------- Asterisk
  |                                     |  ^
  +-- RTP 10000-20000/UDP --------------+  |
                                            |
                            generated config + private AMI reload
                                            |
                                       RingRing app
```

Caddy, the app, and Asterisk run as separate containers. Only Caddy's HTTPS port and Asterisk's SIP/RTP ports are public. The database, Asterisk Manager Interface (AMI), and app origin stay on private container networks.

## Domain model

- **User**: a Google-authenticated adult who may host one or more parties.
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

UDP SIP is supported for older adapters; TLS is the preferred path for capable devices. Strong random SIP credentials, registration throttling, and automated blocking are required before inviting real users.

## Configuration flow

The database is authoritative. When a device or party setting changes, the app:

1. Validates the full desired configuration.
2. Renders party-scoped PJSIP and dialplan fragments into a temporary file.
3. Atomically replaces the generated files on a shared volume.
4. Requests a reload over private AMI.
5. Records success or surfaces a recoverable configuration error.

On startup, the app can regenerate all telephony configuration from the database.

## OpenAI isolation

When `OPENAI_ADMIN_KEY` is configured, party creation can provision an OpenAI project, set restrictive controls, create a service account, encrypt the returned key once, and discard it from logs and responses. Runtime calls use that party key. The organization admin key is never used for model calls.

The first OpenAI voice line may bridge Asterisk audio to the Realtime API through the app. OpenAI's Realtime SIP endpoint is also a candidate, but it will not replace the core local SIP registrar or weaken party isolation.

## Growth seams

- A storage interface permits a later move from SQLite to PostgreSQL.
- A telephony reconciler isolates Asterisk configuration and reload mechanics.
- Service-line handlers are independent adapters.
- Static assets and templates are embedded for a single app artifact, while Caddy can serve them directly later if needed.
