# RingRing worklog

This is the durable, chronological project record. Add new entries at the top. Capture decisions and verification, not a transcript of commands.

## 2026-08-22 — One-phone two-way audio test

### Shipped

- Added `*10` to every generated party context. It answers, beeps, and uses Asterisk's local `Echo()` application so one authenticated phone can prove its microphone, speaker, codec, and both RTP directions without another member being online.
- Added a prominent `*10` test card to one-time setup screens, an always-ready explanation in the host's fun-number panel, and a playful fifth number on the public splash page.
- Generalized the authenticated SIPp caller scenario and extended `make sip-smoke` to dial `*10` after the two-phone extension call, requiring a matching PCMU pattern to return through Asterisk.

### Decisions

- Keep the diagnostic always available instead of making it a host toggle. It has no external provider, paid resource, storage, or cross-party destination and is most useful when a phone is first configured.
- Use Asterisk's built-in beep and echo rather than an app media bridge. The call remains on the authenticated party channel, `#` exits the echo, and no audio is recorded or retained.
- Keep the real-device gate. A deterministic internal echo proves the generated route and media loop, but it does not prove behavior across a family's actual NAT, ATA, handset, or Wi-Fi path.

### Verification

- Renderer tests require the exact answer, beep, echo, completion, and hangup sequence in isolated party contexts, including when every optional service is disabled.
- Web tests require the setup and host pages to explain `*10`. A disposable real-browser flow verified the splash, host panel, and one-time setup card at desktop and phone widths with no horizontal overflow; it also exposed and fixed an existing mobile hero-decoration overflow.

### Remaining

- Run the extended isolated SIP/RTP harness on a Docker host, then deploy and exercise `*10` from real remote hardware.
- Complete a two-way-audio call between two remote physical devices.

## 2026-08-22 — Guarded member, party, and account deletion

### Shipped

- Added host-scoped member deletion with an exact-extension confirmation. Deleting a member cascades through every attached device credential and immediately requests telephony regeneration.
- Added full-party deletion with an exact-name confirmation. Parties with provisioned OpenAI resources must first archive their external project; archival failure keeps all local party data intact, and an already-archived project makes retry safe.
- Added host-account deletion with an exact `DELETE` confirmation. A transactional store guard refuses the operation until every hosted party has been retired, then removes sign-in credentials, recovery-code hashes, and sessions.
- Added no-store mobile confirmation pages, clear completion and delayed-routing notices, destructive-action rate limiting, and 44px-or-larger touch targets.
- Documented the external archive ordering, telephony-reconciliation recovery path, and the fact that historical backups retain deleted records until operator retention removes them.

### Decisions

- Archive an OpenAI project before discarding its local encrypted key or ownership identifier. This fails closed when the external administrator connection is unavailable and avoids orphaning a usable paid project.
- Keep deletion authorization in the store query as well as the route. A party host cannot address another host's member or party, even if a handler supplies attacker-controlled identifiers.
- Make SQLite deletion authoritative and Asterisk configuration derived, matching rotation and revocation. A failed private reload produces an explicit urgent notice and remains recoverable through reconciliation or startup.
- Require parties to be deleted before a host account. That preserves the external-project cleanup boundary and makes the final account transaction small and unambiguous.

### Verification

- Package tests cover host scoping, foreign-key cascades, session invalidation, exact confirmations, archive failure preserving local state, retry-safe archival, and completion notices.
- A disposable real-browser flow covered party creation, one-time invitation, member claim, each deletion confirmation, party removal, account removal, and loss of the deleted session.
- The confirmation UI passed a phone-sized viewport check with no horizontal overflow and 44–54px primary, cancel, and back targets.

### Production

- GitHub Actions passed commit `5f23a35`. Created a verified root-only recovery archive immediately before deployment and another from the deployed commit; both sealed reports match the existing one-host/one-party state and confirm integrity, foreign keys, and party-key decryption.
- Deployed from the clean `/opt/ringring` checkout without schema or secret changes. The live family access code remains configured as `fruit-loops`; no deletion form was submitted and no production OpenAI project was archived.
- App, Asterisk, and Caddy are healthy; public readiness and signup security headers pass; unauthenticated deletion routes return to login; and the `ringring-sip` Fail2Ban jail is active. The first Compose start produced one transient app-to-Asterisk discovery warning while Asterisk was still being recreated; the verified post-deploy app restart reconciled cleanly and settled app logs contain no warnings or errors.

