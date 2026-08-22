# Reference deployment

The reference deployment runs the Go app, Asterisk 22 LTS, and Caddy with Docker Compose. The public instance is `ringring.live`.

## Public ports

- `80/tcp` and `443/tcp` for the web app and automatic TLS.
- `443/udp` for HTTP/3.
- `5060/udp` for compatibility SIP registration.
- `10000-10199/udp` for RTP media.

No database, app origin, Docker API, or Asterisk Manager Interface port is published.

## Server prerequisites

Install Docker Engine and the Compose v2 plugin, then create state directories:

```sh
install -d -m 0755 /opt/ringring
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/app
install -d -o 10001 -g 10001 -m 0770 /opt/ringring/deploy/state/asterisk
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
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
OPENAI_ADMIN_KEY=<organization admin key>
OPENAI_PARTY_SPEND_LIMIT_CENTS=1000
SIP_PUBLIC_HOST=ringring.live
ASTERISK_CONFIG_DIR=/asterisk
ASTERISK_AMI_ADDR=asterisk:5038
ASTERISK_AMI_USER=ringring
ASTERISK_AMI_SECRET=<URL-safe random secret>
TZ=America/New_York
```

Create `/etc/ringring/asterisk.env` with mode `0600` and only:

```dotenv
SIP_PUBLIC_HOST=ringring.live
ASTERISK_AMI_SECRET=<same AMI secret>
TZ=America/New_York
```

Generate application keys with `openssl rand -base64 32` and the AMI secret with `openssl rand -hex 32`. Do not place either file inside the repository.

For Google OAuth, create a web application and register `https://ringring.live/auth/google/callback` as an authorized redirect URI. The splash page remains available while these fields are empty, but host sign-up stays closed.

## Deploy and verify

```sh
cd /opt/ringring
docker compose build
docker compose up -d
docker compose ps
curl --fail https://ringring.live/readyz
docker compose exec asterisk asterisk -rx 'pjsip show transports'
```

Review `docker compose logs --tail=100` after every deployment. Asterisk is compiled from the pinned official source release and its published SHA-256 file is checked during the image build.

## Update

```sh
cd /opt/ringring
git pull --ff-only
docker compose build
docker compose up -d --remove-orphans
```

Back up `/opt/ringring/deploy/state/app` before schema-changing upgrades. Generated Asterisk files can be regenerated from the database and do not need separate backups.
