# Production voice acceptance

Use this runbook when a production special line answers and hangs up, produces
silence, or appears to pause before speaking. It is also the acceptance checklist
for changes to `*11` time, `*12` weather, the RingRing operator, FastAGI, OpenAI
speech, bundled prompts, or the Asterisk image.

The central lesson is to prove each layer independently. An answered SIP dialog
does not prove that Asterisk could read a prompt, that RTP reached the client, or
that the friendly provider voice ran. Likewise, a successful outbound `INVITE`
does not prove that the disposable phone left a reachable registration.

## Safety boundary

- Get explicit authorization before creating a production test host, party,
  member, phone, or provider project. Use the public host, invitation, claim,
  provisioning, and deletion flows; do not seed or delete live database rows.
- Use neutral disposable labels and a clearly reserved test extension. Never
  print the SIP username, password, provisioning URL/XML, party or member ID,
  contact URI, address, cookie, recovery code, provider key, or project ID.
- Capture only aggregate metrics and a disposable client's received test audio.
  Delete that audio with the rest of the test state. Never record a family call.
- Check for zero active calls and channels immediately before recreating a
  service or originating a diagnostic call. Do not restart Asterisk merely to
  clear an app warning or refresh a test.
- Take a baseline with the exact checkout commit, a clean worktree,
  `ringringctl doctor`, aggregate state counts, configured endpoint count, and
  zero channels. Cleanup must return all of them to the baseline.

## 1. Prove the prompt and dialplan before calling

Check the loaded `*11`/`*12` route, the `SayUnixTime` fallback, and endpoint
`language=en`. Test prompt access as the runtime account, not as root:

```sh
cd /opt/ringring
docker compose exec -T asterisk \
  runuser -u asterisk -g ringring -- test -r /var/lib/asterisk/sounds/ringring-here.wav
docker compose exec -T asterisk \
  runuser -u asterisk -g ringring -- test -r /var/lib/asterisk/sounds/digits/day-1.gsm
docker compose exec -T asterisk stat -c '%a' /var/lib/asterisk/sounds
docker compose exec -T asterisk stat -c '%a' /var/lib/asterisk/sounds/en
```

Both directories must be `0755`; packaged prompt files must be `0644`. A guarded
checkout runs with `umask 077`, and Docker `COPY` can preserve a source
directory's `0700` mode. Seeing the file as root, checking only its file mode, or
successfully loading the dialplan is not enough: every parent directory must be
traversable by `asterisk:ringring`.

After a call, reject any bounded Asterisk trace containing `Permission denied`,
`does not exist in any format`, or `Unable to open ... sound`. Keep both the
language-prefixed and root English prompt trees because physical and local
channel lookups have exercised both paths.

## 2. Keep the disposable SIP phone alive until it is reachable

Create the disposable host and party through HTTPS, consume a one-time Linphone
provisioning URL, and register the client with public TLS certificate and
hostname verification enabled. The provisioning XML contains credentials: mount
it read-only with the exact UID that runs Linphone. A separate test program
contains no secret and may be mode `0444`; making that helper `0600 root:root`
causes the unprivileged container's Python process to exit before registration.

RingRing AORs currently use `qualify_frequency=60`. A newly registered contact
can therefore appear as `Unknown` for almost a minute. Keep the client registered
for at least 65 seconds and require the exact current contact to become `Avail`
before accepting the test. An immediate `pjsip qualify` request is not a
substitute for observing the authoritative `Avail` transition. Do not log the
endpoint or contact while checking it.

This wait is deliberate. A client that can send an authenticated outbound call
but never becomes `Avail` has not passed the phone-reachability contract.

## 3. Prove media, not merely answer

Dial the target from the disposable client and require all of the following:

- media reaches `StreamsRunning` before the call ends;
- the codec is one of the deployment's allowed audio codecs;
- at least 20 RTP packets arrive;
- at least 0.5 seconds of received linear PCM is recorded in disposable state;
- the recording has meaningful non-silent RMS energy; and
- Asterisk returns to zero channels after teardown.

An Asterisk channel, a `200 OK`, or a nonzero call duration alone does not prove
that the caller heard anything. The August 2026 incident answered successfully
while prompt lookup failed, then hung up with no useful RTP.

## 4. Distinguish the friendly voice from the local fallback

