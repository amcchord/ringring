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
- [x] One-time Linphone QR-assisted softphone setup with official engine import, registration, party-call, and echoed-audio smoke; real-phone UX remains part of the usability pass.
- [x] Host member directory and device controls.
- [x] Self-service two-way echo test.
- [x] Host-only live registration and reachability hints.
- [ ] Voice-guided extension selection.
- [x] Host-controlled time and weather service lines.
- [x] Internet radio line with a safe fixed station.
- [ ] Vetted host station selection without arbitrary URL injection.
- [ ] Accessibility and real-device usability pass.

## Milestone 3 — OpenAI voice

- [x] Automated per-party OpenAI project/service-account provisioning.
- [x] Per-party encrypted runtime key use.
- [x] Immediate party-key revocation and replacement.
- [ ] Host-set project spend limit; service toggles are implemented.
- [x] Realtime voice line with child-appropriate instructions, disclosure, bounded calls, and no tools.
- [x] AI disclosure for generated weather speech and no default transcript retention.
- [ ] External child-safety review and confirmation of OpenAI Zero Data Retention before use by callers under 13.

## Milestone 4 — Dependable self-hosting

- [x] Reproducible Docker Compose deployment and SIP firewall installer.
- [ ] Guided installation and upgrade command.
- [x] Backup, restore, and disaster-recovery verification.
- [ ] SIP TLS device compatibility matrix.
- [ ] Observability without call-content collection.
- [ ] Security review and threat-model tests.
- [ ] Optional PostgreSQL and multi-node migration path.
