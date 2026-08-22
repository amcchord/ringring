# Roadmap

## Milestone 1 — A party can ring

- [x] Public splash page and production health endpoint.
- [x] Native no-email host signup, login, and offline recovery; Google is optional.
- [x] Party creation and isolated extension range.
- [x] Single-use member invite.
- [x] Member and device provisioning with one-time SIP setup card.
- [x] Generated Asterisk PJSIP endpoints and party dialplans.
- [x] Simulated two-household NAT registration, party-call, and bidirectional-media gate.
- [ ] Calls between two remotely registered devices, verified with real hardware.
- [x] Device revocation and credential rotation.
- [x] Guarded member, party, and host-account deletion with OpenAI project archival.

## Milestone 2 — Friendly enough for family

- [x] Device-specific setup guides for ATAs, VoIP phones, and softphones.
- [x] One-time Linphone QR-assisted softphone setup with official engine import, registration, party-call, and echoed-audio smoke; physical-phone UX remains separately gated.
- [x] Host member directory and device controls.
- [x] Self-service two-way echo test.
- [x] Host-only live registration and reachability hints.
- [x] Host-confirmed real-phone echo, remote outgoing-call, and remote incoming-call checklist with reset-on-reconfiguration semantics.
- [x] Authenticated voice-guided extension selection with DTMF confirmation.
- [x] Host-controlled time and weather service lines.
- [x] Internet radio line with a safe fixed station.
- [x] Vetted host station selection without arbitrary URL injection.
- [x] Web accessibility and simulated mobile usability pass.
- [ ] Real-device usability pass with family phones, ATAs, and mobile background transitions.

## Milestone 3 — OpenAI voice

- [x] Automated per-party OpenAI project/service-account provisioning.
- [x] Per-party encrypted runtime key use.
- [x] Immediate party-key revocation and replacement.
- [x] Host-set project hard spend limit with an operator ceiling and fail-closed provider reconciliation.
- [x] Realtime voice line with child-appropriate instructions, disclosure, bounded calls, and no tools.
- [x] AI disclosure for generated weather speech and no default transcript retention.
- [ ] External child-safety review and confirmation of OpenAI Zero Data Retention before use by callers under 13.

## Milestone 4 — Dependable self-hosting

- [x] Reproducible Docker Compose deployment and SIP firewall installer.
- [x] Guided installation and upgrade command with secret-safe input, verified backups, and exact interrupted-operation resume.
- [x] Backup, restore, and disaster-recovery verification.
- [x] SIP TLS 1.2 transport, trusted-certificate handoff, UDP fallback, and automated SIPp/Linphone compatibility matrix.
- [ ] Physical ATA, desk-phone, and softphone SIP TLS compatibility matrix.
- [ ] Observability without call-content collection.
- [ ] Security review and threat-model tests.
- [ ] Optional PostgreSQL and multi-node migration path.
