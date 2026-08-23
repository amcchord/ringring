# Reference deployment

The reference deployment runs the Go app, Asterisk 22 LTS, and Caddy with Docker Compose. The public instance is `ringring.live`.

## Public ports

- `80/tcp` and `443/tcp` for the web app and automatic TLS.
- `443/udp` for HTTP/3.
- `5061/tcp` for preferred SIP TLS 1.2 registration and call setup.
- `5060/udp` for compatibility SIP registration.
- `10000-10199/udp` for RTP media.

No database, app origin, Docker API, or Asterisk Manager Interface port is published.

## Guided fresh install

The guided command supports a clean Debian or Ubuntu host. Install Docker Engine, the Compose v2 plugin, Git, OpenSSL, cURL, and GNU Make first; point the deployment hostname's DNS records at the server; and confirm ports `80`, `443`, `5061/tcp`, `5060/udp`, and `10000-10199/udp` are allowed by the provider firewall. RingRing configures its checked-in SIP authentication jails, but it does not change SSH policy, cloud firewall rules, DNS, or install Docker for the operator.

Clone a clean published checkout, prepare the root-only answers file, and review a no-mutation plan:

```sh
cd /opt
sudo git clone https://github.com/amcchord/ringring.git
cd /opt/ringring
sudo install -m 0600 deploy/install.answers.example /root/ringring-install.answers
sudoedit /root/ringring-install.answers
sudo ./ringringctl install --answers /root/ringring-install.answers --dry-run
sudo ./ringringctl install --answers /root/ringring-install.answers
```

Fill every required blank before running the command. The domain is a hostname such as `phone.example.com`, not a URL. The family access code must be 8–64 letters, numbers, dots, dashes, or underscores; choose a memorable multi-word value and share it only with trusted hosts. The OpenAI administrator key is optional. Leaving it blank disables automatic party AI provisioning without affecting private calling.

Instead of an answers file, `sudo ./ringringctl install` asks for ordinary settings and reads the access code and optional administrator key with terminal echo disabled. Secrets are deliberately not accepted as command-line flags, where shell history and process listings could retain them. `--yes` approves the displayed non-secret plan for unattended execution. Use `--skip-public-check` only while DNS or certificate issuance is pending, then run `sudo /opt/ringring/ringringctl doctor` without that option as soon as the public name is ready.

The installer refuses a dirty or unpublished source commit, existing configuration, occupied public ports, symlinked managed paths, and unsafe answer-file ownership or permissions. It generates separate 32-byte application and session keys plus the private AMI secret, writes root-only app/Asterisk environments, and puts only `RINGRING_DOMAIN` in the checkout's mode-`0600` Compose `.env`. After building, it creates the Asterisk security log and installs both Fail2Ban jails before starting any public SIP listener. It installs a root-only certificate synchronization timer, waits for Caddy's exact trusted hostname certificate unless public checks were explicitly deferred, and gives Asterisk that validated pair. Success requires app and Asterisk health, sealed state verification, private AMI verification, both SIP jails, public HTTPS and SIP TLS readiness unless explicitly deferred, and clean recent app logs.

An interrupted install retains `/etc/ringring/install.pending` and the already-generated secrets. Fix the reported condition and run:

```sh
sudo /opt/ringring/ringringctl install --yes
```

Do not supply the answers file again during a resume. After a successful install, move or securely retire that file according to the operator's credential-handling policy.

## Manual deployment prerequisites

Install Docker Engine, the Compose v2 plugin, Git, and GNU Make. Clone the repository into `/opt/ringring`, then create its state and configuration directories:

```sh
cd /opt
git clone https://github.com/amcchord/ringring.git
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/app
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/asterisk
install -d -o 10002 -g 10001 -m 0770 /opt/ringring/deploy/state/log/asterisk
install -d -m 0700 /etc/ringring
install -d -m 0700 /etc/ringring/tls
```

## Secrets

Create `/etc/ringring/app.env` with mode `0600`:

