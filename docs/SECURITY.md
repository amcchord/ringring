# Security and privacy model

RingRing is designed for family use and may handle children's voices and names. That raises the bar even for a small personal deployment.

The adversaries, trust boundaries, abuse cases, executable controls, and
accepted preview risks are tracked in the [RingRing threat model](THREAT_MODEL.md).

## Boundaries

- A party is both a tenant boundary and a call-routing boundary.
- Hosts authenticate with a RingRing username/password account and may administer only parties they host. Google OpenID Connect is optional.
- Invitees receive the narrow right to claim one membership/device; they do not receive host access.
- SIP devices authenticate with unique CSPRNG-generated credentials and enter a server-selected party context. New identities use six digits; the database rejects collisions and the app retries with another random value. New 12-digit passwords have about 39.7 bits of entropy, are not user-chosen PINs, and are paired with progressive Fail2Ban limits; TLS remains the preferred transport because UDP exposes a digest exchange to an on-path observer.
- OpenAI, weather, and radio integrations are outbound services. None may receive credentials for another integration or party.

## Data minimization

RingRing stores a host name and username, member display labels, extensions, device labels, and operational timestamps. An email address is neither requested nor required by native accounts. It does not record calls. RingRing does not persist AI input audio, output audio, or transcript events. Provider-side API retention is controlled separately by the OpenAI organization's data controls. The default frontend includes no advertising, behavioral analytics, or third-party trackers.

Current SIP contact presence is visible only to the authenticated host of that party. The app queries it on demand over private AMI and keeps only the generated SIP username plus a normalized state long enough to render the no-store response. It does not persist or expose the registered contact URI, network address, port, call ID, or phone user-agent. Public, signup, login, invitation, and one-time setup pages do not query or reveal presence.

Active party calls are likewise visible only on the authenticated host page and are never stored. A no-store fragment polls private AMI, accepts only validated RingRing conference names for the host's exact party, maps authenticated PJSIP endpoints back to active same-party members, and emits only display labels, phone count, and a fixed `*16…` join code. It drops caller ID, channel IDs, addresses, timing, unknown endpoints, non-PJSIP announcement channels, foreign rooms, and call history. Ordinary answered calls and later join legs explicitly disable CDR, and every ConfBridge profile has recording disabled.

Joining is available only from an authenticated party phone through an exact generated route. The joining member's display name is sent as text to that party's OpenAI project for the fixed spoken announcement; no call audio, dialed digits, extension, SIP credential, caller ID, party name, or transcript is sent. The synthesized file contains the spoken name but uses a random name-free filename, is deleted after one minute, and is swept at application startup if an interruption leaves it behind. If text-to-speech or the party key is unavailable, a bundled beep announces the join instead of sending more data or denying the call.

Phone setup QRs are rendered inside RingRing and contain one-time HTTPS URLs rather than SIP passwords. Linphone XML returns only the generated SIP identity, password, extension, registrar, and transport needed by that device. The vendor-neutral phone API additionally returns a reduced same-party button snapshot. Neither format returns a party name, host identity, device label, email, provider identifier, or OpenAI credential, and no third-party QR/provisioning service is involved.

The host's invitation QR is also rendered locally, but it has a different purpose: a normal Camera app opens the same one-time `/join/` link shown beside it, while a trusted native phone app may recognize that exact path and complete the claim through the documented JSON API. RingRing never sends the bearer token to a QR provider. The authenticated party response is `no-store`, the encrypted reveal cookie is consumed once, and the durable database keeps only the token hash. The code must be shared as privately as the link because either form can claim the invitation before its 48-hour expiry.

An authenticated host can see only the number of active links for their party—not token hashes, URLs, recipients, or a global count—and can cancel all of those unused links after a mistaken share. Counting joins the exact party and host. Cancellation repeats that ownership check inside the same transaction that deletes only unclaimed, unexpired rows and clears any pending reveal cookie; cross-host, used, expired, and already claimed state is unchanged. The route is body-bounded, same-origin/CSRF protected, and included in the party-write rate limit.

Only after a recipient successfully claims an invitation, the same no-store page that reveals that new phone's credential may show a first-call snapshot. Each directory entry is constructed as display label plus extension; members with no active phone are omitted, and device labels, SIP identities, credentials, readiness, contacts, and timestamps never enter the view. Special lines are derived from current routable state, so paused AI-powered lines and the default-closed `*14` gate are not advertised. The unclaimed invitation form, host setup reveals, provisioning XML/QR, logs, metrics, and future page loads do not receive the snapshot.

The `*10` phone test is an in-memory Asterisk media loop inside the caller's party context. It sends the caller's RTP straight back to that same authenticated channel and does not record, persist, transcribe, or send audio to another service.

