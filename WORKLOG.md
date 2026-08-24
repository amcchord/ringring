# RingRing worklog

This is the durable, chronological project record. Add new entries at the top. Capture decisions and verification, not a transcript of commands.

## 2026-08-24 — Quieter voice help and simpler party-call joining

### Shipped

- Removed the repeated AI-voice notices from the RingRing operator and weather speech. Removed the open-ended conversation line end to end: no `*14` route, AudioSocket/Realtime listener, host switch, setup-card promotion, invitation adult-extension choice, iOS control, runtime configuration, or conversation-specific dependency remains. Startup clears an older saved enablement, new claims force the legacy adult flag off, and released iOS payloads may still include that now-ignored field while upgrading.
- Added a bundled 0.68-second mono 8 kHz PCM “Ring ring! RingRing here!” recording to the Asterisk image. Operator, weather setup, and forecast work now begins concurrently and the local greeting plays immediately while cached or generated text-to-speech becomes ready; the generated remainder no longer repeats the introduction.
- Made dialing an extension that already owns an active party conference enter the same authenticated join path as its hidden `*16…` action. The operator announces the joining member resolved from the authenticated endpoint, ConfBridge recording and CDR stay disabled, and a new call still rings normally when no conference exists.
- Kept active calls at the top of the iPhone call menu with participant names, phone count, and one-tap joining. People retain the large primary cards; **More to call** now sits below a divider with explicit optional copy, smaller muted rows, and reduced visual weight. The debug preview no longer contacts the live phone API, keeping visual and App Store captures deterministic.

### Decisions

- Keep provider speech one-way and code-controlled for operator help, weather, and join announcements; remove the interactive model surface rather than merely hide it in the UI. Current model calls receive no caller audio.
- Use the ordinary called extension as the natural join action while retaining the hidden join destination used by configured phone-app buttons. Both paths remain generated inside the authenticated caller's exact party context.
- Retain only inert legacy SQLite columns so forward-only migrations and intentional rollback remain safe. Current models, store inputs, APIs, and routing no longer expose adult/conversation state, and fresh databases no longer create the retired disclosure table.

### Verification

- `make check`, `make security`, and `make admin-test` pass. This includes formatting, shell and administrative lifecycle checks, `go vet`, the complete race-enabled Go suite, executable security contracts, and `govulncheck` with no called vulnerability.
- The iPhone 17 Pro simulator passes all 16 Swift tests. A clean configured-phone preview confirms a prominent **Happening now** join card, primary **People** cards, and the separated muted **More to call** section without an alert or horizontal overflow.
- The bundled greeting is verified as 16-bit mono PCM at 8 kHz and 0.6835 seconds. Voice tests pin pre-roll-before-generated-audio ordering, cache behavior, fixed prompt privacy, and the absence of the removed notices.
- `make sip-smoke` passes with verified TLS 1.2 and UDP phones, live three-phone joining by dialing the already-active extension, announcement authorization, original-owner teardown, zero residual channels/CDR, mixed-transport media, operator fallbacks, `*10`, and authenticated `*15`.

### Remaining

- Listen to the bundled greeting and measure perceived first-word latency on physical family handsets. Complete one TestFlight one-tap join and one three-phone direct-extension join before treating the interaction as hardware-verified.

## 2026-08-24 — Refresh the contributor contract and project tour

### Shipped

- Expanded `AGENTS.md` with the current authentication, invitation, short SIP credential, multi-phone, weather, special-line, adult-AI, live-call, operator-disclosure, and call-joining product contract.
- Added explicit hardware diagnosis rules learned from the HT801 V2 work: prove the physical-device identity before changing it, treat Asterisk contacts as the incoming-call authority, verify registration from both ends, distinguish a channel from confirmed ringing, and retain the complete physical acceptance matrix.
- Documented the current privacy boundary for live AMI state and fixed TTS, credential-rotation behavior, safe screenshot rules, the guarded production workflow, and the app-before-Asterisk reconciliation race.
- Added a visual README tour with the public landing page and a populated party dashboard showing live call joining, friendly companion labels, timers, and phone controls. Corrected the README's weather description from party-scoped to member-scoped and added the ephemeral live-call privacy contract.

### Decisions

- Keep the durable agent contract focused on invariants and hard-won operational lessons rather than a transcript of individual incidents. Historical detail stays in this worklog.
- Build documentation screenshots from rendered RingRing pages, not hand-drawn mockups. The party screenshot uses a disposable local database, a private mock AMI, and neutral fictional location/phone labels; no family name, credential, token, address, or production party data enters either image.
- A documentation-only release does not justify restarting Asterisk or interrupting calls. Publishing these files requires no reference-server deployment.

### Verification

- Inspected both checked-in 1265×712 PNG files at original size and their intended README composition. The landing image contains only public content; the party image contains only the fictional `Demo Party` state and exposes no setup values or invitation link.
- `git diff --check`, `make check`, `make security`, and `make admin-test` pass locally. The release gate includes shell and lifecycle tests, SIP TLS synchronization, `go vet`, the race-enabled Go suite, and `govulncheck` with no called vulnerability.

## 2026-08-23 — Investigate asymmetric extension calls

### Findings

- Verified that the HT801V2 at the authorized local test address is extension `101`: its configured SIP identity exactly matched RingRing's active `101` device. The separate member labeled Grandstream at `103` is a different device.
- An initial device-name inference temporarily applied `103` to the HT801V2. The exact credential comparison exposed the mismatch; the adapter was immediately restored to its already-issued `101` SIP User ID, Authentication ID, and password. No server credential, party membership, database row, generated route, or public service configuration changed.
- After restoration, Asterisk reports both `101` and `103` registered and available. A bounded direct probe to the actual `103` contact reached SIP state `Ringing` in `AppDial2`, proving that Asterisk can route to and receive ringing acknowledgement from that destination; the probe was cleared immediately.

### Remaining diagnosis

- The two reported failed attempts from `101` and `102` produced no contemporaneous caller channel or authentication event at RingRing. At that snapshot `102` had no registered contact, while `101` was being restored. Capture one fresh `101`→`103` attempt with both contacts available before attributing the failure to the server, caller dial plan, or destination hardware.

## 2026-08-23 — Show live phone activity and call timers

### Shipped

- Added current `Off hook`, `Calling`, `Ringing`, and `On a call` states to each active phone and its member card on the host-only party page. Validated party conferences continue to name same-party companions and now show an elapsed timer on both the call board and participant cards.
- Reduced Asterisk's current-channel response at the private AMI boundary to the authenticated endpoint, one friendly state, and bounded elapsed seconds. Caller and connected-line values, dialed digits, application data, channel and bridge identifiers, addresses, exact timestamps, unknown endpoints, foreign parties, and non-PJSIP channels never enter the page view.
- Kept the private server snapshot at three seconds while adding a one-second browser-only clock, so timers feel live without increasing Asterisk or web polling. The script has an integrity pin and no network, storage, cookie, logging, or history primitive beyond the existing same-origin fragment request.

### Decisions

- Treat these values as ephemeral host visibility, not call records. The database and metrics receive no state, start time, duration, peer, or history, and the response remains private and `no-store`.
- Query per-phone activity only for the authenticated party page. The configured-phone menu continues to receive its narrower conference snapshot and cannot invoke this per-phone query.
- Report only what Asterisk can authoritatively observe. An analog handset lift that an ATA keeps local before sending a SIP `INVITE` cannot appear as off hook; the state becomes visible as soon as the adapter opens a SIP channel.

### Verification

- Focused AMI, web, and executable security-contract tests cover all four state reductions, state priority, duplicate channels, bounded/malformed duration handling, an empty completed channel list, fail-closed denial, same-party endpoint mapping, foreign/revoked omission, conference companions, and exact rendered timer metadata.
- In-app browser QA with three disposable members covered a two-phone live party call plus a third outgoing call, then separate off-hook, ringing, and calling states. The rendered clock advanced from `0:12` to `0:13` in one second between private refreshes. The desktop populated layout is clean; the browser's fixed viewport still leaves the existing CSS/accessibility contract responsible for the 850px and 520px breakpoints.
- `make check`, `make security`, `make admin-test`, and the Docker-shareable `make sip-smoke` pass locally. This includes formatting, shell and lifecycle checks, `go vet`, the race-enabled suite, `govulncheck` with no called vulnerability, verified SIP TLS 1.2 and UDP registration, live three-phone conference joining, mixed-transport calls, operator fallbacks, RTP echo, and authenticated extension selection.

### Production

- Published and deployed exact runtime commit `888e791448fd3d144fa2aff7652bb01f5bcef9f1` through the guarded fast-forward upgrader after confirming no active Asterisk channels. Pre/post archives `ringring-20260824T022442Z-5242c68.tar.gz` and `ringring-20260824T022630Z-888e791.tar.gz` both pass checksum, safe-extraction, SQLite, credential-decryption, isolated-readiness, and telephony-regeneration drills.
- Production `ringringctl doctor`, database and credential integrity, public `/healthz` and `/readyz`, all three service health checks, the installed `CoreShowChannels` AMI command, and the public timer-script SHA-256 pass. Asterisk reports the expected registered contact after reconciliation. The known app-before-Asterisk startup race produced one initial warning; recreating only the app after Asterisk was healthy cleared it without restarting the PBX, and the fresh app log is clean.

## 2026-08-23 — Auto-fill the WP826 phonebook

### Shipped

- Added a private Grandstream `AddressBook` XML endpoint that returns other active phones in the authenticated device's party plus the `*` services currently enabled for that extension.
- Extended the one-file WP826 setup and MAC-specific GDMS renderer to install the fixed HTTPS address-book path, reuse the device credential for HTTP Basic authentication, replace stale managed entries, and refresh every five minutes.
- Updated the setup card, WP826 runbook, architecture, deployment, phone API, and security documentation with the managed-contact behavior and manual-contact tradeoff.

### Decisions

- Reuse each phone's existing revocable SIP credential rather than introduce another persistent token. Rotation, revocation, or deletion now invalidates both SIP registration and phonebook access.
- Keep cached handset contacts deliberately smaller than the live iOS menu: no live-conference buttons, own extension, party/host/device metadata, presence, history, or timestamps enter the XML.
- Let RingRing own Contacts on a dedicated WP826 so removed members and disabled services disappear automatically; unrelated manual contacts should be stored elsewhere.

### Verification

- Focused provisioning, web, and security-contract tests cover exact WP826 aliases, URL validation, XML escaping, Basic authentication, privacy headers, same-party people/services, self/secret omission, duplicate rejection, and removal of a revoked contact on the next fetch.
- `make check`, `make security`, and `make admin-test` pass locally; `go vet`, the race-enabled suite, installer lifecycle checks, TLS synchronization tests, and `govulncheck` are clean.
- The physical WP826 accepted a phonebook-only alias XML without changing its existing SIP or Memphis-theme settings. Its web UI confirms HTTPS download, the authenticated RingRing path, a five-minute interval, replace mode, and stale/manual-entry removal. The first download populated both other active party extensions plus every currently enabled `*10`–`*15` service while omitting its own extension.

### Production

- Published and deployed runtime commit `5242c681e4ce7e09e52f71a03835a50c2f609722` through the guarded fast-forward upgrader. Pre/post archives `ringring-20260824T015233Z-f8fd69a.tar.gz` and `ringring-20260824T015419Z-5242c68.tar.gz` passed checksum, safe-extraction, SQLite, credential, isolated-readiness, and telephony restore drills.
- Public health/readiness and `ringringctl doctor` pass at the deployed commit. The unauthenticated phonebook endpoint returns a generic `401` with Basic challenge, no-store/no-referrer/noindex protections, same-origin resource policy, and `Vary: Authorization`; the authenticated handset fetched and displayed the live directory immediately.
- A user-authorized long-running test `*10` echo channel was ended before the Compose restart, avoiding an in-call deployment. The phonebook-only XML was generated from encrypted server state without printing or rotating the credential, stored locally as mode `0600`, uploaded over the explicitly authorized local admin connection, and deleted immediately after acceptance; its temporary server-side renderer was also removed.

## 2026-08-23 — Clip compact member cards to their rounded corners

### Shipped

- Restored the compact member card's inherited overflow clipping so the colored heading and white phone strip stay inside the 22px rounded outline.
- Added an executable CSS contract to prevent reintroducing `overflow: visible`.

### Decision

- Keep the gear panel in normal card flow and clip only paint outside the outer card; expanding the gear still grows the card and keeps every control visible.

### Verification

- `go test ./internal/webapp -count=1` passes.
- Browser QA with a populated member checked the collapsed and expanded gear states; corners stay rounded and weather, phone, add, and remove controls remain visible.
- `make check` passes locally, including the security contract, shell validation, `go vet`, and the race-enabled Go suite.

### Production

- Published the corner fix as `e071f2d128228609af6e9ad21329765f527c444a`; production is running descendant `f8fd69adda826c1d6790a0ba1035f9f8499b4a2d`, which also repairs the APNs provider-key runtime permission guard. Verified post-deploy archive `ringring-20260823T221101Z-f8fd69a.tar.gz` is present with its checksum.
- Public verification initially caught Docker reusing the previous app image even though the checkout contained the fix. With zero active Asterisk channels, rebuilt only the app image without cache and recreated only the app container; the public stylesheet now serves `.compact-member-card { overflow: hidden; }`.
- `ringringctl doctor`, database and credential integrity, SIP contact checks, clean-checkout verification, and public `/healthz` and `/readyz` all pass with the app, Asterisk, and Caddy healthy.

## 2026-08-23 — Wake the iPhone for calls and bring the party menu to life

### Shipped

- Added a RingRing-controlled PushKit/APNs wake path for the iOS endpoint. The configured phone registers its VoIP token with the server using its existing device credential; the server encrypts that token, authenticates the calling Asterisk endpoint inside the party, sends Apple only an opaque call UUID, and then continues the ordinary same-party SIP dial.
- Integrated PushKit with CallKit so a background or locked iPhone reports the system call immediately, refreshes SIP registration, attaches the authenticated Linphone invitation to the existing call UUID, and safely handles an answer that arrives before SIP reconnects.
- Added an authenticated, no-store configured-phone API that refreshes current same-party people and services and includes friendly `Happening now` buttons for active party conferences. The iOS app keeps the last validated menu through transient failures and never renders the hidden join number.
- Bundled the four original RingRing ringtone WAVs, added a local preview/selector in Phone settings, and supplies the selected sound to CallKit. Updated the app/server privacy, security, architecture, recovery, OpenAPI, deployment, App Store, and TestFlight documentation.
- Added `ringringctl configure-apns`, a read-only Compose key mount, and verified-backup support for the root-owned provider key. The command validates the P-256 key and identifiers, atomically installs configuration, recreates only the app, and runs the complete deployment doctor.

### Decisions

- Keep APNs content-minimized and non-authoritative. A wake contains no family label, extension, SIP identity, credential, or audio; the later authenticated SIP invitation remains the only call-signaling authority and supplies any friendly name.
- Reuse the revocable SIP device credential for HTTPS menu/push operations instead of creating a second persistent bearer. Rotation, revocation, deletion, disconnect, PushKit invalidation, and permanent APNs rejection remove the push registration.
- Treat background and locked-screen calls as supported only after physical TestFlight acceptance. Explicit force-quit delivery remains controlled by iOS and is not promised.

### Verification

- Focused APNs, configuration, provisioning, storage, telephony, FastAGI, web API, and executable security-contract tests pass. A live provider-authentication check used an all-zero invalid token: Apple accepted the team-scoped production key and `com.mcchord.ringring.voip` topic without waking a real device.
- `make check`, `make security`, and `make admin-test` pass locally; the race-enabled suite and `go vet` are clean, the installer/APNs lifecycle tests pass, and `govulncheck` finds no called vulnerability.
- The Docker-shareable `make sip-smoke` gate passes TLS/UDP registration, same-extension fan-out, a live three-phone join, party calls, operator fallbacks, RTP echo, and authenticated extension selection with the new pre-dial hook. The first macOS `/tmp` attempt hit the known Docker bind-mount limitation before startup; the documented state-directory rerun passed.
- The iPhone 17 Pro simulator passes all 16 Swift tests. Visual QA on a 1320×2868 iPhone surface covers the live-call menu and settings; all four 4.05–4.85 second WAVs are present in the built app and the call menu respects the bottom safe area.

### Production and TestFlight

- Published server release `1f72ac3c682278e235ecc4389fe117cb3dbdc83a` and deployed its schema, configured-phone API, telephony wake hook, and APNs client. The first full-stack verification stopped on the known app-before-Asterisk AMI race; recreating only the app after Asterisk was healthy and resuming the recorded target completed the guarded upgrade and its pre/post restore drills.
- The first APNs configuration restart exposed a real permission mismatch: the installer made the provider key `root:root 0400` while the app deliberately runs as UID/GID `10001`. Service was restored with a root-owned, app-group-readable, read-only mount; hotfix `f8fd69adda826c1d6790a0ba1035f9f8499b4a2d` makes that exact `root:10001`, directory `0750`, key `0440` boundary executable and restore-drillable. Production passes `ringringctl doctor`, public readiness/OpenAPI checks, authenticated-service isolation, key readability from only the app group, and clean recent logs. Backup `ringring-20260823T221101Z-f8fd69a.tar.gz` includes the active provider key and passes the full restore drill; the temporary server copy was removed.
- Recaptured and uploaded the App Store's three 1320×2868 Memphis screenshots. Visual review caught a debug preview trying the real configured-phone API and placing a revoked-credential alert over the menu; published iOS-only fix `9ba88bad3129c1336cea8df3a1ef9734c1c6cc0a` keeps preview capture fully offline. App Store Connect reports all three current images complete alongside the existing 1024×1024 build icon and updated background/live-call copy.
- Archived, production-push-signed, validated, and uploaded `0.1.0` build `5` from the latest published iOS source with the AustinLand App Store Connect key. The 23,579,409-byte IPA has SHA-256 `91331b92121a2c1e0bb08902ad7e9a805825a7de9113f61fa4b445f20762e367`, includes `aps-environment=production`, `applinks:ringring.live`, and `audio`/`voip` background modes, and passed Apple package verification without errors. Delivery/build `73c07960-1607-4127-a8ad-040f6b77b4aa` is **VALID · APP_STORE_ELIGIBLE · IN_BETA_TESTING**, is attached to **RingRing Internal** with the 721-character physical-device test matrix, and `mcwiggin@mac.com` reports **INSTALLED**. The group now contains builds 1–5.

### Remaining

- On build 5, place real calls with RingRing foregrounded, backgrounded for at least one minute, and behind the lock screen; answer from CallKit and verify ringing plus two-way audio. Then audition each ringtone, join a live party call, and repeat the Wi-Fi/cellular transition. Explicit force-quit remains an open iOS-controlled case rather than a promised behavior.

## 2026-08-23 — Add one-file WP826 onboarding

### Shipped

- Added a one-time `ringring-wp826.xml` download to every new/rotated phone setup screen. The partial Grandstream alias document configures Account 1 with the issued SIP-over-TLS credential, PCMU/RFC2833 preferences, certificate and hostname checks, five-minute registration, the Memphis wallpaper, four original ringtones, and friendly idle keys.
- Reused the existing hashed, expiring provisioning token. The WP826 attachment is no-store, no-referrer, same-origin-resource restricted, generic on failure, refused for `HEAD`, and atomically disables every other format after its first successful fetch.
- Kept the upload portable and bounded: it contains no Wi-Fi, IP/network, administrator, party/member/device label, provider, or Accounts 2–6 setting. The UI explains that the XML contains the live SIP password, must be uploaded directly, and should be deleted afterward.
- Reframed GDMS as the fleet option while documenting direct upload as the fastest household path, including the exact WP826 menu and the distinction between portable XML and device-specific `.uf` backups.

### Decisions

- Prefer a minimal partial configuration over exporting or cloning a handset's complete state. This makes first-phone setup substantially easier without copying wireless credentials, administrator access, or stale vendor settings between devices.
- Use the WP826's alias XML version 2 for the direct browser upload. The format is self-describing, was taken from the handset's own `AliasTemplate.xml`, and was proven through the physical upload UI; the existing P-value renderer remains available for GDMS By CFG.
- Share the existing one-time provisioning token across WP826, Linphone, RingRing app, and canonical phone API formats. The setup recipient chooses exactly one format, and no additional reusable credential URL is introduced.

### Verification

- A physical WP826 running firmware 1.0.1.61 accepted a minimal alias XML through **Upload Device Configuration** and immediately reported the RingRing wallpaper source and public asset URL, without changing its working extension 103 SIP account.
- Focused provisioning and web tests cover exact alias/value output, XML escaping, unsafe input rejection, attachment and privacy headers, `HEAD` non-consumption, first-GET token consumption, generic reuse failure, omission of private labels, and the setup-page instructions.
- Browser QA at 1440×1000 and 390×844 confirms the new setup card, touch-sized download, disclosure, and expandable upload steps remain readable with no horizontal overflow.
- `make check`, `make security`, and `make admin-test` pass; the race-enabled suite is clean and `govulncheck` reports no reachable vulnerability. The native SIP smoke also passes verified TLS 1.2 and UDP registration, same-extension routing, party calling/joining, `*10`, bidirectional RTP, and authenticated `*15` selection.
- The optional official-Linphone client stage is blocked on this ARM development host before XML import: its checksum-pinned upstream wheel is x86_64-only, while the legacy Docker builder emits an ARM image. The failure occurs while importing `pylinphone.so`, not in RingRing configuration, registration, or media; the native TLS gate above remains authoritative for this change.
- Applied a secret-free maximum-theme XML to the physical test WP826 without changing its SIP credential. The web UI confirmed the `RingRing 103` account and idle labels, party-yellow Memphis wallpaper, four custom ringtone slots with Ring 1 selected, Contacts/History/Menu idle layout, verified HTTPS resource path, and a 60-second concise idle display. The handset returned normally after reboot and production AMI reported active contacts.