```dotenv
APP_ENV=production
APP_BASE_URL=https://ringring.live
HTTP_ADDR=:8080
METRICS_ADDR=127.0.0.1:9090
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
# Set true only when intentionally offering the adults-only *14 preview.
AI_ADULT_ONLY_ENABLED=false
# Optional; configure with ringringctl rather than pasting the .p8 here.
APNS_TEAM_ID=
APNS_KEY_ID=
APNS_PRIVATE_KEY_FILE=
APNS_BUNDLE_ID=com.mcchord.ringring
APNS_ENVIRONMENT=production
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

Phone-app and Linphone QR setup require no additional environment variable. Their one-time URLs point back to the deployment's `APP_BASE_URL`, so production must keep that origin on trusted HTTPS; generated accounts use verified SIP TLS on public port `5061`. The canonical JSON contract is published at `/openapi.yaml` and documented in [Phone API](PHONE_API.md). It is safe to health-check the OpenAPI document, but never place a real `/api/v1/phone-provisioning/{token}`, `/provision/ios/{token}`, or `/provision/linphone/{token}` URL into a monitor because a successful `GET` intentionally consumes it. The additive `device_provisioning_tokens` table remains rollback-compatible: an older binary ignores it, and rotating a device after returning to this release creates a fresh link.

Background incoming calls for the iOS app additionally require an Apple Push Notification service provider key authorized for the app bundle. Keep the downloaded `.p8` in a root-only file outside the checkout, enable the Push Notifications capability for the bundle identifier, and install it with the source-controlled helper:

```sh
sudo install -m 0600 /private/path/AuthKey_<KEY_ID>.p8 /root/AuthKey_<KEY_ID>.p8
sudo /opt/ringring/ringringctl configure-apns \
  --key-file /root/AuthKey_<KEY_ID>.p8 \
  --team-id <APPLE_TEAM_ID> \
  --key-id <KEY_ID> \
  --bundle-id com.mcchord.ringring \
  --environment production \
  --dry-run
sudo /opt/ringring/ringringctl configure-apns \
  --key-file /root/AuthKey_<KEY_ID>.p8 \
  --team-id <APPLE_TEAM_ID> \
  --key-id <KEY_ID> \
  --bundle-id com.mcchord.ringring \
  --environment production \
  --yes
