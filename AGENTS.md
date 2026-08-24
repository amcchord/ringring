# AGENTS.md

This file is the working contract for every person or coding agent contributing to RingRing.

## Mission

RingRing is a self-hosted, private VoIP playground for families. A host creates a party, invites members, and gives each member an extension. SIP phones and SIP-to-FXS adapters register with RingRing and can call other members in the same party. RingRing never connects to the public telephone network.

The experience should feel friendly enough for a child to use and safe enough for a parent to trust.

## Current product contract

- Hosts create an account with a RingRing username, password, shared family access code, and one-time recovery codes. Google sign-in may be optional, but the product must never require an organization account or email confirmation.
- A single-use, expiring invitation creates one member and its first phone. A member owns one party-local 2–5 digit extension and may have several independently revocable phones that ring together.
- New and rotated phones receive a globally unique 6-digit SIP username and a 12-digit SIP password. Both are digits only; visual grouping must never become part of the copied or provisioned value. Use the SIP username for both User ID and Authentication ID when a device exposes both fields.
- SIP TLS on `5061` is preferred. UDP `5060` remains the explicit compatibility path for older adapters; never imply that it is as private as verified TLS.
- The host phone book is the high-frequency surface: people, per-phone presence/activity, current same-party calls, join codes, and elapsed time. Credentials, phone lifecycle, per-member weather, optional lines, AI controls/spend, and destructive actions belong in focused settings surfaces.
- Weather location is member-scoped. Phones sharing an extension share that member's location; other members remain independent.
- `0` and `*0` reach the RingRing operator. `*10` is echo, `*11` time, `*12` weather, `*13` host-selected radio, `*14` the gated adult AI preview, `*15` extension selection, and `*16<extension>` joins a current party call. Optional lines must disappear safely when disabled or unavailable.
- Open-ended AI is enabled only for an extension deliberately marked adult at member creation and while the server-operator gate is open. Do not add recurring adult checkboxes to ordinary call flows or expose `*14` to child/shared phones.

## Product principles

1. **A phone should feel like a phone.** Pick it up, dial a short extension, and talk.
2. **Joining should take minutes.** Prefer a single-use invite and a small device-specific setup card over generic SIP documentation.
3. **Parties are hard security boundaries.** A caller can only reach members and services in their own party.
4. **Hosts stay in control.** Hosts can revoke devices, rotate credentials, manage extensions, and disable optional services.
5. **Private by default.** Do not record calls, retain transcripts, publish directories, or expose presence by default.
6. **No PSTN.** Do not add trunks, emergency calling, phone-number purchasing, or routes to the regular telephone network without an explicit change to project scope and safety design.
7. **Keep operations boring.** One small server and Docker Compose should be a supported production deployment.

## Repository map

- `cmd/ringring/`: Go web application entry point.
- `internal/`: application packages; packages must not be imported from outside this module.
- `web/`: HTML templates and static assets embedded into the Go binary.
- `deploy/`: Docker, Caddy, Asterisk, and production deployment files.
- `docs/`: architecture, security, setup, and decisions.
- `scripts/`: repeatable developer and deployment helpers.
- `WORKLOG.md`: chronological record of meaningful work, decisions, verification, and blockers.

## Architecture rules

- The web app owns parties, memberships, invitations, encrypted SIP credentials, and generated telephony configuration.
- Asterisk owns SIP registration, RTP media, and dialing. Its configuration is generated from application state; hand-edited generated files are never a source of truth.
- Dialplan contexts must be party-scoped. Never route a user-supplied extension through a global context.
- SIP auth usernames must be globally unique and unrelated to a child's real name. Extensions only need to be unique inside a party.
- Credential rotation takes effect immediately. Every setup surface and runbook must explain that the physical phone must be updated with the newly issued username and password; an old device may still claim locally that it is registered.
- SIP and RTP should remain server-mediated (`direct_media=no`) so party boundaries and NAT behavior are predictable.
- Optional voice services must be explicit dialplan destinations that a host can disable.
- The RingRing operator uses fixed, code-controlled one-way TTS and discloses that it is an AI voice the first time each extension encounters it. Do not send caller audio, names, extensions, dialed digits, or credentials to TTS.
- Active-call joining uses party-scoped ConfBridge rooms and a fixed `*16` destination. Joining must announce the validated member display name to everyone already in the call; recording stays disabled.
- The host-only live phone book may reduce current AMI state to `off hook`, `calling`, `ringing`, or `on a call`, same-party companion labels, and bounded elapsed seconds. It must never persist call state/history or expose caller ID, dialed digits, channel/bridge IDs, addresses, or exact timestamps.
- SQLite in WAL mode is the initial supported database. Keep migrations forward-only and portable enough for a later PostgreSQL adapter.
- Server-rendered HTML and small, progressive-enhancement JavaScript are preferred. The core join and admin flows must work without a large client bundle.

## Security and privacy non-negotiables

