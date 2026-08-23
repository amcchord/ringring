# RingRing threat model

This model covers the source-controlled RingRing web application, SQLite data,
Asterisk configuration, Caddy boundary, deployment tooling, and outbound voice
services. It is a preview security review, not a claim that the service is safe
for unsupervised child use. The data-handling rules live in
[Security and privacy](SECURITY.md); this document records adversaries, abuse
cases, controls, executable evidence, and accepted gaps.

Review baseline: 2026-08-22. Re-review whenever authentication, public ports,
telephony routing, provider integrations, secret storage, or tenant identifiers
change.

## Security objectives

RingRing must:

1. Keep each party an independent administration and dialing boundary.
2. Make calls only to authenticated devices in the caller's party or to a
   small source-controlled set of special service lines.
3. Have no PSTN trunk, global outbound context, or route to a regular telephone
   network.
4. Keep host sessions, SIP passwords, invitation/provisioning tokens, the
   application master key, and party OpenAI keys confidential.
5. Avoid retaining call audio, transcripts, or per-call observability data.
6. Bound expensive authentication and AI operations without making ordinary
   family setup unreasonably difficult.
7. Expose only the web, SIP signaling, and RTP ports required for the service.
8. Preserve recoverability without silently weakening deletion or secret
   handling.

These availability goals are secondary to tenant isolation, secret protection,
and the no-PSTN invariant. A rejected call is safer than a call routed into the
wrong party or provider.

## Assets and actors

Protected assets include:

- host accounts, sessions, password hashes, and recovery-code hashes;
- party membership, display labels, extensions, device state, and invitations;
- SIP usernames, encrypted SIP passwords, and one-time provisioning material;
- party OpenAI project identifiers, encrypted runtime keys, and spend limits;
- live family voice media and AI conversation content;
- the authoritative database, generated Asterisk configuration, backups, and
  deployment credentials;
- service availability and the operator's OpenAI budget.

The model considers an unauthenticated internet attacker, a person who learns
the shared family signup phrase, a holder of an invitation or provisioning
link, a compromised or deliberately modified SIP device, an authenticated host
trying to access another party, a compromised third-party service, and a host
operator with root access. A party member is not an authenticated web user and
has only the dialing rights of that member's SIP endpoint.

The root operator can read application memory, the database, deployment
secrets, backups, generated SIP configuration, and unencrypted RTP. RingRing
does not attempt to defend family data from a malicious or compromised root
operator.

## Trust boundaries and data flows

```text
Browser -- HTTPS --> Caddy -- private HTTP --> app -- local volume --> SQLite
                                             |  \
                                             |   +-- HTTPS/WSS --> OpenAI
                                             +------ HTTPS ----> Open-Meteo

Phone -- SIP TLS or UDP --> Asterisk -- private AMI/AGI/AudioSocket --> app
  \------------ unencrypted, relayed RTP --------/
```

- Caddy is the sole public web reverse proxy. The app's public HTTP listener is
  reachable only on the Compose network.
- Asterisk is the only public SIP/RTP component. AMI, FastAGI, AudioSocket, and
  metrics are control-plane interfaces on the private container network or
  loopback.
- SQLite is authoritative. Generated PJSIP and dialplan files are derived
  output and are mounted read-only into Asterisk.
- OpenAI, Open-Meteo, and the fixed SomaFM streams are outside the deployment
  trust boundary. The app sends each only the minimum data described in
  [Security and privacy](SECURITY.md).
- Backup archives cross into operator-controlled storage. Root access and the
  backup retention policy are external trust assumptions.

## Threat register