```

The command validates the P-256 key and identifiers, creates `/etc/ringring/apns` as root-owned mode `0750`, installs the key as root-owned mode `0440` for the RingRing app group (GID `10001`), atomically writes the five `APNS_…` variables, recreates only the app container, and runs the complete deployment checks. This is the minimum access required by the deliberately unprivileged app process; Compose mounts only that directory read-only at `/run/secrets/ringring-apns`. TestFlight and App Store builds use `production`; a separately signed debug build and sandbox key may deliberately use `development`. Do not put the `.p8`, APNs token, SIP credential, or a real phone API response in the repository, shell output, or support logs.

The new `phone_push_registrations` table is additive and device-cascading. It stores a token hash, encrypted token ciphertext, environment, and update time—no call record or Apple account data. An older rollback ignores the table and stops sending wake pushes; the next current app resumes using surviving registrations. Rotation and revocation deliberately delete a phone's registration. Verified backups include the active APNs provider key because the archived environment references it; treat those already root-only archives as deployment secrets and private data.

Real-phone acceptance adds an additive `device_readiness` table containing only a device foreign key, three nullable host-confirmation timestamps, and an update timestamp. Startup creates the empty table without changing an existing member, device, credential, route, or provider resource. Older app builds ignore it, so an application rollback remains database-compatible; confirmations made before rollback reappear if this release returns. Credential rotation on this release intentionally clears that phone's confirmations. Verify the empty schema and host page after upgrade, but do not submit a real family's checklist merely as a deployment probe.

Member weather adds five nullable/defaulted columns to `members`: query, resolved label, latitude, longitude, and update time. On the first upgraded startup, RingRing copies any existing party-wide resolved weather place to members that already exist; members created afterward start empty and establish their own place. The legacy `party_services` weather-location columns remain untouched for rollback compatibility, while new code reads member locations. Older builds therefore still see their last party-wide value if rolled back. Do not drop either set of columns during a normal rollback. The backup verifier requires the new member columns after migration but never reports their values.

Party OpenAI key replacement adds a nullable `parties.openai_api_key_id` column on startup. Existing party keys remain usable with an empty identifier; their first host-requested replacement records the fresh key ID before retiring all older keys owned by the dedicated service account. Older app builds ignore the additive column. If rollback occurs while a party says its key is rotating or needs retry, its AI-powered routes remain paused; return to this release and use the host's **Finish key replacement** action before re-enabling them. Never test this control against production merely as a deployment probe because a successful submission deliberately revokes the current party key.

Host-set spend limits add three forward-only `parties` columns for the last confirmed amount, one pending amount, and its reconciliation state. Existing parties migrate to an honest `unknown` local state without changing their provider project or interrupting current routing; the host's first save verifies or replaces the chosen amount. A new update also mirrors its pause into the older `openai_status` column, so rollback keeps AI routes unavailable rather than ignoring an uncertain provider result. Do not roll back or edit the pending amount mid-update. Return to this release and choose **Finish spend limit update**, which safely repeats that exact amount until OpenAI confirms active enforcement. Do not submit the production form as a deployment probe because it deliberately changes the party's provider limit.

`METRICS_ADDR` and `AI_AUDIO_ADDR` are private container traffic and must not be published on the host. The metrics listener exports only bounded aggregate health/activity and Caddy does not proxy it; see [Privacy-preserving observability](OBSERVABILITY.md). The reference limits `*14` calls to three minutes and two concurrent sessions in addition to each party project's hard monthly spend limit.

`AI_ADULT_ONLY_ENABLED` is a server-operator gate for the open-ended `*14` conversation line, not a party preference. It defaults to `false`; fresh installs write that value explicitly, and older deployments that omit it are also closed. While closed, RingRing rejects host attempts to enable `*14`, clears any older saved `ai_enabled` preference at startup, removes the route from generated Asterisk configuration, refuses FastAGI authorization and party-key decryption, and refuses the Realtime WebSocket bridge. Time, weather text-to-speech, radio, echo, extension selection, and ordinary party calls are unaffected.

Set the gate to `true` only when this deployment intentionally offers an adults-only preview. A member chooses **Adult extension (18+)** once while claiming a new extension. Existing extensions migrate to non-adult, the host cannot enable `*14` until at least one adult extension exists, and every call is authorized from the active authenticated SIP endpoint in the same party. Do not mark a child or shared handset adult. The call disclosure explains that audio goes to OpenAI and provider retention may apply even though RingRing stores no audio or transcript.

Edit the root-only `/etc/ringring/app.env`, set exactly `AI_ADULT_ONLY_ENABLED=true`, run `cd /opt/ringring && docker compose up -d --force-recreate app`, and then run `sudo /opt/ringring/ringringctl doctor`. To revoke the preview, set the value back to `false` and recreate the app container; startup closes saved conversation preferences before telephony reconciliation.

The independent `sudo /opt/ringring/ringringctl openai-retention` command still makes bounded, read-only requests to OpenAI's official [organization](https://developers.openai.com/api/reference/python/resources/admin/subresources/organization/subresources/data_retention/methods/retrieve) and [project](https://developers.openai.com/api/reference/python/resources/admin/subresources/organization/subresources/projects/subresources/data_retention/methods/retrieve) data-retention endpoints. Success prints only `status`, the non-secret organization ZDR type, and the number of party projects checked. A forbidden or `not_eligible` response, modified-abuse-monitoring or project `none` mode, unknown/malformed result, missing administrator key, timeout, or transport error fails the command and is not confirmation. This audit is not a runtime precondition for the adult-only preview and must not be used to imply that RingRing is ready for minors. OpenAI's [Under 18 API Guidance](https://developers.openai.com/api/docs/guides/safety-checks/under-18-api-guidance) and an external under-18 safety/privacy review remain release gates before offering `*14` to minors. Do not put review evidence, provider responses, or administrator credentials in the repository.

## SIP authentication firewall

Asterisk already limits unidentified request tracking and uses globally random SIP usernames and passwords. The reference host also uses Fail2Ban to temporarily block repeated PJSIP authentication failures before Docker's published-port rules. Install the checked-in jail after Docker is running:

```sh
/opt/ringring/scripts/install-sip-firewall.sh
```

The guided installer runs this after the image build and security-log creation but before `docker compose up`, so neither SIP listener is intentionally published without its jail. Guided upgrades refresh both checked-in jails without repeating package installation.

The jail reads Asterisk's dedicated security log and inserts its jump in Docker's `DOCKER-USER` chain, which Docker processes before its forwarding rules. Ten failures in ten minutes produce a 15-minute ban; repeat bans grow up to 24 hours. Check it with:

```sh
fail2ban-client status ringring-sip
fail2ban-client status ringring-sip-tls
iptables -n -L DOCKER-USER
```

## SIP TLS certificate lifecycle

Caddy owns ACME issuance and renewal in its persistent storage. RingRing does not make Asterisk a second ACME client. Install the root-only synchronization timer after the Compose `.env` contains the exact deployment hostname and Caddy has started:

```sh
sudo /opt/ringring/scripts/install-sip-tls-sync.sh /opt/ringring
sudo /opt/ringring/scripts/sync-sip-tls.sh --wait 180
systemctl status ringring-sip-tls-sync.timer
```

Every six hours, with a randomized delay, the timer asks Caddy's official `storage export` command for an archive. The synchronizer rejects unsafe archive entries, selects only a certificate whose SAN matches `RINGRING_DOMAIN`, requires its private key to match and at least seven days of validity to remain, and stages the pair as mode `0600` files under `/etc/ringring/tls`. Asterisk makes its own runtime copy with a mode-`0600` key. It reloads PJSIP only after confirming that zero calls are active; an active or indeterminate call returns a successful deferral status so the persistent timer retries later.

On first startup, Asterisk may create a two-day self-signed fallback so the configured transport can initialize while Caddy is still issuing. That fallback is not public-ready: `ringringctl install`, `upgrade`, and `doctor` require a system-trusted certificate for the exact hostname unless `--skip-public-check` was explicitly used. Do not distribute a fallback certificate, copy Caddy's entire data directory into Asterisk, or disable phone certificate verification.

## Deploy and verify

```sh
cd /opt/ringring
docker compose build
docker compose up -d
docker compose ps
curl --fail https://ringring.live/readyz
curl --fail --silent https://ringring.live/openapi.yaml | grep -q '^openapi: 3.1.2$'
test "$(curl --silent --output /dev/null --write-out '%{http_code}' https://ringring.live/metrics)" = 404
docker compose exec -T app curl --fail --silent http://127.0.0.1:9090/metrics
docker compose exec asterisk asterisk -rx 'pjsip show transports'
docker compose exec -T app ringring verify-ami
fail2ban-client status ringring-sip
fail2ban-client status ringring-sip-tls
systemctl is-active ringring-sip-tls-sync.timer
openssl s_client -connect ringring.live:5061 -servername ringring.live -verify_hostname ringring.live -verify_return_error -tls1_2 </dev/null
```

`verify-ami` exercises the private manager login and complete PJSIP contact-list action, then prints only an `ok` status and aggregate contact count. It never prints endpoint names, contact URIs, addresses, user agents, or credentials. The private metrics scrape repeats that aggregate AMI/database health boundary and must remain unreachable through public HTTPS. `ringringctl install`, `upgrade`, and `doctor` check both invariants. Review identifier-free application logs with `docker compose logs --tail=100 app`; treat the separate Asterisk authentication security log as restricted, short-retention abuse data. Asterisk is compiled from the pinned official source release and its published SHA-256 file is checked during the image build.

The host-triggered incoming ring uses the same private AMI listener. Its source-controlled account has Asterisk's narrow `originate` write privilege in addition to the existing reload permissions, but not `all` or configuration access; the ACL still permits only the fixed app-container address. The application exposes no arbitrary Originate fields and the internal prompt context has no public-network route. After an upgrade, use the disposable `make sip-smoke` gate—not a real family device—to verify the complete host request, contact recheck, AMI Originate, answered SIP call, bundled prompt, clean hangup, and absent CDR.

The Asterisk image and entrypoint normalize every static config—and especially `extensions.conf`—to a group-readable mode owned by the runtime account. This is required because `ringringctl` intentionally uses a restrictive root umask, and a Git-updated config may otherwise enter the Docker build context as mode `0600`. Install and upgrade verification fail unless the fixed phone-check context is actually loaded, so container health and AMI reachability alone cannot mask a declined dialplan module.

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

The test renders two disposable devices through the production telephony code and builds SIPp 3.7.7 from its checksum-pinned official source release with TLS support. A disposable CA proves a TLS 1.2 handshake with exact hostname verification. One SIPp phone authenticates and calls through TLS while the other uses compatibility UDP; they must call extension `102` from `101`, complete the single-phone `*10` echo route, exchange the expected bidirectional PCMU patterns, receive answered prompt-length responses for an invalid extension and disabled star line, and complete authenticated `*15`. It uses a dedicated internal Docker network, fixed smoke-only credentials, no production environment or database, and no published host ports. Exact-name collision checks prevent it from disturbing an already-running smoke test; its containers, network, generated state, and certificates are removed on exit. Set `RINGRING_SMOKE_TMP_ROOT` to an existing host-shared directory when a VM-backed Docker runtime cannot bind-mount host `/tmp`. Passing these software loops does not replace a call between remote physical devices across real NATs.

## Isolated two-household NAT smoke test

Run the NAT-sensitive gate after changing PJSIP endpoint settings, Asterisk transport behavior, registration handling, or media routing:

```sh
cd /opt/ringring
make nat-smoke
```

The target creates two private phone namespaces with distinct public SNAT identities inside a privileged, disposable topology container. Both SIPp clients advertise private contact and SDP addresses, authenticate through RingRing's production endpoint renderer, and call extension `102` from `101`. The test requires Asterisk to rewrite both contacts to their respective public identities, keep media server-relayed, complete the party call, carry more than 100 non-SIP UDP packets across each NAT in each direction, and finish with no active channels.

The topology container receives no Docker socket, deployment environment, production state, host network, or published port. A fixed internal Docker network connects only that container and disposable Asterisk. The generated-state bind stays read-only but is explicitly traversable by Asterisk's dropped UID when the operator runs the candidate under `umask 077`; before the one-shot SIP transaction starts, a bounded readiness loop requires the rendered PJSIP endpoint to be loaded and both nested household paths to reach the PBX. An exit trap removes the containers, nested namespaces, network, generated configuration, logs, and audio. The test needs a Docker runtime that permits a privileged container to create nested Linux network namespaces. It models two household paths but does not prove real router, carrier-grade NAT, mobile handoff, or physical-device behavior.

## Isolated Linphone provisioning and call smoke test

Run the heavier client interoperability check after changing Linphone XML or setup behavior:

```sh
cd /opt/ringring
make linphone-smoke
```

The target builds a test-only image from the checksum-pinned official Linphone Python wheel, renders two disposable members through RingRing's production telephony functions, and starts Linphone with a local HTTP provisioning URI for extension `101`. It installs a disposable root CA and explicitly enables server-certificate and common-name verification before starting the engine. The generated account must use TLS `5061`, fetch exactly once, import exactly one account, authenticate successfully, and appear as a reachable Asterisk contact alongside UDP SIPp extension `102`. Linphone then calls `102` through the generated party dialplan using a file-backed 440 Hz input while SIPp echoes RTP. The test requires an established media state, an allowed codec, more than 100 RTP packets sent and received, the expected tone in Linphone's returned-audio WAV, clean dialog teardown, and no remaining Asterisk channels.

Image/dependency acquisition happens before the runtime check; the actual clients and Asterisk run on a dedicated internal network with fixed smoke-only credentials, no production environment or database, and no published host ports. The generated state, provisioning, and certificate bind roots are explicitly read-only/traversable to their dropped runtime UIDs even when an exact candidate is run under `umask 077`, while the disposable TLS private key stays root-only. Exact-name collision checks and an exit trap remove the containers, network, generated XML and audio, certificates, Linphone state, and Asterisk state. `make sip-tls-smoke` runs both TLS-aware client suites; see the evidence in [SIP TLS compatibility](SIP_TLS_COMPATIBILITY.md).

The official wheel is GPLv3 and intentionally remains outside the MIT-licensed RingRing binary and production images. This headless check does not replace scanning a real QR in the mobile app or testing push, background ringing, Wi-Fi/cellular changes, and remote NAT behavior on family hardware.

## Internet-radio compatibility check

Run the external radio check after changing the station catalog, Asterisk image, or MP3 playback route:

```sh
cd /opt/ringring
make radio-smoke
```

The application image emits only its fixed station IDs and URLs into a disposable production Asterisk image. The runner rejects anything outside the exact SomaFM MP3 host/path shape and requires every catalog entry to deliver decodable MPEG Layer III audio. It receives no deployment environment, production state, family credential, or published port. This is an intentional outbound availability check and is kept outside the default offline test suite.

## Guided upgrade

The installed checkout must be clean and its root-only environment files must still pass validation. Fetch first if the dry run should inspect the newest remote `main`; the dry run itself intentionally performs no fetch, backup, file write, service action, firewall change, or Git move:

```sh
cd /opt/ringring
sudo git fetch --prune origin
sudo ./ringringctl upgrade --dry-run
sudo ./ringringctl upgrade
```

The real upgrade fetches again, resolves one exact target commit, and refuses any non-fast-forward history. Before moving Git it creates and drills a checksummed backup, then records the old commit, target commit, and backup path in root-only `/etc/ringring/upgrade.pending`. It runs from an immutable temporary copy so replacing `ringringctl` in the target commit cannot corrupt the operation in progress. It fast-forwards, builds the complete stack, refreshes both checked-in Fail2Ban jails, installs the certificate synchronization timer, reconciles services, synchronizes the trusted SIP certificate, verifies private and public health, and creates and drills a post-upgrade backup.

If any post-backup step fails, keep the named backup and marker, fix the reported condition, and run `sudo /opt/ringring/ringringctl upgrade --yes`. Resume uses the exact recorded target and does not repeat the pre-upgrade backup or drill. RingRing never attempts an automatic Git or database rollback across a forward migration; follow the release-specific notes below before considering a manual rollback.

Existing manual deployments must first create `/opt/ringring/.env` with mode `0600` and this single non-secret setting, using the hostname from `APP_BASE_URL` without `https://`:

