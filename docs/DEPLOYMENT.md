# Reference deployment

The reference deployment runs the Go app, Asterisk 22 LTS, and Caddy with Docker Compose. The public instance is `ringring.live`.

## Public ports

- `80/tcp` and `443/tcp` for the web app and automatic TLS.
- `443/udp` for HTTP/3.
- `5060/udp` for compatibility SIP registration.
- `10000-10199/udp` for RTP media.

No database, app origin, Docker API, or Asterisk Manager Interface port is published.

## Server prerequisites

Install Docker Engine, the Compose v2 plugin, Git, and GNU Make, then create state directories:

```sh
install -d -m 0755 /opt/ringring
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/app
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/asterisk
install -d -o 10002 -g 10001 -m 0770 /opt/ringring/deploy/state/log/asterisk
install -d -m 0700 /etc/ringring
```

Clone this repository into `/opt/ringring`.

## Secrets

Create `/etc/ringring/app.env` with mode `0600`:

```dotenv
APP_ENV=production
APP_BASE_URL=https://ringring.live
HTTP_ADDR=:8080
DATABASE_PATH=/data/ringring.db
RINGRING_MASTER_KEY=<base64 32-byte key>
SESSION_SECRET=<base64 32-byte key>
# Deployment-chosen code shared out of band with trusted party hosts.
HOST_SIGNUP_CODE=<family access code>
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
OPENAI_ADMIN_KEY=<organization admin key>
OPENAI_PARTY_SPEND_LIMIT_CENTS=1000
SIP_PUBLIC_HOST=ringring.live
ASTERISK_CONFIG_DIR=/asterisk
ASTERISK_AMI_ADDR=asterisk:5038
ASTERISK_AMI_USER=ringring
ASTERISK_AMI_SECRET=<URL-safe random secret>
FASTAGI_ADDR=:4573
AI_AUDIO_ADDR=:4574
AI_REALTIME_MODEL=gpt-realtime-2.1
AI_CALL_MAX_DURATION=3m
AI_MAX_CONCURRENT=2
VOICE_AUDIO_DIR=/asterisk/audio
VOICE_PLAYBACK_DIR=/var/lib/ringring/asterisk/audio
TZ=America/New_York
```

Create `/etc/ringring/asterisk.env` with mode `0600` and only:

```dotenv
SIP_PUBLIC_HOST=ringring.live
ASTERISK_AMI_SECRET=<same AMI secret>
TZ=America/New_York
```

Generate application keys with `openssl rand -base64 32` and the AMI secret with `openssl rand -hex 32`. Choose a family access code that is easy to share with trusted hosts; a multi-word phrase is safer than one common word. Do not place any of these values inside the repository. `OPENAI_PARTY_SPEND_LIMIT_CENTS` is both the default hard monthly limit for a new party and the maximum amount its host may select; `1000` means `$10.00`. Lowering the deployment value narrows future host choices but does not silently mutate existing OpenAI projects.

Native username/password login is always available. In production, new-account signup is open only while `HOST_SIGNUP_CODE` is nonempty; trusted hosts enter that shared code once during account creation. No email address or confirmation is required. A host must save the one-time recovery codes because the server cannot email a reset link.

Google OAuth is optional. If desired, create a web application and register `https://ringring.live/auth/google/callback` as an authorized redirect URI, then set the two Google fields. Leaving them empty does not affect native login.

Linphone QR setup requires no additional environment variable or public port. The QR points back to the deployment's `APP_BASE_URL`, so production must keep that origin on trusted HTTPS. The first startup of this release adds an additive `device_provisioning_tokens` table; a rollback to the prior app binary safely ignores it, and rotating a device after returning to this release creates a fresh link. Do not place a provisioning URL into monitoring probes because a real `GET` intentionally consumes it.

Party OpenAI key replacement adds a nullable `parties.openai_api_key_id` column on startup. Existing party keys remain usable with an empty identifier; their first host-requested replacement records the fresh key ID before retiring all older keys owned by the dedicated service account. Older app builds ignore the additive column. If rollback occurs while a party says its key is rotating or needs retry, its AI-powered routes remain paused; return to this release and use the host's **Finish key replacement** action before re-enabling them. Never test this control against production merely as a deployment probe because a successful submission deliberately revokes the current party key.

