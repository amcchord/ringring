#!/bin/sh
set -eu
umask 077

checkout=/opt/ringring
backup_root=/root/ringring-backups
app_environment=/etc/ringring/app.env
target_commit=${1:-}
approval=${2:-}

fail() {
  echo "$*" >&2
  exit 1
}

test "$(id -u)" -eq 0 || fail "Run this recovery as root."
printf '%s\n' "$target_commit" | grep -Eq '^[0-9a-f]{40}$' ||
  fail "Usage: scripts/repair-orphaned-state.sh TARGET_COMMIT --yes"
test "$approval" = --yes || fail "Review the recovery plan, then pass --yes."
for command_name in awk basename cp cut date docker find git grep install mktemp realpath seq sha256sum sleep stat tar; do
  command -v "$command_name" >/dev/null 2>&1 || fail "Required command is missing: $command_name"
done
test -d "$checkout/.git" && test ! -L "$checkout" || fail "The production checkout is unavailable or unsafe."
test -f "$app_environment" && test ! -L "$app_environment" || fail "The application environment is unavailable or unsafe."
test "$(stat -c %a "$app_environment")" = 600 || fail "The application environment must have mode 0600."
test -z "$(git -C "$checkout" status --porcelain)" || fail "The production checkout must be clean."
test ! -e /etc/ringring/upgrade.pending || fail "Finish the pending upgrade before running state recovery."

git -C "$checkout" fetch --prune origin
git -C "$checkout" cat-file -e "$target_commit^{commit}" 2>/dev/null || fail "The target commit is unavailable."
current_commit=$(git -C "$checkout" rev-parse HEAD)
git -C "$checkout" merge-base --is-ancestor "$current_commit" "$target_commit" ||
  fail "The repair target must be a fast-forward descendant of production."
git -C "$checkout" ls-remote origin | grep -Eq "^${target_commit}[[:space:]]" ||
  fail "The repair target is not advertised by the production origin."
git -C "$checkout" cat-file -e "$target_commit:scripts/repair-orphaned-state.sh" 2>/dev/null ||
  fail "The target does not contain this documented recovery."

cd "$checkout"
docker compose config --quiet
test "$(docker compose ps app --format '{{.Health}}')" = healthy || fail "The RingRing app is not healthy."
test "$(docker compose ps asterisk --format '{{.Health}}')" = healthy || fail "Asterisk is not healthy."

install -d -m 0700 "$backup_root"
test "$(realpath "$backup_root")" = "$backup_root" || fail "The backup root is not canonical."
work_directory=$(mktemp -d /root/ringring-state-repair.XXXXXX)
case "$work_directory" in
  /root/ringring-state-repair.??????) ;;
  *) fail "The recovery workspace is unsafe." ;;
esac
source_directory="$work_directory/source"
payload="$work_directory/quarantine"
image="ringring-state-repair:$(printf '%s' "$target_commit" | cut -c1-12)"
app_stopped=0
image_built=0

cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  set +e
  if test "$app_stopped" -eq 1; then
    cd "$checkout" && docker compose start app >/dev/null
  fi
  if test "$image_built" -eq 1; then
    docker image rm "$image" >/dev/null 2>&1
  fi
  case "$work_directory" in
    /root/ringring-state-repair.??????) find "$work_directory" -depth -delete >/dev/null 2>&1 ;;
  esac
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

install -d -m 0700 "$source_directory" "$payload" "$payload/app" "$payload/secrets"
git -C "$checkout" archive "$target_commit" | tar -x -C "$source_directory"
docker build --quiet --tag "$image" --file "$source_directory/Dockerfile" "$source_directory" >/dev/null
image_built=1

test "$(docker compose ps app --format '{{.Health}}')" = healthy || fail "The RingRing app became unhealthy during preparation."
test "$(docker compose ps asterisk --format '{{.Health}}')" = healthy || fail "Asterisk became unhealthy during preparation."
channels=$(docker compose exec -T asterisk asterisk -rx 'core show channels count' </dev/null)
printf '%s\n' "$channels" | grep -q '^0 active calls' || fail "A call is active; wait before repairing state."
printf '%s\n' "$channels" | grep -q '^0 active channels' || fail "A channel is active; wait before repairing state."

echo "Stopping only the RingRing app for a consistent state repair..."
docker compose stop app >/dev/null
app_stopped=1
cp -a "$checkout/deploy/state/app/." "$payload/app/"
chown 10001:10001 "$payload/app"
chmod 0770 "$payload/app"
install -m 0600 "$app_environment" "$payload/secrets/app.env"
test ! -e "$payload/app/ringring.db-wal" && test ! -e "$payload/app/ringring.db-shm" ||
  fail "SQLite retained a WAL sidecar after clean app shutdown."

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
archive="$backup_root/ringring-quarantine-${timestamp}-$(printf '%s' "$current_commit" | cut -c1-7).tar.gz"
test ! -e "$archive" && test ! -e "$archive.sha256" || fail "The quarantine archive already exists."
{
  printf 'format=1\n'
  printf 'created_at=%s\n' "$timestamp"
  printf 'git_commit=%s\n' "$current_commit"
  printf 'repair_commit=%s\n' "$target_commit"
  printf 'database=app/ringring.db\n'
  printf 'includes_application_secrets=true\n'
  printf 'requires_orphan_repair=true\n'
} >"$payload/manifest.txt"
chmod 0600 "$payload/manifest.txt"
tar --create --gzip --file "$archive" --directory "$work_directory" quarantine
chmod 0600 "$archive"
digest=$(sha256sum "$archive" | awk '{print $1}')
printf '%s  %s\n' "$digest" "$(basename "$archive")" >"$archive.sha256"
chmod 0600 "$archive.sha256"

# Prove the immutable quarantine snapshot is recoverable on its disposable copy
# before touching the live database.
docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges \
  --env-file "$payload/secrets/app.env" --volume "$payload/app:/data" \
  "$image" repair-state | grep -q '"status":"ok"' || fail "The quarantine copy could not be repaired and verified."

docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges \
  --env-file "$app_environment" --volume "$checkout/deploy/state/app:/data" \
  "$image" repair-state | grep -q '"status":"ok"' || fail "The live state repair did not verify."

docker compose start app >/dev/null
for _ in $(seq 1 60); do
  if test "$(docker compose ps app --format '{{.Health}}')" = healthy; then
    app_stopped=0
    break
  fi
  sleep 1
done
test "$app_stopped" -eq 0 || fail "The RingRing app did not return to healthy."
docker compose exec -T app ringring verify-state </dev/null | grep -q '"status":"ok"' ||
  fail "The running release did not verify the repaired state."

echo "State repair passed against a disposable copy and production."
echo "Root-only pre-repair quarantine: $archive"
echo "Run the guarded upgrade to $target_commit immediately."