### Remaining

- Complete a two-way-audio call with two remote physical devices.
- Copy verified backups into operator-controlled encrypted off-host storage and apply a retention schedule.

## 2026-08-22 — Verifiable disaster-recovery set

### Shipped

- Added a root-only `make backup` workflow that briefly stops the app for a consistent WAL-mode snapshot, copies the full SQLite state plus both deployment environment files, restarts the app, and seals a checksummed archive with the exact Git commit and a non-sensitive verification report.
- Added `ringring verify-state`, which opens a cleanly closed copied database as immutable and without migration, then checks SQLite integrity, foreign keys, current schema, aggregate counts, and decryption of every persisted SIP and party credential without printing record identifiers or secret values.
- Added `make restore-drill BACKUP=...`. It validates the checksum and archive paths, extracts into root-only temporary state, rechecks the sealed report, and starts the restored app in a read-only, capability-dropped, network-disabled container with no host ports, organization admin key, or AMI secret.
- Documented encrypted off-host handling, derived-state exclusions, the isolated drill boundary, and a recoverable full-host procedure that preserves pre-restore state.

### Decisions

- Treat the application environment as part of durable recovery state. SQLite intentionally cannot recover encrypted SIP and OpenAI credentials without the original master key; the Asterisk environment also carries the matching private AMI secret.
- Stop only the app for the snapshot instead of copying an active SQLite main file. SQLite documents the WAL as persistent database state and normally checkpoints it when the last connection closes.
- Exclude generated Asterisk configuration, synthesized voice caches, certificates, images, and the Git checkout. They can be regenerated from the restored database, root secrets, and manifest commit.
- Keep the automated restore exercise isolated. Replacing live state remains a deliberate maintenance operation with a retained rollback directory, not a side effect of routine verification.

### Verification

- Race-enabled tests cover valid restored counts and encrypted credentials, wrong-master-key rejection without secret disclosure, and corrupt-database rejection.
- `make check`, shell syntax, ShellCheck, and whitespace checks pass locally.
- A candidate image verified the existing offline production copy with SQLite integrity, foreign keys, current schema, aggregate counts, and the saved master key's ability to decrypt the party credential.
- A separate network-disabled Compose project exercised the complete backup command against copied state, including app stop, WAL cleanup, snapshot verification, restart, archive/checksum creation, and the full restore drill. It left the live app healthy and removed every candidate container, extracted copy, and temporary secret archive.

### Production

- GitHub Actions passed commit `4270a1d`. Took an additional offline root-only rollback copy, deployed the verified image from the clean production checkout, and installed GNU Make so the documented operational targets work directly on the host.
- Created the first checked-in production recovery archive and sidecar under `/root/ringring-backups`, both mode `0600` inside a mode-`0700` directory. The sidecar checksum passes and the sealed report confirms SQLite and foreign-key integrity, the existing one host/one party state, and successful decryption of the saved party key.
- Ran the checked-in restore drill against that exact archive from `/opt/ringring`. Safe extraction, the sealed report, isolated app readiness, telephony regeneration, clean shutdown, and unchanged post-start state all passed with no network or host ports.
- The live app and Asterisk are healthy, readiness and signup security headers pass, Fail2Ban is active, and settled runtime logs contain no warnings or errors. Candidate images, containers, clones, extracted state, and temporary secret archives were removed; the verified production archive and pre-deploy rollback copy remain root-only.

### Remaining

- Copy verified archives into operator-controlled encrypted off-host storage and apply a retention schedule; periodically repeat the drill as state changes.
- Complete a two-way-audio call with two remote physical devices.

## 2026-08-22 — First authenticated SIP/RTP loop

### Shipped

- Added a repeatable `make sip-smoke` harness that renders two disposable devices through RingRing's real telephony renderer, performs challenged SIP registrations, calls extension `102` from extension `101`, and requires bidirectional PCMU RTP through Asterisk.
- Corrected generated PJSIP AOR object names to match each device's SIP username. Asterisk's dynamic registrar resolves the REGISTER `To` user as the AOR name; the former suffixed AOR name caused a correctly authenticated registration to return `404 Not Found`.
- Pinned the official SIPp 3.7.7 static release and checksum. The harness uses an internal Docker network, smoke-only credentials, no production environment or database, and no published ports, then removes its exact containers, network, and generated state on exit.

### Decisions