```dotenv
RINGRING_DOMAIN=phone.example.com
```

The value must match both `APP_BASE_URL` and `SIP_PUBLIC_HOST` in `/etc/ringring/app.env`. The guided installer creates this file automatically. Do not copy the application or Asterisk secrets into the Compose `.env`, because Compose passes that file to Caddy's variable substitution path.

At any time with no install or upgrade pending, run the read-only deployment check:

```sh
sudo /opt/ringring/ringringctl doctor
```

## Manual update (advanced)

```sh
cd /opt/ringring
make backup
git pull --ff-only
docker compose build
docker compose up -d --remove-orphans
scripts/install-sip-firewall.sh --skip-packages /opt/ringring
scripts/install-sip-tls-sync.sh /opt/ringring
scripts/sync-sip-tls.sh --wait 180
```

Drill the backup and perform every verification from **Deploy and verify** when using the manual path. Generated Asterisk files can be regenerated from the database and do not need separate backups. The guided upgrade is preferred because it binds the verified backup, exact commit, failure marker, service checks, and post-upgrade recovery point into one resumable operation.

### SIP TLS upgrade and rollback

The SIP TLS release adds public `5061/tcp`, a read-only `/etc/ringring/tls` mount, a second Fail2Ban jail, the root synchronization timer, and TLS-first Linphone XML. It adds no database column or application secret. Before upgrading, allow `5061/tcp` at the provider firewall and confirm it is unused. A rollback removes TLS-first provisioning and may stop publishing `5061`; already configured TLS-only phones will then lose registration until the release is restored or those clients are deliberately changed to UDP. Keep UDP `5060` available during the compatibility period. The copied certificate/key are replaceable derived state and can be removed only after Asterisk no longer mounts them and the timer has been disabled; Caddy's own storage must remain intact.

