# Backup and disaster recovery

RingRing's database contains family labels, password and recovery-code hashes, encrypted SIP credentials, and encrypted party service keys. The master key needed to decrypt those credentials lives in the application environment, not in SQLite. A usable disaster-recovery set must therefore keep the database and both deployment environment files together.

## Create a verified backup

Run from the clean production checkout as root:

```sh
cd /opt/ringring
make backup
```

The command briefly stops only the app so SQLite can close and checkpoint its [write-ahead log](https://www.sqlite.org/wal.html), requires the closed copy to have no WAL/SHM sidecars, copies the complete app-state directory plus `/etc/ringring/app.env`, `/etc/ringring/asterisk.env`, and the active root-only APNs provider key when iPhone background calls are configured, restarts the app, and waits for it to become healthy. It then uses the deployed RingRing image on a network-disabled container to open the closed copy as immutable and check SQLite integrity, foreign keys, the current schema, record counts, and decryption of every stored SIP and party credential.

The output is a mode-`0600` `ringring-<UTC>-<commit>.tar.gz` archive and matching `.sha256` file under the mode-`0700` `/root/ringring-backups` directory. The manifest records the exact Git commit. Generated Asterisk configuration and generated voice cache files are omitted because the restored app regenerates them from the database.

> [!CAUTION]
> The archive contains deployment secrets and private family data. Root-only permissions are a local safeguard, not encryption. Copy backups off the server only into encrypted storage with tightly limited access, and apply an operator-chosen retention policy.

Deleting a member, party, or host account does not rewrite archives that were created earlier. A restore can therefore reintroduce data and credentials that existed at the backup timestamp. After a privacy-driven deletion, expire every affected backup according to the operator's retention policy; if an early purge is required, create and verify a post-deletion archive before securely retiring the older copies.

An optional absolute output directory can be passed directly to `scripts/backup.sh`. The script refuses broad paths, repository-contained destinations, dirty checkouts, loose environment-file permissions, and symlinked environment files.

## Exercise a restore without touching production

Run the drill against an archive from the current or an ancestor RingRing commit:

```sh
cd /opt/ringring
make restore-drill BACKUP=/root/ringring-backups/ringring-<UTC>-<commit>.tar.gz
```

The drill verifies the sidecar checksum, rejects unsafe archive paths and special files, extracts only into a root-only temporary directory, and reruns the sealed state report. It then starts the restored app with no network, no host ports, no OpenAI admin key, and no AMI secret. Readiness and telephony-config regeneration must pass, the app must stop cleanly, and the state report must remain unchanged. The exact temporary container and extracted copy are removed; the archive and live deployment are not changed.

## Resume an interrupted guided operation

`ringringctl install` keeps `/etc/ringring/install.pending` after configuration has been generated but before every deployment verification passes. Preserve that marker and the environment files, fix the reported host, DNS, build, firewall, or service condition, and run `sudo /opt/ringring/ringringctl install --yes`. Do not supply a new answers file: resume intentionally reuses the previously generated secrets.

`ringringctl upgrade` creates and drills a pre-upgrade backup before it writes `/etc/ringring/upgrade.pending` or moves Git. The marker fixes the old commit, exact target commit, and verified backup path. If the upgrade stops, retain both marker and backup and run `sudo /opt/ringring/ringringctl upgrade --yes` after correcting the failure. The command accepts only the recorded target and adds the post-upgrade backup after full verification; it does not repeat the pre-upgrade snapshot. Do not delete the marker, improvise a different target, or roll the database backward across an unknown forward migration.

## Recover a failed host

Use a maintenance window and keep every replacement reversible:

1. Preserve the failed host and its current state. If the app can still run, create a new backup before changing anything.
2. Copy the chosen archive and its `.sha256` sidecar to the recovery host through an encrypted channel.
3. Clone the public repository, inspect `ringring-backup/manifest.txt`, and check out the recorded commit or a reviewed forward-compatible commit. Install Docker and the state directories from [the deployment guide](DEPLOYMENT.md).
4. Build the `ringring-app` image, then run the isolated restore drill. Do not continue if checksum, decryption, integrity, startup, or regenerated telephony configuration fails.
5. Stop the Compose stack. Move any existing `deploy/state/app` and `/etc/ringring` files into a new root-only rollback directory; do not delete them.
6. Extract the already-verified archive into a new root-only staging directory. Copy `ringring-backup/app` into `deploy/state/app`, set it recursively to UID/GID `10001:10001`, and install the two archived environment files into `/etc/ringring` as root with mode `0600`. If `ringring-backup/secrets/apns` contains a provider key, recreate `/etc/ringring/apns` with mode `0700` and install the key as root with mode `0400`. Recreate `/opt/ringring/.env` as a mode-`0600` file containing only `RINGRING_DOMAIN=<hostname>` derived from the restored `APP_BASE_URL`.
7. Start the stack, reinstall/enable the SIP TLS synchronization timer, and wait for it to reacquire the exact hostname certificate from Caddy. Verify app/Asterisk health, public readiness and security headers, trusted SIP TLS, both Fail2Ban jails, generated party routes, and the isolated SIP/RTP smoke test before allowing family devices to reconnect.
8. Keep the pre-restore rollback directory until hosts verify their parties and devices. Securely retire superseded backup material only under the operator's retention policy.

The backup does not include the Git checkout, its non-secret domain-only Compose `.env`, container images, Caddy certificates, the root-only `/etc/ringring/tls` copy, or generated Asterisk state. The repository and images are reproducible from the recorded commit, the Compose value is derivable from the restored application origin, Caddy can obtain replacement certificates, the synchronization timer can validate and stage the exact hostname pair again, and RingRing regenerates telephony configuration from restored application state. Never restore an expired copied certificate or private key from an old filesystem snapshot merely to make SIP start; reacquire it through Caddy and verify `ringringctl doctor` instead.
