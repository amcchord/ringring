# AGENTS.md

This file is the working contract for every person or coding agent contributing to RingRing.

## Mission

RingRing is a self-hosted, private VoIP playground for families. A host creates a party, invites members, and gives each member an extension. SIP phones and SIP-to-FXS adapters register with RingRing and can call other members in the same party. RingRing never connects to the public telephone network.

The experience should feel friendly enough for a child to use and safe enough for a parent to trust.

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
- SIP and RTP should remain server-mediated (`direct_media=no`) so party boundaries and NAT behavior are predictable.
- Optional voice services must be explicit dialplan destinations that a host can disable.
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
3. Render or open changed pages at mobile and desktop widths when UI changed.
4. Validate generated Asterisk configuration when telephony changed.
5. Run `make nat-smoke` when NAT-sensitive endpoint behavior changes.
6. Run `make sip-tls-smoke` when SIP transport, certificates, or Linphone provisioning changes.
7. Update `WORKLOG.md` with what changed, decisions, verification, and remaining work.

## Git and change hygiene

- Keep commits focused and use imperative commit subjects.
- Do not rewrite or discard another contributor's uncommitted work.
- Schema and config-format changes require migration and rollback notes.
- Deployment changes must be reproducible from the repository; do not rely on undocumented server edits.
- Never commit `.env`, runtime databases, generated Asterisk party configuration, certificates, or backups.

## Definition of done

A change is done when behavior, tests, documentation, operational impact, and `WORKLOG.md` agree. A deployed change is done only after public health checks and the relevant internal service checks pass.