### Production

- Published and deployed exact runtime commit `f020e0bd276e757d15857fb4c837a71251d5bd61` through the guarded fast-forward upgrader. It created and restore-drilled pre/post archives `ringring-20260823T214759Z-8b7e5cc.tar.gz` and `ringring-20260823T215050Z-f020e0b.tar.gz`.
- The app, Asterisk, and Caddy are healthy; `ringringctl doctor`, public health/readiness, database and credential integrity, private AMI access, SIP TLS synchronization, the Memphis wallpaper, and all four ringtone downloads pass. A false-token WP826 request returns the generic `410` response with no-store, no-referrer, noindex, and same-origin-resource protections, and recent app logs contain no warning or error.
- The first verification pass caught transient live-status AMI warnings while Asterisk was being recreated. Recreating only the app after Asterisk was healthy cleared the startup race; the exact pending upgrade then passed without changing production phone credentials or downloading any live provisioning document.

### Remaining

- Audition the four custom ringtone binaries on the physical handset and complete the `*10`, incoming, outgoing, and cross-party hardware matrix before marking WP826 fully verified.

## 2026-08-23 — Simplify the phone book and personalize weather

### Shipped

- Rebuilt the host phone book around compact member cards. The default view now shows extension, member presence or live-call companions, and small per-phone status chips; one 52px gear opens personal weather, ring/password/disconnect controls, add-phone, and remove-member actions. The host-confirmed setup checklist is no longer presented in the primary UI, while its storage and endpoint remain compatible for rollback.
- Added per-member live-call labels such as `On a call with Bea` from the same ephemeral, same-party AMI reduction used by the join board. The no-store phone-book fragment continues to refresh members, presence, calls, and companion labels every three seconds without adding call history.
- Moved optional lines into a dedicated host-only party settings page. Fun-number switches occupy one bright card; AI status, hard monthly spend guardrail, and encrypted runtime-key controls occupy a separate card with two focused safety bubbles. Party deletion moved into that settings page's danger zone.
- Made weather locations member-scoped. Phones on one extension share a place, other members stay independent, and `*12` maps Asterisk's authenticated endpoint to the member before it reads or fills a ZIP. The member gear lets the host set, replace, or clear a city/state/postal code. An enabled weather route asks an empty member for five ZIP digits, and the storage write independently rechecks active endpoint, party, member, host setting, and empty-value boundaries.

### Decisions

- Keep the high-frequency phone-book surface about people and calls; treat credentials, device lifecycle, personal weather, optional services, AI money/key controls, and party deletion as settings.
- Preserve the legacy party-wide weather columns and copy a resolved place once to members present at upgrade time. New code reads member columns; older code can still use its last party-wide value after rollback, and new members start empty.
- Keep the existing checklist data model for compatibility but remove the checklist from the everyday interface. Physical two-way-device acceptance remains a deployment/release gate rather than permanent phone-book clutter.
- Rate-limit member-weather writes in their own bucket so repeated location edits cannot starve unrelated party administration.

### Verification

- Focused store, voice, web, maintenance, and executable security-contract tests cover forward migration/backfill, new-member independence, authenticated endpoint lookup, host/member scoping, first-ZIP behavior, per-member cache names, service enablement recheck, AI/settings separation, call-companion labels, checklist omission, CSRF, outsider rejection, and independent throttling.
- `make check`, `make security`, and `make admin-test` pass locally. This includes formatting, shell/admin lifecycle tests, `go vet`, the complete race-enabled Go suite, and `govulncheck` with no called vulnerabilities.
- `make sip-smoke` passes from the repository's Docker-shareable temporary root: TLS and UDP phones register, same-extension fanout and the live three-phone join stay intact, unavailable `*12` gets an answered operator response, and the existing mixed-transport RTP, `*10`, and authenticated `*15` checks remain green. The default macOS `/tmp` attempt could not open its bind-mounted disposable SQLite file; rerunning from the documented override removed that Docker Desktop path artifact.
- In-app browser review with an empty and populated party covered the compact directory, collapsed and expanded member gear, separate settings page, disabled/offline states, keyboard-visible semantic controls, horizontal bounds, and a clean console. The visual pass caught and fixed a cramped phone-action row. The browser's documented viewport override remained at its 1280×720 surface, so the embedded CSS/accessibility contract—not a second rendered screenshot—currently covers the one-column 850px and full-width 520px breakpoints.

### Production

- Published and deployed exact runtime commit `c70ca7004277ba26e9a841f41a8342b2dcba5106` through the guarded fast-forward upgrader. It created and restore-drilled pre/post archives `ringring-20260823T215352Z-f020e0b.tar.gz` and `ringring-20260823T215639Z-c70ca70.tar.gz`.
- The checkout is clean with no pending marker; `ringringctl doctor`, public health/readiness, database and credential integrity, private AMI access, SIP TLS synchronization, the three new public settings-style markers, and one existing SIP contact pass. The authenticated party URL still redirects a signed-out request to login, and recent app logs contain no warning or error.
- The first verification pass caught only transient live phone/call polling warnings while Asterisk was being recreated. With zero active channels, recreating only the app after Asterisk was healthy gave it a clean startup; resuming the exact recorded target completed every verification and the post-upgrade recovery point without rotating or exposing a production credential.

### Remaining

- Repeat the host-page visual check in a true 390px production browser after deployment, then place `*12` calls from two different physical extensions and confirm that each hears only its own saved ZIP.

## 2026-08-23 — Package a Memphis WP826 handset experience

### Shipped

- Added three RingRing Memphis wallpapers at the WP826's native 240×320 resolution: cream/day, purple/twilight, and yellow/party. The application embeds public deployment copies under `/static/wp826/` so the existing HTTPS origin can serve them without another file server.
- Added four original, sample-free ringtone compositions with auditionable WAV files and Grandstream `ring1.bin` through `ring4.bin` handset binaries. The deterministic generator writes mono 8 kHz G.711 μ-law audio, the vendor header/checksum, and enforces the 64 KiB phone limit.
- Added a reusable GDMS WP826 model-template block for wallpaper, ringtone resources, friendlier Contacts/History/Menu idle softkeys, HTTPS validation, and deliberate-only firmware updates.
- Added an optional GDMS By CFG renderer for a complete TLS-first RingRing account and theme. It prompts for the SIP password, emits the exact lowercase MAC filename GDMS requires with mode `0600`, refuses overwrite, prefers PCMU and RFC2833 DTMF, and enables certificate chain/domain checks.
- Documented deployment, account assignment, real-handset verification, credential handling, asset rebuilding, and rollback alongside the bundle. Recorded the exact built-in image-generation prompts used for the wallpaper masters.

### Decisions

- Keep the shared visual template separate from per-device SIP accounts by default. GDMS SIP Account is the easiest rotation path; the secret-bearing XML is an explicit one-file alternative and must be deleted locally after upload.
- Serve non-secret theme resources from RingRing's existing public static path. This keeps wallpaper versions and the WP826's fixed-name ringtone downloads reproducible from the deployed revision.
- Use the firmware-resource path for WP826 ringtones because the current GDMS guide describes its direct custom-ringtone picker only for GXP/DP models, while the WP826 configuration template exposes custom ringtone count and slots.

### Verification

- `make check`, `make security`, and `make admin-test` pass on the trusted development machine, including formatting, shell and administrative lifecycle checks, `go vet`, the race-enabled full Go suite, and `govulncheck` with no called vulnerabilities.
- Embedded-asset tests decode all three PNGs at exactly 240×320 and validate every Grandstream ringtone's size, header word count, format version, μ-law codec marker, and checksum.
- The ringtone encoder matches the standard-library G.711 μ-law implementation for every PCM16 input. FFprobe confirms mono 8 kHz PCM previews from 4.05 to 4.85 seconds; peak levels are intentionally bounded from -4.0 dB to -6.9 dB.
- A generated MAC-specific configuration parses as XML, contains the expected 27 settings and asset paths, and is created with mode `0600`. Deployment and static copies of every PNG/BIN are byte-identical.

### Remaining

- Push the model template and one SIP account to a physical WP826 on the deployed firmware. Verify wallpaper and ringtone download after reboot, TLS registration, `*10` bidirectional audio, same-party extension calling, and cross-party denial before promoting WP826 from expected to verified in the hardware compatibility matrix.

## 2026-08-23 — Make live party calls visible and joinable

### Shipped

- Turned answered same-party extension calls into private, party/extension-derived ConfBridge rooms. The two original phones own the call; either original hangup removes every later joiner, while a later joiner can leave independently.
- Added exact `*16{extension}` join routes for authenticated party phones. The route verifies an active room, resolves the authenticated endpoint to its same-party member, and plays `Ring ring! {name} is joining the party.` before admitting the phone.
- Added a bright live-call board to the host phone book. A small integrity-pinned helper replaces a no-store, host-authenticated server-rendered fragment every three seconds, so new extensions, presence, and active calls appear without a page reload.
- Kept the new activity ephemeral and minimized: the UI receives display names, phone count, and join code only; ConfBridge recording and CDR are disabled; caller/channel/network/time data are discarded; and random-filename name announcements are deleted after one minute with an additional interrupted-process cleanup at startup. TTS failure falls back to a bundled beep without blocking the join.

### Decisions

- Use progressive polling rather than a WebSocket or a client framework. Polling pauses while the page is hidden or a phone-book form/detail control is in use, and the complete host flow still works without JavaScript.
- Key a live call by the originally dialed extension and show the corresponding `*16…` code. Conference names are generated from validated identifiers and are never selected by a caller.
- Use the authenticated device-to-member mapping for announcements rather than caller ID. Send OpenAI only the fixed sentence containing the display name—never the call audio—and use the party-scoped runtime key.

### Verification

- Focused store, telephony, voice, web, observability, and executable security-contract tests cover same-party authorization, cross-party rejection, AMI reduction, fixed Originate fields, ephemeral voice cleanup, beep fallback, host-only fragments, identifier omission, and live UI rendering.
- `make sip-smoke` passes locally with pinned Asterisk 22.10.1: a TLS caller and UDP receiver form the private room, the authenticated fragment reports two phones, a separately credentialed UDP phone joins through `*16102`, the fragment reports three phones, the join metric reports ready, an original hangup removes the joiner, and Asterisk finishes with zero channels and no CSV call record.
- `node --check web/static/party-live.js` and its exact CSP/SRI contract pass. The existing mixed-transport RTP, operator/fallback, `*10`, and authenticated `*15` scenarios remain green in the same SIP gate.
- Browser QA at 1440×1000 and 390×844 confirms that the active-call board, join code, privacy note, and member cards stay legible without horizontal overflow. Claiming extension 103 in a separate tab updated the already-open host phone book to three members within one polling cycle while preserving the active call.
- `make check`, `make security`, and `make admin-test` pass locally; the race-enabled full suite is clean and `govulncheck` reports no reachable vulnerability. The NAT gate also passes two authenticated registrations and bidirectional RTP across separate household translations. Its temporary-root override keeps Docker Desktop bind mounts and cleanup reproducible on macOS.

### Production

- Published and deployed exact runtime commit `eabc8d9422db4fc83deeecd112c6c89fe7f97830` with the guarded fast-forward upgrader. It created and restore-drilled pre/post archives `ringring-20260823T205848Z-6de65b5.tar.gz` and `ringring-20260823T210036Z-eabc8d9.tar.gz`; public health/readiness, database and credential integrity, private AMI access, SIP TLS synchronization, the loaded ConfBridge module and announcement context, the live-script SRI digest, and all container checks pass. Recent app logs contain no warning or error.
- Production reported zero registered SIP contacts during verification, so deployment did not place, join, or interrupt a real family call.

### Remaining

- Repeat one three-phone join on family hardware when phones are online before treating the feature as fully hardware-verified.

## 2026-08-23 — Finish general phone invitations in the iPhone app

### Shipped

- Added a vendor-neutral two-stage phone-invitation API. A non-consuming preview returns only the party name and one advisory safe extension; a bounded JSON claim chooses the phone name, extension, and adult classification, then creates the member/device and consumes the existing 48-hour invitation atomically.
- Returned the new device's validated SIP-over-TLS account and same-party call-menu snapshot directly from the successful claim. Native claims create no undisclosed follow-on provisioning token, while existing web claims and preconfigured-phone setup links remain compatible.
- Added a bright Memphis-style native invitation sheet to the iPhone app. Scan, paste, and universal-link entry points now accept general `/join/…` links, handle an extension race with a fresh suggestion, save the completed account/menu in the Keychain, and preserve the existing one-step phone-provisioning flow.
- Added the reciprocal `applinks:ringring.live` app entitlement and no-redirect Apple association endpoint restricted to `/join/*`. Expanded the embedded OpenAPI 3.1 contract and phone API documentation so third-party apps can implement the same flow without scraping the web UI.

### Decisions

- Reuse the existing invitation bearer and claim transaction rather than minting an app-only invitation type. Preview is intentionally minimal and non-consuming; the database uniqueness constraint remains authoritative when two invitees choose the same suggestion.
- Treat the native JSON request as bearer-authenticated rather than session-authenticated. It accepts only bounded `application/json`, attaches no cookie, permits no CORS preflight, uses generic unavailable-token errors, and retains the invitation after validation or extension-conflict failures.
- Limit universal-link ownership to the reference `ringring.live` build. Any self-hosted operator can scan or paste their HTTPS invite immediately; claiming another website's links requires that operator's own signed associated-domain entitlement and matching server association file.

### Verification

- Focused Go tests cover preview minimization/no-store headers, invalid JSON non-consumption, extension conflict, atomic successful claim, same-party call buttons, generic used/unknown errors, omission of a second provisioning token, safe metrics routing/rate limits, and the exact Apple association document.
- The iOS simulator suite passes all 15 Swift tests, including strict invitation URL derivation, URL credential/query rejection, ASCII/reserved extension rules, preview validation, and documented claim JSON. An iPhone 17 Pro simulator rendered the complete setup sheet without clipping or horizontal overflow; controls retain touch-sized targets and the existing Memphis palette.
- The isolated release commit passes `make check`, `make security`, and `make admin-test`; the race suite is clean and `govulncheck` reports no reachable vulnerability. Apple's App Store package validation also returned **VERIFY SUCCEEDED with no errors**.

### Production and TestFlight

- Published and deployed exact runtime commit `6de65b578cc6882c109b9bd74f2e31ea7dff437d` with the guarded fast-forward upgrader. It created and restore-drilled pre/post archives `ringring-20260823T203511Z-15e57e0.tar.gz` and `ringring-20260823T203658Z-6de65b5.tar.gz`; public health/readiness, the invitation/OpenAPI/Apple-association endpoints, generic unavailable response, private integrity/credential checks, one live SIP contact, and all container health checks pass. Post-upgrade app logs contain no warning or error.
- Archived, App Store-signed, validated, and uploaded `0.1.0` build `4` with the AustinLand App Store Connect key. The 23,475,852-byte IPA has SHA-256 `7cb389b1a4f72ea7f517d1813720e60155dcb56de9936d48cb1d65a4723e2747`, includes the `applinks:ringring.live` entitlement, and declares no non-exempt encryption. Delivery `357053f5-3f5f-4e02-aeb1-7e4118d83bb2` is **VALID · APP_STORE_ELIGIBLE · IN_BETA_TESTING** with English test notes in **RingRing Internal**, now containing four builds.

### Remaining

- Install build 4 on a physical iPhone, scan and tap a fresh general invitation, finish the native claim, and complete the private two-way call/background matrix. PushKit/APNs and the Linphone licensing decision remain gates before a public App Store release.

## 2026-08-23 — Give the iPhone app a complete App Store presentation

### Shipped

- Filled the existing iOS `1.0` App Store version with English name, subtitle, promotional text, description, search keywords, marketing/support/privacy URLs, `2026 Austin McChord` copyright, and Utilities / Social Networking categories. The version remains **Prepare for Submission** and was not sent to App Review.
- Generated and uploaded three native 6.9-inch iPhone screenshots at 1320×2868: one-scan setup, the named one-tap party menu, and an active private Echo test call. All use generic debug-only fixtures; no real family name, invitation, credential, extension owner, phone number, server secret, or call record entered an asset.
- Kept the existing 1024px Memphis handset icon as the canonical artwork. App Store Connect obtains that icon from the uploaded app build, and the current TestFlight build already displays it; there is no separate metadata icon upload to perform.
- Added reproducible screenshot capture and App Store Connect synchronization scripts. The sync requests the tracked shared Team key directly from AustinLand, signs API requests in memory, never prints or writes the private key, validates Apple text limits and image properties, uploads assets through Apple's reservation/checksum workflow, and verifies processing completion.
- Added dedicated iPhone privacy and support documents and version-controlled App Store metadata under `ios/fastlane/metadata/en-US`.

### Decisions

- Use the existing `1.0` App Store shell rather than creating a second version. Keep it unsubmitted while PushKit/APNs and the Linphone SDK public-distribution license remain open release gates.
- Leave age rating, App Privacy nutrition labels, pricing/availability, review contact, and release timing untouched. Those require explicit compliance and business decisions and should not be inferred from visual presentation work.
- Upload only the highest-resolution current iPhone set. Apple scales that set for smaller iPhones, while the repository can regenerate the source screens from the real SwiftUI app whenever the interface changes.

### Verification

- The iPhone 17 Pro Max simulator generated three opaque JPEG screenshots at exactly 1320×2868; visual inspection confirmed complete, unclipped Memphis layouts and neutral fixtures. The App Store metadata validator confirms the 1024×1024 opaque build icon, Apple's text limits, HTTPS URLs, and all three screenshot properties.
- The iOS simulator test suite passes all 11 Swift tests. `make check`, `make security`, and `make admin-test` pass; the race suite is clean and `govulncheck` reports no reachable vulnerability.
- App Store Connect API readback reports **UTILITIES / SOCIAL_NETWORKING**, copyright **2026 Austin McChord**, version **1.0 · PREPARE_FOR_SUBMISSION**, and **APP_IPHONE_67: 3/3 complete**. The AustinLand credential remained in memory and no `.p8`, JWT, archive, IPA, or secret entered the worktree.

### Remaining

- Complete the physical TestFlight call matrix, PushKit/APNs wake path, Linphone licensing decision, age-rating questionnaire, App Privacy labels, review contact, and availability choices before considering App Review. A public `1.0` submission will also need a release build whose marketing version matches the App Store version.

## 2026-08-23 — Let a party phone teach `*12` its ZIP

### Shipped

- Made `*12` available as a setup-only route when a voice-ready party has weather turned on but no saved location. A friendly OpenAI-generated voice asks the authenticated party phone for exactly five U.S. ZIP digits, resolves the place through Open-Meteo, saves it once for the party, reconciles Asterisk, and reads the first forecast in the same call.
- Bound the write to Asterisk's authenticated PJSIP endpoint and an active device in the same party. The phone can fill only an unknown, host-allowed location; it cannot replace another phone's result or the host's choice, and the atomic update preserves every unrelated service setting.
- Kept the host in control from the mobile-friendly party page. Checking weather with a blank place allows phone setup, entering a city/state/postal code resolves it immediately, and unchecking weather removes both setup and forecast routing.
- Added the setup route to the RingRing operator tour and the one-time phone menu, with a versioned upbeat voice prompt and the same bundled failure behavior as the existing weather line.

### Decisions

- Collect a ZIP with DTMF instead of caller speech. This works on analog keypads, records no caller audio, creates no transcript, and sends OpenAI only fixed setup text plus the fixed forecast sentence; Open-Meteo receives the ZIP and forecast coordinates.
- Default the additive `weather_setup_allowed` migration to enabled so an existing voice-ready party with no known location can call `*12` immediately. The host can explicitly disable it, and forward-only rollback must preserve the added SQLite column even if older code ignores it.
- Keep richer city/state/postal entry in the web app. Five-digit ZIP is the deliberately narrow phone interface because it is easy to validate and enter without speech capture.

### Verification

- Store tests cover setup-only routing, active endpoint authorization, input validation, first-writer behavior, no replacement, host disable, and additive migration. Voice tests cover the first prompt, invalid-digit retry, same-call forecast, atomic save/reconcile, fixed disclosure text, and the absence of recording/transcription commands; renderer and web tests cover endpoint forwarding and both host states.
- A live Open-Meteo request resolved the representative five-digit ZIP `02138` to Cambridge, Massachusetts. The host page was inspected at 1280×900 and 390×844; it has no horizontal overflow, the setup explanation remains readable, and controls retain touch-friendly sizing.
- `make check`, `make security`, `make admin-test`, and the complete local TLS/UDP/media/operator `make sip-smoke` gate pass. The race suite is clean and `govulncheck` reports no reachable vulnerability.

### Production

- Published and deployed exact runtime commit `15e57e05c5b82f69a3e01e4e2fb435726962b321` with the guarded fast-forward upgrader. It created, checksummed, and restore-drilled pre/post archives `ringring-20260823T155002Z-dd5e34d.tar.gz` and `ringring-20260823T155149Z-15e57e0.tar.gz` before reporting success at the same commit.
- The forward migration added one `weather_setup_allowed` column. The sole existing party remains intentionally unresolved with forecast disabled and phone setup allowed; no location was guessed or prefilled. Generated and loaded Asterisk state each contain exactly one endpoint-authenticated weather route.
- Public health and readiness return 200; the app and Asterisk are healthy; `ringringctl doctor` passes with one live Grandstream contact; the production checkout is clean; and the post-upgrade app log contains no warning or error.

### Remaining

- Dial `*12` on the Grandstream, enter the party ZIP when asked, and listen for the immediate forecast to complete the physical handset check. The saved place should make later `*12` calls skip setup and go straight to weather.

## 2026-08-23 — Remember the operator introduction per extension

### Shipped