- Exercise the production renderer instead of keeping a parallel hand-written endpoint fixture. This lets the loop catch registration and party-routing mistakes in the configuration RingRing actually emits.
- Keep `direct_media=no` in the test so successful RTP proves that audio traversed Asterisk rather than taking a synthetic phone-to-phone shortcut.
- Treat the software loop as a deployment gate, not a substitute for remote physical-device testing across household NATs and real adapters.

### Verification

- The isolated harness passed on the reference deployment host with two authenticated contacts, the party-scoped `101` to `102` call, and a bidirectional PCMU pattern returned through Asterisk.
- The harness exposed and then regression-tested the AOR-name requirement. It also verifies the generated party context, extension route, and server-mediated media setting before starting Asterisk.
- The candidate run left no smoke-test container, Docker network, generated state, host port, or production data change behind.

### Production

- GitHub Actions and the local race-enabled checks passed for commit `472ad6e`. Took a new offline, root-only app-state and generated-config backup, then deployed that commit from the clean production checkout.
- Ran the checked-in smoke test from `/opt/ringring`; it passed both challenged registrations, the party-scoped extension call, and bidirectional RTP, then left no test container or network behind.
- The app and Asterisk are healthy, live readiness and signup return `200` with the expected security headers, private AI port `4574` remains unpublished, Fail2Ban is active, and settled runtime logs contain no warnings or errors.
- SQLite reports `integrity_check=ok`; the existing one host and one party remain intact and no production device was created or changed.

### Remaining

- Complete a two-way-audio call with two remote physical devices, then exercise backup/restore and add host/member deletion flows.

## 2026-08-21 — Disclosed, bounded RingRing AI calls

### Shipped

- Added host-controlled `*14` RingRing AI calls using a private Asterisk AudioSocket channel and OpenAI's server-side Realtime WebSocket API. The bridge converts Asterisk 8 kHz signed-linear audio to G.711 μ-law in each direction and paces outbound frames at telephone cadence.
- Added a short-lived, one-use call ticket so a random Asterisk call UUID must first be authorized by private FastAGI. The bridge then rechecks the party setting, decrypts only that party's runtime key, and never receives the organization admin key.
- Added an exact AI-voice disclosure before live audio, child-appropriate instructions, no tools, no input transcription, disabled tracing, ignored transcript events, bounded output, a three-minute call limit, and a two-call deployment concurrency limit.
- Added an adult-host confirmation and prominent under-13/Zero Data Retention warning to the responsive service controls. The AI route is disabled by default and omitted from a party's dialplan unless its party key is ready and the host turns it on.
- Added a forward-only `party_services.ai_enabled` migration, deployment configuration, rollback guidance, architecture/security documentation, and the current `gpt-realtime-2.1` default.

### Decisions

- Keep media on RingRing's existing Asterisk path instead of transferring SIP control to a model provider. This preserves the local registrar, party-scoped dialplan, SIP gear compatibility, and the no-PSTN boundary.
- Use Asterisk's AudioSocket channel driver rather than its dialplan application so the regular bridge owns telephone media timing and the application's two-second idle behavior is avoided.
- RingRing stores no AI call audio or transcripts, but that does not claim provider-side Zero Data Retention. Callers under 13 remain out of scope until the operator confirms that OpenAI organization control and an external child-safety review is complete.

### Verification

- Race-enabled tests cover one-use ticket capacity, party-key decryption, the exact disclosure, disabled-service rechecks, privacy headers and session settings, transcript-event omission, bidirectional PCMU conversion, paced AudioSocket frames, party-isolated route generation, host confirmation, and legacy SQLite migration.
- The host controls passed real-browser desktop and 390×844 checks with no horizontal overflow. Enabling without adult confirmation was rejected; an unavailable party shows a disabled control without a misleading confirmation checkbox.
- `make check`, shell syntax, whitespace checks, and the Compose model pass locally or on the deployment host as applicable.

### Production

- Took an offline, root-only backup of app state and the previous environment before migrating the live database. The existing one host and one party remained intact, no device was added or changed, and `ai_enabled` migrated to its disabled default.
- Deployed commit `90b8521` after GitHub Actions passed. The app and Asterisk are healthy, the live readiness and signup pages pass, the new listener is reachable from Asterisk on private port `4574`, and that port has no host publication.
- Verified the loaded AudioSocket application, channel, support modules, and UUID function. An ephemeral Asterisk plus TCP sink exercised the exact `AudioSocket/<service>/<uuid>/c(slin)` channel syntax and was removed afterward.
- The production party's OpenAI provisioning state remains ready, but `*14` is off and no AI dialplan route exists until the host explicitly enables it. No model call was made during deployment verification, and app, Asterisk, and Caddy produced no runtime warnings or errors after startup settled.

