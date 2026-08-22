# RingRing

**Private phone parties for families.**

RingRing turns SIP phones, softphones, and ordinary telephones connected through SIP-to-FXS adapters into a tiny private phone network. Create a party, invite the people you trust, choose short extensions, and call one another—without connecting to the public telephone network.

The hosted reference instance will live at [ringring.live](https://ringring.live).

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

RingRing is at the beginning of active development. Do not use it as a production phone service yet. The public repository intentionally contains no deployment credentials or family data.

## Development

Requirements:

- Go (version in `go.mod`)
- Docker with Compose for the full VoIP stack
- `make`

The project commands will be:

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