Unknown numeric extensions and disabled or unknown star codes are handled only by fallback patterns in the caller's generated party context. They answer with bundled Asterisk prompts and never perform a global lookup, trunk dial, provider request, or cross-party transfer. A member route that is busy, offline, or unanswered similarly plays a generic retry-later prompt after its explicit party endpoint list returns; it does not identify which phone or member failed. FastAGI and radio-player failures use the same generic service-level response.

The authenticated host may save three real-phone setup confirmations: successful `*10` echo, an outgoing party call to a phone on a different internet connection, and an incoming ring from RingRing or another party phone. These are explicitly host-confirmed observations, not automatic call records. RingRing stores only one nullable timestamp per completed check plus an update timestamp; it stores no peer identity, dialed number, contact address, network detail, user-agent, call time/duration, audio, or transcript. Unchecking an item removes that timestamp, and clearing all three removes the row. SIP credential rotation clears all three because the connection under test has changed, and device deletion cascades through the record.

The host-only incoming setup ring does not save a new record or mark the checklist automatically. A CSRF-protected request must resolve an active device through the authenticated host and party, then pass per-host and per-device limits. The app immediately rechecks private AMI contact state and refuses an offline, unknown, revoked, cross-party, or unavailable target. Its AMI client accepts only an allowlisted generated SIP username and ordinary non-reserved extension; every other Originate field and the internal prompt context are fixed. The prompt uses bundled Asterisk audio, disables CDR for the setup channel, and makes no provider or public-network request. Application logs retain only the route template, result class, and fixed error class—not the party, member, device, SIP username, extension, or AMI fields.

Adding another phone is host-only, same-origin/CSRF protected, and scoped by party, host, and member inside one transaction. A member can retain at most eight device rows; after that the host must reconnect an existing disconnected phone. Each new device receives an unrelated random SIP username/password and one-use Linphone token, while every existing credential remains unchanged. Asterisk rings only the active endpoint names rendered for that member's party context—there is no dynamic contact wildcard or global endpoint lookup.

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
- Native invitation preview returns only the party name already shown on the web invitation and one advisory unoccupied extension. Its JSON claim uses the same atomic member/device/invitation transaction as the CSRF-protected web flow. Invalid details and extension conflicts do not consume the invitation; all unavailable-token states are generic. The endpoint accepts only bounded `application/json`, does not allow CORS or cookies, and never creates an undisclosed follow-on bearer token.
- Phone provisioning tokens have 32 random bytes, are stored only as hashes, expire after 30 minutes, and are atomically consumed once across the Linphone XML, WP826 XML, canonical phone API, and released-iOS compatibility routes. Rotation replaces them and revocation or device deletion removes them.
- The downloadable WP826 XML contains one device's SIP password and therefore receives attachment, `no-store`, no-referrer, same-origin-resource, generic-error, masked-log-path, and one-use-token treatment. Its allowlisted alias set configures Account 1, TLS validation, the RingRing asset paths, ringtones, wallpaper, and idle keys; it cannot carry Wi-Fi, network, administrator, party/member label, or unrelated account state. The setup page tells the recipient to upload it directly and delete it afterward.
- The vendor-neutral phone API returns only one device's validated SIP account plus a bounded snapshot of active, same-party member labels and enabled call services. It excludes the device's own member and every host, party, device, email, provider, readiness, presence, and timestamp field. The RingRing app stores the snapshot and credential together in the device-only Keychain and renders labels instead of dial targets on its primary call screen. Third-party client requirements are documented in [Phone API](PHONE_API.md).
- Universal links are deliberately narrow: the server association document names only the RingRing team/bundle and `/join/*`, and the signed reference app names only `applinks:ringring.live`. The native parser still requires an exact token path, HTTPS outside loopback debug builds, no URL credentials, query, fragment, or redirect. Other deployments remain scan/paste capable without pretending to own their web links.
- `/openapi.yaml` is public, contains fictitious examples only, and permits cross-origin documentation reads. Credential responses are `no-store`, same-origin-resource restricted, omit CORS permission, refuse `HEAD`, and return generic errors that never echo or classify the bearer token.
- Phones sharing an extension never share a SIP credential. Each encrypted password uses its own device ID as associated data and can be revoked without changing the other phones. Numeric setup values never start with zero, avoiding phone UIs that coerce them to numbers; spaces on the one-time card are display-only and never become part of authentication.
- Real-phone readiness records contain only host-confirmed check timestamps. They are host-only, cannot be updated for a revoked phone, reset on credential rotation, and cascade with device deletion.
- The AMI account is ACL-restricted to the fixed app-container address. Its system/command rights support reload and aggregate status; call/reporting reads list validated ConfBridge rooms and PJSIP participants; and its narrow `originate` privilege can reach only the application client's fixed setup-ring or join-announcement shapes. It has neither `all` nor configuration permission, and AMI is never exposed through HTTP.
- Static Asterisk configuration is normalized to mode `0640` and the dedicated runtime user/group during both image construction and container entry. This prevents the root operator's restrictive checkout umask from making a changed dialplan unreadable while leaving Asterisk's health socket and AMI available.
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

