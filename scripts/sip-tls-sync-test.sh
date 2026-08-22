#!/bin/sh
set -eu
umask 077

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
test_root=$(mktemp -d /tmp/ringring-sip-tls-sync-test.XXXXXX)
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  find "$test_root" -depth -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

fail() {
  echo "SIP TLS sync test failed: $*" >&2
  exit 1
}

file_mode() {
  if stat -c %a "$1" >/dev/null 2>&1; then stat -c %a "$1"; else stat -f %Lp "$1"; fi
}

new_fixture() {
  name=$1
  fixture="$test_root/$name"
  checkout="$fixture/checkout"
  output="$fixture/tls"
  storage="$fixture/storage"
  archive="$fixture/storage.tar"
  refresh_log="$fixture/refresh.log"
  lock="$fixture/sync.lock"
  install -d -m 0700 "$checkout" "$storage/caddy/certificates/test-ca/phone.example.test"
  printf 'name: ringring\nservices: {}\n' >"$checkout/compose.yaml"
  printf 'RINGRING_DOMAIN=phone.example.test\n' >"$checkout/.env"
  chmod 0600 "$checkout/.env"
  cat >"$fixture/refresh.sh" <<'EOF'
#!/bin/sh
set -eu
test "$1" = --reload
printf 'refresh\n' >>"$RINGRING_TLS_TEST_REFRESH_LOG"
exit "${RINGRING_TLS_TEST_REFRESH_EXIT:-0}"
EOF
  chmod 0700 "$fixture/refresh.sh"
  openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 30 \
    -subj '/CN=RingRing test CA' -keyout "$fixture/ca.key" -out "$fixture/ca.crt" >/dev/null 2>&1
  openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=phone.example.test' \
    -keyout "$fixture/server.key" -out "$fixture/server.csr" >/dev/null 2>&1
  printf 'subjectAltName=DNS:phone.example.test\n' >"$fixture/server.ext"
  openssl x509 -req -sha256 -days 30 -in "$fixture/server.csr" \
    -CA "$fixture/ca.crt" -CAkey "$fixture/ca.key" -CAcreateserial \
    -extfile "$fixture/server.ext" -out "$fixture/server.crt" >/dev/null 2>&1
  install -m 0600 "$fixture/server.crt" \
    "$storage/caddy/certificates/test-ca/phone.example.test/phone.example.test.crt"
  install -m 0600 "$fixture/server.key" \
    "$storage/caddy/certificates/test-ca/phone.example.test/phone.example.test.key"
  tar --create --file "$archive" --directory "$storage" caddy
}

run_sync() {
  env \
    RINGRING_TLS_SYNC_TEST_MODE=1 \
    RINGRING_TLS_SYNC_TEST_CHECKOUT="$checkout" \
    RINGRING_TLS_SYNC_TEST_OUTPUT="$output" \
    RINGRING_TLS_SYNC_TEST_ARCHIVE="$archive" \
    RINGRING_TLS_SYNC_TEST_REFRESH="$fixture/refresh.sh" \
    RINGRING_TLS_SYNC_TEST_LOCK="$lock" \
    RINGRING_TLS_TEST_REFRESH_LOG="$refresh_log" \
    RINGRING_TLS_TEST_REFRESH_EXIT="${RINGRING_TLS_TEST_REFRESH_EXIT:-0}" \
    "$repository/scripts/sync-sip-tls.sh"
}

new_fixture success
sync_output=$(run_sync 2>&1)
printf '%s\n' "$sync_output" | grep -q 'SIP TLS certificate synchronized' || fail 'success was not reported'
test -f "$output/certificate.pem" && test ! -L "$output/certificate.pem" || fail 'certificate was not installed safely'
test -f "$output/private-key.pem" && test ! -L "$output/private-key.pem" || fail 'private key was not installed safely'
test "$(file_mode "$output/certificate.pem")" = 600 || fail 'certificate is not mode 0600'
test "$(file_mode "$output/private-key.pem")" = 600 || fail 'private key is not mode 0600'
cmp -s "$fixture/server.crt" "$output/certificate.pem" || fail 'wrong certificate was installed'
cmp -s "$fixture/server.key" "$output/private-key.pem" || fail 'wrong private key was installed'
test "$(grep -c '^refresh$' "$refresh_log")" -eq 1 || fail 'Asterisk refresh was not requested once'
if printf '%s\n' "$sync_output" | grep -q -- 'BEGIN.*PRIVATE KEY'; then
  fail 'command output exposed private-key material'
fi
second_output=$(run_sync 2>&1)
printf '%s\n' "$second_output" | grep -q 'SIP TLS certificate is current' || fail 'idempotent status was not reported'
test "$(grep -c '^refresh$' "$refresh_log")" -eq 2 || fail 'current certificate did not recheck the running Asterisk container'

new_fixture mismatch
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out "$fixture/wrong.key" >/dev/null 2>&1
install -m 0600 "$fixture/wrong.key" \
  "$storage/caddy/certificates/test-ca/phone.example.test/phone.example.test.key"
tar --create --file "$archive" --directory "$storage" caddy
if run_sync >/dev/null 2>&1; then
  fail 'a certificate with the wrong private key was accepted'
fi
test ! -e "$output/certificate.pem" && test ! -e "$output/private-key.pem" || fail 'mismatched material reached the managed output'

new_fixture unsafe-archive
ln -s /tmp/ringring-sip-tls-sync-test-escape \
  "$storage/caddy/certificates/test-ca/phone.example.test/unsafe-link"
tar --create --file "$archive" --directory "$storage" caddy
if run_sync >/dev/null 2>&1; then
  fail 'an archive containing a symbolic link was accepted'
fi
test ! -e "$output/certificate.pem" && test ! -e "$output/private-key.pem" || fail 'unsafe archive material reached the managed output'

new_fixture concurrent-start
mkdir -m 0700 "$lock"
(
  sleep 1
  printf '%s\n' "$$" >"$lock/pid"
) &
lock_writer=$!
concurrent_output=$(run_sync 2>&1)
wait "$lock_writer"
printf '%s\n' "$concurrent_output" | grep -q 'Another SIP TLS synchronization is already running' || fail 'a concurrent lock creation was not recognized safely'
test ! -e "$output/certificate.pem" && test ! -e "$output/private-key.pem" || fail 'a concurrent run synchronized certificate material'
find "$lock" -depth -delete

new_fixture stale-lock
mkdir -m 0700 "$lock"
printf '99999999\n' >"$lock/pid"
run_sync >/dev/null
test -f "$output/certificate.pem" && test -f "$output/private-key.pem" || fail 'a stale lock was not recovered'
test ! -e "$lock" || fail 'the recovered lock was not released by its new owner'

new_fixture deferred
export RINGRING_TLS_TEST_REFRESH_EXIT=75
set +e
run_sync >/dev/null 2>&1
deferred_result=$?
set -e
unset RINGRING_TLS_TEST_REFRESH_EXIT
test "$deferred_result" -eq 75 || fail 'an active-call reload deferral did not remain distinguishable'
test -f "$output/certificate.pem" && test -f "$output/private-key.pem" || fail 'deferred material was not staged for the next safe reload'

new_fixture symlink
real_output="$fixture/real-output"
install -d -m 0700 "$real_output"
ln -s "$real_output" "$output"
if run_sync >/dev/null 2>&1; then
  fail 'a symlinked managed TLS directory was accepted'
fi
test -z "$(find "$real_output" -mindepth 1 -print -quit)" || fail 'a symlink target received certificate material'

echo "SIP TLS sync tests passed: private material, lock races/recovery, reload deferral, unsafe input rejection, and symlink refusal."