### Real-phone readiness upgrade and rollback

The readiness checklist adds only the `device_readiness` table described in environment setup. Take and drill the normal app-state backup before upgrading. Existing rows in every other table remain untouched, no Asterisk configuration changes, and no external service is contacted. The current backup verifier requires this table after migration and reports only its aggregate row count. Older app builds safely ignore it on rollback. If an operator needs to remove the table permanently, first preserve the drilled pre-upgrade backup, stop the app, confirm no readiness evidence must be retained, and use a deliberate SQLite migration rather than editing a live WAL database; normal rollback does not require removal.

### Safe extension suggestion upgrade and rollback

This release adds no schema, secret, public port, or provider call. At startup,
it idempotently moves any legacy member using `000`, `111`, `112`, `911`, `988`,
or `999` to that party's first available ordinary extension beginning at `101`.
This repairs values that older web or `*15` paths could accept and prevents a
stale public-safety-like route from remaining loaded. The party page immediately
shows the replacement. Take and drill the normal pre-upgrade backup; an older
binary accepts the replacement on rollback, so no reverse data migration is
required. Do not manually restore the reserved value.

### Copy-ready setup sheet upgrade and rollback

The copy-ready setup release adds one embedded static JavaScript asset and
changes Content Security Policy from `script-src 'none'` to the exact SHA-256
digest of that asset. The base template carries the same Subresource Integrity
digest, so a partial or modified asset fails closed instead of executing. The
release adds no schema, stored state, secret, public port, provider call, or
external script. Deploy the application binary normally; its embedded template
and asset cannot drift independently. Rolling back restores the script-free
sheet and needs no data or configuration migration.