### Remaining

- Confirm Zero Data Retention and complete an external child-safety review before enabling `*14` for any caller under 13.
- Complete an isolated two-endpoint SIP/RTP smoke test and then a two-way-audio call using remote physical devices.
- Exercise backup/restore and add host/member deletion flows.

## 2026-08-21 — Native accounts and family service controls

### Shipped

- Replaced Google as the host-signup prerequisite with RingRing-native username/password accounts. Production signup uses a deployment-chosen shared family access code and requires no email, organization membership, approval, or confirmation link; Google remains optional.
- Added salted Argon2id password hashing, generic login failures, per-address and per-username throttling, bounded password-hash concurrency, 32 KiB auth-form limits, server-side sessions, and CSRF-protected signup/login/recovery forms.
- Added eight random offline recovery codes, one-time encrypted reveal screens, hashed storage, full code rotation after use, and automatic invalidation of every session after password recovery.
- Added host device reconnect/disconnect controls with fresh one-time SIP credentials and device-specific ATA, VoIP-phone, and softphone setup guides.
- Added host-controlled `*11` time, `*12` weather, and `*13` Groove Salad radio routes. Weather uses Open-Meteo, a private FastAGI service, party-scoped OpenAI text-to-speech, settings-aware caching, and explicit AI/source disclosure.
- Added a reproducible Fail2Ban Asterisk jail, dedicated PJSIP security log, and installer targeting Docker's pre-forwarding `DOCKER-USER` chain.

### Decisions

- A completely open signup would let bots provision paid OpenAI projects. The shared family code keeps enrollment immediate while placing the public reference instance behind one out-of-band secret.
- Recovery is offline because RingRing intentionally does not require an email address. Recovery codes are the proof of control and are never recoverable from the database.
- Weather speech uses only the party key; the organization admin key remains provisioning-only. Disabling weather is checked before cached audio can play.
- Internet radio starts with a code-controlled direct stream. Arbitrary URLs and interactive AI calling remain deferred until SSRF, dialplan-injection, under-18 safety, disclosure, and retention rules are designed.

### Verification

- `make check` passes with formatting, vet, and race-enabled tests. Coverage includes native signup/login/recovery, duplicate usernames, one-time recovery reveal, old-session and old-password invalidation, host-scoped device changes, party-scoped service routing, weather lookup, speech conversion, disclosure text, and cache-disable behavior.
- The complete native signup and sign-in flow passed in a real browser at desktop and 390×844 mobile viewports. The recovery screen issued exactly eight codes and the second reveal was rejected.
- Shell syntax, Compose configuration, and whitespace checks pass.
- Production image verification confirmed that `MP3Player` is loaded and `mpg123` can decode the fixed SomaFM stream over HTTP; the HTTPS form fails because this decoder has no TLS URL support.

### Production

- Took an offline, root-only backup of the closed SQLite state before migration, restarted the old app successfully, then deployed commit `c427b06` from the clean `/opt/ringring` checkout.
- Installed the family access code only in the root-readable app environment and later changed it to a memorable phrase at the operator's request. Google credentials remain empty and are not needed.
- Verified healthy app and Asterisk containers, the live native signup/login/recovery pages at a 390×844 viewport, secure/HTTP-only/strict authentication CSRF cookies, CSP/HSTS/no-store headers, generic invalid-login behavior, and rejection of an invalid family code. Verification created no user, party, device, or OpenAI project.
- Verified the forward-created `local_credentials`, `recovery_codes`, and `party_services` tables; private FastAGI connectivity; Asterisk's UDP transport; and the loaded `MP3Player` application.
- Installed Fail2Ban 1.1.0, validated the Asterisk jail, and exercised a reversible ban of the reserved test address `192.0.2.1`. The rule appeared before Docker's `RETURN` in `DOCKER-USER` and was removed afterward.
- Production logs contained no application or Asterisk errors/warnings after deployment.

### Remaining

- Verify registration and two-way audio with two remote physical devices.
- Exercise backup/restore and add host/member deletion flows.
- Design the interactive OpenAI voice line with child-appropriate instructions, clear disclosure, and no default transcript retention.

