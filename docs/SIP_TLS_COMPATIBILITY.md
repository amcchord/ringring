# SIP TLS compatibility

RingRing prefers SIP over TLS 1.2 on `5061/tcp` and retains SIP over UDP on `5060/udp` for older adapters. This matrix records only behavior exercised by a repeatable test or a named physical device; an “expected” row is not a compatibility claim.

TLS covers registration credentials and SIP call setup. Voice currently uses unencrypted, server-relayed RTP on `10000-10199/udp`. RingRing does not yet provide SRTP, so a network observer with access to the media path may hear calls even when SIP TLS is enabled.

## Evidence matrix

| Client or boundary | Signaling | Certificate validation | Registration | Party call | Audio | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| SIPp 3.7.7, source-checksummed build | TLS 1.2 caller plus UDP peer | Disposable CA and exact DNS name | Pass on both transports | Pass, TLS-to-UDP | Bidirectional PCMU and `*10` echo pass | `make sip-smoke` |
| Official Linphone Python engine 5.5.3 | Generated TLS account on `5061` plus UDP peer | Root CA, server-certificate verification, and common-name verification explicitly enabled | Pass | Pass, TLS-to-UDP | More than 100 packets each way and returned 440 Hz tone | `make linphone-smoke` |
| OpenSSL client | TLS 1.2 | Disposable CA/name in isolation; public system trust/name in deployment verification | Not applicable | Not applicable | Not applicable | `make sip-smoke`; `ringringctl doctor` |
| Generic TLS 1.2 softphone, desk phone, or ATA | Expected on `5061/tcp` | Must trust the public CA and verify the deployment hostname | Not yet tested on physical hardware | Not yet tested | Not yet tested | Physical matrix remains open |
| Older UDP-only ATA or phone | UDP on `5060/udp` | None | Automated pass | Automated pass with TLS peer | Automated PCMU pass; real hardware pending | `make sip-smoke`; `make nat-smoke` |

`make sip-tls-smoke` runs both TLS-aware client suites. They use fixed smoke-only SIP credentials, disposable certificates, an internal Docker network, no production state, and no published host ports. SIPp is built from the official `3.7.7` release archive with a pinned SHA-256 checksum and TLS support. Linphone is kept in a separate test-only GPLv3 image and is not linked into or shipped with the MIT-licensed RingRing application.

## Device setup baseline

For a capable client, configure:

- Registrar/server: the RingRing deployment hostname, never its bare IP address.
- Port: `5061`.
- Transport: TLS, with TLS 1.2 support and server-certificate/name verification enabled.
- SIP username and password: the generated per-device values from the one-time setup card.
- Extension/display identity: the party extension shown on that card.

Use UDP port `5060` only when the client cannot use TLS. Do not disable certificate verification to make a TLS device connect; fix its clock, trust store, server name, or firmware instead.

When one person uses both an ATA and a softphone, add them as separate phones under that member in the host console. They share the visible extension and ring together, but each setup card has its own SIP username and password. Never copy one device's credential into the other.

## Adding physical evidence

Record the exact vendor, model, firmware/app version, transport, certificate behavior, NAT type, registration duration, host-triggered incoming ring result, incoming and outgoing party-call result, two-way audio result, and any reboot or renewal behavior. Use **Ring this phone** first to prove that an online device accepts an inbound server call and speaks its extension without needing a second device. A physical row must still cover at least one call to another remote network. Mobile apps additionally need foreground ringing, background/push ringing, and Wi-Fi/cellular transitions before they count toward the family-hardware gate.

The current automated evidence follows the official [Asterisk PJSIP transport options](https://docs.asterisk.org/Latest_API/API_Documentation/Module_Configuration/res_pjsip/), [SIPp TLS build and runtime options](https://sipp.readthedocs.io/en/latest/installation.html), and [Liblinphone certificate-verification controls](https://download.linphone.org/releases/docs/liblinphone/latest/python/). Caddy remains the certificate issuer and renewer through its [automatic HTTPS storage lifecycle](https://caddyserver.com/docs/automatic-https).