| ID | Threat | Control and executable evidence | Residual risk / status |
| --- | --- | --- | --- |
| T01 | One party calls or administers another party | Store operations scope host reads and writes by both host and party ID. Every SIP endpoint receives a server-generated party context. Renderer tests create the same extension in multiple parties and assert no endpoint crosses contexts. | A compromised root operator can alter live state. Accepted trust assumption. |
| T02 | Two different party IDs collapse to one Asterisk context | IDs are restricted to 1–48 Asterisk-safe bytes and the full validated ID is preserved in the context. Regression seeds cover underscore/hyphen and shared-long-prefix collisions. | Fixed in the 2026-08-22 review; this was a real pre-review isolation flaw. |
| T03 | Caller reaches PSTN, a trunk, or a global outbound route | The base PJSIP and extensions files include only generated RingRing configuration. Generated `Dial` operations name party SIP endpoints or the private AI AudioSocket. Security-contract tests reject registration, trunk, and common outbound dial primitives. | A malicious root operator can add a trunk outside source control. No in-scope application path can do so. |
| T04 | Attacker registers as a family phone | Each device has an independent CSPRNG-generated identity and password. New identities are six nonzero-leading decimal digits; the global database constraint rejects a collision and the application makes a bounded retry. New passwords are 12 such digits (about 39.7 bits), keeping manual keypad entry practical. Authenticated username precedes IP/anonymous identification. Fail2Ban and separate UDP/TLS limits slow online guessing; rotation and revocation regenerate live configuration. | A captured digest has a smaller offline search space than the former 24-digit password. UDP SIP exposes that exchange to an on-path observer, so prefer TLS. Old adapters may have weak TLS support. Existing credentials retain their original stronger format until deliberately rotated. |
| T05 | Signaling or audio is intercepted | SIP TLS 1.2 with hostname-valid certificates is preferred and continuously checked. Media is server-relayed to avoid peer address disclosure. | UDP SIP and RTP remain unencrypted. Anyone able to observe those paths, plus the root operator, may observe signaling or audio. Open preview gap. |
| T06 | Host account guessing, enumeration, or recovery abuse | Argon2id, generic login/recovery failures, dummy-hash timing work, per-source and per-username limits, an Argon2 concurrency ceiling, random one-use recovery codes, and reset-time session invalidation. | Limits are process-local and reset on restart; there is no MFA. Accepted for the single-node family preview. |
| T07 | Shared signup phrase is guessed or leaked and used to create OpenAI projects | Production signup is disabled without a phrase, signup is source-IP limited, the phrase is compared without logging, and every party has an operator-bounded provider spend ceiling. The operator can rotate or remove the phrase. | The memorable phrase is deliberately easy to share and is only an anti-drive-by gate, not proof of identity. Someone who knows it can create a host account. Treat it like a family door code, rotate it after unwanted disclosure, and do not reuse it as a password. |
| T08 | Cross-site request forgery or session theft | All state-changing host routes use POST, authenticate server-side sessions, validate a session-derived CSRF value, and enforce same-origin when Origin is present. Auth and invitation forms use strict same-site double-submit tokens. A path-scoped invitation cookie may pair with an opaque `null` browser origin only when its exact hidden token also matches; ordinary cross-site origins and all host/admin opaque origins remain rejected. Cookies are Secure, HttpOnly, and SameSite; responses containing secrets use `no-store`. OAuth uses random state and PKCE. | Browser or host-device compromise remains out of scope. Native accounts avoid email takeover but depend on saved recovery codes. |
| T09 | Invitation or phone setup link/QR is replayed, logged, or grants excess access | Tokens have 32 random bytes, are stored hashed, expire, and are atomically consumed once. Both QR images are rendered in memory without a third-party service. The host invitation reveal is consumed once on a no-store authenticated page; a host-scoped transaction can count and cancel only that party's active unused links without selecting hashes. Only a successful claim response gets a reduced first-call snapshot of dialable display labels/extensions and enabled lines; the invite form does not. Linphone XML returns one device's technical fields only. The vendor-neutral JSON API additionally receives a bounded setup-time menu made only from active members and routable services in that device's party, excluding itself; server and client validate labels and hidden dial targets. Its canonical and released-iOS compatibility paths share the same token and rate limit. Credential responses refuse `HEAD`, caching, browser CORS, and cross-origin resource use; generic problem responses never classify or echo the token. Token-bearing routes are represented by templates in logs. The public OpenAPI document embeds fictitious data only. | Anyone who obtains an unused bearer link or photograph of its QR before the recipient can consume it. The host must share it privately and cancel active links or rotate a claimed device after suspected disclosure. A successful phone app receives the same party labels/extensions needed to place calls. The menu is a snapshot and may remain stale until fresh settings are issued. A malicious native app selected by the recipient can exfiltrate the issued SIP credential; app trust remains part of the private handoff. |
| T10 | Identifier or SIP secret injects Asterisk configuration | Party, device, SIP username, and extension formats are allowlisted; identifier length is bounded; secrets containing newlines are rejected; global duplicate device/SIP identities fail reconciliation. Tests exercise newline, overlength, duplicate, and collision cases. | A database modified by root may make reconciliation fail closed and cause downtime. |
| T11 | Radio/weather input becomes SSRF or command/config injection | Radio stores a catalog identifier and both the web layer and renderer resolve it to a fixed source-controlled URL. Weather uses a bounded host-chosen place through an HTTPS API client. Caller input is not used as a URL, shell command, or Asterisk application name. | External providers learn the normal request data and may be unavailable or compromised. Radio content is not moderated by RingRing. |
| T12 | One party's OpenAI key or budget is used by another | A party has a dedicated provider project/service account; the runtime key is encrypted with party-associated storage context. Authorization resolves the authenticated endpoint back to the same party. Key rotation pauses service until old keys are deleted, and spend-limit reconciliation fails closed. | Provider organization administrators and root can access provider/project control. External provider retention and billing behavior require separate review. |
| T13 | Calls or family identifiers leak through logs/metrics | RingRing stores no call records, audio, or transcripts. Logs use route templates and fixed error classes. Private metrics have bounded aggregate labels, live only on loopback, and are neither persisted nor remote-written. Contract tests prevent Caddy/Compose exposure. | Asterisk security logs retain attacking source IPs for Fail2Ban; RTP is visible to root. Operators must set backup/log retention. |
| T14 | Public access reaches the database, AMI, app control ports, metrics, or Docker | Compose publishes exactly Caddy 80/443 and Asterisk SIP/RTP. AMI denies all except the app's fixed private address and disables its HTTP interface. Caddy proxies only app port 8080. Contract tests reject host networking, privileged mode, and Docker socket mounts. | A container-engine or kernel compromise crosses this boundary. Patch the host and container images. |
| T15 | Vulnerable dependency or unreviewed boundary change ships | `make check` runs race tests, vet, shell syntax checks, admin tests, and the boundary contracts. `make security` adds pinned `govulncheck`. Both commands plus `make admin-test` run on a trusted local development machine before publication, and the exact results are recorded in `WORKLOG.md`; the public repository does not automatically execute contributions on GitHub-hosted runners. | Local release checks depend on contributor discipline, and automated scanners miss logic flaws and vulnerabilities not yet in their database. Human review remains required for boundary changes. |
| T16 | Expensive auth, AI, or malformed traffic exhausts the service | Source/category limits, username limits, bounded request bodies, authentication worker slots, AI concurrency/call-duration/token limits, Asterisk limits, and Fail2Ban constrain common paths. | The single host has no upstream DDoS protection and in-memory limits are not multi-node. Availability is best effort. |
| T17 | Backup, deletion, or upgrade exposes or resurrects data | Root-only secret files, encrypted application fields, guarded deletion, pre/post-upgrade backups, isolated restore drills, and exact pending-operation markers are verified by deployment tooling. | Deletion does not erase retained backups immediately. Root controls archive confidentiality and expiration. |
| T18 | Child receives unsafe AI or broadcast content | The open-ended AI conversation is disabled by default behind an operator gate. At extension creation, one explicit **Adult extension (18+)** checkbox stores a minimal boolean that defaults false for existing members. `*14` authorization uses the authenticated PJSIP endpoint and requires an active same-party device attached to an adult extension; caller ID, unknown, revoked, non-adult, and cross-party devices cannot authorize. The host cannot enable the party line without at least one adult extension. Calls hear an adults-only AI/provider disclosure; the model has no tools and calls are bounded. Radio choices are fixed. | An adult classification is an administrative assertion, not age verification, and a handset can still be shared after setup. OpenAI provider retention may apply. Keep `*14` unavailable to minors until external under-18 safety/privacy review and required ZDR evidence are complete. Live radio is not guaranteed child-appropriate. Open gap. |
| T19 | Host ring-test control becomes cross-party dialing, call spam, or an outbound-route primitive | CSRF and host/party/device joins scope an active target; current contact state must be online; per-host and per-device limits bound repeated rings. SIP usernames and extensions are allowlisted, while AMI channel technology, internal context, priority, timeout, caller-ID shape, variable, and async mode are fixed. The context disables CDR, has an absolute timeout, and contains bundled playback only. The private AMI ACL gains `originate` but not `all` or configuration permissions; no trunk or PSTN channel exists. | A compromised host can ring phones they administer twice per minute, and a compromised app container can use its AMI `originate` privilege against configured internal endpoints. Root/Asterisk compromise remains out of scope. Accepted for the single-node family preview. |
| T20 | Adding another phone clones credentials, crosses parties, or turns one extension into unbounded fan-out | One transaction requires the authenticated host's exact party and member, caps saved devices at eight, creates independent random/encrypted credentials and a one-use provisioning token, and leaves existing phones unchanged. Rendering names only active validated endpoints already attached to that member and party; tests prove explicit same-extension fan-out, cross-host/cross-party rejection, independent revocation, and no wildcard contact lookup. | A compromised host can add phones to members they administer and receive each new one-time credential. That is an intended host capability; family hosts remain trusted within their own party. |
| T21 | Misdialed digits or an idle handset inject content, cross party boundaries, or create unbounded AI work | Asterisk maps exact operator/help routes and party-local fallback patterns to one of four code constants. FastAGI validates the server-selected party ID, loads only that party's key/settings, never accepts caller text or audio, and caches versioned prompts. Renderer and voice tests assert party isolation, fixed prompts, disclosure, optional-line filtering, and bundled fallback. | Repeated calls can still consume local media and a cache miss can incur one TTS request per party/reason/version. Existing SIP authentication, spend limits, and provider pause controls bound access and cost. |