Host-set spend limits add three forward-only `parties` columns for the last confirmed amount, one pending amount, and its reconciliation state. Existing parties migrate to an honest `unknown` local state without changing their provider project or interrupting current routing; the host's first save verifies or replaces the chosen amount. A new update also mirrors its pause into the older `openai_status` column, so rollback keeps AI routes unavailable rather than ignoring an uncertain provider result. Do not roll back or edit the pending amount mid-update. Return to this release and choose **Finish spend limit update**, which safely repeats that exact amount until OpenAI confirms active enforcement. Do not submit the production form as a deployment probe because it deliberately changes the party's provider limit.

`AI_AUDIO_ADDR` is private container traffic and must not be published on the host. The reference limits `*14` calls to three minutes and two concurrent sessions in addition to each party project's hard monthly spend limit. Before a party enables the line for anyone under 13, confirm that the OpenAI organization has Zero Data Retention as required by the official [Under 18 API Guidance](https://developers.openai.com/api/docs/guides/safety-checks/under-18-api-guidance).

## SIP authentication firewall

Asterisk already limits unidentified request tracking and uses globally random SIP usernames and passwords. The reference host also uses Fail2Ban to temporarily block repeated PJSIP authentication failures before Docker's published-port rules. Install the checked-in jail after Docker is running:

```sh
/opt/ringring/scripts/install-sip-firewall.sh
```

The jail reads Asterisk's dedicated security log and inserts its jump in Docker's `DOCKER-USER` chain, which Docker processes before its forwarding rules. Ten failures in ten minutes produce a 15-minute ban; repeat bans grow up to 24 hours. Check it with:

```sh
fail2ban-client status ringring-sip
iptables -n -L DOCKER-USER
```

## Deploy and verify

```sh
cd /opt/ringring
docker compose build
docker compose up -d
docker compose ps
curl --fail https://ringring.live/readyz
docker compose exec asterisk asterisk -rx 'pjsip show transports'
docker compose exec -T app ringring verify-ami
fail2ban-client status ringring-sip
```

`verify-ami` exercises the private manager login and complete PJSIP contact-list action, then prints only an `ok` status and aggregate contact count. It never prints endpoint names, contact URIs, addresses, user agents, or credentials. Review `docker compose logs --tail=100` after every deployment. Asterisk is compiled from the pinned official source release and its published SHA-256 file is checked during the image build.

To verify the Linphone path without touching a family phone, use a disposable party/invitation in an isolated development database. Confirm that the QR decodes to the setup page's provisioning URL, the first `GET` returns `application/xml` with `Cache-Control: no-store`, and the second returns `410`. Never print the URL, XML, or setup-screen credentials into deployment logs.

## Backup and recovery

Create a root-only, checksummed backup and exercise it without replacing live state:

```sh
cd /opt/ringring
make backup
make restore-drill BACKUP=/root/ringring-backups/ringring-<UTC>-<commit>.tar.gz
```

The backup includes the SQLite state and both deployment environment files because the application master key is required to recover encrypted SIP and party credentials. It briefly stops only the app for a consistent WAL-mode copy, restarts it, and verifies the copy with a network-disabled container. The restore drill also has no network or host ports and never mounts production state. See [Backup and disaster recovery](RECOVERY.md) for archive handling, drill boundaries, and the recoverable full-host procedure.

## Isolated SIP and media smoke test

Run the checked-in loop test after telephony renderer or Asterisk changes:

```sh
cd /opt/ringring
make sip-smoke
```

The test renders two disposable devices through the production telephony code, authenticates both with SIPp, calls extension `102` from extension `101`, dials the single-phone `*10` echo route, and requires the expected bidirectional PCMU patterns through Asterisk. It uses a dedicated internal Docker network, fixed smoke-only credentials, no production environment or database, and no published host ports. Exact-name collision checks prevent it from disturbing an already-running smoke test; its containers, network, and generated state are removed on exit. Passing these software loops does not replace a call between remote physical devices across real NATs.

## Isolated two-household NAT smoke test

Run the NAT-sensitive gate after changing PJSIP endpoint settings, Asterisk transport behavior, registration handling, or media routing:

```sh
cd /opt/ringring
make nat-smoke
```

The target creates two private phone namespaces with distinct public SNAT identities inside a privileged, disposable topology container. Both SIPp clients advertise private contact and SDP addresses, authenticate through RingRing's production endpoint renderer, and call extension `102` from `101`. The test requires Asterisk to rewrite both contacts to their respective public identities, keep media server-relayed, complete the party call, carry more than 100 non-SIP UDP packets across each NAT in each direction, and finish with no active channels.