### `*14` upgrade and rollback

The `*14` release adds `party_services.ai_enabled` with a forward-only startup migration. Take the app-state backup while the app is stopped, then restart the old version before beginning the normal update. The column defaults to disabled and older RingRing builds ignore it, so rolling the app image and checkout back leaves the migrated database usable; the four `AI_*` environment variables are also ignored by older builds. Keep the database backup until the upgraded app, private port `4574`, and generated Asterisk dialplan have all been verified. Do not publish port `4574` during either upgrade or rollback.

The adults-only release adds the forward-only `members.adult_extension` boolean and replaces the former operator variable with `AI_ADULT_ONLY_ENABLED`. Existing members migrate to false, and older builds safely ignore the additive column. A deployment that still has only `AI_CHILD_SAFETY_APPROVED` remains closed because the new variable defaults false. Before rollback, set `AI_ADULT_ONLY_ENABLED=false`, recreate the app so durable `*14` preferences are cleared, and leave the older gate false as well. The isolated restore drill explicitly overrides the new gate to false. The standalone read-only ZDR audit remains available but is no longer coupled to startup or `doctor`; it changes no provider state.

### `*15` upgrade and rollback

The voice extension chooser adds no schema, secret, environment variable, public port, or external service. Upgrading the app regenerates each party dialplan with `*15`; the private FastAGI listener already used by weather handles the interaction. Rollback regenerates the dialplan without `*15`. Any extension a member already selected remains an ordinary compatible `members.extension` value, while its SIP username and encrypted password remain unchanged.

