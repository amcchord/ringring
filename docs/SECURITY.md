# Security and privacy model

RingRing is designed for family use and may handle children's voices and names. That raises the bar even for a small personal deployment.

The adversaries, trust boundaries, abuse cases, executable controls, and
accepted preview risks are tracked in the [RingRing threat model](THREAT_MODEL.md).

## Boundaries

- A party is both a tenant boundary and a call-routing boundary.
- Hosts authenticate with a RingRing username/password account and may administer only parties they host. Google OpenID Connect is optional.
- Invitees receive the narrow right to claim one membership/device; they do not receive host access.
- SIP devices authenticate with unique random credentials and enter a server-selected party context.
- OpenAI, weather, and radio integrations are outbound services. None may receive credentials for another integration or party.

## Data minimization

RingRing stores a host name and username, member display labels, extensions, device labels, and operational timestamps. An email address is neither requested nor required by native accounts. It does not record calls. RingRing does not persist AI input audio, output audio, or transcript events. Provider-side API retention is controlled separately by the OpenAI organization's data controls. The default frontend includes no advertising, behavioral analytics, or third-party trackers.

Current SIP contact presence is visible only to the authenticated host of that party. The app queries it on demand over private AMI and keeps only the generated SIP username plus a normalized state long enough to render the no-store response. It does not persist or expose the registered contact URI, network address, port, call ID, or phone user-agent. Public, signup, login, invitation, and one-time setup pages do not query or reveal presence.

The Linphone QR is rendered inside RingRing and contains a one-time HTTPS URL rather than a SIP password. Fetching it returns only the generated SIP identity, password, extension, registrar, and transport needed by that device; it does not return a member name, party name, host identity, or OpenAI credential. No third-party QR/provisioning service is involved.

The `*10` phone test is an in-memory Asterisk media loop inside the caller's party context. It sends the caller's RTP straight back to that same authenticated channel and does not record, persist, transcribe, or send audio to another service.

The authenticated host may save three real-phone setup confirmations: successful `*10` echo, an outgoing party call to a phone on a different internet connection, and an incoming call from that remote phone. These are explicitly host-confirmed observations, not automatic call records. RingRing stores only one nullable timestamp per completed check plus an update timestamp; it stores no peer identity, dialed number, contact address, network detail, user-agent, call time/duration, audio, or transcript. Unchecking an item removes that timestamp, and clearing all three removes the row. SIP credential rotation clears all three because the connection under test has changed, and device deletion cascades through the record.

The private metrics listener keeps process-lifetime aggregate counters only. HTTP labels are coarse surfaces rather than paths; voice labels are a fixed service/result vocabulary; SIP presence is reduced immediately to four state counts. Metrics contain no party, host, member, device, SIP username, extension, token, address, user-agent, place, prompt, error, per-call timestamp/duration, audio, or transcript, and RingRing does not persist or remote-write them. Application logs use route templates and omit record identifiers and caller-controlled values. Asterisk's root-restricted authentication security log retains source addresses only for Fail2Ban and needs a short operator-chosen rotation/retention policy. See [Privacy-preserving observability](OBSERVABILITY.md).

The `*15` extension chooser trusts only Asterisk's authenticated PJSIP endpoint identity, not caller ID or a user-entered identity. Its database update requires an active device mapped to the supplied party and accepts only 2–5 ASCII digits that are not reserved for public safety; uniqueness is enforced inside that party. It changes only the member extension, sends no data outside the private app/PBX network, and records no prompt audio or DTMF. Invalid, occupied, revoked, unknown, and cross-party attempts receive generic phone prompts.