## 2026-08-21 — First vertical slice deployed

### Shipped

- Created the public MIT-licensed repository at `github.com/amcchord/ringring` with contribution rules, architecture, security, roadmap, CI, and deployment documentation.
- Built the Go/SQLite web application with Google OAuth boundaries, server-side sessions, CSRF protection, route-specific rate limiting, host-owned parties, single-use expiring invitations, and one-time SIP setup cards.
- Added encrypted-at-rest SIP and party OpenAI credentials. Invitation tokens are stored only as hashes and sensitive URL paths are redacted from application logs.
- Added party-scoped PJSIP endpoint and dialplan generation, atomic configuration replacement, private AMI reloads, NAT-safe media settings, and the `*11` time service.
- Added the bright responsive splash page, mobile host console, party directory, invitation flow, and setup screen.
- Created a pinned Docker Compose stack with the app, Asterisk 22.10.1 built from checksum-verified official source, and Caddy 2.11.4.

### Production

- Installed Docker Engine and Compose on the Debian 13 host at `ringring.live` and deployed from `/opt/ringring`.
- Stored generated application, session, and AMI secrets in root-readable files outside the repository.
- Installed the AustinLand OpenAI organization admin key as a provisioning-only secret. A read-only organization API request verified that OpenAI accepts it; no project was created during deployment verification.
- Obtained and verified automatic TLS for `https://ringring.live`. Only SSH, web, SIP UDP, and the 10000–10199 UDP RTP range are published; the app origin, SQLite, AMI, and Docker socket are not public.
- Verified the public readiness response and security headers, Asterisk 22.10.1 health, the UDP PJSIP transport on port 5060, the private AMI account/ACL, and the live splash page in a real browser.

### Verification

- `make check` passes, including `gofmt`, `go vet`, race-enabled tests, cross-party dialplan tests, invite single-use behavior, production origin rejection, OpenAI provisioning request tests, and an end-to-end web flow.
- The full local browser flow passed at a mobile viewport: host login, party creation, invitation creation, member claim, one-time setup card, invitation reuse rejection, and directory update.
- GitHub Actions passed on the deployed commit.

### Remaining

- Google OAuth credentials are not available yet. Register `https://ringring.live/auth/google/callback` and add the client ID/secret to `/etc/ringring/app.env` before opening host sign-up.
- Verify registration and two-way audio with two remote physical devices, then add device revocation/rotation and host-visible test calling.
- Add host-configurable weather, radio, and disclosed OpenAI Realtime voice lines; the current special service is time only.
- Add SIP authentication failure blocking/rate limiting, backup/restore verification, and a deletion lifecycle before treating the service as production-ready for families.

## 2026-08-21 — Project bootstrap started

### Intent

- Build an open-source, self-hosted VoIP service for private family groups called parties.
- Give each party an isolated short-extension dial plan, simple SIP onboarding, a mobile-first host console, and optional time, weather, radio, and OpenAI voice destinations.
- Deploy the reference instance at `ringring.live` without any connection to the public telephone network.

### Decisions

- Use a Go monolith with server-rendered HTML for a small operational footprint and resilient mobile flows.
- Use Asterisk/PJSIP for SIP registration, RTP, NAT handling, and party-scoped dial plans.
- Start with SQLite in WAL mode and generated Asterisk configuration, with clear seams for later PostgreSQL or Asterisk Realtime adoption.
- Use Docker Compose with Caddy for the reference deployment.
- Use Google OpenID Connect for hosts. Invited members do not need an account to claim a device.
- Provision one OpenAI project and service account per party when an organization admin key is configured. Keep the admin key on the server and encrypt party keys at rest.
- Use the MIT license for a low-friction open-source starting point.

### Discovery

- The local project directory was empty and was not a Git repository.
- GitHub CLI is authenticated as `amcchord`; `amcchord/ringring` was available.
- `ringring.live` resolves to `45.79.150.193`.
- SSH access as `root@ringring.live` works. The host is a clean Debian 13 server with only SSH listening and no container runtime or reverse proxy installed.
- Neither `OPENAI_ADMIN_KEY` nor Google OAuth credentials were initially present on the server; the AustinLand OpenAI key was installed during deployment, while Google credentials remain outstanding.
- Official OpenAI documentation confirms organization endpoints for projects, project service accounts/API keys, project spend controls, and Realtime SIP call handling.

### Next

- Continue through the milestone plan above; this bootstrap list is retained as historical context.