### Radio-selection upgrade and rollback

The curated selector adds `party_services.radio_station` with a forward-only startup migration and defaults every existing row to Groove Salad, preserving the prior route. Take and drill the normal app-state backup before upgrading. Older app builds ignore the additive column; rolling back regenerates any enabled `*13` route with their fixed Groove Salad setting, while the chosen catalog ID remains stored for a later return to this release. No secret, environment variable, public port, or new container is added.

### Host-set AI spend-limit upgrade and rollback

The spend-limit control uses the additive `parties` columns described in environment setup above and adds no secret, public port, container, or provider resource. Take and drill the normal app-state backup before upgrading. Existing projects are not contacted during migration or startup. After upgrade, verify the page and schema without submitting the control against a real party. If a host update is pending, finish it on this release before any rollback; the mirrored legacy status keeps older releases fail-closed but only this release knows how to reconcile the pending amount.

### Host-triggered phone ring upgrade and rollback

The incoming setup ring adds no schema, stored call state, secret, public port, provider request, or generated party route. It adds a fixed internal Asterisk prompt context and the narrow AMI `originate` write privilege described above. Upgrading or rolling back recreates Asterisk from the matching source-controlled configuration; older application builds never invoke the permission. A ring already queued at rollback may finish within its 20-second absolute timeout. Use the isolated SIP smoke before promotion and do not ring a real family device as a deployment probe.

