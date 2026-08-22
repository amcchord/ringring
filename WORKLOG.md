# RingRing worklog

This is the durable, chronological project record. Add new entries at the top. Capture decisions and verification, not a transcript of commands.

## 2026-08-22 — A private first-call card for new members

### Shipped

- Added a bright claim-only setup card that tells a newly joined member which existing party extensions still have an active phone route and which utility/fun numbers are currently routable. A child can finish setup and place a first call without asking the host to transcribe a phonebook or stale service list.
- Reduced each directory item to display label and extension and included it only when at least one attached phone remains active. Device labels, SIP identities, credentials, readiness, contact state, timestamps, and disconnected-only members never enter the view.
- Derived `*10`, `*15`, and optional `*11`–`*14` entries from the same party/service readiness rules used by routing. Paused AI-powered lines are omitted, and `*14` still requires the default-closed operator child-safety gate.
- Hardened the isolated two-household NAT gate after its known Docker startup race recurred: it now waits for the exact PJSIP endpoint and successful paths from both nested household namespaces before starting SIPp's one-shot registration.

### Decisions

- Put the snapshot only on the successful member-claim response. The unclaimed invite page cannot enumerate a party, while host-created/rotated cards sent to a device technician and the Linphone provisioning payload stay free of unrelated family names.
- Keep it ephemeral rather than create another bearer directory URL. The setup response already carries a one-time SIP credential, is private/no-store/no-referrer/noindex, and tells the member to ask the host for changes made later.
- Describe only currently dialable destinations. A disconnected-only member or paused service is more confusing than helpful on a first-call card.
- Treat PBX endpoint loading and nested-network reachability as fixture readiness, while keeping challenged registration, rewritten contacts, the actual call, and bidirectional RTP as the assertions under test.

### Verification

- Focused web and pure policy tests cover active-versus-disconnected members, an unclaimed invite with no directory, successful claim rendering, reduced fields, child-safety gating, spend-paused service omission, and host setup-card exclusion.
- A disposable browser claim rendered two existing members and the exact enabled utility lines at 1280×900 and 390×844. The card used two 347px columns on desktop and one 305px column on mobile, had zero horizontal overflow, and kept every call row at least 44px tall. Its viewport override and tab were reset, and the temporary database was moved to Trash.
- `make check`, `make security`, and `make admin-test` pass locally, including formatting, shell/operator fixtures, vet, the complete race-enabled suite, and the reachable-vulnerability scan. The accepted module advisory remains unreachable from RingRing.
- The first isolated candidate passed the complete SIP/TLS/multi-phone gate, then hit the recurring NAT pre-registration timeout before receiving a `401` challenge. The harness now has bounded endpoint and two-path readiness prerequisites; its revised exact-candidate evidence is recorded after the rerun below.

### Remaining

- Complete the exact-candidate SIP gates, guarded production upgrade, and zero-mutation production audit without opening the real invitation or family directory.
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
