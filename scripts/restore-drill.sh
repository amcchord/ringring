#!/bin/sh
set -eu
umask 077

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
container=ringring-restore-drill
app_image=${RINGRING_APP_IMAGE:-ringring-app}

if test "$(id -u)" -ne 0; then
  echo "The restore drill must run as root because RingRing backups contain deployment secrets." >&2
  exit 1
fi
if test "$#" -ne 1; then
  echo "usage: $0 /absolute/path/to/ringring-backup.tar.gz" >&2
  exit 2
fi
case "$1" in
  /*) ;;
  *) echo "The backup archive path must be absolute." >&2; exit 1 ;;
esac
for command_name in docker git realpath sha256sum tar; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command is missing: $command_name" >&2
    exit 1
  fi
done

archive=$(realpath "$1")
checksum="$archive.sha256"
if test ! -f "$archive" || test -L "$archive" || test ! -f "$checksum" || test -L "$checksum"; then
  echo "The archive and its .sha256 sidecar must be regular, non-symlink files." >&2
  exit 1
fi
archive_name=$(basename "$archive")
checksum_line=$(sed -n '1p' "$checksum")
expected_digest=${checksum_line%% *}
recorded_name=${checksum_line#*  }
if ! printf '%s\n' "$expected_digest" | grep -Eq '^[0-9a-f]{64}$' || test "$recorded_name" != "$archive_name"; then
  echo "The checksum sidecar is malformed or names a different archive." >&2
  exit 1
fi
actual_digest=$(sha256sum "$archive" | awk '{print $1}')
if test "$actual_digest" != "$expected_digest"; then
  echo "The backup archive checksum does not match." >&2
  exit 1
fi

cd "$repository"
if test -n "$(git status --porcelain)"; then
  echo "Refusing a restore drill from a dirty checkout." >&2
  exit 1
fi
docker image inspect "$app_image" >/dev/null
if test -n "$(docker ps -aq --filter "name=^/${container}$")"; then
  echo "The isolated restore-drill container already exists." >&2
  exit 1
fi

work_directory=$(mktemp -d /tmp/ringring-restore-drill.XXXXXX)
case "$work_directory" in
  /tmp/ringring-restore-drill.*) ;;
  *) echo "Unexpected temporary directory: $work_directory" >&2; exit 1 ;;
esac
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  set +e
  docker rm -f "$container" >/dev/null 2>&1
  find "$work_directory" -depth -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

listing="$work_directory/archive.list"
tar --list --gzip --file "$archive" >"$listing"
while IFS= read -r entry; do
  case "$entry" in
    ringring-backup|ringring-backup/*) ;;
    *) echo "Backup contains an unexpected archive path." >&2; exit 1 ;;
  esac
  case "$entry" in
    /*) echo "Backup contains an absolute archive path." >&2; exit 1 ;;
  esac
  case "/$entry/" in
    */../*|*/./*) echo "Backup contains an unsafe archive path." >&2; exit 1 ;;
  esac
done <"$listing"
tar --extract --gzip --file "$archive" --directory "$work_directory" \
  --no-same-owner --no-same-permissions
payload="$work_directory/ringring-backup"
for required in manifest.txt verification.json app/ringring.db secrets/app.env secrets/asterisk.env; do
  if test ! -f "$payload/$required"; then
    echo "Backup is missing required file: $required" >&2
    exit 1
  fi
done
install -d -m 0700 "$payload/secrets/apns"
if test -n "$(find "$payload" ! -type f ! -type d -print -quit)"; then
  echo "Backup contains a symbolic link or special file." >&2
  exit 1
fi
if test "$(sed -n 's/^format=//p' "$payload/manifest.txt")" != 1; then
  echo "Unsupported RingRing backup format." >&2
  exit 1
fi
backup_commit=$(sed -n 's/^git_commit=//p' "$payload/manifest.txt")
if ! printf '%s\n' "$backup_commit" | grep -Eq '^[0-9a-f]{40}$' || ! git cat-file -e "$backup_commit^{commit}" 2>/dev/null; then
  echo "The backup references an unavailable Git commit." >&2
  exit 1
fi
if ! git merge-base --is-ancestor "$backup_commit" HEAD; then
  echo "The backup was created by code newer than this checkout." >&2
  exit 1
fi

chown -R 10001:10001 "$payload/app"
install -d -o 10001 -g 10001 -m 0750 "$work_directory/asterisk"
before=$(docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --env-file "$payload/secrets/app.env" \
  --volume "$payload/app:/data:ro" --entrypoint /usr/local/bin/ringring \
  "$app_image" verify-state)
if test "$before" != "$(sed -n '1p' "$payload/verification.json")"; then
  echo "Current verification does not match the report sealed in the backup." >&2
  exit 1
fi

docker run -d --name "$container" --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --env-file "$payload/secrets/app.env" \
  --env APP_BASE_URL=https://restore.invalid --env HOST_SIGNUP_CODE= \
  --env OPENAI_ADMIN_KEY= --env GOOGLE_CLIENT_ID= --env GOOGLE_CLIENT_SECRET= \
  --env AI_ADULT_ONLY_ENABLED=false \
  --env SIP_PUBLIC_HOST=restore.invalid --env ASTERISK_AMI_SECRET= \
  --env HTTP_ADDR=:8080 --env FASTAGI_ADDR=:4573 --env AI_AUDIO_ADDR=:4574 \
  --volume "$payload/app:/data" --volume "$work_directory/asterisk:/asterisk" \
  --volume "$payload/secrets/apns:/run/secrets/ringring-apns:ro" \
  "$app_image" >/dev/null
ready=0
for _ in $(seq 1 30); do
  if docker exec "$container" curl --fail --silent http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if test "$ready" -ne 1; then
  echo "The isolated restored app did not become ready." >&2
  docker logs --tail 100 "$container" >&2 || true
  exit 1
fi
test -f "$work_directory/asterisk/pjsip.conf"
test -f "$work_directory/asterisk/extensions.conf"
if docker logs "$container" 2>&1 | grep -Eq '"level":"(WARN|ERROR)"'; then
  echo "The isolated restored app logged a warning or error." >&2
  docker logs --tail 100 "$container" >&2
  exit 1
fi
docker stop --time 15 "$container" >/dev/null
if test "$(docker inspect --format '{{.State.ExitCode}}' "$container")" -ne 0; then
  echo "The isolated restored app did not stop cleanly." >&2
  exit 1
fi
docker rm "$container" >/dev/null
after=$(docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --env-file "$payload/secrets/app.env" \
  --volume "$payload/app:/data:ro" --entrypoint /usr/local/bin/ringring \
  "$app_image" verify-state)
if test "$after" != "$before"; then
  echo "The isolated app changed the verified state report." >&2
  exit 1
fi
echo "Restore drill passed: checksum, safe extraction, SQLite integrity, foreign keys, credential decryption, isolated readiness, and telephony regeneration."