### Joinable party-call upgrade and rollback

Joinable calls add no schema, durable call record, environment variable, secret, public port, or new provider credential. The release replaces ordinary answered member calls with source-controlled ConfBridge profiles and exact generated `*16{extension}` routes, adds bounded AMI conference reads and a fixed announcement Originate, and adds a host-authenticated no-store polling fragment. A normal upgrade or rollback recreates the app and Asterisk, so any call already in progress is intentionally disconnected; phone registrations and every persisted party/member/device record remain compatible. Rolling back removes live-call display and join routes without migrating or deleting data. Run the isolated three-phone SIP gate before promotion, verify the ConfBridge module and fixed announcement context after deployment, and wait for a deliberate family-hardware test rather than joining an existing production call as a probe.

### iPhone background calls and live menu upgrade and rollback

This release adds the device-cascading `phone_push_registrations` table, two authenticated HTTPS endpoints, a private FastAGI wake route, and an optional root-owned APNs provider key exposed only to the app's unprivileged group. It adds no public port, call-detail record, audio storage, or new container. Upgrade and verify the code first, then use `ringringctl configure-apns` so a failed app build never strands the existing deployment on new secret configuration. The next telephony reconcile places the minimized wake before ordinary member dialing; a successful APNs request adds only a short pre-dial delay. Rollback ignores the additive rows, regenerates member routes without the wake step, and leaves the managed key in place for a later forward return. Remove or revoke that provider key only as a deliberate credential-retirement operation, never as part of a routine application rollback. A physical TestFlight iPhone must pass foreground, background, and lock-screen calls before this path is promoted beyond preview.

### Additional member phones upgrade and rollback

Adding another phone uses the existing `devices` and `device_provisioning_tokens` tables, encryption key, setup reveal, and generated PJSIP/dialplan files. It adds no schema, environment variable, public port, provider request, or background service. Existing members and phones are unchanged at startup. Newer releases may store several independent device rows for one member; older releases already list, route, rotate, revoke, and cascade-delete those rows correctly, so rollback preserves same-extension ringing. The isolated SIP smoke creates the extra device through the host web route and verifies the loaded explicit fan-out without changing a production family record.

### Invitation cancellation upgrade and rollback

Counting and canceling active invitations uses the existing `invitations` ownership, expiry, and `used_at` fields. It adds no schema, environment variable, public port, provider request, or background service, and an upgrade does not inspect, consume, or delete an invitation. A host cancellation permanently removes only that party's active unused token hashes; rollback cannot restore a deliberately canceled bearer link, while used/expired invitations and every member/device remain compatible with older releases.

### Private first-call card upgrade and rollback

The claim-only first-call card reads existing `members`, `devices`, `party_services`, and party readiness fields and adds no schema, secret, environment variable, route, public port, provider request, or background service. Upgrading does not inspect an invitation or family directory until a recipient submits a valid claim. Rolling back removes only the rendered setup aid; memberships, credentials, services, generated telephony state, and invitation lifecycle remain compatible and unchanged.

### Keypad-friendly SIP credential upgrade and rollback

The numeric credential release adds no schema, environment variable, port, or migration. Startup decrypts and renders every existing username/password unchanged, so registered phones do not reconnect or rotate merely because of the upgrade. Only a successful new invitation claim, host-added phone, or deliberate credential rotation receives the current 6-digit username and 12-digit password format. The existing global username constraint and bounded application retry make the shorter identity collision-safe. Older releases already accept those values as safe Asterisk identifiers/secrets, so rollback preserves and can continue routing them. The setup page's spaces are presentation only; raw database, Asterisk, copy, and Linphone values contain digits without separators. Verify the format with the isolated SIP smoke, which creates and authenticates a generated numeric endpoint; do not rotate a real family phone as a deployment probe.