The topology container receives no Docker socket, deployment environment, production state, host network, or published port. A fixed internal Docker network connects only that container and disposable Asterisk; an exit trap removes the containers, nested namespaces, network, generated configuration, logs, and audio. The test needs a Docker runtime that permits a privileged container to create nested Linux network namespaces. It models two household paths but does not prove real router, carrier-grade NAT, mobile handoff, or physical-device behavior.

## Isolated Linphone provisioning and call smoke test

Run the heavier client interoperability check after changing Linphone XML or setup behavior:

```sh
cd /opt/ringring
make linphone-smoke
```

The target builds a test-only image from the checksum-pinned official Linphone Python wheel, renders two disposable members through RingRing's production telephony functions, and starts Linphone with a local HTTP provisioning URI for extension `101`. It requires exactly one XML fetch, exactly one imported account, successful authenticated registration, and independently reachable Asterisk contacts for Linphone and SIPp extension `102`. Linphone then calls `102` through the generated party dialplan using a file-backed 440 Hz input while SIPp echoes RTP. The test requires an established media state, an allowed codec, more than 100 RTP packets sent and received, the expected tone in Linphone's returned-audio WAV, clean dialog teardown, and no remaining Asterisk channels.

Image/dependency acquisition happens before the runtime check; the actual clients and Asterisk run on a dedicated internal network with fixed smoke-only credentials, no production environment or database, and no published host ports. Exact-name collision checks and an exit trap remove the containers, network, generated XML and audio, Linphone state, and Asterisk state.

The official wheel is GPLv3 and intentionally remains outside the MIT-licensed RingRing binary and production images. This headless check does not replace scanning a real QR in the mobile app or testing push, background ringing, Wi-Fi/cellular changes, and remote NAT behavior on family hardware.

## Internet-radio compatibility check

Run the external radio check after changing the station catalog, Asterisk image, or MP3 playback route:

```sh
cd /opt/ringring
make radio-smoke
```

The application image emits only its fixed station IDs and URLs into a disposable production Asterisk image. The runner rejects anything outside the exact SomaFM MP3 host/path shape and requires every catalog entry to deliver decodable MPEG Layer III audio. It receives no deployment environment, production state, family credential, or published port. This is an intentional outbound availability check and is kept outside the default offline test suite.

## Update

```sh
cd /opt/ringring
git pull --ff-only
docker compose build
docker compose up -d --remove-orphans
```

Run `make backup` before schema-changing upgrades. Generated Asterisk files can be regenerated from the database and do not need separate backups.

### `*14` upgrade and rollback

The `*14` release adds `party_services.ai_enabled` with a forward-only startup migration. Take the app-state backup while the app is stopped, then restart the old version before beginning the normal update. The column defaults to disabled and older RingRing builds ignore it, so rolling the app image and checkout back leaves the migrated database usable; the four `AI_*` environment variables are also ignored by older builds. Keep the database backup until the upgraded app, private port `4574`, and generated Asterisk dialplan have all been verified. Do not publish port `4574` during either upgrade or rollback.

### `*15` upgrade and rollback

The voice extension chooser adds no schema, secret, environment variable, public port, or external service. Upgrading the app regenerates each party dialplan with `*15`; the private FastAGI listener already used by weather handles the interaction. Rollback regenerates the dialplan without `*15`. Any extension a member already selected remains an ordinary compatible `members.extension` value, while its SIP username and encrypted password remain unchanged.

### Radio-selection upgrade and rollback

The curated selector adds `party_services.radio_station` with a forward-only startup migration and defaults every existing row to Groove Salad, preserving the prior route. Take and drill the normal app-state backup before upgrading. Older app builds ignore the additive column; rolling back regenerates any enabled `*13` route with their fixed Groove Salad setting, while the chosen catalog ID remains stored for a later return to this release. No secret, environment variable, public port, or new container is added.

### Host-set AI spend-limit upgrade and rollback

The spend-limit control uses the additive `parties` columns described in environment setup above and adds no secret, public port, container, or provider resource. Take and drill the normal app-state backup before upgrading. Existing projects are not contacted during migration or startup. After upgrade, verify the page and schema without submitting the control against a real party. If a host update is pending, finish it on this release before any rollback; the mirrored legacy status keeps older releases fail-closed but only this release knows how to reconcile the pending amount.