- Made the RingRing operator's AI-voice introduction a once-per-extension message. The first successfully played operator greeting identifies the voice as AI-generated; later operator calls from any active phone on that same extension use the shorter greeting.
- Bound the decision to Asterisk's authenticated PJSIP endpoint and the caller's party. The web app records only a disclosure timestamp for the owning member, not a call, reason, destination, recording, transcript, or caller-ID history; unknown, revoked, and cross-party endpoints fail closed to the full introduction.
- Changed the tested Grandstream HT801 V2 guidance and device configuration to delayed `*0`. This firmware accepts manual `0` but handles bare zero specially in Off Hook Auto Dial; the adapter applied and read back `*0` with an eight-second delay.

### Decisions

- Mark disclosure only after Asterisk successfully streams the complete first greeting. Database lookup or playback failures do not silently suppress the required introduction.
- Share one disclosure bit across all active devices belonging to the same member extension so replacing or adding a phone does not repeatedly announce the same information.
- Keep weather disabled until the host supplies an intentional city/state. The production dialplan has no `*12` route because the party has no weather location; this is configuration, not an OpenAI voice failure.

### Verification

- Store tests cover first use, repeat use across two devices, revocation, party isolation, and additive migration of an older database. Voice tests cover first/repeat prompt selection, successful marking, playback failure, and private-line omission; renderer and SIP smoke assertions cover the authenticated endpoint argument.
- `make check`, `make security`, `make admin-test`, and the complete local TLS/UDP/media/operator `make sip-smoke` gate pass. `govulncheck` reports no reachable vulnerability.
- The changed setup guidance was inspected at 1280×900 and 390×844. The Grandstream card remains readable, its controls retain touch-sized targets, and the mobile document has no horizontal overflow.

### Production

- Published and deployed exact runtime commit `dd5e34d051c4925b0482f3e760995c4f74b0fd44` with the guarded fast-forward upgrader. It created, checksummed, and restore-drilled pre/post archives `ringring-20260823T151744Z-3fb8aae.tar.gz` and `ringring-20260823T151931Z-dd5e34d.tar.gz` before reporting success at the same commit.
- Initialized the sole existing test extension's disclosure timestamp because its operator introduction had already been heard and confirmed. The database reports one disclosure row and no foreign-key violations; generated and loaded Asterisk dialplans each contain all eight endpoint-authenticated operator sites.
- Public health and readiness return 200; the app and Asterisk are healthy; and `ringringctl doctor` passes after the Grandstream returned to one live SIP contact. The only deployment log warning was the known first reconcile racing Asterisk's container name during startup; generated state was present, all routes loaded, and no subsequent warning or error was observed.

### Remaining

- Verify delayed `*0` off-hook routing and the shorter repeat greeting on the physical handset.
- Collect the host's city/state, enable weather for that party, and then physically verify `*12`.

## 2026-08-23 — Publish an open phone provisioning API

### Shipped

- Added the vendor-neutral `GET /api/v1/phone-provisioning/{token}` endpoint. One successful, no-store JSON response contains a strictly validated SIP-over-TLS account and a bounded snapshot of friendly same-party call buttons; it contains no host controls, presence, call history, provider key, PSTN route, or persistent directory credential.
- Embedded and publicly served an OpenAPI 3.1.2 contract at `GET /openapi.yaml`, with fictitious examples, bounded problem responses, and explicit schemas for the account and menu. Added `docs/PHONE_API.md` as the implementation guide covering token lifecycle, client validation, secret storage, version evolution, privacy limits, and the separate background-push requirement.
- Kept released TestFlight build 3 working through the deprecated `/provision/ios/{token}` compatibility path. The canonical and compatibility URLs share the same stored token and first-fetch-wins behavior, while the one-time setup card exposes the canonical URL and API contract to other native-app builders.
- Updated the RingRing iOS source to accept either documented path and to count destination text by the same Unicode-scalar limits as the server. Existing installs still migrate safely and retain their two always-available utility buttons when an older payload has no menu.
- Made the official Linphone smoke harness honor the documented `RINGRING_SMOKE_TMP_ROOT`, matching the SIP harness so VM-backed Docker runtimes can bind-mount disposable generated state from a host-shared directory.
- Removed an Asterisk comment delimiter from the RingRing operator's fallback `NoOp`, preventing Asterisk from treating its closing parenthesis as a comment when it loads generated dialplans.

### Decisions

- Treat the API as an open contract for trusted native clients, not a browser-readable credential endpoint. The public specification permits cross-origin reads; provisioning responses intentionally omit CORS, reject `HEAD` without consuming the token, mask the token-bearing route in logs, share the existing provisioning rate limit, and return generic errors that disclose no token state.
- Preserve the setup token as a single-use provisioning bearer. Do not turn it into a persistent directory credential or use it for refresh; the party-scoped Asterisk dialplan remains authoritative after the device stores its setup-time menu snapshot.
- Permit unknown optional fields within version 1 for forward-compatible additions while requiring clients to validate all required fields and reject unsupported versions. Any change to an existing field's meaning or safety boundary requires a new integer version.

### Verification

- Focused provisioning, web, accessibility, and executable security-contract tests pass, followed by `go test ./...`. The embedded OpenAPI document parses successfully and reports version 3.1.2 with the three documented paths.
- The iPhone 17 Pro simulator passes all 11 Swift tests, including canonical and compatibility URL validation, legacy payload migration, menu validation, and Unicode-scalar limits.
- The live local setup page was inspected at 1280×720 and at the 500px phone breakpoint. The expanded API disclosure reflows to one column, has no horizontal overflow, and keeps its copy button and documentation link as 44px-or-larger targets.
- Exact integrated candidate `3fb8aae91c719ebb4c14c63c92121e9de4d41846` passes `make check`, `make security`, and `make admin-test` from a clean worktree; `govulncheck` finds no reachable vulnerability. Its runtime tree is identical outside this worklog to candidate `2d0c2cced447ac3bc57fb6ebbc49bc47264d439f`, which passed the complete local SIP/TLS/UDP/media/operator smoke and the official x86-64 Linphone provisioning, registration, extension-call, and echoed two-way-audio smoke on the reference host.

### Production

- Published and deployed exact commit `3fb8aae91c719ebb4c14c63c92121e9de4d41846` with the guarded fast-forward upgrader. It created, checksummed, and restore-drilled pre/post archives `ringring-20260823T141641Z-2226206.tar.gz` and `ringring-20260823T141828Z-3fb8aae.tar.gz` before reporting success at the same commit.
- Public health, readiness, and `/openapi.yaml` return 200 while `/metrics` remains 404. The live OpenAPI SHA-256 exactly matches the embedded source at `b05ece6e117fb93dd873becf6724495c67899a5ecfa605a412b0172587220e81`; its public CORS/cache headers are present, and non-consuming `HEAD` requests to both token routes return 405 with `Allow: GET`, same-origin resource policy, and no-store/no-referrer/noindex protections.
- The production checkout is clean; the app and Asterisk are healthy; Caddy is running; `ringringctl doctor` passes with the existing live SIP contact; and Asterisk's loaded dialplan contains all eight operator fallback sites without the truncated-comment form. One initial app reconcile warning occurred before the Asterisk container name resolved during startup; the generated files were already present, Asterisk loaded them on startup, and subsequent app logs contain no warning or error.

### Remaining

- Issue fresh settings and finish the physical TestFlight call matrix. PushKit/APNs and the Linphone licensing decision remain gates before a public App Store release.

## 2026-08-23 — Give every party a RingRing operator

### Shipped

- Added exact party-scoped `0` and `*0` help routes plus an OpenAI text-to-speech operator for help, misdialed numbers, unavailable phones, and temporarily unavailable services. The operator uses `gpt-4o-mini-tts` with the `marin` voice and explicit bright, playful RingRing performance direction; the adult-only Realtime line receives the same brand direction.
- Kept every operator script fixed in code and clearly disclosed as AI-generated. OpenAI receives no caller audio, dialed digits, party/member names, extensions, or SIP credentials. Versioned 8 kHz WAV prompts cache per party and reason, invalidate after service changes, and never represent call activity.
- Preserved a bundled Asterisk explanation behind every AI-assisted route so a missing/paused party key, provider failure, or private app failure still answers instead of returning fast busy. Added a bounded `operator` metric label without any party, caller, reason, or error value.
- Added a visible operator card to the host and one-time setup pages. The Grandstream HT801 V2 guide now shows **Off Hook Auto Dial = 0** with an **8 second** delay, and the generic ATA guide maps equivalent hotline/warmline settings to the same help route.

### Decisions

- Use one-way text-to-speech for the family operator, not open-ended Realtime. This keeps the universally reachable line deterministic and suitable for every extension while the conversational `*14` preview remains restricted to adult extensions.
- Implement idle-handset help at the ATA. SIP sends no request merely because an analog handset is off hook; the adapter must originate `0` after its delay. Any phone can still dial `0` or `*0` manually.
- Mention only code-controlled, currently enabled family-safe lines in the tour. The operator never advertises the adult-only `*14` line and repeats that RingRing cannot place regular or emergency calls.

### Verification

- Focused operator, speech, observability, renderer, web, and security-contract tests pass. `make check`, `make security`, and `make admin-test` pass locally; `govulncheck` reports no reachable vulnerability.
- The local SIP smoke suite passes with TLS and UDP registration, party-scoped operator generation, an authenticated answered call to `0`, bundled no-provider fallback, friendly invalid/disabled destinations, same-extension calling, `*10`, RTP, and `*15`.
- The changed host and setup pages were inspected at 1280×900 and 390×844. The operator and HT801 guidance remain readable, and the mobile document has no horizontal overflow.

### Production

- Published exact runtime commit `2226206194be5912fd32f18ee864711d954e0fa3` to the public repository after validating that isolated commit through `make check`, `make security`, `make admin-test`, and `make sip-smoke`. The guarded fast-forward production upgrade created, checksummed, and restore-drilled its pre/post archives before completing at that commit.
- Public health and readiness return 200; `ringringctl doctor` passes with one live SIP contact; and the generated and loaded Asterisk dialplans contain the party-scoped operator routes. All four operator prompts were generated through the private listener with the party key, cached without caller data, and left behind their bundled no-provider fallbacks; the post-generation app log is clean.
- Configured the authorized Grandstream HT801 V2 test adapter to auto-dial `0` after eight seconds off hook. The device applied the change and returned exact readback values `P71=0` and `P4045=8`; production still reports its live SIP contact after the apply.

### Remaining

- Lift the physical handset, wait about eight seconds, and listen to complete the subjective voice/energy check. Also dial a bad extension once to confirm the same operator style feels clear in the misdial path.

## 2026-08-23 — Make the AI line an adult-extension choice

### Shipped

- Replaced the host-wide safety form and repeated keypad confirmation with one **Adult extension (18+)** checkbox on the invitation claim form. The value is stored as a minimal member boolean; existing extensions migrate to false, and there is no birthday collection or phone-book toggle.
- Restricted `*14` at call time using Asterisk's authenticated PJSIP endpoint. Only an active device in the same party whose extension was marked adult can receive a bridge ticket; unknown, revoked, non-adult, and cross-party endpoints fail closed with the same friendly spoken response.
- Kept the deployment gate default-closed as `AI_ADULT_ONLY_ENABLED`, required at least one adult extension before a host can enable the party line, and limited first-call advertising of `*14` to the adult extension that was just created.
- Simplified the caller experience to one short adults-only AI/provider disclosure followed by the live bridge. RingRing still exposes no tools, disables input transcription and tracing, discards transcript events, bounds call duration/concurrency/output, and stores no call audio or transcript.

### Decisions

- Treat the checkbox as an administrative classification, not age verification. Child and shared phones stay unmarked, and minor-facing AI remains out of scope until the separate under-18 safety/privacy review and required provider-retention evidence are complete.
- Keep provider retention visible in the setup copy and audible disclosure. The standalone `ringringctl openai-retention` audit remains available, but it is not a runtime precondition for this explicitly adults-only preview.
- Authorize from the registered endpoint rather than caller ID so a handset cannot gain access by presenting another extension number.

### Verification

- Focused store, web, telephony, voice, configuration, and security-contract tests pass, followed by `go test ./...`, `make check`, `make security`, and `make admin-test`; `govulncheck` reports no reachable vulnerability.
- The local SIP smoke suite passes for TLS and UDP registrations, same-extension and mixed-transport calls, friendly invalid/disabled routes, `*10`, RTP, and `*15`. The Colima run used the documented host-shared temporary-root override and left the source tree unchanged.
- The invitation page was inspected at 1280×900 and 390×844. The single yellow adult-extension choice has a 44px-or-larger target, clear focus styling, and no mobile horizontal overflow.

### Production

- Published commit `cb39dd716a7c0dbf35adbb63b313dcadc17bc7f1` to the public repository after validating that exact isolated commit. The guarded fast-forward upgrade created and restore-drilled checksummed pre/post archives before completing at the same commit.
- Added the adults-only operator gate to the root-readable environment file, retained a mode-0600 pre-change copy, classified the one existing test extension as adult, and enabled its party's AI service. The database now reports one adult member, one enabled AI party, and one ready party-scoped provider key.
- Public health and readiness return 200; the app, Asterisk, and Caddy are running; `ringringctl doctor` passes with one live SIP contact; the generated and live Asterisk dialplans each contain the endpoint-authenticated `*14` route; and the post-deploy app log has no warnings or errors.

### Remaining

- Dial `*14` from the adult Grandstream handset to complete the physical Realtime audio check. Child and shared extensions remain unable to authorize, and the separate under-18 review remains open.
- `*12` still needs a host-chosen weather location before it can be enabled; that unrelated configuration was left unchanged.

## 2026-08-23 — Put the party behind one-tap iPhone call buttons

### Shipped

- Replaced the iPhone app's keypad-first home screen with a bright Memphis call menu. **People** contains large buttons for other active phones in the party and **More to call** contains friendly buttons for currently routable services; extensions and star codes remain hidden behind each action. The call screen and CallKit resolve known destinations back to their labels, while **Dial manually** preserves the keypad as a secondary compatibility path.
- Extended the version-1 iOS provisioning document with a bounded call-menu snapshot derived from the claimed device's party. It excludes the phone's own extension, members without an active phone, paused/disabled services, and every unrelated host, party, device, email, provider, readiness, and timestamp field. Go and Swift independently validate kind, label/detail size and control characters, target shape, uniqueness, and maximum menu size.
- Migrated secure local storage from a bare SIP account to the account plus menu snapshot in the device-only Keychain. Build-1 installs decode through an explicit legacy path and keep the always-available **Echo test** and **Pick another extension** buttons instead of losing their working SIP setup.
- Archived, App Store-signed, validated, and uploaded version `0.1.0` build `3` with the AustinLand App Store Connect key, superseding build 2 after aligning the native CallKit surface with the extension-free friendly labels. App Store Connect reports processing state **VALID** and internal state **IN_BETA_TESTING**; the automatic **RingRing Internal** group now shows one invited tester and three builds. Build-specific “What to Test” notes describe the fresh-setup, upgrade, call, privacy, and keypad checks.

### Decisions

- Treat the call menu as minimum necessary party data: names and hidden dial targets are available only after the same one-time provisioning bearer is successfully consumed, travel over the existing no-store HTTPS response, and remain in the device-only Keychain. They do not enter the QR, setup page, `UserDefaults`, logs, metrics, or CallKit labels for unknown numbers.
- Keep provisioning version `1` because adding an ignored JSON field is backward-compatible with build 1. Build 3 also accepts the older response without that field and supplies only the two always-routable local utilities.
- Make this first menu a setup-time snapshot. Automatically refreshing family names later requires a new authenticated, revocable device-directory channel; the one-use setup token must not be repurposed as a persistent bearer.
- Keep the matching server release separate from TestFlight delivery. A fresh setup receives the complete menu only after this server-side provisioning change is deliberately deployed.

### Verification

- Focused provisioning, storage, and web tests pass, including active-member filtering, own-extension omission, service readiness, unsafe/duplicate destination rejection, and one-time-token behavior. `make check`, `make security`, and `make admin-test` pass; `govulncheck` finds no reachable vulnerability.
- The iPhone 17 Pro simulator passes all 10 Swift tests, including legacy payload migration and menu validation. A debug-only generic fixture rendered the complete call menu at 1206×2622; every primary call row exceeds the 44-point touch target, labels wrap cleanly, the numeric targets stay absent, and the manual dialer remains visible below the menu. The fixture and screenshot were removed after inspection.
- Apple's package verification returned **VERIFY SUCCEEDED with no errors**. Upload delivery `6eef2695-69ef-4b0f-a921-6754d0892fbf` transferred the 43,662,618-byte IPA; its SHA-256 is `b39a24705df3db6b340ce05b0f5d95b0bd914b1e4ee179f261e6f3f11406dd57`. The signed-in App Store Connect group page confirms **1 Tester · 3 Builds** and the tester remains **Invited**. Temporary private-key copies were permanently removed; the nonsecret archive, IPA, and export logs were moved to Trash after verification.

### Remaining

- Deliberately deploy the matching server change, rotate or issue fresh phone settings, then verify that real family names and enabled services arrive on a physical iPhone and that each button reaches only its intended same-party destination.
- Accept the internal tester invitation and complete the physical Wi-Fi/cellular, two-way audio, CallKit, mute/speaker/DTMF, and background matrix. PushKit/APNs and the Linphone licensing decision remain release gates.

## 2026-08-22 — Ship the first RingRing iPhone endpoint to TestFlight

### Shipped

- Added a dedicated native SwiftUI app under `ios/` for iOS 17 and later. Its bright Memphis-style onboarding scans a RingRing QR, keeps the SIP secret in the device-only Keychain, registers over SIP TLS through Linphone SDK 5.5.15, and presents outgoing and incoming calls through CallKit. The dialer supports party extensions and service codes, plus mute, speaker routing, in-call DTMF, duration, hangup, and an explicit no-PSTN/privacy settings screen.
- Added a versioned, one-time `/provision/ios/{token}` JSON boundary alongside the existing Linphone route. The successful setup page now leads with a yellow RingRing-app card, custom `ringring://join` deep link, scannable QR, and copyable fallback URL. Both client and server strictly validate token shape, TLS/5061 account fields, identity, password, and extension; the shared provisioning token retains its 30-minute, first-fetch-wins behavior.
- Registered `com.mcchord.ringring`, created the App Store Connect record **RingRing Family Phone**, archived and App Store-signed version `0.1.0` build `1`, and uploaded it with an AustinLand-provisioned App Store Connect API key. Created the automatic **RingRing Internal** group, added the account holder as its single internal tester, attached the valid build, and published focused “What to Test” notes.
- Added the generated 1024px Memphis handset icon, reproducible XcodeGen project, pinned Swift package resolution, simulator tests, TestFlight notes, and explicit Linphone AGPL/commercial-license notice. No API key, provisioning profile, archive, IPA, credential, or token entered the repository.

### Decisions

- Keep Linphone's config path empty and rehydrate the account from Keychain so its persistent preferences never become a second SIP-secret store. Reduce SDK logging to fatal-only and never show the SIP username or password after setup.
- Require HTTPS provisioning in Release builds; Debug accepts HTTP only on loopback for local tests. Refuse redirects, credential-bearing URLs, query/fragment additions, oversized bodies, non-JSON responses, unexpected versions, UDP provisioning, and malformed accounts.
- Treat this as an honest first TestFlight endpoint, not a finished push architecture. Incoming calls work while the core is active and during ordinary short background periods; reliable ringing after iOS suspends or terminates the app requires a later RingRing-controlled PushKit/APNs and Asterisk holding path.
- Use Linphone under AGPLv3 for this internal build while preserving the licensing decision as a public-release gate. Public App Store distribution requires a reviewed AGPL source-distribution path or a commercial Belledonne license.

### Verification

- Focused Go provisioning, web-flow, accessibility, and executable security-contract tests pass. `make check` passes, including formatting, operator fixtures, vet, and the complete race-enabled suite.
- `xcodebuild` resolves the exact `5.5.15` package, builds the iPhone simulator target, and passes the Swift Testing suite for deep-link parsing, provisioning validation, insecure/malformed rejection, and callable dial strings. The welcome UI was rendered on an iPhone 17 Pro simulator; the web setup card was inspected at 1280×720 and 390×844 with zero mobile horizontal overflow.
- Xcode archived an arm64 iPhone build, App Store export validation returned **VERIFY SUCCEEDED with no errors**, and upload returned delivery `fe7988d0-7bd4-4da7-ad58-b5387f6b0a92`. App Store Connect reports processing state **VALID**, build audience **APP_STORE_ELIGIBLE**, minimum iOS `17.0`, no non-exempt encryption, and a 90-day TestFlight expiry. The final 42 MB IPA SHA-256 is `a703257e835c172a3caceb3c562fbb6be8b4ffd7823070657b10e912e6ae925d`.

### Remaining

- Accept the internal TestFlight invitation on a physical iPhone, make a fresh setup card from a reachable RingRing deployment containing this server change, then prove TLS registration, `*10` two-way audio, extension calls in both directions, mute/speaker/DTMF, CallKit, Wi-Fi/cellular switching, and ordinary background ringing.
- Deploying the server-side iOS QR route is intentionally separate from this repository/TestFlight delivery; do not change production merely to complete an app upload. Add and validate PushKit/APNs before promising suspended or force-quit incoming calls.
- Resolve the Linphone AGPL-versus-commercial-license decision before any public App Store release.

## 2026-08-22 — Give failed calls a friendly voice

### Shipped

- Added party-scoped spoken fallbacks for invalid numeric extensions and disabled or unknown star codes. Asterisk now answers and says that the call cannot be completed before asking the caller to try again, rather than returning a bare fast-busy tone.
- Made explicit member routes inspect `DIALSTATUS`. Busy, offline, or unanswered party phones now receive a generic number-not-answering and retry-later response; a successfully answered conversation bypasses the fallback.
- Added generic retry-later responses when the private FastAGI connection is unavailable, the AI authorization gate refuses a call, or a radio player exits unexpectedly. Existing FastAGI handlers retain their own safe in-call failure prompts.
- Extended the local SIP smoke harness to accept a host-shared temporary root for Colima and added authenticated end-to-end calls that require both an invalid number and a disabled star line to answer, play out, send BYE, and leave no channel behind.

