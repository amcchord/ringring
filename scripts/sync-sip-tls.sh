#!/bin/sh
set -eu
umask 077

test_mode=${RINGRING_TLS_SYNC_TEST_MODE:-0}
if test "$test_mode" = 1; then
  checkout=${RINGRING_TLS_SYNC_TEST_CHECKOUT:?RINGRING_TLS_SYNC_TEST_CHECKOUT is required}
  tls_dir=${RINGRING_TLS_SYNC_TEST_OUTPUT:?RINGRING_TLS_SYNC_TEST_OUTPUT is required}
  storage_archive=${RINGRING_TLS_SYNC_TEST_ARCHIVE:?RINGRING_TLS_SYNC_TEST_ARCHIVE is required}
  refresh_command=${RINGRING_TLS_SYNC_TEST_REFRESH:?RINGRING_TLS_SYNC_TEST_REFRESH is required}
  lock_file=${RINGRING_TLS_SYNC_TEST_LOCK:?RINGRING_TLS_SYNC_TEST_LOCK is required}
elif test "$test_mode" = 0; then
  checkout=/opt/ringring
  tls_dir=/etc/ringring/tls
  storage_archive=
  refresh_command=
  lock_file=/run/ringring-sip-tls-sync.lock
  if test -n "${RINGRING_TLS_SYNC_TEST_CHECKOUT:-}${RINGRING_TLS_SYNC_TEST_OUTPUT:-}${RINGRING_TLS_SYNC_TEST_ARCHIVE:-}${RINGRING_TLS_SYNC_TEST_REFRESH:-}${RINGRING_TLS_SYNC_TEST_LOCK:-}"; then
    echo "Test overrides require RINGRING_TLS_SYNC_TEST_MODE=1." >&2
    exit 1
  fi
else
  echo "RINGRING_TLS_SYNC_TEST_MODE must be 0 or 1." >&2
  exit 1
fi

wait_seconds=0
while test "$#" -gt 0; do
  case "$1" in
    --wait)
      test "$#" -ge 2 || { echo "--wait needs a number of seconds." >&2; exit 2; }
      wait_seconds=$2
      shift 2
      ;;
    -h|--help)
      echo "usage: $0 [--wait SECONDS]"
      exit 0
      ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done
printf '%s\n' "$wait_seconds" | grep -Eq '^[0-9]+$' || {
  echo "--wait must be a whole number of seconds." >&2
  exit 2
}
test "$wait_seconds" -le 600 || {
  echo "--wait cannot exceed 600 seconds." >&2
  exit 2
}

if test "$test_mode" != 1 && test "$(id -u)" -ne 0; then
  echo "SIP TLS synchronization must run as root." >&2
  exit 1
fi
for command_name in awk cmp date docker find grep install mkdir mktemp openssl sha256sum sort stat tar; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "Required command is missing: $command_name" >&2
    exit 1
  }
