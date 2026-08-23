#!/bin/sh
set -eu
umask 077

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
backup_root_input=${1:-/root/ringring-backups}
app_environment=${RINGRING_APP_ENV_FILE:-/etc/ringring/app.env}
asterisk_environment=${RINGRING_ASTERISK_ENV_FILE:-/etc/ringring/asterisk.env}
apns_directory=${RINGRING_APNS_DIR:-/etc/ringring/apns}
app_image=${RINGRING_APP_IMAGE:-ringring-app}

if test "$(id -u)" -ne 0; then
  echo "RingRing backups must run as root because they include deployment secrets." >&2
  exit 1
fi
case "$backup_root_input" in
  /*) ;;
  *) echo "The backup directory must be an absolute path." >&2; exit 1 ;;
esac
for command_name in docker git realpath sha256sum tar; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command is missing: $command_name" >&2
    exit 1
  fi
done

install -d -m 0700 "$backup_root_input"
backup_root=$(realpath "$backup_root_input")
case "$backup_root" in
  /|/root|/etc|/opt|/tmp|"$repository"|"$repository"/*)
    echo "Refusing broad or repository-contained backup directory: $backup_root" >&2
    exit 1
    ;;
esac
for environment_file in "$app_environment" "$asterisk_environment"; do
  if test ! -f "$environment_file" || test -L "$environment_file"; then
    echo "Required environment file is missing or is a symbolic link: $environment_file" >&2
    exit 1
  fi
  if test "$(stat -c %a "$environment_file")" != 600; then
    echo "Environment file must have mode 0600: $environment_file" >&2
    exit 1
  fi
done

cd "$repository"
if test -n "$(git status --porcelain)"; then
  echo "Refusing to back up from a dirty checkout; the recorded commit must be reproducible." >&2
  exit 1
fi
if test ! -f deploy/state/app/ringring.db; then
  echo "RingRing database is missing." >&2
  exit 1
fi
docker image inspect "$app_image" >/dev/null
app_container=$(docker compose ps -q app)
if test -z "$app_container" || test "$(docker inspect --format '{{.State.Running}}' "$app_container")" != true; then
  echo "The RingRing app must be running before an offline backup." >&2
  exit 1
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
commit=$(git rev-parse HEAD)
short_commit=$(git rev-parse --short HEAD)
archive_name="ringring-${timestamp}-${short_commit}.tar.gz"
archive="$backup_root/$archive_name"
checksum="$archive.sha256"
temporary_archive="$backup_root/.$archive_name.tmp"
temporary_checksum="$backup_root/.$archive_name.sha256.tmp"
for output in "$archive" "$checksum" "$temporary_archive" "$temporary_checksum"; do
  if test -e "$output"; then
    echo "Backup output already exists: $output" >&2
    exit 1
  fi
done

work_directory=$(mktemp -d /tmp/ringring-backup.XXXXXX)
case "$work_directory" in
  /tmp/ringring-backup.*) ;;
  *) echo "Unexpected temporary directory: $work_directory" >&2; exit 1 ;;
esac
payload="$work_directory/ringring-backup"
app_stopped=0
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  set +e
  if test "$app_stopped" -eq 1; then
    cd "$repository" && docker compose start app >/dev/null
  fi
  rm -f "$temporary_archive" "$temporary_checksum"
  find "$work_directory" -depth -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

install -d -m 0700 "$payload" "$payload/secrets" "$payload/secrets/apns"
echo "Stopping only the RingRing app for a consistent WAL-mode snapshot..."
docker compose stop app >/dev/null
app_stopped=1
cp -a deploy/state/app "$payload/app"
install -m 0600 "$app_environment" "$payload/secrets/app.env"
install -m 0600 "$asterisk_environment" "$payload/secrets/asterisk.env"
apns_key_id=$(sed -n 's/^APNS_KEY_ID=//p' "$app_environment")
if test -n "$apns_key_id"; then
  printf '%s\n' "$apns_key_id" | grep -Eq '^[A-Z0-9]{10}$' || {
    echo "APNS_KEY_ID is invalid in the application environment." >&2
    exit 1
  }
  apns_key="$apns_directory/AuthKey_${apns_key_id}.p8"
  if test ! -f "$apns_key" || test -L "$apns_key"; then
    echo "The configured APNs provider key is missing or is a symbolic link." >&2
    exit 1
  fi
  case "$(stat -c %a "$apns_key")" in 400|440|600) ;; *)
    echo "The APNs provider key must have mode 0400, 0440, or 0600." >&2
    exit 1
  esac
  install -m 0400 "$apns_key" "$payload/secrets/apns/AuthKey_${apns_key_id}.p8"
fi
test -f "$payload/app/ringring.db"
if test -e "$payload/app/ringring.db-wal" || test -e "$payload/app/ringring.db-shm"; then
  echo "SQLite left a WAL or SHM sidecar after clean shutdown; refusing an immutable snapshot." >&2
  exit 1
fi
printf '%s\n' \
  'format=1' \
  "created_at=$timestamp" \
  "git_commit=$commit" \
  'database=app/ringring.db' \
  'includes_deployment_secrets=true' \
  "includes_apns_provider_key=$(test -n "$apns_key_id" && echo true || echo false)" \
  'generated_asterisk_state=false' >"$payload/manifest.txt"
chmod 0600 "$payload/manifest.txt"

docker compose start app >/dev/null
healthy=0
for _ in $(seq 1 45); do
  if docker compose ps app --format '{{.Health}}' | grep -qx healthy; then
    healthy=1
    break
  fi
  sleep 1
done
if test "$healthy" -ne 1; then
  echo "The RingRing app did not return to healthy after the snapshot." >&2
  exit 1
fi
app_stopped=0

verification=$(docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --env-file "$payload/secrets/app.env" \
  --volume "$payload/app:/data:ro" --entrypoint /usr/local/bin/ringring \
  "$app_image" verify-state)
if ! printf '%s\n' "$verification" | grep -q '"status":"ok"'; then
  echo "The copied state did not pass verification." >&2
  exit 1
fi
printf '%s\n' "$verification" >"$payload/verification.json"
chmod 0600 "$payload/verification.json"
if test -n "$(find "$payload" -type l -print -quit)"; then
  echo "Refusing to archive a symbolic link." >&2
  exit 1
fi

tar --create --gzip --file "$temporary_archive" --directory "$work_directory" ringring-backup
chmod 0600 "$temporary_archive"
digest=$(sha256sum "$temporary_archive" | awk '{print $1}')
printf '%s  %s\n' "$digest" "$archive_name" >"$temporary_checksum"
chmod 0600 "$temporary_checksum"
mv "$temporary_archive" "$archive"
mv "$temporary_checksum" "$checksum"
printf 'Backup created: %s\nChecksum: %s\n' "$archive" "$checksum"