### Findings and decisions

- The physical HT801 V2's configured dial plan already accepts variable-length numeric destinations and star codes, so no adapter dial-plan change was needed. Its SIP registration was enabled over UDP, but the default 60-minute refresh left the adapter showing Registered after a server restart while Asterisk had no live contact. Setting only Register Expiration to 5 minutes and applying the page immediately restored the live contact; no server, identity, password, codec, or DTMF value changed. The setup guide now carries that recovery setting.
- Production currently generates one member extension for one configured phone. Calling that same extension from its only phone is not a two-phone call and can return busy. Only `*10`, `*11`, and `*15` are currently routed; `*12`, `*13`, and `*14` are disabled and previously had no star-code fallback.
- Keep every failure pattern inside the caller's authenticated party context and use only bundled Asterisk sounds. The response discloses no endpoint, member, party, presence, credential, or failure detail and cannot reach a public or cross-party destination.

### Verification

- Focused renderer tests pass and cover per-party numeric/star fallbacks, unreachable-member handling, FastAGI failure handling, prompt selection, and tenant-local duplication.
- The complete local `make sip-smoke` gate passes in Colima. It loads the generated patterns into pinned Asterisk 22.10.1, confirms every prompt file, places authenticated TLS calls to invalid `222` and disabled `*12`, observes answered calls of prompt-length duration with server BYE and zero remaining channels, and preserves the existing mixed-transport member call, bidirectional PCMU, `*10`, and authenticated `*15` checks.
- `make check`, `make security`, and `make admin-test` all pass locally after the change, including formatting, shell/operator fixtures, `go vet`, the complete race-enabled suite, reachable-vulnerability scanning, and administrator tests. The physical adapter's five-minute registration refresh is saved and production reports one live contact again.

### Production

- Published exact runtime candidate `eef32621ce456c97ff6a957eddca5c3fa98dd467` to the public repository without a hosted GitHub workflow. The guarded fast-forward upgrade created and restore-drilled checksummed pre/post archives `ringring-20260823T025201Z-5522b93.tar.gz` and `ringring-20260823T025351Z-eef3262.tar.gz` before reporting completion at that exact commit.
- Public health and readiness return 200, all three Compose services are running without an unhealthy or exited state, the checkout is clean, and the AI child-safety gate remains closed. The live Asterisk context contains one numeric fallback, one star fallback, and one member-unavailable handler; direct lookups for invalid `222` and disabled `*12` resolve to the friendly prompt sequence.
- The app logged one initial reconciliation warning while Asterisk was still starting. The generated files were already written, Asterisk loaded the new routes on its own startup, and the direct live route checks pass. The Asterisk restart cleared its volatile contact; applying the unchanged HT801 SIP page prompted an immediate refresh, and production again reports one registered contact.

### Remaining

- Retry invalid, disabled, `*11`, `*15`, and two-device member calls on physical hardware. The current one-phone/one-extension party still cannot prove an ordinary family call by dialing its only phone from itself.

## 2026-08-22 — Keep the release gate local

### Shipped

- Removed the GitHub-hosted CI and Security workflows. Public pushes and pull requests no longer execute repository code on GitHub runners.
- Made the trusted local release gate explicit in `AGENTS.md`, the README, and the threat model: run `make check`, `make security`, and `make admin-test` before publishing, then record the exact result here.

### Decisions

- Prefer the project's already-complete local gate over duplicate hosted runs. Pull requests remain a review surface, while build, test, race, vulnerability, operator-fixture, and security-contract execution stays on a contributor-controlled development machine.
- Accept contributor discipline as an operational dependency and preserve human boundary review; moving runners does not make automated scanners complete.

### Verification

- The complete local release gate passes after deleting the hosted workflows and updating the policy documentation.
- The public repository contains no remaining GitHub workflow file or reference claiming that hosted CI is required.

### Remaining

- Keep local verification evidence current in each release worklog entry and add a self-hosted option only if local release cadence later needs automation.

## 2026-08-22 — Add a real Grandstream HT801 V2 setup path

### Shipped

- Confirmed the active source already issues a 6-digit SIP username and 12-digit numeric password for every new, added, or deliberately rotated phone. Existing longer credentials remain unchanged so an upgrade cannot silently disconnect a working adapter; the one-time setup card continues to show explicit length/no-space hints and copies only exact raw digits.
- Added a prominent Grandstream HT801 V2 shortcut beside Manual settings and a credential-aware, five-minute guide on the same private page. It covers physical ports, the `***` / `02` address prompt, exact current menu paths and field labels, TLS/UDP ports, username/auth reuse, NAT keep-alive, PCMU, RFC2833 DTMF, certificate-chain validation, registration status, saved-password behavior, and `*10` verification.
- Kept the model-specific guide from duplicating the raw username or password. It points back to the single credential card, leaves the outbound proxy blank, contains no factory-reset or router-port-forwarding instruction, and links to Grandstream's official installation and administration guides.

### Decisions

- Put the exact guide on the one-time credential page because that is where a person can safely see and copy the fields the adapter needs. A prominent jump link avoids making them hunt through the longer softphone and universal-device guidance.
- Prefer TLS on `5061`, Grandstream's `sips` URI mode, certificate-chain authentication, NAT keep-alive, PCMU/G.711 μ-law, and RTP/RFC2833 DTMF. Keep UDP on `5060` as an explicitly secondary compatibility path; one ordinary SIP 401 challenge is not a reason to switch transports.
- Use the HT801 V2's real labels—`SIP Authenticate ID` and `SIP Authentication Password`—rather than relying only on generic provider terminology.

### Verification

- Focused web and executable security-contract tests pass. They verify rendered 6/12-digit credentials, the complete HT801 V2 field mapping, safe routing/defaults, accessible guide relationships, and minimum touch-target contracts.
- Disposable in-app browser review at 1280×900 and 390×844 verifies the direct guide jump, two-column desktop and stacked mobile layouts, no page or guide overflow, 44px external links, a 52px fallback disclosure, exact numeric credential shapes, and a clean console.
- A physical Grandstream HT801 V2 confirmed the documented current menu paths and labels. Its status page reported Registered before any settings were changed, establishing that the previously observed 401 could complete the normal challenge-response flow.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, operator fixtures, vet, the complete race-enabled suite, reachable-vulnerability scanning, exact security contracts, and administrator tests.
- GitHub CI and Security passed exact guide candidate `5522b93f6ba0893a887b9c96fc108fd477ae9f2d` before hosted workflows were retired. The guarded production upgrade then created and restore-drilled checksummed pre/post archives `ringring-20260823T021447Z-23d669b.tar.gz` and `ringring-20260823T021638Z-5522b93.tar.gz`; production finished clean at that exact candidate, and the physical HT801 V2 still reported Registered afterward with its existing credential unchanged.

### Remaining

- Deliberately issue fresh 6/12-digit settings through the authenticated host console, then complete TLS registration, `*10`, incoming ring, and two-phone calling on the physical adapter.
- Expand the same credential-aware pattern after physical validation of other common FXS adapters. Complete the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Turn the phone book into a friendly control board

### Shipped

- Replaced the sparse flat member row with a colorful extension-first card. Each member now has a large, labeled extension tile, a readable member-wide status pill, a saved-phone count, and a contained footer for adding a phone or removing the member.
- Gave every saved phone its own compact card with a phone icon, explicit text-and-dot status, a single prominent incoming-ring action, and short explanatory copy. Multiple phones on the same extension stack cleanly instead of becoming one long unstructured column.
- Reframed the three host-confirmed hardware checks as a visible setup checklist with a 0–3 progress track. Checklist and phone-settings panels sit side by side on desktop, stack on mobile, and expand to the full card width without displacing unrelated controls.
- Kept destructive and credential actions secondary inside native disclosure controls. Repeated ring, checklist, settings, add-phone, and remove-member controls now have device- or member-specific accessible names while their visible labels remain short.

### Decisions

- Make the extension the strongest visual landmark because it is the thing family members actually dial. Member name, aggregate reachability, phone identity, and device reachability follow in that order.
- Use text labels plus color for every state; never ask a host to interpret a lone dot. Keep a real status pill at both the member and phone levels because one extension may ring several independently connected devices.
- Preserve server-rendered HTML and native `details` interactions. The redesign adds no JavaScript, schema, credential, routing, presence, or telephony behavior and remains fully usable without a client-side bundle.

### Verification

- Focused web-flow tests cover the new directory/member/device hierarchy, visible member and device status treatments, one- and three-step checklist progress, incoming-ring action, same-extension add-phone flow, rotation reset, disconnection state, and every existing authorization boundary.
- Accessibility contracts pin device-specific accessible names, native disclosure touch targets, mobile full-width actions, visible text status, and WCAG AA contrast for every status-pill palette.
- Disposable in-app browser review at 1280×900 and 390×844 covered empty, one-phone, two-phone, collapsed, and expanded checklist/settings states. Desktop tool cards align side by side, mobile tools stack, all visible controls are at least 44px tall, the page and directory have no horizontal overflow, and the console is clean. The viewport, tab, process, and temporary database were cleaned up afterward.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, the reachable-vulnerability scan, exact security contracts, and administrator tests.

### Remaining

- Publish and deploy through the guarded upgrade after the actively retrying physical phone is briefly disabled; deployment must not rotate any existing family credential.
- Complete the physical ATA, desk-phone, and mobile softphone matrix. Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Shorter keypad credentials and clearer 401 setup

### Shipped

- Shortened credentials for newly claimed, host-added, or deliberately rotated phones to a 6-digit SIP username and 12-digit numeric password. Existing phone identities and encrypted secrets remain byte-for-byte unchanged until their host explicitly requests fresh settings.
- Kept the short username globally unique with SQLite's existing `devices.sip_username` constraint, a specific retryable store error on claim/add/rotation, and 16 bounded CSPRNG regeneration attempts. A collision rolls back the complete transaction, including invitation and provisioning state, before retrying.
- Put explicit `6 digits · no spaces` and `12 digits · no spaces` hints beside the one-time values. A prominent help box distinguishes the normal first SIP `401` challenge from a registration that repeatedly fails and tells people to use the generated auth ID—not the extension—for both SIP User ID and Authentication ID.
- Kept the visual grouping out of every credential boundary. Individual copy, the private six-field setup note, encrypted storage, Asterisk rendering, and Linphone provisioning use the exact unspaced digits; the note builder now removes the visual length hint from its field label.

### Decisions

- Optimize for real ATA and desk-phone keypads. Six and twelve digits are short enough to enter without changing input mode, while the password remains a machine-generated secret with about 39.7 bits of entropy rather than a user-chosen PIN.
- Record the security tradeoff directly: a captured digest has a smaller offline search space than the former 24-digit password. Prefer TLS; retain progressive UDP/TLS Fail2Ban limits; do not weaken the per-device isolation, rotation, or revocation boundaries.
- Preserve all existing credentials on upgrade. The new format applies only when RingRing actually issues fresh settings, so publishing or deploying this release cannot disconnect the phone currently being configured.

### Verification

- Focused generator, transactional store, web-flow, accessibility, and executable security-contract tests cover exact lengths and numeric format, nonzero leading digits, rollback/retry for invitation claim, host-added phone and rotation collisions, non-collision error propagation, encrypted association, display grouping, raw copy attributes, and the updated guidance.
- Disposable in-app browser checks at 1280×900 and 390×844 proved the 6/12-digit raw values match their grouped display, both per-field copies and the complete setup note contain exact unspaced values, visual hints do not pollute copied field names, copy controls remain at least 44px tall, the help box is visible, and neither the page nor setup card overflows horizontally. No console error occurred; the viewport, clipboard, tab, process, and database were cleaned up.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, the reachable-vulnerability scan, and exact CSP/SRI agreement for the changed copy helper. GitHub CI run `32599516816` and Security run `32599516785` passed exact candidate `4dbd920317938c172a36b4dc5bfde222d36460eb`.
- From an isolated server checkout of that exact candidate, the complete SIP gate authenticated a host-added generated 6/12-digit phone alongside the fixed TLS and UDP peers, then passed same-extension routing, the host-triggered ring, mixed-transport calling, `*10`, bidirectional PCMU media, and authenticated `*15` route replacement. The first candidate run stopped at the harness's old 15/24-digit assertion before generated-phone registration; the source-pinned assertion was corrected, the entire gate passed, and every disposable container, network, checkout, and generated state directory was removed.

### Remaining

- Deploy through the guarded production upgrade after the current physical-phone setup attempts have settled; do not interrupt or silently rotate the real phone while it is retrying authentication.
- Complete the physical ATA, desk-phone, and mobile softphone matrix. Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Machine-checked Zero Data Retention gate

### Shipped

- Added bounded, read-only OpenAI organization- and project-retention verifiers to the existing administrator client. They accept only an explicitly safe ZDR mode—or a project inheriting the already-verified organization—and reject a missing key, provider denial, modified monitoring, a project `none` override, an unknown object/value, malformed JSON, timeout, or transport error.
- Made an explicitly open `AI_CHILD_SAFETY_APPROVED` startup depend on current organization evidence plus every stored party project before RingRing opens any listener. The default/explicitly closed path makes no provider request and continues to clear stale conversation settings and routes as before.
- Rechecked both organization and new-project retention before saving a newly provisioned party key while the gate is open. Either failure archives the unverified project and keeps its identifiers and credential out of local party state.
- Added `ringringctl openai-retention` for a secret-safe preflight and made `ringringctl doctor` recheck retention whenever the configured gate is open. The successful report contains only `status`, the non-secret organization retention type, and a project count; neither command mutates provider state.
- Kept restore verification honest and isolated by explicitly forcing the child-safety gate closed in the network-disabled restored app. State and credential verification still use the saved root-only environment, while no recovery drill can claim or require live ZDR.

### Decisions

- Treat the operator's external child-safety approval and current provider ZDR as two independent prerequisites. A manually edited Boolean is not sufficient evidence for the provider-controlled condition.
- Verify before opening listeners. A requested-open deployment with stale, ambiguous, or unavailable provider evidence remains unhealthy instead of serving the host UI with a misleadingly open conversation gate.
- Keep the organization administrator key out of model requests. Its new use is one administrative compliance read at startup or explicit operator verification; Realtime calls still receive only the encrypted party-scoped runtime key.
- Do not update provider retention automatically. Eligibility and legal/safety approval are external operator decisions, and a read-only failure must not turn into an attempted policy mutation.

### Verification

- Focused admin-client tests cover accepted organization/project ZDR types, inherited project state, rejected modified-monitoring and `none` modes, missing/unknown/wrong-object/malformed responses, safe provider denial, exact method/path/authentication, and absence of the administrator key from errors. Main-package tests prove the closed gate never calls either verifier, every stored project is checked, and provider or database denial propagates without leaking a project identifier. Web tests prove open-gate provisioning rechecks both levels, archives either failed project, and stores no unverified key, while the closed gate preserves current provisioning without provider retention calls.
- Operator fixtures cover the standalone retention report and conditional doctor recheck. Executable security contracts pin the provider path/types, startup ordering hook, deployment check, and offline restore override.
- `make check` and `make security` pass, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan.
- A direct read-only call from the reference host to the documented organization endpoint returned `403 not_eligible`. No provider or family resource was changed; this is positive evidence that `*14` must remain closed, not approval.

### Remaining

- Obtain OpenAI ZDR eligibility and an external child-safety review before setting the operator gate to true.
- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks.

## 2026-08-22 — Keypad-friendly phone credentials

### Shipped

- Replaced the 28-character mixed SIP username and 32-character mixed password for newly claimed, added, or rotated phones with a 15-digit username and 24-digit password. Every value is uniformly selected with the operating system CSPRNG and never begins with zero.
- Grouped the username in threes and password in fours on the one-time setup card. Clear guidance says the spaces are visual only; the integrity-pinned local copy helper and Linphone provisioning use the exact unspaced value.
- Extended the disposable SIP gate to create a phone through the real host route, extract only its test-only numeric pair, load it into Asterisk, and complete an authenticated registration. Existing fixed mixed-format smoke phones continue to prove backward compatibility.

### Decisions

- Prefer keypad-native decimal entry to a shorter alphabet that still forces a desk phone through multi-tap input modes. The username space is about 49.7 bits, while the password retains about 79.6 bits against offline guessing of a captured SIP digest; this is a generated secret, not a human PIN.
- Keep existing device rows byte-for-byte unchanged. Only explicit issuance or rotation adopts the new format, avoiding a surprise outage and making rollback compatible.
- Keep grouping out of authentication state. SQLite, encrypted plaintext, generated PJSIP, clipboard text, and provisioning XML contain digits only, with no separators.

### Verification

- Focused generator and complete web tests cover fixed length, nonzero leading digits, CSPRNG failure, identity/password uniqueness sampling, numeric format rejection, encryption association, display grouping, raw copy attributes, one-use provisioning, new-phone issuance, and rotation.
- A disposable browser claim at 1280×900 and 390×844 proved the grouped values match the raw 15/24-digit pair, both individual and copy-all actions preserve the exact unspaced credentials, every copy control is at least 44px tall, and the setup card has no horizontal overflow. The temporary viewport, clipboard, tab, process, and database were cleaned up afterward.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. GitHub CI run `32597377790` and Security run `32597377830` passed exact candidate `23d669bf7ae6b5e471d491aadb47968dd8ca7849`.
- From an isolated server worktree of that exact candidate, the complete SIP/TLS/multi-phone gate authenticated a newly generated numeric phone while retaining the fixed legacy mixed-format phones, and the official Linphone one-fetch provisioning, TLS registration, extension-call, and returned-audio suite passed. Two consecutive two-household NAT registration/call/media runs also passed, and every candidate smoke container, network, and worktree was removed.

### Production

- The guarded fast-forward created and restore-drilled checksummed pre/post archives `ringring-20260822T204422Z-b569daa.tar.gz` and `ringring-20260822T204605Z-23d669b.tar.gz`. Production is clean at exact runtime candidate `23d669bf7ae6b5e471d491aadb47968dd8ca7849`; doctor, public splash, signup, and readiness pass, while public metrics remain `404` and the AI child-safety gate remains closed.
- The pre/post archives have the same application table set and byte-identical rows across all 11 tables. In particular, the already-present phone's SIP identity and encrypted secret are unchanged before, after, and in the live database; nothing was revealed or rotated.
- The sealed live aggregate is one user, one party, one member, one device, two invitations, one session, eight recovery codes, one provisioning token, zero readiness records, one verified party key, one verified device secret, and zero active Asterisk contacts. Settled app, Asterisk non-security, and Caddy warning/error checks are zero.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including manual entry of the new numeric format on representative keypads.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Accept secure invitation claims from opaque browser previews

### Shipped

- Fixed member claims from embedded/preview browsers that serialize a production form submission with an opaque `Origin: null`. The live failure had a present path-scoped strict-same-site cookie and hidden token but stopped at the origin check before validating any member fields.
- Kept the exception limited to invitation claims and conditioned it on an exact constant-time double-submit-token match. Arbitrary cross-site origins, missing or mismatched tokens, and every host/admin request still fail closed.
- Added value-free diagnostic booleans for token matching and opaque-origin classification; no origin value, invitation token, member field, cookie, or credential enters logs.

### Decisions

- Treat the path-scoped HttpOnly/Secure/SameSite=Strict cookie plus its random hidden value as the authorization for this narrow compatibility case. An opaque attacker page cannot read the invitation document's form value, while anyone who already possesses the bearer invitation can claim it directly.
- Do not weaken the shared `sameOrigin` policy. Native account, session, party, device, key, and deletion actions continue to reject opaque production origins.

### Verification

- A focused regression proves a matching opaque invitation form is accepted, while the same request with a missing/mismatched token and an arbitrary cross-site origin is rejected. The existing production test continues to prove the host/admin origin policy rejects `null`.
- `make check`, `make security`, and `make admin-test` pass, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, executable boundary contracts, and the reachable-vulnerability scan.
- GitHub CI run `32596112467` and Security run `32596112530` passed exact candidate `b569daa692a1b1861266e3787a0f58c3673803d6`.

### Production

- Masked live logs proved both failed submissions had a present cookie and hidden token but failed only the origin check. The configured base URL and request host were both canonical `https://ringring.live`; the bearer token and submitted member values were never logged or replayed.
- The guarded fast-forward created and restore-drilled checksummed pre/post archives `ringring-20260822T201530Z-fb8d468.tar.gz` and `ringring-20260822T201712Z-b569daa.tar.gz`. Production runs exact candidate `b569daa692a1b1861266e3787a0f58c3673803d6`.
- A live fake-token probe with a matching cookie and opaque origin passed the CSRF boundary and reached the nonexistent-invitation response. The same fake request with an arbitrary cross-site origin or mismatched token remained `403`; the real invitation was never submitted.
- After clearing only those intentional probe warnings by recreating the stateless app container, doctor and public readiness pass. The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, contacts, channels, or calls.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.
## 2026-08-22 — A private first-call card for new members

### Shipped

- Added a bright claim-only setup card that tells a newly joined member which existing party extensions still have an active phone route and which utility/fun numbers are currently routable. A child can finish setup and place a first call without asking the host to transcribe a phonebook or stale service list.
- Reduced each directory item to display label and extension and included it only when at least one attached phone remains active. Device labels, SIP identities, credentials, readiness, contact state, timestamps, and disconnected-only members never enter the view.
- Derived `*10`, `*15`, and optional `*11`–`*14` entries from the same party/service readiness rules used by routing. Paused AI-powered lines are omitted, and `*14` still requires the default-closed operator child-safety gate.
- Hardened the isolated two-household NAT and Linphone gates after an apparent startup race recurred. The stricter endpoint check exposed the actual cause: root-created bind directories remained mode `0700` under exact-candidate `umask 077`, so dropped runtime UIDs could start without readable generated includes, provisioning XML, or the public test CA. Those read-only bind roots are now explicitly traversable while the TLS private key remains root-only; NAT also proves the exact endpoint and both nested household paths before SIPp's one-shot registration.