Read the private aggregate time metrics immediately before and after the call.
Exactly one of the `ready` or `error` results should advance. A `ready` result
must correspond to a nonempty `time-v1-*.wav` that the Asterisk runtime user can
read. An `error` result is acceptable only when the call delivered the local
`SayUnixTime` fallback.

A new disposable OpenAI project may accept its monthly hard limit while
reporting enforcement as `inactive` for longer than a minute. RingRing must fail
closed in that state: it must not issue or store a runtime service-account key.
Do not bypass this safety gate merely to make a production test use the friendly
voice.

Use two pieces of evidence when the provider stays `inactive`:

1. The real disposable SIP extension proves registration, Asterisk routing,
   prompt access, RTP, audible audio, fallback behavior, and cleanup.
2. With separate authorization, a private Asterisk local call to the one already
   `ready` party's `*11` context proves the friendly OpenAI route. It must ring no
   family phone, send only the fixed code-controlled time sentence, record no
   audio, advance `ready` without advancing `error`, create readable derived
   audio, and leave zero channels.

Never copy a family party's key into a disposable party or weaken the spend
enforcement check. Provider list responses can lag behind project updates, so
confirm cleanup by retrieving each matching disposable project directly and
requiring `status=archived`. Archive a partially provisioned project after any
spend, service-account, credential-response, cancellation, or timeout failure.

## 5. Clean up and restore the diagnostic baseline

Delete the disposable party and host through their supported authenticated
flows, then require:

- aggregate users, parties, members, devices, sessions, recovery codes,
  invitations, provisioning tokens, and decrypted-key counts match baseline;
- generated endpoint count matches baseline;
- `PRAGMA foreign_key_check` is empty through `ringring verify-state`;
- every matching disposable provider project is directly confirmed archived;
- public `/healthz` and `/readyz` return HTTP 200;
- `ringringctl doctor` passes; and
- all exact-name containers, state directories, received audio, cookies,
  provisioning files, and root test helpers are removed.

A deliberate disposable provisioning failure can leave a recent
`party OpenAI provisioning failed` warning, and app startup can log the known
app-before-Asterisk reconciliation race. Once test state is gone, Asterisk is
healthy, no operation is pending, and channels are zero, recreate **only the
app**, wait for health, and rerun doctor. Do not recreate the PBX to clear an app
log.

Use HTTP success for the public health endpoints; do not assert an undocumented
literal JSON status word. `ringringctl openai-retention` is an independent
provider-posture audit, not part of `doctor` or the deployment gate unless the
operation explicitly requires it.

## Database and backup recovery

SQLite foreign-key enforcement is connection-scoped. RingRing sets it in the
driver DSN, reads it back, deletes authorized dependent rows explicitly, and
requires an empty foreign-key check after its idempotent orphan repair. Do not
move those settings back into a multi-statement `Exec` or rely only on cascades.

If `ringringctl upgrade` cannot create its pre-upgrade backup because an affected
older build already left orphan rows, stop. Do not edit the live database or its
WAL files. Follow the exact published `repair-orphaned-state.sh` procedure in
[Deployment](DEPLOYMENT.md#friendly-time-voice-and-english-prompt-routing-upgrade-and-rollback).
It creates a root-only pre-repair quarantine, proves the repair on a copy, applies
the same published target to live state, and then returns control to the normal
guarded upgrade.

## Local smoke-test traps

On Docker Desktop or another VM-backed Docker runtime, host `/tmp` may not be a
shared bind root. The container can then see a different root-owned directory or
miss the disposable SQLite file entirely. Run the gate from a Docker-shared
workspace path:

```sh
cd /path/to/RingRing
RINGRING_SMOKE_TMP_ROOT="$PWD" make sip-smoke
```

If a large build fills the Docker VM, inspect usage first. Remove only exact
disposable resources or dangling intermediate images; never broadly prune tagged
images, volumes, or production state.

## Final handoff evidence

Record the following in `WORKLOG.md` without identifiers or addresses:

- exact published and production commit;
- guarded pre/post backup and any quarantine result;
- disposable contact availability, codec, received RTP count, media seconds,
  and non-silent RMS result;
- whether that call used friendly speech or the local fallback;
- separate friendly-route metric/audio proof when needed;
- prompt access as `asterisk:ringring` and absence of missing-sound errors;
- direct confirmation that every disposable provider project is archived;
- final doctor, public health, Compose health, database/credential, AMI, channel,
  clean-checkout, and temporary-helper cleanup results; and
- any expected test limitation, especially provider spend enforcement remaining
  `inactive`.
