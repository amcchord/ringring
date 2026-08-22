# Security and privacy model

RingRing is designed for family use and may handle children's voices and names. That raises the bar even for a small personal deployment.

## Boundaries

- A party is both a tenant boundary and a call-routing boundary.
- Hosts authenticate with a RingRing username/password account and may administer only parties they host. Google OpenID Connect is optional.
- Invitees receive the narrow right to claim one membership/device; they do not receive host access.
- SIP devices authenticate with unique random credentials and enter a server-selected party context.
- OpenAI, weather, and radio integrations are outbound services. None may receive credentials for another integration or party.

## Data minimization

RingRing stores a host name and username, member display labels, extensions, device labels, and operational timestamps. An email address is neither requested nor required by native accounts. It does not record calls. Transcripts and audio persistence are off by default. The default frontend includes no advertising, behavioral analytics, or third-party trackers.

## Secrets

- Deployment secrets live outside Git in a root-readable environment file or secret manager.
- The application master key encrypts SIP passwords and party-scoped integration keys.
- Invitation tokens are random, expire, are single-use, and are stored as hashes.
- Session cookies are secure, HTTP-only, same-site, rotated at authentication, and backed by server-side state.
- Host passwords use salted Argon2id hashes at the [OWASP password-storage minimum](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html). Passwords and the family access code are never logged.
- Random offline recovery codes follow [OWASP's offline-recovery guidance](https://cheatsheetseries.owasp.org/cheatsheets/Forgot_Password_Cheat_Sheet.html): they are stored only as domain-separated hashes, revealed once, and rotated as a set after use. A reset invalidates all sessions.
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
- Native login and recovery are limited both per source address and per normalized username; Argon2 work also has a small concurrency ceiling.
- Production host signup is closed unless a high-entropy `HOST_SIGNUP_CODE` is configured. This prevents unauthenticated bots from provisioning party OpenAI resources.
- Repeated SIP failures trigger temporary address blocking.
- Hosts can revoke devices and disable integrations immediately.
- OpenAI projects should use model restrictions and hard spend limits where available.

The reference deployment writes Asterisk PJSIP security events to a dedicated file. Fail2Ban uses its maintained Asterisk filter and inserts bans into Docker's `DOCKER-USER` chain, before published-port forwarding. A legitimate first SIP challenge is not a failure; repeated bad authentication responses are banned with increasing durations.

The weather line sends a host-chosen place to Open-Meteo and a short forecast sentence to the party's OpenAI project for speech generation. Its AI-generated voice identifies itself and names Open-Meteo. RingRing does not send caller audio, member names, or SIP credentials to either service for weather playback.

## Reporting

Do not open a public issue for a vulnerability that could expose credentials or private calls. Until a dedicated security address is published, contact the repository owner privately through GitHub.

## Known preview gaps

HTTPS/TLS, narrow published ports, cross-party configuration isolation, native account recovery, SIP credential rotation/revocation, and live authentication blocking are verified. The service remains a preview until backup/restore is exercised, two remote physical devices pass two-way audio, and host/member deletion flows exist.
