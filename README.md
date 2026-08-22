# RingRing

**Private phone parties for families.**

RingRing turns SIP phones, softphones, and ordinary telephones connected through SIP-to-FXS adapters into a tiny private phone network. Create a party, invite the people you trust, choose short extensions, and call one another—without connecting to the public telephone network.

The hosted reference instance is live at [ringring.live](https://ringring.live).

> [!IMPORTANT]
> RingRing cannot place emergency calls or reach regular phone numbers. Keep another way to contact emergency services available.

## What we are building

- Isolated parties with a host and a private extension directory.
- One-time invite links and device-specific SIP setup cards.
- Friendly setup guidance for common ATAs, VoIP phones, and softphones.
- A bright, mobile-first host dashboard for invitations, members, devices, and optional lines.
- Optional dialable services for time, weather, internet radio, and an OpenAI voice companion.
- A reproducible, self-hosted Docker Compose deployment using Asterisk and Caddy.

See [the architecture](docs/ARCHITECTURE.md), [the security model](docs/SECURITY.md), and [the roadmap](docs/ROADMAP.md).

## Status

The core private-phone flow is live: hosts can create parties, issue one-time member invitations, provision devices, rotate or revoke SIP credentials, and follow setup guides for ATAs, VoIP phones, and softphones. Hosts sign up immediately with a RingRing username, password, shared family access code, and offline recovery codes—Google and email confirmation are not required.

Party-scoped `*11` time, `*12` weather, `*13` internet-radio, and opt-in `*14` RingRing AI lines are deployed alongside automated per-party OpenAI key provisioning. The AI line uses a clearly disclosed voice, a party key, privacy-preserving safety identifiers, child-appropriate instructions, no tools, bounded calls, and no RingRing audio or transcript storage. The reference instance remains a preview until two remote devices pass a real two-way-audio call and backup/restore and deletion drills are complete.

The public repository intentionally contains no deployment credentials or family data.

## Development

Requirements:

- Go (version in `go.mod`)
- Docker with Compose for the full VoIP stack
- `make`

Project commands:

```sh
cp .env.example .env
make setup
make dev
make test
make compose-up
```

## Contributing

Read [AGENTS.md](AGENTS.md) before making changes and record meaningful work in [WORKLOG.md](WORKLOG.md). Issues and small, focused pull requests are welcome once the initial vertical slice lands.

## License

[MIT](LICENSE)
