# Security and privacy model

RingRing is designed for family use and may handle children's voices and names. That raises the bar even for a small personal deployment.

## Boundaries

- A party is both a tenant boundary and a call-routing boundary.
- Hosts authenticate with Google OpenID Connect and may administer only parties they host.
- Invitees receive the narrow right to claim one membership/device; they do not receive host access.
- SIP devices authenticate with unique random credentials and enter a server-selected party context.
- OpenAI, weather, and radio integrations are outbound services. None may receive credentials for another integration or party.

## Data minimization

RingRing stores a host email/name from Google, member display labels, extensions, device labels, and operational timestamps. It does not record calls. Transcripts and audio persistence are off by default. The default frontend includes no advertising, behavioral analytics, or third-party trackers.

## Secrets

- Deployment secrets live outside Git in a root-readable environment file or secret manager.
- The application master key encrypts SIP passwords and party-scoped integration keys.
- Invitation tokens are random, expire, are single-use, and are stored as hashes.
- Session cookies are secure, HTTP-only, same-site, rotated at authentication, and backed by server-side state.
- Setup screens reveal a newly issued SIP password only once. A lost password is rotated, not retrieved.

## Network exposure

Expected public ports are:

- `22/tcp`: SSH administration, key authentication only.
- `80/tcp`: ACME and redirect to HTTPS.
- `443/tcp`: web application.
- `5060/udp`: compatibility SIP registration; rate limited.
- `10000-10199/udp`: negotiated RTP media.

SIP over TLS on `5061/tcp` is planned but is not part of the first deployment.

The database, AMI, metrics, debug endpoints, and container APIs are never public.

## Abuse controls

- There are no PSTN trunks, so toll fraud is structurally unavailable.
- Authentication, invitations, registration, and expensive service lines are rate limited separately.
- Repeated SIP failures trigger temporary address blocking.
- Hosts can revoke devices and disable integrations immediately.
- OpenAI projects should use model restrictions and hard spend limits where available.

## Reporting

Do not open a public issue for a vulnerability that could expose credentials or private calls. Until a dedicated security address is published, contact the repository owner privately through GitHub.

## Known pre-production gaps

The first deployment has verified HTTPS/TLS, narrow published ports, and cross-party configuration isolation. It is not production-ready until the worklog also records SIP authentication failure blocking/rate limiting, backup/restore, SIP credential rotation and revocation, real-device two-way audio, and deletion flows.