The weather line sends a bounded host-entered place or first-call five-digit U.S. ZIP to Open-Meteo and a short forecast sentence containing the resolved label to the party's OpenAI project for speech generation. Its AI-generated voice identifies itself and names Open-Meteo. First-call setup accepts only DTMF from Asterisk's authenticated same-party endpoint, can fill only an unknown location, and never replaces a host choice. RingRing does not record or transcribe weather callers and sends no caller audio, member name, extension, or SIP credential to either service. The host can change the resolved place or disable both setup and forecast routing.

The RingRing operator is one-way text-to-speech, not an open-ended conversation. Its help, misdial, phone-unavailable, and service-unavailable scripts are fixed in code and mention optional lines only from the caller’s party-scoped service snapshot. Asterisk supplies the authenticated endpoint rather than caller ID; the first successful operator playback for that member extension clearly discloses that the voice is AI-generated, and later calls skip the repeated disclosure. The database retains only one member-keyed disclosure timestamp—no call, destination, reason, audio, or transcript history. Unknown, revoked, and cross-party endpoints fail safely into the disclosed prompt. OpenAI receives only the selected fixed script through the party runtime key—never caller audio, dialed digits, party/member names, extensions, or SIP credentials. Cached audio is derived state, records no caller activity, and is invalidated when relevant service settings change. A missing or paused key, provider failure, or private FastAGI failure falls back to local Asterisk prompts.

The radio line sends only a normal anonymous player request to a fixed SomaFM endpoint. A party stores a catalog ID, never a URL; unknown IDs fail before telephony reconciliation, and the renderer independently refuses them. The three source-controlled choices use SomaFM's official direct links for personal listening. No caller identity, party data, SIP credential, OpenAI key, redirect target, custom header, or host-supplied address enters the request. Live programming is selected by the station and is not moderated or guaranteed child-appropriate by RingRing.

The `*14` conversation line is an adults-only preview behind a separate default-closed `AI_ADULT_ONLY_ENABLED` operator gate. A closed gate rejects web and storage updates, clears older saved enablement on startup, filters the generated Asterisk route, and independently refuses FastAGI, key access, AudioSocket, and Realtime bridging. This does not disable weather's one-way speech generation. A party may enable the line only after at least one newly claimed extension is marked **Adult extension (18+)**. That single boolean is collected at extension creation, stores no birthday or age, defaults false for every migrated member, and cannot be changed from the phone book. Child and shared phones must remain unmarked.

At call time the dialplan supplies Asterisk's authenticated PJSIP endpoint rather than caller ID. The database authorizes only an active device whose member belongs to the same party and has the adult-extension bit. Unknown, revoked, non-adult, and cross-party endpoints fail closed. The line uses the current `gpt-realtime-2.1` default, conservative system instructions, no tools, a hashed privacy-preserving `OpenAI-Safety-Identifier`, disabled tracing, no input transcription, bounded response tokens, a three-minute call limit, and a deployment concurrency limit. The app ignores transcript events and never logs WebSocket payloads. A short adults-only AI/provider disclosure plays before the live bridge starts; there is no repeated form or keypad confirmation. RingRing stores no call audio or transcript, but OpenAI provider retention may apply.

OpenAI's [Under 18 API Guidance](https://developers.openai.com/api/docs/guides/safety-checks/under-18-api-guidance) calls for age-appropriate disclosure, content safeguards, monitoring/escalation paths, and heightened privacy care. It also says not to process personal data of children under 13 without first implementing Zero Data Retention. RingRing keeps `ringringctl openai-retention` as a bounded, read-only audit of the organization and every stored party project, but the audit is not a runtime precondition for the adult-only preview. A forbidden or `not_eligible` response from the official [organization data-retention endpoint](https://developers.openai.com/api/reference/python/resources/admin/subresources/organization/subresources/data_retention/methods/retrieve) is not ZDR evidence. Keep `*14` unavailable to minors until external under-18 safety and privacy review—including ZDR where required—is complete.

## Reporting

Do not open a public issue for a vulnerability that could expose credentials or private calls. Until a dedicated security address is published, contact the repository owner privately through GitHub.

## Known preview gaps

HTTPS, SIP TLS 1.2 with certificate/name verification, narrow published ports, cross-party configuration isolation, native account recovery, one-time XML and JSON provisioning, OpenAPI/runtime contract alignment, official Linphone-engine TLS registration, mixed-transport party calling, echoed bidirectional audio, authenticated DTMF extension selection, simulated distinct NAT paths, SIP credential rotation/revocation, retry-safe party OpenAI key replacement, guarded member/party/account deletion, live authentication blocking, and isolated backup/restore are verified in code and disposable environments. SIP media is not yet encrypted. The physical ATA/desk-phone TLS matrix, Linphone mobile UI, push/background ringing, real household and carrier-grade NAT paths, and a two-way call between two remote physical devices still need to pass on family hardware before the service leaves preview status.