done
for path in "$checkout" "$tls_dir"; do
  case "$path" in /*) ;; *) echo "Managed paths must be absolute." >&2; exit 1 ;; esac
  test ! -L "$path" || { echo "Managed paths must not be symlinks: $path" >&2; exit 1; }
done
test -f "$checkout/compose.yaml" && test ! -L "$checkout/compose.yaml" || {
  echo "The RingRing Compose file is missing or unsafe." >&2
  exit 1
}
compose_environment="$checkout/.env"
test -f "$compose_environment" && test ! -L "$compose_environment" || {
  echo "The root-only RingRing Compose environment is missing." >&2
  exit 1
}
file_mode() {
  if stat -c %a "$1" >/dev/null 2>&1; then stat -c %a "$1"; else stat -f %Lp "$1"; fi
}
file_owner() {
  if stat -c %u "$1" >/dev/null 2>&1; then stat -c %u "$1"; else stat -f %u "$1"; fi
}
mode=$(file_mode "$compose_environment")
case "$mode" in 400|600) ;; *) echo "The Compose environment must have mode 0400 or 0600." >&2; exit 1 ;; esac
if test "$test_mode" != 1 && test "$(file_owner "$compose_environment")" -ne 0; then
  echo "The Compose environment must be owned by root." >&2
  exit 1
fi
domain_count=$(awk -F= '$1 == "RINGRING_DOMAIN" { count++ } END { print count + 0 }' "$compose_environment")
test "$domain_count" -eq 1 || { echo "The Compose environment must contain exactly one RINGRING_DOMAIN." >&2; exit 1; }
domain=$(awk -F= '$1 == "RINGRING_DOMAIN" { sub(/^[^=]*=/, ""); print }' "$compose_environment")
printf '%s\n' "$domain" | grep -Eq '^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$' || {
  echo "RINGRING_DOMAIN must be a DNS hostname." >&2
  exit 1
}

install -d -m 0700 "$tls_dir"
lock_owned=0
test ! -L "$lock_file" || { echo "The SIP TLS synchronization lock must not be a symlink." >&2; exit 1; }
if ! mkdir -m 0700 "$lock_file" 2>/dev/null; then
  running_pid=
  if test -f "$lock_file/pid" && test ! -L "$lock_file/pid"; then
    running_pid=$(sed -n '1p' "$lock_file/pid")
  fi
  case "$running_pid" in
    ''|*[!0-9]*) ;;
    *)
      if kill -0 "$running_pid" 2>/dev/null; then
        echo "Another SIP TLS synchronization is already running."
        exit 0
      fi
      ;;
  esac
  test -d "$lock_file" && test ! -L "$lock_file" || { echo "The SIP TLS synchronization lock is unsafe." >&2; exit 1; }
  find "$lock_file" -depth -delete >/dev/null 2>&1
  mkdir -m 0700 "$lock_file" 2>/dev/null || {
    echo "Another SIP TLS synchronization started concurrently."
    exit 0
  }
fi
lock_owned=1
printf '%s\n' "$$" >"$lock_file/pid"

work_directory=$(mktemp -d /tmp/ringring-sip-tls-sync.XXXXXX)
case "$work_directory" in /tmp/ringring-sip-tls-sync.*) ;; *) echo "Unexpected temporary directory." >&2; exit 1 ;; esac
staging=
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  set +e
  if test -n "$staging" && test -d "$staging" && test ! -L "$staging"; then
    find "$staging" -depth -delete >/dev/null 2>&1
  fi
  find "$work_directory" -depth -delete >/dev/null 2>&1
  if test "$lock_owned" -eq 1 && test -d "$lock_file" && test ! -L "$lock_file"; then
    find "$lock_file" -depth -delete >/dev/null 2>&1
  fi
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

certificate_matches_domain() {
  openssl x509 -in "$1" -noout -checkhost "$domain" >/dev/null 2>&1
}

matching_key() {
  certificate_public=$(openssl x509 -in "$1" -pubkey -noout 2>/dev/null | sha256sum | awk '{print $1}')
  key_public=$(openssl pkey -in "$2" -pubout 2>/dev/null | sha256sum | awk '{print $1}')
  test -n "$certificate_public" && test "$certificate_public" = "$key_public"
}

attempt_export() {
  attempt=$(mktemp -d "$work_directory/attempt.XXXXXX")
  archive="$attempt/caddy-storage.tar"
  if test "$test_mode" = 1; then
    test -f "$storage_archive" && test ! -L "$storage_archive" || return 1
    install -m 0600 "$storage_archive" "$archive"
  else
    cd "$checkout"
    docker compose exec -T caddy caddy storage export \
      --config /etc/caddy/Caddyfile --output - >"$archive" 2>/dev/null || return 1
  fi
  test -s "$archive" || return 1
  listing="$attempt/archive.list"
  tar --list --file "$archive" >"$listing" 2>/dev/null || return 1
  while IFS= read -r entry; do
    case "$entry" in /*) return 1 ;; esac
    case "/$entry/" in */../*|*/./*) return 1 ;; esac
  done <"$listing"
  verbose_listing="$attempt/archive.verbose"
  tar --list --verbose --file "$archive" >"$verbose_listing" 2>/dev/null || return 1
  while IFS= read -r metadata; do
    case "$metadata" in
      -*) ;;
      d*) ;;
      *) return 1 ;;
    esac
  done <"$verbose_listing"
  extracted="$attempt/extracted"
  install -d -m 0700 "$extracted"
  tar --extract --file "$archive" --directory "$extracted" \
    --no-same-owner --no-same-permissions 2>/dev/null || return 1
  test -z "$(find "$extracted" ! -type f ! -type d -print -quit)" || return 1
  candidates="$attempt/candidates"
  find "$extracted" -type f -name '*.crt' -print | sort >"$candidates"
  selected_certificate=
  selected_key=
  while IFS= read -r candidate; do
    candidate_key=${candidate%.crt}.key
    if test -f "$candidate_key" && test ! -L "$candidate_key" && \
      certificate_matches_domain "$candidate" && \
      openssl x509 -in "$candidate" -noout -checkend 604800 >/dev/null 2>&1 && \
      matching_key "$candidate" "$candidate_key"; then
      selected_certificate=$candidate
      selected_key=$candidate_key
      break
    fi
  done <"$candidates"
  test -n "$selected_certificate"
}

deadline=$(( $(date +%s) + wait_seconds ))
while ! attempt_export; do
  if test "$(date +%s)" -ge "$deadline"; then
    echo "Caddy does not yet have a matching, week-valid SIP TLS certificate." >&2
    exit 1
  fi
  sleep 5
done

staging=$(mktemp -d "$tls_dir/.sync.XXXXXX")
install -m 0600 "$selected_certificate" "$staging/certificate.pem"
install -m 0600 "$selected_key" "$staging/private-key.pem"
changed=1
if test -f "$tls_dir/certificate.pem" && test ! -L "$tls_dir/certificate.pem" && \
  test -f "$tls_dir/private-key.pem" && test ! -L "$tls_dir/private-key.pem" && \
  cmp -s "$staging/certificate.pem" "$tls_dir/certificate.pem" && \
  cmp -s "$staging/private-key.pem" "$tls_dir/private-key.pem"; then
  changed=0
else
  test ! -L "$tls_dir/certificate.pem" && test ! -L "$tls_dir/private-key.pem" || {
    echo "Refusing to replace a symlinked SIP TLS file." >&2
    exit 1
  }
  mv "$staging/certificate.pem" "$tls_dir/certificate.pem"
  mv "$staging/private-key.pem" "$tls_dir/private-key.pem"
fi

set +e
if test "$test_mode" = 1; then
  "$refresh_command" --reload
  refresh_result=$?
else
  cd "$checkout"
  docker compose exec -T asterisk /usr/local/bin/ringring-asterisk-refresh-tls --reload
  refresh_result=$?
fi
set -e
case "$refresh_result" in
  0) ;;
  75) echo "The renewed SIP TLS certificate is staged; an active call deferred the Asterisk reload."; exit 75 ;;
  *) echo "Asterisk did not accept the synchronized SIP TLS certificate." >&2; exit "$refresh_result" ;;
esac
if test "$changed" -eq 1; then
  echo "SIP TLS certificate synchronized."
else
  echo "SIP TLS certificate is current."
fi