## Proxy and rate-limit invariant

The reference Compose network assigns Caddy `172.31.88.30` and the app accepts
`X-Forwarded-For` only from that immediate peer. Direct clients, including other
containers, cannot choose their rate-limit identity with a forwarding header.
[Caddy's default reverse-proxy behavior](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy#defaults)
replaces untrusted incoming `X-Forwarded-*` values. If another proxy or CDN is
added, this invariant must be redesigned and tested before deployment; merely
trusting private ranges would make spoofing easier.

## Security evidence

The review is reproducible with:

```sh
make check
make security
make sip-smoke
make nat-smoke
make linphone-smoke
./ringringctl doctor
```

The first two commands are source and dependency gates. The SIP commands use
disposable identities and isolated PBX state; they must never use family
records. `doctor` verifies the live public/private listener and service
invariants without printing secret values.

The 2026-08-22 dependency review used
[`golang.org/x/vuln/cmd/govulncheck@v1.7.0`](https://go.dev/doc/security/vuln/).
It reported no vulnerabilities in symbols reachable by RingRing. Its verbose
module inventory reported
`GO-2026-5932` for the unmaintained `golang.org/x/crypto/openpgp` package, with
no fixed version. RingRing does not import `openpgp`; its `x/crypto` usage is
Argon2. This non-reachable module finding is accepted and should be revisited if
imports or upstream guidance change.

## Release and review rule

A change is security-sensitive if it adds a public port, proxy, SIP transport,
Asterisk include or dial primitive, identifier transformation, login method,
token, provider destination, secret, transcript/audio persistence, metrics
label, or backup destination. Such a change must update this model, add or
adjust an executable contract, run the full relevant smoke gates, and preserve
the no-PSTN invariant. Passing automated tests is necessary but does not waive
the external under-18 safety and real-hardware gates.