### Decisions

- Put the snapshot only on the successful member-claim response. The unclaimed invite page cannot enumerate a party, while host-created/rotated cards sent to a device technician and the Linphone provisioning payload stay free of unrelated family names.
- Keep it ephemeral rather than create another bearer directory URL. The setup response already carries a one-time SIP credential, is private/no-store/no-referrer/noindex, and tells the member to ask the host for changes made later.
- Describe only currently dialable destinations. A disconnected-only member or paused service is more confusing than helpful on a first-call card.
- Treat readable generated state, PBX endpoint loading, and nested-network reachability as fixture readiness, while keeping challenged registration, rewritten contacts, the actual call, and bidirectional RTP as the assertions under test.

### Verification

- Focused web and pure policy tests cover active-versus-disconnected members, an unclaimed invite with no directory, successful claim rendering, reduced fields, child-safety gating, spend-paused service omission, and host setup-card exclusion.
- A disposable browser claim rendered two existing members and the exact enabled utility lines at 1280×900 and 390×844. The card used two 347px columns on desktop and one 305px column on mobile, had zero horizontal overflow, and kept every call row at least 44px tall. Its viewport override and tab were reset, and the temporary database was moved to Trash.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. The accepted module advisory remains unreachable from RingRing.
- The first isolated candidate passed the complete SIP/TLS/multi-phone gate, then hit the recurring NAT pre-registration timeout before receiving a `401` challenge. A stricter candidate then failed at endpoint readiness and its restricted logs proved the generated include was unreadable, matching a direct dropped-UID bind probe. Candidate `0be17f7` passed the complete SIP gate and two consecutive NAT registration/call/media runs; its subsequent Linphone `PermissionError` exposed the same restrictive-umask defect on the provisioning and certificate bind roots. The repaired exact-candidate evidence is recorded after the rerun below.
- From an isolated server checkout of exact candidate `fb8d468c44e482dcf20c527a56f3edb9cc56f170` created under `umask 077`, the complete SIP/TLS/multi-phone gate, two consecutive two-household NAT registration/call/media runs, and the official Linphone one-fetch provisioning, TLS registration, extension-call, and returned-audio suite all passed. Every exact smoke container and network was removed. GitHub CI run `32594682278` and Security run `32594682318` passed the same candidate.

### Production

- The guarded fast-forward created and restore-drilled checksummed pre/post archives `ringring-20260822T195013Z-6f6f402.tar.gz` and `ringring-20260822T195156Z-fb8d468.tar.gz`. Production is clean at exact runtime candidate `fb8d468c44e482dcf20c527a56f3edb9cc56f170`; doctor, splash, signup, and readiness pass, while public metrics remain `404`.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, device secrets, contacts, channels, calls, or CDR files. Neither the authenticated party page nor the outstanding invitation was requested, canceled, consumed, or replaced.
- The app/Asterisk environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The older environment still uses the documented absent-variable default for the closed AI gate, and generated routing contains neither `*14` nor AudioSocket.
- `pbx_config` is running, the fixed phone-check context contains no dialing/network primitive, and the actual AMI account remains ACL-bound with only `system,command,originate` write access. The app, Asterisk, and Caddy container identities are `7b108f8485cb0e7f07fc4a72e0945f0f3319c416f098540f5b7695b3467f9e72`, `ee0c71982cc55762f4b7311ae41438910c54389aeb5edaa4ef2d4fd0ba830ddf`, and `8a4925416f76a6f3a92913935006529b019e2314267e9e9b154b52e893d88e20`. Settled app, Asterisk, and Caddy warning/error checks are zero.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Cancel a mis-sent invitation

### Shipped

- Added a host-visible count of active unused invitations and a deliberately labeled control to cancel all of those links for one party. A host can now recover after pasting a bearer link into the wrong conversation instead of waiting up to 48 hours.
- Added a transactional store boundary that repeats exact party/host ownership and removes only unclaimed, unexpired invitation rows. Used invitations, expired rows, members, phones, provisioning, and routing remain unchanged; a concurrent claim or cancellation serializes through SQLite.
- Bounded and CSRF-protected both invitation write forms, included cancellation in the existing party-write rate limit, cleared a pending encrypted reveal cookie, and kept counts, IDs, hashes, links, expiry, and removal totals out of logs and metrics.

### Decisions

- Show only an aggregate active count. RingRing does not know a recipient and should not add names, delivery history, token fragments, or a persistent invitation directory merely to support revocation.
- Cancel every active unused link for the party rather than asking the host to distinguish opaque links that RingRing intentionally reveals only once. Making a fresh link is cheap and clearer after suspected disclosure.
- Preserve used and expired records. Cancellation is a bearer-link safety action, not deletion of members or historical lifecycle state.

### Verification

- Focused store, web, rate-limit, accessibility, and executable security-contract tests cover active/used/expired boundary semantics, exact host isolation, failed-request non-mutation, cancellation invalidation, preserved claimed state, bounded forms, the cleared reveal, route classification, and unambiguous touch-sized controls.
- A disposable browser host exercised singular and plural active counts, the expanded warning, cancellation, and the clean post-cancel party state at 1280×900 and 390×844. Summary and action targets were 44px tall, the 301px mobile control fit without horizontal overflow, the notice confirmed cancellation, and both the pending QR and management control disappeared. The viewport/tab/listeners were removed and the disposable database was moved to Trash.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. The accepted module advisory remains unreachable from RingRing.
- Exact candidate `6f6f4020ca4bf333d72ad96f1abd21b9a1196acb` passed the isolated SIP/TLS/multi-phone gate, two successful two-household NAT call/media runs, and official Linphone provisioning, registration, extension-call, and two-way-audio checks. An earlier NAT registration timeout also reproduced unchanged on known-good runtime `b991d834953f80c366a4bffb5121baca6bc2a593`; after the Docker fixture settled, the candidate passed twice and every disposable container/network was gone. GitHub CI run `32590182754` and Security run `32590182720` passed the exact candidate.
- The guarded upgrade created and restore-drilled checksummed pre/post archives `ringring-20260822T182348Z-b991d83.tar.gz` and `ringring-20260822T182532Z-6f6f402.tar.gz`. Production is clean at the exact candidate; doctor, splash, signup, and readiness pass, public metrics remain `404`, `pbx_config` is running, the fixed incoming-check context has no dialing primitive, and the actual AMI account remains ACL-bound with only `system,command,originate` write access.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, device secrets, contacts, channels, calls, or CDR files. Neither the authenticated party page nor the outstanding invitation was requested or canceled. The app/Asterisk environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The access-code prompt remains present without printing its phrase, the AI gate remains closed, and no family/OpenAI/phone/route record changed.
- The app, Asterisk, and Caddy container identities are `06d4634f8577194b4af6768faa391538bafb85fac90b9b84ba320c24641cdfe1`, `74bfe5ae0d77c663824eb7fa3b71b521d33ad9356cda8f663936f2165ec34bd5`, and `4ee5e3602dba8e2c1c96b0a0fe28286d9426b202045ffc3a75c72da5500bb158`. Settled app, Caddy, and Asterisk configuration warning/error checks are zero.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.

## 2026-08-22 — Private scan-to-join invitations

### Shipped

- Added a scannable QR beside the host's one-time invitation link. A family member can point the invited phone's normal Camera app at it and open the existing join page without typing a long token.
- Reused RingRing's in-process PNG renderer, data-URI CSP allowance, encrypted one-view host flash, hashed durable token, 48-hour expiry, and atomic invitation claim. The bearer link never goes to an external QR or analytics service, and a refreshed party page reconstructs neither link nor image.
- Added explicit host guidance that the link and QR are equivalent secrets and must be sent privately. This remains distinct from the Linphone provisioning QR: the invitation QR is for a normal Camera app and opens the member join page.

### Decisions

- Keep scanning as a second representation of the existing invitation, not a new token or claim path. Expiry, CSRF, safe extension selection, and single-use consumption therefore stay centralized.
- Render the QR only during the authenticated no-store reveal. Do not persist an image, add an image endpoint, embed a remote tracking request, or expose the code after refresh.

### Verification

- Focused web, accessibility, QR, and executable security-contract tests cover the local data URI, no-store first reveal, disappearance on the next party view, privacy warning, associated accessible description, and absence of network QR-provider primitives.
- A disposable browser host created an invitation and opened its card at 1280×900 and 390×844. The PNG loaded at a crisp 270×270 intrinsic size and a 202px rendered square, the link field remained 52px tall, the mobile card had no horizontal overflow, and the code plus privacy warning stayed legible. Refresh removed both link and image while the saved disposable link still opened the join form. The viewport/tab/listeners were removed and the database was moved to Trash.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. The accepted module advisory remains unreachable from RingRing.
- From an isolated checkout of exact candidate `b991d834953f80c366a4bffb5121baca6bc2a593` created under `umask 077`, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` passed same-extension fan-out, TLS 1.2 and UDP registration, incoming ring testing, mixed-transport calling, `*10`, authenticated `*15`, two-household NAT media, one-use provisioning, and official-engine two-way audio. GitHub CI run `32589241085` and Security run `32589241093` passed the same candidate; smoke containers were gone and the isolated checkout was moved to root's Trash.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T175939Z-4cf2fb1.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T180123Z-b991d83.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `b991d834953f80c366a4bffb5121baca6bc2a593`. Doctor passes; splash, signup, and readiness return `200`; public `/metrics` returns `404`; `pbx_config` is running; the fixed incoming-check context has no dialing primitive; and the actual AMI account remains ACL-bound with only `system,command,originate` write access. There are zero SIP contacts, channels, calls, or CDR files.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. The authenticated party page and outstanding invitation were never requested, so the real one-view reveal was not consumed or replaced. The app/Asterisk environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The AI operator gate remains tested default-closed, and no family record, party key, provider setting, secret, invitation, or route changed.
- The app, Asterisk, and Caddy container identities are `b6e24b41b3a87cc33ff73c5798a987ded07b136d57cc0c23a1b27792048e690b`, `a3b7159c2416a45519b472cfa1b1a730d783980d3a51bab760eee54e43d5489b`, and `ffa03393fc145868e885ada7ca93542137f4093a6769995926d2428d6342ed28`. The only deployment-window application warning was the expected initial AMI lookup race while Asterisk was being recreated; settled app, Caddy, and Asterisk configuration warning/error checks are zero.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — One extension, several independent phones

### Shipped

- Added a host-only **Add another phone** flow to every member. An ATA, desk phone, and softphone can now share the member's familiar extension and ring together without sharing a SIP username or password.
- Reused the private one-time setup sheet and Linphone QR path for each added device. The new page says that existing phones remain connected, while every device retains independent presence, incoming-ring testing, readiness checks, credential rotation, revocation, and cascading deletion.
- Added one transactional store boundary that requires the exact host, party, and member, caps a member at eight saved devices, inserts a fresh encrypted credential, and creates its hashed expiring provisioning token. Rejected, cross-host, cross-party, invalid-token, and over-limit requests leave routing unchanged.
- Extended the disposable SIP gate to create the extra phone through the authenticated web route, verify its generated PJSIP endpoint, and require the loaded extension to contain both explicit party endpoint names before continuing the call/media suite.

### Decisions

- Model multiple physical devices as separate credentials on one member, not as copies of one password. Losing or disconnecting a softphone must not force the family to reconfigure the ATA beside it.
- Keep fan-out explicit in generated Asterisk configuration. RingRing joins only validated active endpoints already attached to that member and party; it does not use a wildcard contact lookup, global context, or dynamic destination supplied by the host.
- Bound retained devices at eight per member. A family that reaches the cap can reconnect a saved disconnected phone, avoiding unbounded database and dialplan growth without complicating the normal one-or-two-phone path.

### Verification

- Focused store, web, renderer, accessibility, and executable security-contract tests cover transactional ownership, independent secrets and one-use tokens, one-time reveal, exact same-extension fan-out, rejection without mutation, individual revocation, unchanged first-phone routing, accessible labels/help, and the device cap.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. The accepted module advisory remains unreachable from RingRing.
- A disposable browser host, party, member, and added second phone exercised the complete one-time handoff. The first 390×844 pass exposed a squeezed form caused by the member grid; the corrected layout gives both input and action 258px of usable width, keeps the targets 52px and 54px tall, has no horizontal overflow, and remains clean at 1280×900. Both independently managed device labels remained on the shared member, the temporary viewport/tab/listeners were removed, and both disposable databases were moved to Trash.
- From an isolated checkout of exact candidate `4cf2fb1e9301326f6748e34949ad7158600e9852` created under `umask 077`, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` passed host-created same-extension fan-out, TLS 1.2 and UDP registration, incoming ring testing, mixed-transport calling, `*10`, authenticated `*15`, two-household NAT media, one-use provisioning, and official-engine two-way audio. GitHub CI run `32588446842` and Security run `32588446867` passed the same candidate; smoke containers were gone and the isolated checkout was moved to root's Trash.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T174407Z-e8dfae5.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T174550Z-4cf2fb1.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `4cf2fb1e9301326f6748e34949ad7158600e9852`. Doctor passes; splash, signup, and readiness return `200`; public `/metrics` returns `404`; `pbx_config` is running; the fixed incoming-check context has no dialing primitive; and the actual AMI account remains ACL-bound with only `system,command,originate` write access. There are zero SIP contacts, channels, calls, or CDR files, and no real family phone was added or rung.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. The app/Asterisk environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The AI operator gate remains tested default-closed, and no family record, party key, provider setting, secret, invitation, or route changed.
- The app, Asterisk, and Caddy container identities are `8b0abb392975e696413bde2ab0f79511b773a9910307ed817c55890e852315d2`, `df1418351fc2d722c9dc63409fe3689f1e578583e39bf1247910e767b368fd18`, and `65cbf2a27f689f684e14c4ecb303b066b24de2c7d885f191170a642844a994d7`. The only deployment-window application warning was the expected initial AMI lookup race while Asterisk was being recreated; settled app, Caddy, and Asterisk configuration warning/error checks are zero.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including simultaneous ringing and background transitions.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Host-triggered incoming phone setup ring

### Shipped

- Added a host-only **Ring this phone** action for each active online device. It places one private internal setup call and speaks the phone's extension after answer, so a family can prove incoming ringing before configuring a second phone.
- Scoped every request through the authenticated host, party, active device, generated SIP username, and current private AMI contact state. Cross-party, revoked, missing, offline, and unknown devices cannot be rung; per-host and per-device limits constrain repeated calls.
- Added a fixed Asterisk prompt context with bundled sounds, a 20-second absolute limit, and CDR disabled. The application validates the generated endpoint and ordinary extension, then supplies only fixed AMI Originate fields; the context has no trunk, `Dial`, transfer, URL, AGI, AudioSocket, or shell path.
- Updated the real-phone checklist and setup handoff so hosts can use the incoming ring alongside `*10`, while still requiring a real cross-network two-way call for the physical-hardware milestone.
- Hardened the Asterisk image and entrypoint against restrictive checkout umasks by normalizing static configuration ownership and mode. The guarded verifier now requires the fixed phone-check context to be loaded, rather than accepting container health and AMI reachability alone.

### Decisions

- Use the existing private app-to-Asterisk control connection rather than expose a SIP or web callback. Asterisk classifies the action under its narrow AMI `originate` privilege, which the source-controlled account gains while remaining restricted to the fixed app-container address and receiving no configuration or broad `all` permission.
- Treat the ring as a live diagnostic rather than proof saved by the server. The host still confirms the readiness checklist; RingRing adds no call record, caller history, audio, transcript, or device identifier to application logs.
- Keep the action unavailable until live contact status says online. This avoids presenting a successful queued request as evidence that an unplugged or unreachable family phone rang.

### Verification

- Tests cover the host/party/device query, absence of decrypted credentials, revoked and cross-party rejection, contact-state enforcement, exact fixed AMI frame, input validation before connection, two-rings-per-minute limiting, accessible disabled controls, and executable dialplan/permission boundaries.
- The disposable SIP smoke now signs in as its isolated host, sends the web request to a registered endpoint, answers the resulting SIP call, verifies the caller label and spoken prompt, checks the loaded context has no `Dial`, confirms no CDR file was created, and waits for a clean zero-channel hangup.
- The first production audit exposed a mode-`0600` `extensions.conf` created when Git updated the root checkout under `ringringctl`'s intentional `umask 077`. The running file was restored to the Asterisk account before any device existed, then build-time, entrypoint, smoke, security-contract, and deployment-verifier regressions were added so the failure cannot hide behind a healthy process again.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell validation, vet, the complete race-enabled suite, control-plane fixtures, and the reachable-vulnerability scan.
- From an isolated checkout of exact final candidate `e8dfae55481d0138e32d052d8fa99509c9e8f6d3` created under `umask 077`, the source `extensions.conf` began at mode `0600`; the built container normalized it to runtime mode `0640` and loaded the dialplan. `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` passed the new web-to-incoming-call path, mixed TLS/UDP calling, `*10`, authenticated `*15`, two-household NAT media, one-time provisioning, and official-engine two-way audio. GitHub CI run `32587006440` and Security run `32587006431` passed the same candidate, and the checkout plus disposable containers were removed.

### Production

- The initial feature promotion used verified pre/post backups `/root/ringring-backups/ringring-20260822T170332Z-d63c7da.tar.gz` and `/root/ringring-backups/ringring-20260822T170515Z-b1486ab.tar.gz`. The permission-hardening promotion then used `/root/ringring-backups/ringring-20260822T171506Z-b1486ab.tar.gz` and `/root/ringring-backups/ringring-20260822T171638Z-e8dfae5.tar.gz`. Every archive passed its checksum, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drill.
- Production is clean at exact runtime commit `e8dfae55481d0138e32d052d8fa99509c9e8f6d3`. Doctor passes; splash, signup, and readiness return `200`; public `/metrics` returns `404`; `pbx_config` is running; the fixed context is loaded without a dialing primitive; and the actual AMI account has only `system,command,originate` write access behind its app-container ACL. There are zero SIP contacts, channels, calls, or CDR files, and no real family phone was rung during verification.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. The app/Asterisk environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The AI operator gate remains tested default-closed, and no family record, party key, provider setting, secret, invitation, or route was changed.
- The app, Asterisk, and Caddy container identities are `d8bffeb1f7cc64c4bc03ea90434a1153ab1d91fc7174a2a306841200182f452d`, `6db2745f2d3fc5cfbc29a09fc64cb792f89ede35f9b43399e7b5cf5fabc3a613`, and `8bb4b6a1140f71749f0be5d8dfa248ee9a3fd3d7c0699c33eff8d70e4af2fede`. Settled app and Caddy logs have no warning/error matches, and the recreated Asterisk log has no `pbx_config` decline.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including background transitions.
- Obtain the external child-safety review and OpenAI Zero Data Retention eligibility before opening the AI conversation gate.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Fail-closed child-safety gate for AI conversation

### Shipped

- Added a deployment-wide `AI_CHILD_SAFETY_APPROVED` gate for the open-ended `*14` RingRing AI conversation. It defaults closed when omitted, fresh installs write `false`, and malformed values prevent startup instead of being interpreted as approval.
- Enforced the closed gate independently in the host form, durable service-setting update, startup state reconciliation, Asterisk configuration reconciliation, FastAGI authorization, party-key access, AudioSocket handoff, and the final Realtime WebSocket bridge. Time, weather's one-way disclosed speech, radio, echo, extension selection, and ordinary party calling remain available.
- Added a clear locked state and official under-18 guidance link to the host UI. Even after an operator approval, the existing party-level opt-in and adult-host confirmation remain required.
- Documented the exact two-condition operator procedure, revocation path, upgrade/rollback behavior, architecture, security boundary, and threat-model residual risk. The external child-safety/ZDR roadmap item intentionally remains open.

### Decisions

- OpenAI's current under-18 guidance says not to process personal data from children under 13 without first implementing Zero Data Retention. A read-only request using the configured production administrator credential returned HTTP `403` with code `not_eligible`; that is not ZDR confirmation, so the reference deployment must remain closed.
- Do not convert provider ambiguity into a host warning or accept an adult checkbox as operator approval. The deployment operator alone may open the gate after both an external child-safety review and provider confirmation for the exact organization/project.
- Clear stale durable `ai_enabled` preferences at closed-gate startup. This makes an upgrade safe immediately and prevents an older binary from reviving a previously enabled route after rollback.

### Verification

