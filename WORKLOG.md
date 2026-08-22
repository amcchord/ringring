# RingRing worklog

This is the durable, chronological project record. Add new entries at the top. Capture decisions and verification, not a transcript of commands.

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