- Never commit or log API keys, OAuth secrets, SIP passwords, session keys, encryption keys, invite tokens, cookies, or private user data.
- Production secrets belong in a root-readable environment file or secret manager outside the repository.
- Store only hashes of invitation and one-time provisioning tokens. Mask token-bearing paths in logs and show SIP passwords only when they are issued or deliberately rotated.
- Encrypt recoverable credentials at rest with the application master key.
- Use cryptographically secure random values for every token and password.
- Treat names and email addresses as personal data. Collect the minimum needed and avoid analytics/trackers by default.
- Do not record audio or retain model transcripts by default. Any future recording feature requires explicit consent and a documented retention policy.
- A party's OpenAI credentials are server-side only. Browsers, SIP devices, and invite pages must never receive them.
- The organization admin key is provisioning-only. Runtime model calls use a party-scoped service-account key.
- Add spend limits and model allowlists to automatically provisioned OpenAI projects when the Admin API supports them.
- Authentication and invite endpoints require CSRF protection, rate limiting, expiration, and safe error messages.
- Do not expose Asterisk AMI, app debug ports, databases, or Docker sockets to the public network.
- Authenticated party and setup responses are `no-store`. Keep small browser enhancements integrity-pinned and free of analytics, storage, cookies, cross-origin requests, logging, or secret-bearing URLs unless the feature explicitly requires and documents them.

## SIP hardware and call acceptance

- Never infer which physical phone owns an extension from a member name, device label, screenshot, or remembered setup. Before changing hardware, match its configured SIP identity to the active RingRing device record and state the mapping being used.
- Treat Asterisk's current contact list as authoritative for incoming reachability. A phone can authenticate an outgoing `INVITE` without leaving a usable registration, and some adapters keep displaying `Registered` for a stale account.
- Verify registration from both ends after a credential or transport change: the device must report registered, and Asterisk must have the exact current endpoint contact as available. Do not expose the endpoint identity or contact address in logs or screenshots.
- The existence of an Asterisk channel alone does not prove a phone rang. For an incoming test, require a `Ringing`/answer response or physical confirmation, then clear the bounded diagnostic call.
- An analog handset lift is invisible when an ATA keeps it local. Report `off hook` only after the adapter opens a SIP channel; never fabricate state from contact presence.
- Grandstream-style adapters commonly need the same issued value in SIP User ID and Authenticate ID. Keep Account Active and registration enabled; document exact field names per tested model rather than relying on generic SIP jargon.
- Before calling hardware support complete, test `*10`, outgoing and incoming extension calls, two-way audio, DTMF, an invalid destination/operator fallback, registration renewal, reboot recovery, and remote NAT. Software smoke tests do not replace this physical matrix.

## Coding conventions

- Target the Go version declared in `go.mod`.
- Format Go with `gofmt`; keep `go vet ./...` clean.
- Prefer the standard library. Introduce a dependency only when it removes meaningful security or maintenance risk.
- Pass `context.Context` through I/O boundaries.
- Wrap errors with operation context; do not include secret values.
- Keep handlers thin. Put business rules in testable services and storage code.
- Use semantic HTML, visible focus styles, 44px minimum touch targets, and WCAG AA contrast.
- Use cheerful language, but never make security actions or destructive actions ambiguous.
- Avoid child-specific imagery, names, or sample data in fixtures and screenshots.

## Local workflow

Common commands are exposed through the Makefile:

```sh
make setup
make dev
make test
make check
make security
make admin-test
make sip-smoke
make sip-tls-smoke
make nat-smoke
make linphone-smoke
make compose-up
```

Before handing off a change:

1. Run the narrowest relevant tests while developing.
2. Run `make check` before committing.
3. Run `make security` and `make admin-test` locally before publishing a release candidate.
4. Do not use GitHub-hosted Actions for routine CI; the repository's release gate runs on the contributor's trusted local machine.
5. Render or open changed pages at mobile and desktop widths when UI changed.
6. Validate generated Asterisk configuration when telephony changed.
7. Run `make nat-smoke` when NAT-sensitive endpoint behavior changes.
8. Run `make sip-tls-smoke` when SIP transport, certificates, or Linphone provisioning changes.
9. Update `WORKLOG.md` with what changed, decisions, verification, and remaining work.
10. For documentation screenshots, use neutral fictional labels, hide credentials/tokens/addresses, and verify the checked-in image at its rendered README size.

## Production operations

- Use the guarded `ringringctl upgrade` workflow; do not replace it with an undocumented pull/restart sequence. It binds an exact fast-forward target to drilled pre/post backups and complete health verification.
- Before any action that recreates Asterisk, check for active channels and avoid interrupting a family call. A documentation-only commit does not justify a PBX restart.
- After deployment, verify the exact runtime commit, `ringringctl doctor`, public `/healthz` and `/readyz`, Compose health, database/credential integrity, relevant AMI/dialplan behavior, and recent application logs.
- The app can become healthy before Asterisk's service name exists and log one initial reconciliation warning. Once Asterisk is healthy and no operator action is pending, recreate only the app, then rerun the doctor; do not restart the PBX merely to clear that race.
- Do not make an undocumented production database, generated Asterisk, or container edit. Authorized test-device settings may be adjusted when necessary, but first verify the device identity, keep credentials out of output, record the operational change in `WORKLOG.md`, and restore any temporary diagnostic state.

## Git and change hygiene

- Keep commits focused and use imperative commit subjects.
- Do not rewrite or discard another contributor's uncommitted work.
- Schema and config-format changes require migration and rollback notes.
- Deployment changes must be reproducible from the repository; do not rely on undocumented server edits.
- Never commit `.env`, runtime databases, generated Asterisk party configuration, certificates, or backups.

## Definition of done

A change is done when behavior, tests, documentation, operational impact, and `WORKLOG.md` agree. A deployed change is done only after public health checks and the relevant internal service checks pass.