- Regression tests prove the strict default, rejected storage update, durable stale-state clearing, preservation of non-conversation services, dialplan filtering, disabled host control, rejected FastAGI/key access, and a Realtime bridge that exits before network setup. An executable security contract requires every gate wiring point and the installer default.
- `make check` passes shell validation, installer/certificate fixtures, vet, and the complete race-enabled suite. `make security` reports no reachable vulnerability and only the previously accepted non-reachable module advisory. `make admin-test` passes.
- The local containerized SIP smoke could not start because Docker Desktop was not running. From an isolated checkout of exact candidate `d63c7daf1e8563268a9382243a4f4b63293e86a8` on the Docker-enabled reference host, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` then passed TLS 1.2 and UDP registration, mixed-transport calling, `*10`, authenticated `*15`, two-household NAT media, one-time Linphone provisioning, and official-engine two-way audio. GitHub CI run `32584405562` and Security run `32584405565` passed the same candidate. The candidate checkout and all test containers were removed.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T162427Z-a444135.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T162609Z-d63c7da.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `d63c7daf1e8563268a9382243a4f4b63293e86a8`. Doctor passes; splash, signup, and readiness return `200`; public `/metrics` returns `404`; and there are zero contacts, channels, or calls. With the new environment variable deliberately omitted, the tested runtime default is closed and the generated dialplan contains zero `*14`, `ai-authorize`, or `AudioSocket` routes. No provider setting, party key, or family record was changed.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. Both root environment hashes remain `b8b0380b49d0351023b3ab083ce61862f5dc753c3dedd48a99d5387f97ac16e1` and `a26e7c0b918957a56e5f78dbabb950c1d65a1aac6e48f60b79f6e2a578d7727a`; both empty generated-routing hashes remain `36ca1c5d8684333af387db0fdedbf2ab9e9f2306b4334bc5d741998ac7f7b5a5`. The app, Asterisk, and Caddy container identities are `01c9c289e719060b66da246da90cb29a7f508ec6dc472a3c92b783e1ca891e20`, `d6a746004b903cbd84e7c0c38294a724b1a2648f35067067b923656ccf7b2e60`, and `c7d64d14c57dcbabe713632cfaa2155c059c9d567b884999cec442d3c4a39fdb`.
- Settled application logs contain no new warning or error. The only deployment-window warning was the known initial AMI lookup race while Asterisk was still being recreated; the guarded verifier explicitly recognizes that startup ordering case and the subsequent AMI check and doctor both passed.

### Remaining

- Obtain the external child-safety review and OpenAI ZDR eligibility before changing the production gate from `false`.
- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including background transitions.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Copy-ready real-phone setup handoff

### Shipped

- Turned the one-time manual phone card into a copy-ready setup sheet. A person can copy the registrar, generated SIP username, one-time password, extension, or transport individually, or copy all six values as a plain private setup note for the person configuring the phone.
- Added a visible warning that the complete note contains the password and should be pasted only into the device or a private message that is removed after setup. Copy actions are always user-initiated, announce their result through an ARIA live status, and leave the original selectable values usable when browser copying is unavailable.
- Added a field-name translator for the inconsistent labels used by ATAs, desk phones, and softphones: registrar/proxy, SIP user/authentication ID, display number, realm/domain, and outbound proxy. Expanded the device guides with generated-username versus extension guidance, NAT keep-alive/rport, no router port forwarding, registration, and `*10` acceptance steps.
- Kept the core setup path script-free. The optional helper is one embedded local asset whose exact SHA-256 is required by both Subresource Integrity and Content Security Policy; inline JavaScript and arbitrary same-origin scripts remain blocked.

### Decisions

- Prefer a vendor-neutral field translator over claiming untested automatic provisioning for particular ATA models. RingRing can make the common setup values unambiguous now while keeping each physical model in the still-open compatibility matrix until real hardware proves it.
- Permit only the exact reviewed copy helper rather than broadly changing CSP to `script-src 'self'`. An executable contract recomputes the asset digest and rejects network, browser-storage, cookie, navigation, and logging primitives in the helper.
- Do not send a copy event or credential back to RingRing. The helper reads only the already visible one-time values after a tap and writes only to the browser/operating-system clipboard; clipboard lifetime is outside RingRing's control and is stated in the security documentation.

### Verification

- Focused web and security-contract tests cover the rendered controls, distinct accessible labels, the ARIA status, private-message warning, no-script fallback, field translator, exact SRI/CSP digest agreement, and absence of the bounded transmission/persistence primitives. `make check` passes formatting, operator/certificate fixtures, vet, and the complete race-enabled suite; `make security` reports no reachable vulnerability and only the previously accepted non-reachable module advisory.
- A disposable browser host, party, member, and rotated setup card exercised individual and complete-note copy actions. At 390×844 and 1280×900 the page had no horizontal overflow, all eight enhanced controls were at least 44px tall, and the credential card plus field translator fit the one-column phone layout. The helper reported successful copy without reading the clipboard back. The tab and viewport override were removed, the app and four listeners stopped, and the disposable database was moved to Trash.
- From an isolated checkout of exact candidate `a44413529acc69a7b92850413822334f213625d6` on the reference host, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` passed TLS 1.2 and UDP registration, mixed-transport calling, `*10`, authenticated `*15`, two-household NAT media, one-time Linphone provisioning, and official-engine two-way audio. GitHub CI run `32583330840` and Security run `32583330817` passed the same candidate. The candidate checkout, containers, networks, generated identities, and state were removed.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T160220Z-fafc282.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T160402Z-a444135.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `a44413529acc69a7b92850413822334f213625d6`. Doctor passes; splash, signup, readiness, and the new static helper return `200`; public `/metrics` returns `404`; and there are zero contacts, channels, or calls. The public helper body matches the pinned digest, and both the SRI tag and restrictive CSP carry that same digest. The app, Asterisk, and Caddy container identities are `ed10bd2420f20f15e8cef5e26d24ba799d63caaef4bb89124c429d8367dfc36c`, `8c4a9ee2467e8393db0e7eacccc0bc98055920258655510ba10e8646f820b273`, and `db58ae76f0cbc631b736cc59e017e21e66604425c725062f516f1fb3d9a849c6`.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. Both root environment hashes and both empty generated-routing hashes are unchanged. The memorable family access phrase remains configured outside Git without its value being printed; no production setup card, family record, OpenAI control, SIP credential, invitation, session, or route was opened or changed.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including incoming/outgoing audio, certificate behavior, background ringing, and Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for callers under 13.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Suggested safe extensions and forgiving invitation forms

### Shipped

- Made one-time invitation pages prefill the party's first available extension beginning at `101`. The page tells the family they can keep the suggestion or choose another number, without exposing any existing member name or number.
- Kept the phone-book name and optional device label on validation errors, highlighted only the affected fields for assistive technology, and replaced an occupied or reserved extension with a fresh suggestion. Oversized forms now fail before invitation processing and do not consume the invite.
- Centralized extension rules across invitation claims, authenticated `*15` voice changes, storage, and Asterisk rendering. RingRing will not assign the familiar public emergency/crisis numbers `000`, `111`, `112`, `911`, `988`, or `999`; this does not add PSTN or emergency calling.
- Added an idempotent, schema-free startup migration that moves any legacy reserved value to the first unoccupied ordinary extension within the same party. Documented upgrade and rollback behavior plus the official public-safety references behind the deliberately small reserved set.

### Decisions

- Treat a suggested extension as a convenience, not a reservation. The database transaction and per-party unique constraint remain authoritative when two invitees submit concurrently; the collision page simply offers the next current suggestion.
- Query only extension strings after an invitation has been validated and return only one suggestion. Do not expose a directory or reveal which numbers are occupied on the public join page.
- Reserve familiar emergency and crisis numbers even though RingRing cannot route to the public telephone network. A family member must never appear to answer a number a child may associate with public help.

### Verification

- Focused extension, store, telephony, voice, web, and maintenance tests cover formatting, every reserved number, safe suggestion gaps, invitation non-consumption, preserved fields, occupied-number collisions, authenticated voice rejection, renderer rejection, and idempotent per-party legacy migration.
- A disposable 390×844 browser walkthrough found no horizontal overflow, a 54px primary target, and a prefilled `101` with the public-safety explanation. The disposable app/database were stopped, its directory was moved to Trash, and the temporary tab and viewport override were removed; no production invitation was opened or used.
- `make check` passes the security contracts, formatting, shell/operator fixtures, vet, and complete race-enabled suite. `make security` reports no reachable vulnerability and retains only the previously accepted non-reachable module advisory. GitHub CI run `32581919969` and Security run `32581919970` passed exact feature commit `fafc282241ec028c4e1d4dbdeeaf48629f367d7c`.
- From an isolated checkout of that exact commit on the reference host, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` passed TLS 1.2 and UDP registration, mixed-transport party calling, `*10`, authenticated `*15`, bidirectional PCMU/RTP through two household NATs, one-time Linphone provisioning, and official-engine two-way audio. The candidate checkout, containers, networks, generated identities, and state were removed.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T153509Z-ef5093e.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T153650Z-fafc282.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `fafc282241ec028c4e1d4dbdeeaf48629f367d7c`. Doctor passes, both SIP jails remain active, public splash/signup/readiness return `200`, public `/metrics` returns `404`, and there are zero contacts, channels, or calls. The app, Asterisk, and Caddy container identities are `1448bd1d594462066eae5f278203384bc4a8f51eb3a616eef4e1e4e06bf0d6ed`, `63539d12e7e9c0ad62f4861ef3abaaaf1688bc92d57e99f13478d1e553af4f9f`, and `5980f6b45b199a098a82e2f4ab53c66d92f5282877d25f39a0d9a4206be5d59a`.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or device secrets. Both root environment hashes and both empty generated-routing hashes are unchanged. The memorable family access phrase remains configured outside Git without its value being printed; no family, OpenAI, SIP credential, invitation, session, or route record was submitted or changed.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including incoming/outgoing audio, certificate behavior, background ringing, and Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for callers under 13.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Executable threat model and tenant-boundary hardening

### Shipped

- Added a source-controlled threat model covering assets, actors, data flows, trust boundaries, eighteen concrete abuse cases, controls, residual risks, reproducible evidence, and a review rule for future authentication, telephony, provider, observability, secret, and deployment changes.
- Fixed a real party-isolation flaw in generated Asterisk context names. The former mapping changed underscores to hyphens and truncated IDs, so distinct valid parties could collapse into one dialplan context. RingRing now validates identifiers at 48 bytes and preserves every byte of the party ID, making the context mapping injective; reconciliation also rejects duplicate global device IDs and SIP usernames.
- Added regression cases for underscore/hyphen collisions, long common prefixes, overlong identifiers, duplicate global routing identities, Asterisk config injection, cross-party endpoints, arbitrary radio destinations, and any generated dial primitive outside party PJSIP endpoints or the private AI AudioSocket.
- Added executable deployment contracts that permit only the intended public web, SIP, and RTP ports; reject host networking, privileged mode, and Docker socket mounts; keep Caddy away from metrics/AMI/voice-control ports; preserve the exact AMI deny/permit boundary; and structurally reject PSTN registration, trunk, and global outbound routes.
- Added proxy-trust tests proving that direct clients cannot spoof a rate-limit identity and only the fixed Caddy peer may provide a syntactically valid `X-Forwarded-For` client address. Documented the intentional low assurance of the memorable family signup phrase: it is an anti-drive-by gate, not identity proof or an account password.
- Added `make security` with pinned `govulncheck v1.7.0` and a read-only GitHub Security workflow on every push and pull request, weekly, and on demand. The fast, offline boundary contracts also run inside every `make check`.

### Decisions

- Treat tenant-context naming as a security function, not presentation formatting. Asterisk-safe party IDs remain opaque and must never be normalized or truncated after uniqueness is established.
- Make the absence of PSTN and private-control exposure executable release contracts. A future trunk, extra proxy, CDN, public port, or non-party dial primitive requires an explicit scope/security redesign instead of silently changing configuration.
- Keep the family access phrase easy to communicate as requested. Compensate with source-address limits, independent host passwords, provider spend ceilings, and operator rotation/removal; do not claim the phrase proves who a new host is.
- Accept `GO-2026-5932` only as a non-reachable module finding: RingRing does not import the affected unmaintained `golang.org/x/crypto/openpgp` package and uses `x/crypto` for Argon2. Revisit the decision if imports or upstream guidance change.

### Verification

- `make check` passes formatting, POSIX/operator fixtures, certificate-sync fixtures, vet, the boundary contracts, and the full race-enabled suite. `make security` reports no vulnerable symbol reachable by RingRing and no vulnerability in an imported package; its verbose inventory reports only the accepted non-reachable module finding above.
- The focused web tests cover trusted-proxy, spoofed-forwarding, malformed-forwarding, IPv6, and rate-window behavior. Telephony tests exercise both historical collision classes and inspect every emitted `Dial` destination. Deployment tests parse Compose service publications and inspect Caddy, AMI, base PJSIP, and base dialplan boundaries.
- From an isolated checkout of exact candidate `ef5093e023d3dc951f5000eb76fa10961d840819` on the reference host, `make sip-smoke`, `make nat-smoke`, and `make linphone-smoke` all passed. Evidence includes TLS 1.2 plus UDP registration, mixed-transport party calling, `*10` echo, authenticated `*15` selection, bidirectional RTP through two simulated household NATs, one-time Linphone provisioning, and official-engine two-way audio.
- GitHub CI run `32579944248` and Security run `32579944242` both passed the exact release. The disposable candidate checkout, containers, networks, generated identities, and state were removed; no smoke resource or family record remained.

### Production

- The guarded fast-forward used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T150410Z-5e2e60a.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T150551Z-ef5093e.tar.gz`. Both passed checksums, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills before the recovery marker was cleared.
- Production is clean at exact runtime commit `ef5093e023d3dc951f5000eb76fa10961d840819`. Doctor passes with SQLite and AMI up, both SIP jails active, the trusted SIP certificate current, public readiness `200`, and public `/metrics` `404`. The app, Asterisk, and Caddy container identities are `4404a447cc74c6f0bd384ba7654021210d235a64ebfed4d8b3be553fbec301b0`, `20c194c28b1b5f7a589e4645a50394352c549f097a811dcdff3b69516251d498`, and `40e6a7174b1efa9905ece737982fca2f18ee140dcaac84bccca0208b495fcd20`.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, or contacts. Both root environment hashes and both empty generated-routing hashes are unchanged. The memorable family access phrase remains configured outside Git without its value being printed; no family, OpenAI, session, SIP credential, or route record was submitted or changed.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including incoming/outgoing audio, certificate behavior, background ringing, and Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for callers under 13.
- Copy verified backups to encrypted off-host storage with a retention schedule, and design the optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Privacy-preserving internal observability

### Shipped

- Added a Prometheus-compatible metrics document on a distinct app listener fixed to container loopback `127.0.0.1:9090` in production. Compose neither exposes nor publishes the port, Caddy has no route to it, and the public app intentionally returns `404` for `/metrics`.
- Added live aggregate database and AMI health, four normalized SIP-contact counts, process start time, coarse web request counts/duration histograms, telephony-reconciliation outcomes, bounded voice-service outcomes, and active AI bridge count. Metrics live only in process memory and reset on restart; RingRing installs no scraper, remote-write target, dashboard, or retention store.
- Restricted every metric label through code allowlists. HTTP uses only a coarse surface, bounded method, and status class; voice uses only fixed service/result values. No party, host, member, device, SIP username, extension, token, address, user-agent, place, station, prompt, error, caller, per-call timestamp/duration, audio, or transcript can become a metric label.
- Replaced application request paths with route templates and normalized unknown methods. Removed explicit party/member/device identifiers and raw origin/panic values from subsystem logs; potentially value-bearing operational failures now use fixed error classes while the event name retains the failed operation. Documented Asterisk's separate source-address security log as restricted, short-retention Fail2Ban data rather than application telemetry.
- Extended guided install, upgrade, and doctor verification to require private SQLite/AMI gauges and reject any public `/metrics` response other than `404`. Fresh installs explicitly set the loopback address, existing deployments safely inherit the same default, and both the production app and operator command reject a non-loopback override.

### Decisions

- Keep useful aggregate failure and saturation signals without creating a second family activity database. A scrape may show that a subsystem or coarse surface is busy, but cannot identify which party, phone, extension, caller, or conversation caused it.
- Query private AMI during each scrape, immediately reduce endpoint states to four counts, and discard the endpoint-keyed map. Do not export raw AMI frames, contact URIs, source addresses, ports, call IDs, or user agents.
- Make loopback structural in production rather than relying only on the absence of a Compose port mapping. A future collector needs a deliberate protected host-side design, authenticated dashboards, and an explicit short retention policy.

### Verification

- Registry tests cover deterministic Prometheus output, duration buckets, fixed-label fallback, negative counts, concurrent updates, narrow method/path handling, no-store headers, and safe error classes. Web tests prove aggregate AMI reduction, route-template logging, unknown-method normalization, public metrics absence, and injected party/token/path/device values absent from both logs and scrapes. Voice tests cover extension outcomes, AI active release, and a provider error containing a private party value that remains absent from logs/metrics.
- Guided-operation fixtures cover fresh address rendering, legacy environments without the new optional line, rejection of a wildcard production listener, private metric health, public non-exposure, doctor, exact upgrade resume, and existing secret-safety invariants. `make check` passes formatting, POSIX syntax/operator fixtures, vet, and the complete race-enabled Go suite. GitHub Actions passed exact feature commit `5e2e60a` in run `32579045982`.
- A disposable local process used four isolated loopback ports and a temporary database. Public readiness returned `200`, public metrics returned `404`, private SQLite health returned `1`, unconfigured AMI returned `0`, and injected party/token/path/method values appeared only as safe templates/classes in logs and nowhere in metrics. The process stopped, all listeners closed, and the database directory moved to Trash.

### Production

- Verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T143425Z-8db1a79.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T143606Z-5e2e60a.tar.gz` both passed checksum, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration drills.
- Production is clean at exact runtime commit `5e2e60ae483d1a244c6ee03ea706b03d33a21a6c` with no pending upgrade. The new doctor passes. Internal metrics report database/AMI up, zero contacts in every state, zero active AI calls, and observed coarse HTTP traffic; `/proc/net/tcp` confirms only the container-loopback `9090` listener, Docker publishes no mapping, and external HTTPS returns `404` at `/metrics` while `/readyz` remains healthy.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, readiness records, contacts, channels, or calls. Both root environment hashes and both generated-routing hashes are unchanged. The reconciled app, Asterisk, and Caddy identities are `e26b82184752d97b441754e4b943a336ff5938c2967c33d671fa6a48284dd7c0`, `ea51fcfc2103320423f8fff36ea3d1dfa95938a3569f9727e69ef629f4fdf186`, and `cd15f92dbe6ce2ea78327293f81b580352173198fb48e463ea913ef5c64bbd08`. Both SIP jails remain active, application logs contain no family record identifiers, and no family/OpenAI control was submitted.

### Remaining

- Complete the physical ATA, desk-phone, and mobile softphone matrix across two real networks, including incoming/outgoing audio, certificate behavior, background ringing, and Wi-Fi/cellular transitions.
- Complete the external child-safety and OpenAI Zero Data Retention gate before enabling AI for callers under 13.
- Finish the broader security review/threat-model test matrix and optional PostgreSQL/multi-node migration path.

## 2026-08-22 — Host-confirmed real-phone readiness

### Shipped

- Added an optional host-only **Real phone check** to every active device. It records three deliberately plain acceptance checks: the phone heard the `*10` echo service, completed an outgoing party call to a phone on another internet connection with two-way audio, and received that remote call with ringing and two-way audio.
- Kept automatic AMI registration presence visually and semantically separate from human confirmation. RingRing reports what Asterisk currently sees, while the checklist records only what the host says they tested; neither is presented as an independent synthetic proof of a physical device or network path.
- Added a forward-only `device_readiness` table with only the first confirmation timestamp for each check and one update timestamp. Clearing every check deletes the row, credential rotation clears the old connection's evidence, and device/member deletion cascades. No peer identity, number dialed, call content, call log, IP address, network, or user-agent is stored.
- Scoped updates transactionally to the party host and an active device, returned the same generic not-found response outside that boundary, and allowed each individual check to be reset. Older application code safely ignores the additive table; rollback must not drop it until the newer acceptance data is intentionally discarded.
- Made the checklist occupy the full device row on narrow screens and retained RingRing's 44px touch target, visible focus, semantic label, and no-JavaScript form conventions. Updated the architecture, security, deployment/migration, and roadmap documentation.

### Decisions

- Treat readiness as a privacy-preserving acceptance record, not monitoring or surveillance. RingRing does not retain a detailed call history merely to help a host remember which physical checks are complete.
- Invalidate confirmations when SIP credentials rotate because that operation represents a new phone connection even when the member and extension stay the same.
- Verify SQLite differently in the two supported operational contexts: ordinary read-only mode when a live WAL/SHM sidecar exists, so just-migrated state is visible; immutable read-only mode for a cleanly stopped backup with no sidecars, so verification also works from a read-only mount.

### Verification

- Store, web, maintenance, migration, and template tests cover host scope, outsider denial, revoked devices, resettable checks, empty-row deletion, cascades, credential-rotation reset, legacy database creation, schema verification, and the authenticated form flow. Two verifier regressions separately retain a required table only in a live WAL and mount a checkpointed fixture directory read-only.
- `make check` passes formatting, POSIX/operator fixtures, vet, and the complete race-enabled suite. GitHub Actions passed the feature in run `32576737350`, the live-WAL verifier follow-up in `32577072442`, and the corrected live/offline verifier in `32577412073`.
- A disposable browser party/member/device passed at 320×568, 390×844, and 1280×900 with no horizontal overflow and no target below 44px. Saving all three checks rendered `3/3`; clearing one rendered `2/3`. The test app/database were stopped and moved to Trash, and the tab/viewport override was removed. No production family record or physical phone was used.

### Production

- The exact feature deploy used verified pre-upgrade backup `/root/ringring-backups/ringring-20260822T134754Z-9e6dfd1.tar.gz` and post-upgrade backup `/root/ringring-backups/ringring-20260822T135121Z-d92919b.tar.gz`. Its first verification correctly stopped before completion when the old immutable verifier could not see the newly migrated table still in the live WAL. A clean app restart checkpointed the schema, direct verification passed, and the retained exact upgrade resumed without replacing its recovery point.
- The first WAL-aware follow-up then exposed the inverse offline case during its post-deploy backup: a clean WAL-mode database with no sidecars cannot be opened from a read-only mount without immutable mode. The recovery marker and `/root/ringring-backups/ringring-20260822T135458Z-d92919b.tar.gz` were retained. The corrected descendant verifier passed that sealed archive's isolated drill, completed the intermediate checkpoint with `/root/ringring-backups/ringring-20260822T140321Z-b9db361.tar.gz`, and was removed after the final guarded upgrade.
- Production is at exact runtime commit `8db1a792f42e377e39eeef2982f376a148a8b86c`. Final pre/post backups `/root/ringring-backups/ringring-20260822T140340Z-b9db361.tar.gz` and `/root/ringring-backups/ringring-20260822T140523Z-8db1a79.tar.gz` passed checksum, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration checks. The upgrade marker is absent, the checkout is clean, doctor and public readiness pass, both SIP jails are active, and there are zero contacts, channels, or calls.
- The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, or readiness records. Both root environment hashes and both empty generated-routing hashes are unchanged. The final app, Asterisk, and Caddy identities are `d64989a75cf4f1339c852fe20e96d71fbacfbb714fab9ed7c8ab1bc506dbaf5e`, `028000ce18d211c3559ccaed58822f5c41f662c5c78b773ce6809dff75bcf665`, and `7e9e8b6eafe32f480353a1d28b516db88ad56a1d675db8c02382c9c66ff3d60e`. No real family member/device/check, SIP credential, generated route, OpenAI resource, or family access phrase changed.

### Remaining

- Configure named physical ATAs, desk phones, and mobile softphones; complete the three checks across two real remote networks; and record device/firmware/transport results in the compatibility matrix.
- Verify Linphone foreground and push/background ringing plus Wi-Fi/cellular transitions on family phones.
- Complete the external child-safety and OpenAI Zero Data Retention gate before enabling AI for callers under 13, plus privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Trusted SIP TLS and compatibility evidence

### Shipped

- Added Asterisk PJSIP TLS 1.2 on public `5061/tcp` while preserving UDP `5060` for older adapters. Generated Linphone accounts and the one-time setup page now prefer TLS; the page names UDP as a fallback and explicitly distinguishes encrypted SIP signaling from the current unencrypted, server-relayed RTP media.
- Kept Caddy as the sole ACME owner. A hardened root systemd timer exports Caddy storage, safely selects only the exact deployment hostname's matching certificate/key with at least one week remaining, stages a narrow mode-`0600` copy, and asks Asterisk to reload PJSIP only when no calls are active. Initial startup can use a short self-signed fallback, but guided public verification rejects it.
- Extended the guided installer, upgrader, and read-only doctor with TLS directory safety, occupied-port checks, timer installation, required/deferred certificate synchronization, trusted public hostname verification, and separate Fail2Ban status for TCP `5061`. Added recovery and rollback guidance plus an evidence-based compatibility matrix that keeps physical-device claims open.
- Built SIPp 3.7.7 from its checksum-pinned official source with TLS support. The isolated call suite now holds a real TLS registration connection, verifies a disposable CA and exact DNS name, calls between TLS and UDP phones, checks PCMU media and `*10`, and completes the authenticated `*15` DTMF flow. The official Linphone engine separately enables certificate/name verification before TLS registration and its measured party call.

### Decisions

- Encrypt registration credentials and call setup by default for capable phones without pretending that TLS encrypts RTP. Keep UDP available until the actual family ATA/desk-phone matrix proves it can be retired.
- Export and validate one certificate pair instead of sharing Caddy's complete storage with Asterisk or running a second ACME client. Treat certificate copies as derived, replaceable state outside the backup boundary.
- Never interrupt an active call for certificate rotation. A reload deferral is a successful timer outcome, and the persistent timer retries later.

### Verification

- `make check` passes, including POSIX syntax, secret-safe guided-operation fixtures, certificate-sync fixtures, Go formatting/vet, and the complete race-enabled Go suite. Focused ShellCheck and `git diff --check` also pass.
- On an isolated server candidate, `make sip-smoke`, `make linphone-smoke`, and `make nat-smoke` pass. Evidence covers TLS 1.2 and UDP authentication, exact certificate/name verification, mixed-transport party calling, echoed/bidirectional audio, DTMF extension selection, and two distinct stateful household NAT identities.
- The real Caddy container's storage export passed the candidate synchronizer in test mode: it selected the exact `ringring.live` certificate, matched its private key, and preserved root-only mode without printing private material or reloading production Asterisk. `systemd-analyze verify` accepts the rendered service and timer. No live family, OpenAI, database, routing, firewall, certificate copy, or running service changed during candidate verification.
- GitHub's Ubuntu workflow passed feature run `32575100238` and operator-hardening runs `32575438301`, `32575635376`, and `32575735284`. Live deployment exposed two conditions that the isolated fixtures then made permanent: doctor now ignores only the exact expected pre-Asterisk DNS warning while still rejecting any other app warning/error, and certificate locking now survives simultaneous first syncs while the hardened unit receives only its dedicated writable `RuntimeDirectory`.
- Before deployment, `/root/ringring-backups/ringring-20260822T131427Z-325f2fc.tar.gz` passed its complete restore drill. The app, Asterisk, and Caddy containers were rebuilt/reconciled for the feature while Caddy retained its named storage; both SIP jails were active before the new listener started. The exact `ringring.live` certificate synchronized and loaded, public TLS 1.2 hostname/system-trust verification passed from outside the server, and the real systemd timer service completed successfully through its sandbox with no residual lock.
- Production is at exact tip `9e6dfd10af6bc25e0e0828ea83cb685b4144f4dc`. The sealed aggregate remains one user, one party, one invitation, one session, eight recovery codes, one decryptable party key, and zero members, devices, provisioning tokens, contacts, channels, or calls. Both root environment hashes and both generated routing hashes match their pre-deploy values. Final backup `/root/ringring-backups/ringring-20260822T132701Z-9e6dfd1.tar.gz` passed checksum, safe extraction, SQLite integrity/foreign keys, credential decryption, isolated readiness, and telephony-regeneration checks; production doctor passes afterward.

### Remaining

- Test TLS registration, incoming/outgoing calls, certificate renewal behavior, and two-way audio on named physical ATAs, desk phones, and mobile softphones across remote networks.
- Verify Linphone camera import, foreground and push/background ringing, and Wi-Fi/cellular transitions on family phones.
- Add SRTP only after a device-support and key-management design; until then, keep the unencrypted-RTP boundary prominent.
- Complete the external child-safety/ZDR gate, privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Guided self-host installation and verified upgrades

### Shipped

- Added root-level `ringringctl install`, `upgrade`, and `doctor` commands. A clean Debian/Ubuntu host can now generate its state directories and independent application/session/AMI keys, render service-specific root-only environments, build the complete Compose stack, and prove app, Asterisk, AMI, state, Fail2Ban, public readiness, and settled app logs through one guided flow.
- Added a documented root-only answers template plus hidden interactive secret prompts. The command accepts no secret flags, validates exact known keys and safe value shapes, refuses loose files, symlinks, occupied SIP/RTP/web ports, dirty checkouts, unpublished source commits, mismatched deployed configuration, and accidental fresh-install overwrites.
- Put only the non-secret deployment domain in the checkout's private Compose `.env`, keeping Caddy's substitution environment separate from OpenAI, encryption, session, family-access, and AMI credentials. Fresh install creates the Asterisk security log and installs the checked-in Fail2Ban jail before starting the public SIP listener.
- Added exact fast-forward upgrades with drilled pre- and post-upgrade backups, a root-only old/target/backup marker, immutable-controller execution across its own Git replacement, full-stack reconciliation, firewall-policy refresh, and exact retry after interruption. Added read-only `doctor`, guided/manual deployment and recovery documentation, and a narrow `make admin-test` target.

### Decisions

- Support clean Debian/Ubuntu servers first because the existing SIP firewall installer uses `apt`; Docker installation, DNS, SSH policy, and provider firewalls remain explicit operator prerequisites instead of partially managed host state.
- Keep secrets out of shell history, process listings, Git, Compose's Caddy environment, pending markers, and normal output. An interrupted install must reuse the generated configuration rather than accept replacement answers, while an interrupted upgrade must reuse its exact target and already-drilled backup.
- Never automate rollback across a forward-only database migration. A failed upgrade keeps the known recovery point and resumes forward only after the operator fixes the reported condition.

### Verification

- The isolated POSIX fixture suite covers a complete install, exact environment rendering and permissions, independent 32-byte keys, AMI-secret agreement, public/private checks, firewall-before-listener ordering, doctor, overwrite refusal, no-mutation dry runs, invalid permissions/domain/access code, symlink refusal, build failure and secret-preserving install resume, exact fast-forward upgrade, two backups and drills, firewall refresh, controller replacement during execution, no-mutation upgrade dry run, and a failed/resumed upgrade that does not repeat its pre-backup.
- `sh -n`, focused ShellCheck, `git diff --check`, and `make check` pass. The full check includes the new fixtures, Go formatting and vet, and the complete race-enabled Go suite.
- The first public CI run exposed a platform-dependent bare-repository default branch in the upgrade fixture before any product step ran. Pinning that fixture to `main` made it deterministic across Mac and Linux; `make check` then passed again locally and on exact release tip `649bc1b` in GitHub Actions run `32568205300`.
- This operator-tool release did not read or change the live deployment's environment, family records, OpenAI resources, containers, firewall, database, or generated phone routing.

### Remaining

- Complete the physical-device usability gate: configure real family ATAs/phones, call across two remote networks, and verify foreground/background ringing plus Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for any caller under 13.
- Add a SIP TLS device matrix, privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Web accessibility and simulated mobile usability

### Shipped

- Added a keyboard-first skip link, a consistent high-contrast three-pixel focus ring, and 44px minimum standalone targets across navigation, account links, compact management actions, disclosure controls, and setup links. Checkbox controls retain their native visual size but expose their full 76px-or-taller labeled card as the hit area.
- Replaced the automatically moving highlights ticker with a static semantic list, hid decorative emoji and art from assistive technology where appropriate, and retained meaningful image/status labels. Converted the manual SIP credentials into a definition list so each one-time value has an explicit term/value relationship.
- Added visible, programmatically associated instructions for usernames, recovery codes, extensions, invitation URLs, Linphone provisioning URLs, and OpenAI spend amounts. Authentication errors now identify only the relevant fields with `aria-invalid` and shared descriptions while keeping the existing live alert.
- Tightened the playful palette to meet measured WCAG contrast, including coral heading/accent variants, muted copy, and white copy on the purple service section. The mobile forms retain 16px inputs, clear required/optional guidance, and one-column reflow without changing the no-JavaScript core flow or exposing a SIP secret to the clipboard.

### Decisions

- Use WCAG 2.2 AA as the web baseline while preserving RingRing's stricter 44px project target. The current W3C AA target-size minimum is smaller, but family phone setup benefits from the larger hit areas already promised in `AGENTS.md`.
- Remove nonessential automatic motion instead of adding a pause control to a decorative marketing strip. Respecting reduced-motion preferences remains useful for transitions, but it is not a substitute for a generally operable pause/stop mechanism.
- Keep invitation and provisioning links in labeled read-only fields and manual credentials in semantic HTML without JavaScript copy buttons. The pages still work under the existing `script-src 'none'` policy and avoid silently putting one-time credentials on a persistent system clipboard.
- Split the roadmap's combined accessibility/device item. The web and simulated mobile pass is complete; actual ATA, desk-phone, mobile-background, Wi-Fi/cellular, and two-household experience evidence remains a separate physical-device gate.

### Verification

- Added embedded-template regression tests for the skip/main relationship, language, visible focus/touch contract, decorative-icon treatment, associated field guidance, semantic setup credentials, the static highlights strip, and calculated WCAG contrast ratios for the core palette.
- `make check` passes formatting, vet, and the complete race-enabled suite. GitHub Actions passed exact feature commit `1995bdb` in run `32566732328`. The full local browser flow created disposable host/party state, issued an invite, claimed a neutral test member, and audited the one-time setup without printing or screenshotting its token or credentials.
- Browser QA passed at 320×568, 390×844, and 1280×900 with no horizontal overflow. Standalone controls measure at least 44px high; form fields are 52px with 16px text; checkbox card targets measure 81–99px; the skip link measures 48px and shows a solid 3px focus outline with 3px offset; the corrected purple-section text measures 5.1:1. The setup card exposes five definition pairs and the invite/provisioning fields each have a visible associated label and description.
- The final browser run had no console errors. Its tab and viewport override were closed/reset, and the disposable local server/database were stopped and moved to Trash. No production state, real family record, external provider, or physical phone was used.

### Production

- Created `/root/ringring-backups/ringring-20260822T100726Z-ab30ae8.tar.gz` before deployment and passed its checksummed isolated restore drill. An exact `1995bdb` image then ran network-disabled against the extracted sealed copy with provider credentials disabled; readiness, the new accessibility assets, clean logs/shutdown, unchanged verified state, and byte-identical telephony regeneration all passed. The candidate image, checkout, container, and extracted state were removed.
- Deployed exact commit `1995bdb` by rebuilding and recreating only the app container. Asterisk and Caddy identities, both root-readable environment hashes, both generated routing hashes, the sealed one-host/one-party aggregate, zero contacts/channels, and the SIP Fail2Ban jail remained exact. Public readiness, signup, skip/main landmarks, static highlights, focus CSS, palette tokens, security headers, and settled logs pass.
- Created `/root/ringring-backups/ringring-20260822T101017Z-1995bdb.tar.gz` after deployment and passed its full isolated restore drill. Its sealed verification report and both environment hashes match the pre-deploy archive exactly; the live app and Asterisk are healthy, and no candidate or restore runtime remains.

### Remaining

- Complete the physical-device usability gate: configure real family ATAs/phones, call across two remote networks, and verify foreground/background ringing plus Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for any caller under 13.
- Add guided installation/upgrades, a SIP TLS device matrix, privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Host-set hard monthly AI spend limits

### Shipped

- Added a bright **Monthly AI guardrail** to each provisioned party. A host can choose any exact USD-cent amount from `$0.01` through the deployment's `OPENAI_PARTY_SPEND_LIMIT_CENTS` ceiling; that setting remains the new-party default as well as the operator cap.
- Upgraded automatic provisioning and later host changes to require OpenAI's exact `project.spend_limit` response: requested cents, USD, monthly interval, and `enforcing` status must all match before RingRing calls the amount confirmed.
- Added three forward-only party columns for the last confirmed amount, one immutable pending amount, and reconciliation status. Existing parties migrate to local `unknown` without contacting OpenAI or changing routing; their first host save explicitly verifies or replaces the selected provider limit.
- Paused new `*12` weather and `*14` conversation authorizations in both the generated routes and the live voice handlers while a limit result is uncertain. The host can retry the same pending amount, but a concurrent or tampered request cannot substitute another value.

### Decisions

- Treat an Admin API timeout, inactive or mismatched response, restart, or failed local completion as an unknown provider result rather than assuming success or failure. Record the pending amount before the external call, keep the prior confirmed amount for display, and resume with an idempotent post of that exact pending amount.
- Mirror `spend-updating` and `spend-update-error` into the legacy `openai_status` field. An older binary therefore keeps AI unavailable after rollback instead of ignoring the newer reconciliation columns; only this release can finish the update and restore `ready`.
- Make spend reconciliation and party-key replacement mutually exclusive. Both are external provider lifecycles with their own retry state, so RingRing never overlaps them or exposes provider IDs, administrator credentials, runtime keys, or internal status names in the browser.
- Keep provider changes host-initiated. Migration, startup, health checks, backup drills, and deployment verification never call the OpenAI Admin API or submit the real party control.

### Verification

- OpenAI client tests cover the exact request body, provision-time handoff, active enforcement, wrong amounts/currencies/intervals/object types, inactive enforcement, missing configuration, and zero values. Store tests cover additive legacy migration, host/project scope, immutable pending amounts, concurrent changes, failure/retry/completion, legacy-safe status mirroring, and route pause/resumption.
- The full authenticated web flow covers CSRF, exact decimal parsing, the operator ceiling, an ambiguous provider failure, private retry UI, key-rotation exclusion, a tampered retry amount, provider idempotence, and no identifier/secret/internal-state disclosure. Voice tests prove cached weather or disclosure audio cannot bypass a pending spend pause.
- `make check` passes formatting, vet, and the complete race-enabled suite. GitHub Actions passed exact feature commit `ff88460` in run `32565230972`.
- Browser QA covered both ready and fail-closed states at 390×844 and 1280×900. The mobile amount field measures 264×56px, save/retry actions measure 264×54px, the input uses a 16px font and visible focus ring, AI-powered toggles disable during retry, and neither viewport overflows horizontally. The local app, disposable database, tab, viewport override, ports, and test credentials were removed or reset; no provider form was submitted.

### Production

- Created `/root/ringring-backups/ringring-20260822T093322Z-6e50283.tar.gz` before the schema change and passed its full isolated restore drill. A network-disabled image of exact commit `ff88460` migrated an extracted copy with the administrator key explicitly blank, preserved byte-identical generated phone routing and both environment copies, stopped cleanly without warnings/errors, and matched the sealed integrity, foreign-key, credential-decryption, and aggregate family-state report. All candidate resources were removed.
- Deployed exact commit `ff88460` by rebuilding and recreating only the app container. Asterisk and Caddy identities, both root-readable environment hashes, both generated routing hashes, and every pre-existing database value remained exact. The existing party has zero locally asserted spend, no pending update, and honest `unknown` verification state; no OpenAI project, key, or limit was read or mutated.
- Public readiness and spend-control styling, native signup, container health, private aggregate-only Asterisk checks, the SIP Fail2Ban jail, and settled logs pass. Created `/root/ringring-backups/ringring-20260822T094018Z-ff88460.tar.gz` after deployment and passed its full restore drill; the sealed report and both environment files match the pre-deploy archive exactly, and no candidate/restore runtime remains.

### Remaining

- Complete the accessibility and real-device usability pass, then scan and call with family phones across two remote networks and verify mobile background ringing and Wi-Fi/cellular transitions.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for any caller under 13.
- Add guided installation/upgrades, a SIP TLS device matrix, privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Curated party radio stations

### Shipped

- Replaced the single fixed `*13` choice with a bright host selector for Groove Salad, Drone Zone, or Deep Space One. Each party persists one short catalog ID and keeps Groove Salad as the backward-compatible default.
- Added one code-controlled radio catalog shared by the host UI, storage validation, backup verification, telephony renderer, and a new `make radio-smoke` compatibility command. Unknown or empty persisted values fail before reconciliation; an arbitrary host URL can never become a player request or dialplan argument.
- Added a forward-only `party_services.radio_station` migration, rollback notes, external-content and personal-listening guidance, official station-source links, and a provider-support link. The UI describes the stations as instrumental-leaning without claiming that live programming is moderated or guaranteed child-appropriate.

### Decisions

- Limit the catalog to three official SomaFM direct-player MP3 endpoints whose programming leans ambient and instrumental. SomaFM documents these links for individual personal use, which matches the private family-listening scope; RingRing does not expose a rebroadcast or public-listening feature.
- Store only stable station IDs. Both the store and renderer resolve those IDs independently so direct database corruption still cannot inject a URL, metadata address, redirect, header, credential, or Asterisk syntax.
- Keep HTTP only at the final public audio hop because the production Asterisk `MP3Player` delegates to `mpg123`, which does not open these streams over TLS. The request contains no caller identity, party data, SIP credential, or integration key.

### Verification

- Catalog tests require unique safe IDs, the exact SomaFM host and MP3 path shape, HTTPS source pages, immutable copies, and a valid default. Store, migration, backup, renderer, and web tests cover legacy defaulting, host scope, party-specific routing, unknown/empty/corrupt state, arbitrary URL rejection, selected-state rendering, and unchanged settings after a bad request.
- `make check` passes formatting, vet, and the complete race-enabled suite; ten race-enabled repetitions of the changed packages pass. GitHub Actions passed feature commit `3183669`.
- The Mac Docker daemon was unavailable for the external target. An isolated deployment-host checkout of the exact commit then passed `make radio-smoke`: all three catalog entries delivered decodable MPEG Layer III audio for five seconds in the production Asterisk image. The exact test images and checkout were removed.
- A disposable browser party round-tripped Deep Space One and the enabled state. At 390×844 and 1280×900, the selector measured 301–389×52px, the save button was 54px tall, the mobile layout used one column, and neither viewport overflowed horizontally. The visible content boundary remained below the selector; tabs, viewport override, app, ports, and disposable database were reset or removed.

### Production

- Created `/root/ringring-backups/ringring-20260822T090716Z-17ae8b1.tar.gz` before the schema change and passed its checksummed isolated restore drill. A network-disabled image of exact commit `3183669` migrated an extracted copy, became ready, regenerated telephony state, stopped cleanly, validated the catalog, and produced the exact sealed integrity, foreign-key, credential-decryption, and aggregate family-state report; all candidate resources were removed.
- Deployed exact commit `3183669` by rebuilding and recreating only the app container. Asterisk and Caddy identities, both root-readable environment-file hashes, and both generated phone-routing files remained exact; no production member, device, extension, SIP credential, party key, or external resource changed.
- Public readiness and radio styling, the exact three-entry runtime catalog, private aggregate-only AMI verification, the SIP Fail2Ban jail, container health, and settled logs pass. Created `/root/ringring-backups/ringring-20260822T090914Z-3183669.tar.gz` after deployment and passed its full restore drill; its sealed family report and both environment files match the pre-deploy archive exactly.

### Remaining

- Complete the accessibility and real-device usability pass, then scan and call with family phones across two remote networks and verify mobile background ringing and Wi-Fi/cellular transitions.
- Let hosts choose a bounded OpenAI project spend limit; complete the external child-safety review and confirm Zero Data Retention before enabling AI for any caller under 13.
- Add guided installation/upgrades, a SIP TLS device matrix, privacy-preserving observability, further threat-model tests, and the optional PostgreSQL path.

## 2026-08-22 — Voice-guided extension selection

### Shipped

- Added an always-available `*15` party route. An authenticated phone can enter a new 2–5 digit extension, hear it read back, and press `1` to save without changing its SIP username or encrypted password.
- Bound the request to Asterisk's authenticated PJSIP endpoint and the exact server-selected party. The store performs one scoped update, rejects unknown or revoked devices and cross-party access, and lets the party uniqueness constraint resolve occupied or concurrent choices safely.
- Serialized telephony reconciliation and reload after a selection. The database remains authoritative if reload fails, startup reconciliation repairs generated state, and the caller receives only generic retry guidance for invalid or unavailable choices.
- Added colorful `*15` guidance to the splash, setup screen, and host party page. Upgraded `make sip-smoke` to seed a disposable RingRing database and prove the complete DTMF selection and live Asterisk reload path.

### Decisions

- Trust `CHANNEL(endpoint)` from the authenticated PJSIP channel, never caller ID or a caller-supplied member identifier. Pass the exact party ID into the private FastAGI route so a phone cannot escape its party context.
- Use Asterisk's bundled prompts and RFC4733 DTMF instead of speech recognition. The small confirm-and-save flow is predictable on ATAs and desk phones, adds no provider dependency, and never records or transcribes a child's voice.
- Keep the SIP credential stable when the extension changes. This makes voice selection useful after a phone is already registered and avoids forcing another provisioning or password-sharing step.

### Verification

- Store, renderer, FastAGI, and web tests cover validation, occupied and concurrent choices, successful changes, revoked/unknown/cross-party devices, exact endpoint identity, confirmation retries, reload failure, credential preservation, and the new guidance. `make check` and ten race-enabled repetitions of the changed packages pass.
- Two isolated runs of exact commit `84c3b83` passed authenticated registration, a member call, exact PCMU media, `*10`, the `*15` DTMF exchange, extension `101` to `103` state change, unchanged credentials and peer extension, generated-config regeneration, live dialplan reload, removal of the old route, clean logs, and zero remaining channels. Test containers, networks, candidate images, and state were removed.
- GitHub Actions passed commit `84c3b83`. Real-browser checks at 390×844 and 1280×900 found no horizontal overflow, readable chooser cards, and appropriately sized controls; the disposable app, fixture, database, browser tabs, and viewport override were removed or reset.

### Production

- Created `/root/ringring-backups/ringring-20260822T084240Z-e60c358.tar.gz` before deployment and passed its checksummed isolated restore drill. Fast-forwarded to exact commit `84c3b83`, rebuilt, and recreated only the app container; Asterisk and Caddy identities and both root-readable environment-file hashes remained exact.
- Public readiness and chooser copy, private aggregate-only AMI verification, the SIP Fail2Ban jail, container health, and settled logs pass. With no production members or devices, both generated telephony files remained exact and no family extension or SIP credential changed.
- Created `/root/ringring-backups/ringring-20260822T084344Z-84c3b83.tar.gz` after deployment and passed its full isolated restore drill. Its sealed report matches the pre-deploy family counts and confirms integrity, foreign keys, and credential decryption; the live app returned healthy after the snapshot.

### Remaining

- Scan and call with real family phones across two remote networks; verify mobile background ringing and Wi-Fi/cellular transitions.
- Choose and vet an internet-radio source, complete the accessibility/device pass, and add a guided install flow for common ATAs and phones.
- Let hosts choose a bounded project spend limit, then complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for any caller under 13.

## 2026-08-22 — Immediate party OpenAI key replacement

### Shipped

- Added a host-only **Party AI key** control that creates a fresh key on the party's dedicated OpenAI service account, installs its encrypted value, and revokes every older active key owned by that account. No key value or provider identifier reaches the browser.
- Added a nullable provider key ID to party state with a forward-only startup migration. New parties retain the initial service-account key ID; existing parties acquire one on their first replacement without losing their current runtime key.
- Added an explicit `rotating`/`rotation-error` state that pauses AI-powered routing until the fresh key appears in the provider's active-key list and every older key retirement succeeds. A visible **Finish key replacement** action resumes cleanup without minting another key.

### Decisions

- Use OpenAI's service-account API-key creation endpoint and the project-key list/delete endpoints instead of replacing the project or service account. Filter list results by the exact dedicated service-account owner and keep only the newly persisted key.
- Install the fresh encrypted key with a compare-and-swap against the prior key ID. A concurrent request loses safely and cleans up its unclaimed key; a timeout, restart, invalid provider response, or partial deletion leaves AI lines paused and the same rotation retryable.
- Treat key rotation as a deliberate host security action, never a deployment health probe. Production rollout may migrate and display the control but must not submit it against a real family party.

### Verification

- OpenAI client tests cover one-time key creation, owner filtering, pagination, deletion confirmation, retry-safe missing-key deletion, malformed credentials, and invalid pagination. Store tests cover the additive legacy migration, host scope, stale/concurrent compare-and-swap rejection, encrypted-key installation, routing pause, error transition, retry, and completion.
- The full browser flow covers CSRF rejection, partial external failure, encrypted fresh-key persistence, no key or internal-state disclosure, paused dashboard/party states, retry without a second key, old-key retirement, route resumption, and cross-host denial.
- `make check` passes formatting, vet, and the complete race-enabled suite. At 390×844 and 1280×900, ready and retry controls have no horizontal overflow, use 54–78px buttons, retain friendly status labels, and expose no provider key identifier or runtime secret; the disposable browser, app, database, and viewport override were removed or reset.
- GitHub Actions passed feature commit `501a47e`. A network-disabled image of that exact commit migrated an extracted production backup, became ready, generated telephony state, stopped cleanly, and matched the sealed integrity, foreign-key, credential-decryption, and family-count report. The new key-ID field remained empty, both copied environment files remained exact, and all candidate resources were removed.

### Production

- Created `/root/ringring-backups/ringring-20260822T075725Z-8a1a8ab.tar.gz` before the schema change and passed its checksum, safe-extraction, integrity, foreign-key, credential-decryption, readiness, and telephony-regeneration drill with the pre-upgrade image.
- Deployed exact feature commit `501a47e` by rebuilding and recreating only the app container. Asterisk and Caddy identities, both root-readable environment files, generated PJSIP/dialplan files, and a content hash over every pre-existing family-state column remained exact. The nullable migration exists and is empty; no production party key or OpenAI resource was created, replaced, listed, or revoked.
- Public readiness and signup, the key-control stylesheet, private aggregate-only AMI verification, the SIP Fail2Ban jail, and new app logs pass. Created `/root/ringring-backups/ringring-20260822T080332Z-501a47e.tar.gz` after deployment and passed its full isolated restore drill; the restarted live app remains ready and no restore runtime remains.

### Remaining

- Let hosts choose a bounded project spend limit and verify the provider enforcement lifecycle.
- Complete the external child-safety review and confirm OpenAI Zero Data Retention before enabling AI for any caller under 13.
- Scan and call with real family phones across two remote networks; verify mobile background ringing and Wi-Fi/cellular transitions.

## 2026-08-22 — Two-household NAT interoperability gate

### Shipped

- Added `make nat-smoke`, an isolated call test that runs two authenticated SIPp phones in separate private Linux network namespaces with distinct public SNAT identities and an Asterisk instance rendered from RingRing's production telephony code.
- The clients intentionally advertise unreachable private SIP contacts and media addresses. The harness requires Asterisk to retain the rewritten public contacts, route extension `101` to `102` inside the generated party context, keep `direct_media=no`, and finish with no active channels.
- Added independent raw PCMU sources and router-boundary counters. Each phone must send and receive more than 100 non-SIP UDP media packets across its own NAT path before the test can pass.

### Decisions

- Keep exact audio-content evidence in the existing SIPp echo and official Linphone recording suites. The NAT gate measures the distinct invariant that signaling and media cross both translation boundaries in both directions.
- Use nested namespaces in one privileged topology container so the private paths and SNAT rules are explicit and observable. The container receives no Docker socket, host network, public port, deployment environment, production state, or family credential and is destroyed on exit.
- Treat simulated NAT as a stronger software gate, not as completion of the real-device milestone. Household routers, carrier-grade NAT, mobile handoff, ATA behavior, and background ringing remain hardware checks.

### Verification

- Two consecutive isolated server-candidate runs passed two authenticated registrations, distinct rewritten contacts, the real generated party route, bidirectional PCMU across all four NAT directions, clean SIP teardown, and zero remaining Asterisk channels.
- The existing `make sip-smoke` still passes two authenticated registrations, exact echoed PCMU patterns, extension calling, and `*10`; `make linphone-smoke` still passes official-engine provisioning, registration, party calling, packet checks, and recorded-tone analysis against the same candidate.
- `make check`, POSIX shell syntax validation, XML parsing, and `git diff --check` pass locally.
- Every failed development iteration and both passing runs removed the exact test containers, nested namespaces, Docker network, generated Asterisk state, SIPp state, and audio files; no production service or state was mounted or changed.

### Production

- GitHub Actions passed feature commit `1010c22`. Fast-forwarded the production checkout to that commit without rebuilding or recreating the app, Asterisk, or Caddy; all three container identities and both root-readable environment files remained exact.
- Public readiness, private aggregate-only AMI verification, and the SIP Fail2Ban jail pass after the checkout update. No production account, party, invitation, member, device, credential, OpenAI project, database, or running service changed.

### Remaining

- Scan a production setup QR in the current Linphone mobile app and verify its user-visible import flow, foreground ringing, push/background behavior, and Wi-Fi/cellular transitions.
- Complete a two-way-audio call between two remote physical devices across real family networks.

## 2026-08-22 — Official Linphone party-call interoperability

### Shipped

- Extended `make linphone-smoke` from provisioning and registration into an actual party call: the officially provisioned Linphone account at extension `101` now calls a disposable SIPp member at extension `102` through RingRing's production telephony renderer and generated party dialplan.
- Added deterministic file-backed media to the headless client. Linphone plays a 440 Hz linear WAV, records the RTP returned through Asterisk and SIPp, and requires an established call, more than 100 packets in each direction, an allowed codec, and the expected tone in the recording.
- Added the second disposable account and a two-phase SIPp receiver so Asterisk's contact qualification is isolated from the measured call. Parameterized the shared smoke transport's advertised signaling and media address so each isolated Docker network receives correct in-dialog routing.

### Decisions

- Keep media server-relayed with the same `direct_media=no` production setting, so the audio assertion exercises Asterisk rather than a direct synthetic client path.
- Require independent evidence at each boundary: Liblinphone validates its imported account, registration state, call state, codec, packet counters, and recording; SIPp answers the second member leg and echoes RTP; Asterisk confirms both contacts and a clean zero-channel teardown.
- Keep the official GPLv3 client test-only and isolated from the MIT application, live database, production environment, public ports, and family credentials.

### Verification

- An isolated server candidate passed the extended `make linphone-smoke`: exactly one generated XML fetch, one imported account, authenticated registration, two reachable party contacts, an extension `101` to `102` call, and echoed bidirectional audio.
- The existing `make sip-smoke` still passes two authenticated SIPp registrations, extension calling, the `*10` echo route, and bidirectional PCMU after the shared transport-address correction.
- `make check`, Python bytecode compilation, POSIX shell syntax checks, and `git diff --check` pass locally.

### Production

- GitHub Actions passed feature commit `aaba925`. Fast-forwarded the production checkout to that commit without rebuilding or recreating the app, Asterisk, or Caddy; all three container identities and both root-readable environment files remained exact.
- Public readiness, private aggregate-only AMI verification, and the SIP Fail2Ban jail pass after the checkout update. No production account, party, invitation, member, device, credential, OpenAI project, database, or running service changed.

### Remaining

- Scan a production setup QR in the current Linphone mobile app and verify its user-visible import flow, foreground ringing, push/background behavior, and Wi-Fi/cellular transitions.
- Complete a two-way-audio call between two remote physical devices across real family networks.

## 2026-08-22 — Linphone provisioning interoperability

### Shipped

- Added an opt-in `make linphone-smoke` harness that packages the current official Linux Linphone Python wheel in a separate test-only image with both the Ubuntu base digest and wheel SHA-256 pinned.
- Added a dedicated disposable configuration helper so the existing RingRing telephony renderer and production Linphone XML generator create matching Asterisk and client state from one fixed smoke account without adding network dependencies to the lighter SIPp harness.
- Added a small headless client that serves the generated XML over local HTTP, asks Liblinphone to provision it, iterates the real client engine through registration, and exposes only a success marker for the outer test to verify.

### Decisions

- Keep the large GPLv3 Linphone SDK outside the MIT-licensed RingRing application and production images. It is downloaded only when the explicit interoperability target is built.
- Require two independent assertions: Liblinphone must report exactly one fetch, one account, and successful registration, while Asterisk must separately report that the contact is registered and reachable.
- Keep the runtime path isolated from the live service. It uses a dedicated internal Docker network, fixed checked-in smoke credentials, no production environment or database, and no published host ports; exact-name collision checks and the exit trap remove generated client/server state, containers, and the network.

### Verification

- The official `linphone-5.5.3.post5+git.55b84bc556` wheel passed its pinned checksum and loaded in the pinned Ubuntu runtime with only the declared system libraries.
- An isolated server candidate passed the full target: Linphone fetched RingRing's generated XML exactly once, created exactly one SIP account, authenticated it to disposable Asterisk, and Asterisk marked that contact reachable. The harness then shut Linphone down cleanly and removed its runtime resources.
- The existing `make sip-smoke` harness still passes two authenticated SIPp registrations, an extension call, the `*10` echo route, and bidirectional PCMU after the new helper was kept separate from its network-disabled renderer.
- `make check`, Python bytecode compilation, POSIX shell syntax checks, and `git diff --check` pass locally.

### Production

- GitHub Actions passed feature commit `67444f5`. Fast-forwarded the production checkout to that commit without rebuilding or recreating the app, Asterisk, or Caddy; all three container identities and both root-readable environment files remained exact.
- Public readiness, private aggregate-only AMI verification, and the SIP Fail2Ban jail pass after the checkout update. No production account, party, invitation, device, credential, OpenAI project, database, or running service changed.

### Remaining

- Scan a production setup QR in the current Linphone mobile app and verify its user-visible import flow, foreground ringing, push/background behavior, and Wi-Fi/cellular transitions.
- Complete a two-way-audio call between two remote physical devices across real family networks.

## 2026-08-22 — One-time Linphone QR setup

### Shipped

- Added a bright Linphone card to both invitation-claim and host-rotation setup screens while preserving the universal manual settings for ATAs, desk phones, and other softphones.
- Added locally rendered QR images, a documented desktop `sip-linphone` action, and a copyable remote-provisioning URL. The XML follows Linphone's transient configuration format, uses its current bracketed proxy syntax, and marks every changed entry for overwrite.
- Added an additive provisioning-token table and transactional lifecycle: a 32-byte random token is stored only as a hash, expires after 30 minutes, consumes once, is atomically replaced on rotation, and disappears on revocation or device deletion. Backup verification now checks and counts the new table.
- Added the upstream MIT notices for the QR encoder and Reed–Solomon implementation to the repository and final app image.

### Decisions

- Put only an HTTPS provisioning URL in the QR, never the SIP password itself, and render it without an external QR service. The first real `GET` decrypts only that device credential and returns no member name, party name, host data, or integration key.
- Reject `HEAD` without consuming the link, but make every real fetch one-use. This avoids burning a setup on a preview probe without weakening the bearer-token boundary.
- Keep manual setup first-class and call the feature Linphone-specific. A generic SIP QR is not interoperable, and sending credentials through an OEM provisioning service would create an unnecessary third-party boundary.
- Warn people to use Linphone's scanner instead of a normal camera/browser. Setup and XML responses are no-store, no-referrer, no-index, same-origin resources; token paths are masked in logs and separately rate limited.

### Verification

- Store tests cover hashed-at-rest tokens, unknown, used, expired, replaced, revoked, and cascading lifecycle states. Web tests cover safe headers, non-consuming `HEAD`, exact XML account data, exclusion of unrelated private data, generic invalid-token errors, one-use behavior, rotation replacement, revocation, path masking, and rate limiting.
- `make check` passes formatting, vet, and the complete race-enabled suite. The official Go vulnerability scanner finds no reachable vulnerability in RingRing or any imported package; its only module-level notice is for the unused `x/crypto/openpgp` package.
- A disposable real-browser flow claimed and rotated a phone, rendered the page at 1280×900 and 390×844, and found no horizontal overflow. The QR remained crisp, controls measured 44–54px, and the mobile order was corrected so the scanner warning appears before the code.
- macOS Vision decoded exactly one QR and its payload digest matched the setup page's provisioning URL. The browser tab, explicit viewport, temporary QR artifacts, app process, and disposable database were closed or removed; no production family state was touched.

### Production

- GitHub Actions passed commit `0c60d7e`. Before deployment, created `/root/ringring-backups/ringring-20260822T061218Z-9d58bae.tar.gz` and passed its checksum, credential-decryption, readiness, and telephony restore drill with the pre-upgrade image.
- Built the exact feature commit in a temporary server checkout and started it against an extracted copy of that backup with no network, admin key, Google credentials, AMI secret, or public ports. The additive migration, license notices, app readiness, telephony regeneration, integrity, foreign keys, one decryptable party key, and unchanged one-host/one-party counts passed; the candidate container, image, checkout, and extracted state were removed automatically.
- Deployed only the rebuilt app container. Assertions confirmed that Asterisk and Caddy were not recreated and both root-readable deployment environment files were unchanged. Live readiness, private AMI access, masked/error-free app logs, the SIP Fail2Ban jail, the Linphone stylesheet, non-consuming provisioning `HEAD`, generic unknown-token `GET`, and every provisioning privacy header pass.
- Created `/root/ringring-backups/ringring-20260822T061607Z-0c60d7e.tar.gz` after deployment and passed its full isolated restore drill. Its sealed report confirms integrity, foreign keys, one user, one party, no members or devices, one invitation, one session, eight recovery codes, no outstanding provisioning tokens, one decryptable party key, and no device secret; both environment files match the pre-deploy archive exactly.

### Remaining

- Import a live production setup into current Linphone on a real phone and verify registration, foreground ringing, and the operating system's background-call behavior.
- Complete the broader two-remote-device audio test with family hardware.

## 2026-08-22 — Memorable family access phrase

### Production

- Rotated the live family access code to a short, chat-friendly two-word phrase and copied it directly to the operator's local clipboard instead of writing it into the repository or command output.
- Retired the prior phrase after finding it named in the public worklog, removed that value from the current documentation, and retained a root-only copy of the previous environment for operational rollback.
- Recreated only the app container, confirmed its configured value matches the root-only environment, and verified that the public signup page returns `200`. No account, party, member, device, invitation, or OpenAI project was created or changed.

## 2026-08-22 — Honest host-only phone presence

### Shipped

- Replaced the host directory's static “ready” claim with live per-device states: online, checking, not reachable, not registered, status unavailable, or deliberately disconnected.
- Added a bounded private AMI client for Asterisk's `PJSIPShowContacts` event list. It keeps only each generated endpoint identifier and normalized status, rejects unsafe identifiers and oversized/incomplete responses, and preserves the existing reload behavior.
- Added a safe `ringring verify-ami` operator command that reports only success and the aggregate contact count.
- Added text labels, accessible aggregate status descriptions, distinct color hints, and a compact availability notice that keep the host controls usable if Asterisk cannot answer.

### Decisions

- Keep presence host-only and ephemeral. The query runs only after party ownership is verified; contact URIs, IP addresses, ports, call IDs, and user agents are discarded before the web layer and nothing is persisted.
- Treat registration and reachability as different facts. A missing contact is “not registered,” an Asterisk-qualified failure is “not reachable,” and an initial unknown status is “checking.” Revocation always wins and displays “disconnected.”
- Fail open for administration but closed for claims: a two-second timeout or protocol error yields “status unavailable,” never a false offline/online result and never a failed party page.

### Verification

- Fake AMI protocol tests cover authenticated login, complete and Asterisk-style empty contact lists, duplicate-contact precedence, known and future statuses, unsafe endpoint rejection, permission errors without secret disclosure, incomplete lists, canceled contexts, line-injection rejection, and the existing reload action.
- Authenticated web tests cover every rendered state, explicit AMI failure fallback, revocation overriding a previously online contact, accessible member labels, and denial before any presence query for another signed-in host.
- `make check` passes with formatting, vet, and the full race-enabled suite. A disposable browser flow created and claimed a phone, then verified the unavailable-state directory at desktop and 390×844 widths with no horizontal overflow; its browser tab and all temporary state were removed.

### Production

- GitHub Actions passed the feature commit and its Asterisk compatibility follow-up. The first aggregate-only production probe proved the manager login and action registry but exposed Asterisk's documented implementation behavior: zero contacts returns an error-shaped “No Contacts found” frame instead of a successful empty list. RingRing now accepts only that exact case as empty; unrelated manager errors still fail closed.
- Deployed the corrected app without recreating Asterisk or Caddy. The real private AMI probe passes with zero contacts, matching the untouched production registrar and database; app, Asterisk, Caddy, public readiness, SIP transport, and the SIP Fail2Ban jail are healthy, and settled app logs contain no warnings or errors.
- Created verified root-only backups immediately before and after rollout. Their sealed reports and both deployment environment files match exactly: one user, one party, no members or devices, one unclaimed invitation, one session, eight recovery codes, and one decryptable party key. No access code, family state, credential, or OpenAI project changed.

### Remaining

- Register real remote hardware so the live dashboard can exercise reachable and unreachable transitions.

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
- A clean candidate checkout on the deployment Docker host passed the extended harness: two challenged registrations, the `101` to `102` extension call, the single-phone `*10` call, and matching bidirectional PCMU patterns. The harness removed its exact containers, network, generated state, and temporary checkout.

### Production

- GitHub Actions passed commit `a73bc75`. Deployed only the rebuilt app container from the clean production checkout, leaving Asterisk and Caddy running; startup telephony reconciliation completed without a warning.
- Public readiness and the live `*10` splash copy pass, app/Asterisk/Caddy are healthy, the SIP Fail2Ban jail is active, and settled app logs contain no warnings or errors.
- Created a verified root-only post-deploy recovery archive. Its sealed report confirms unchanged one-host/one-party state, SQLite and foreign-key integrity, and party-key decryption; no production member, device, invitation, access code, or OpenAI project was changed.

### Remaining

- Exercise `*10` from real remote hardware.
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
- Deployed from the clean `/opt/ringring` checkout without schema or secret changes. The live family access code remained configured; no deletion form was submitted and no production OpenAI project was archived.
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