Invitation pages suggest the first available ordinary extension, beginning at
`101`, without exposing the party's existing numbers or names. RingRing never
assigns familiar public safety numbers `000`, `111`, `112`, `911`, `988`, or
`999`. They correspond to official emergency or crisis services in
[Australia](https://www.triplezero.gov.au/Documents/Poster%20-%20Triple%20Zero%20-%20ENGLISH.PDF),
[New Zealand](https://www.govt.nz/browse/law-crime-and-justice/crimes-and-emergencies/111-emergency-service/),
[Europe](https://digital-strategy.ec.europa.eu/en/policies/112), the
[United States](https://www.samhsa.gov/find-support/in-crisis), and the
[United Kingdom](https://www.gov.uk/guidance/999-and-112-the-uks-national-emergency-numbers).
This reservation prevents a family member from appearing to answer one of
those numbers; it does not make RingRing capable of reaching any emergency
service. Every setup page still tells the family to keep another way to call
for help.

## Deletion lifecycle

- A host can delete a member only after retyping that member's extension. The member record and every attached encrypted SIP credential are removed together, then RingRing regenerates and reloads Asterisk configuration.
- A host can delete a party only after retyping its full name. If the party owns an OpenAI project, RingRing must first confirm that the project is archived; an archive failure preserves the complete local party. OpenAI project archival is permanent.
- A host account can be deleted only after all of its parties are gone. Deleting it removes native sign-in data, password and recovery-code hashes, and every active server-side session.
- Successful deletion clears live application state, not historical backup archives. Operators must expire encrypted backups under their retention policy before removed family data disappears from every recovery copy.

Telephony configuration is derived from SQLite. If an Asterisk regeneration or private reload fails after a local deletion commits, the host sees an operator-retry notice and startup reconciliation remains the recovery path. Operators should treat that notice as urgent because an already-loaded credential can remain accepted until reconciliation succeeds.

## Secrets

- Deployment secrets live outside Git in a root-readable environment file or secret manager.
- The guided installer accepts deployment secrets only from hidden terminal input or a root-owned mode-`0400`/`0600` regular answers file. It refuses secret command-line flags, symlinked managed paths, and existing configuration rather than guessing whether an overwrite is safe.
- Caddy receives only the non-secret `RINGRING_DOMAIN` value from the checkout's private Compose `.env`; OpenAI, encryption, session, family-access, and AMI secrets remain confined to the service-specific files under `/etc/ringring`.
- Caddy remains the ACME owner. A root-only timer exports its storage, accepts only the exact hostname's matching certificate/key pair with at least one week remaining, and stages mode-`0600` copies under `/etc/ringring/tls`. Asterisk receives that narrow directory read-only and makes a private runtime copy; it never mounts Caddy's complete storage.
- The application master key encrypts SIP passwords and party-scoped integration keys.
- Each party's OpenAI key identifier is stored alongside its encrypted value so a host can replace it. During replacement, AI-powered routes pause until RingRing confirms the fresh key exists and every older active key owned by that party's dedicated service account is deleted. Partial failures remain retryable and never reveal a key to the browser.
- Invitation tokens are random, expire, are single-use, and are stored as hashes.
- Linphone provisioning tokens have 32 random bytes, are stored only as hashes, expire after 30 minutes, and are consumed once. Rotation replaces them and revocation or device deletion removes them.
- Real-phone readiness records contain only host-confirmed check timestamps. They are host-only, cannot be updated for a revoked phone, reset on credential rotation, and cascade with device deletion.
- Session cookies are secure, HTTP-only, same-site, rotated at authentication, and backed by server-side state.
- Host passwords use salted Argon2id hashes at the [OWASP password-storage minimum](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). Passwords and the family access code are never logged.
- Random offline recovery codes follow [OWASP's offline-recovery guidance](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html): they are stored only as domain-separated hashes, revealed once, and rotated as a set after use. A reset invalidates all sessions.
- Setup screens reveal a newly issued SIP password only once. Their provisioning URLs appear only on that same no-store reveal; rotated settings cross the redirect in a short-lived encrypted cookie, and token-bearing paths are masked from application logs. A lost or prematurely consumed setup is rotated, not retrieved.
- Copy buttons on that reveal are optional progressive enhancement. A single integrity-pinned same-origin script reads only the visible setup values after a user gesture and writes them to the operating-system clipboard; it has no network, browser-storage, cookie, navigation, or logging primitive. The page works without it, warns that the complete note includes the password, and reminds the person to remove any private message after configuration. Clipboard lifetime remains controlled by the browser and operating system, not RingRing.

## Network exposure

Expected public ports are:

- `22/tcp`: SSH administration, key authentication only.
- `80/tcp`: ACME and redirect to HTTPS.
- `443/tcp`: web application.
- `5061/tcp`: preferred SIP TLS 1.2 registration and call setup; rate limited.
- `5060/udp`: compatibility SIP registration; rate limited.
- `10000-10199/udp`: negotiated RTP media.

SIP TLS encrypts registration credentials and call-setup signaling between a capable phone and Asterisk. RingRing's RTP is currently unencrypted and server-relayed, so TLS does not protect voice media from the server or an observer on the RTP path. UDP SIP remains an explicit fallback for clients that cannot use TLS; it does not protect the signaling credential in transit. Setup guidance prefers TLS and does not recommend disabling certificate verification.

The database, AMI, metrics, debug endpoints, and container APIs are never public. Metrics use a distinct loopback-only app listener; Compose does not expose or publish it, Caddy does not route it, the public handler returns `404` at `/metrics`, and guided operations verify those conditions.

## Abuse controls

- There are no PSTN trunks, so toll fraud is structurally unavailable.
- Authentication, invitations, one-time provisioning, registration, and expensive service lines are rate limited separately.
- Native login and recovery are limited both per source address and per normalized username; Argon2 work also has a small concurrency ceiling.
- Production host signup is closed unless a deployment-chosen `HOST_SIGNUP_CODE` is configured. This prevents anonymous visitors from provisioning party OpenAI resources.
- Repeated SIP failures trigger temporary address blocking.
- Hosts can revoke devices, disable integrations, replace a party's OpenAI runtime key, and choose its hard monthly spend limit within the operator's ceiling.
- New parties and host updates accept an OpenAI project limit only after the provider echoes the exact requested USD cents, monthly interval, and active enforcement. An ambiguous update pauses new AI-powered calls until the same pending amount is retried.

The reference deployment writes Asterisk PJSIP security events to a dedicated file. Separate UDP `5060` and TCP `5061` Fail2Ban jails use the maintained Asterisk filter and insert bans into Docker's `DOCKER-USER` chain, before published-port forwarding. A legitimate first SIP challenge is not a failure; repeated bad authentication responses are banned with increasing durations.

On a guided fresh install, the security log and both Fail2Ban jails are in place before Compose starts the public SIP listeners. Asterisk can create a short-lived self-signed fallback so its TLS transport initializes while Caddy obtains the public certificate, but guided public verification rejects that fallback. The synchronization timer validates hostname, remaining lifetime, and key match, refuses unsafe archives and symlink targets, and defers PJSIP reload while any call is active. Upgrades refresh that source-controlled policy before services are reconciled. Both operations retain root-only pending markers on failure; those markers contain only commits, a domain, or a backup path—not deployment credentials. Upgrade rollback is deliberately not automatic because a target may already have applied a forward-only database migration.

The weather line sends a host-chosen place to Open-Meteo and a short forecast sentence to the party's OpenAI project for speech generation. Its AI-generated voice identifies itself and names Open-Meteo. RingRing does not send caller audio, member names, or SIP credentials to either service for weather playback.

The radio line sends only a normal anonymous player request to a fixed SomaFM endpoint. A party stores a catalog ID, never a URL; unknown IDs fail before telephony reconciliation, and the renderer independently refuses them. The three source-controlled choices use SomaFM's official direct links for personal listening. No caller identity, party data, SIP credential, OpenAI key, redirect target, custom header, or host-supplied address enters the request. Live programming is selected by the station and is not moderated or guaranteed child-appropriate by RingRing.

The `*14` conversation line is disabled by default. Enabling it requires an adult-host confirmation. It uses the current `gpt-realtime-2.1` default, child-appropriate system instructions, no tools, a privacy-preserving `OpenAI-Safety-Identifier`, disabled tracing, no input transcription, bounded response tokens, a three-minute call limit, and a deployment concurrency limit. The app ignores transcript events and never logs WebSocket payloads. A short, exact text-to-speech disclosure plays before the live bridge starts.

OpenAI's [Under 18 API Guidance](https://developers.openai.com/api/docs/guides/safety-checks/under-18-api-guidance) calls for age-appropriate disclosure, content safeguards, monitoring/escalation paths, and heightened privacy care. It also says not to process personal data of children under 13 without Zero Data Retention. RingRing's host UI repeats that boundary: do not enable `*14` for a caller under 13 unless the OpenAI organization has Zero Data Retention. Adult supervision and an external child-safety review remain required before treating this preview as a general child-facing service.

## Reporting

Do not open a public issue for a vulnerability that could expose credentials or private calls. Until a dedicated security address is published, contact the repository owner privately through GitHub.

## Known preview gaps

HTTPS, SIP TLS 1.2 with certificate/name verification, narrow published ports, cross-party configuration isolation, native account recovery, one-time Linphone provisioning, official Linphone-engine TLS registration, mixed-transport party calling, echoed bidirectional audio, authenticated DTMF extension selection, simulated distinct NAT paths, SIP credential rotation/revocation, retry-safe party OpenAI key replacement, guarded member/party/account deletion, live authentication blocking, and isolated backup/restore are verified in code and disposable environments. SIP media is not yet encrypted. The physical ATA/desk-phone TLS matrix, Linphone mobile UI, push/background ringing, real household and carrier-grade NAT paths, and a two-way call between two remote physical devices still need to pass on family hardware before the service leaves preview status.
