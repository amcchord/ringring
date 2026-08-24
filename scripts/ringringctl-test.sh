#!/bin/sh
set -eu
umask 077

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
test_root=$(mktemp -d /tmp/ringringctl-test.XXXXXX)
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  find "$test_root" -depth -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

fail() {
  echo "ringringctl test failed: $*" >&2
  exit 1
}

write_fake_commands() {
  fixture=$1
  fake_bin="$fixture/fake-bin"
  install -d -m 0700 "$fake_bin"
  cat >"$fake_bin/docker" <<'EOF'
#!/bin/sh
set -eu
printf 'docker %s\n' "$*" >>"$RINGRING_TEST_LOG"
if test "$1" = compose; then
  shift
  case "$1" in
    config|up) exit 0 ;;
    build)
      if test -n "${RINGRING_TEST_FAIL_BUILD_ONCE:-}" && test ! -e "$RINGRING_TEST_FAIL_BUILD_ONCE"; then
        : >"$RINGRING_TEST_FAIL_BUILD_ONCE"
        exit 42
      fi
      exit 0
      ;;
    ps) printf 'healthy\n'; exit 0 ;;
    exec)
      case "$*" in
        *verify-state*) printf '{"status":"ok","users":0}\n' ;;
        *verify-ami*) printf '{"status":"ok","contact_count":0}\n' ;;
        *verify-openai-retention*)
          if test "${RINGRING_TEST_FAIL_RETENTION:-0}" = 1; then exit 43; fi
          printf '{"status":"ok","organization_type":"zero_data_retention","projects_verified":1}\n'
          ;;
        *"dialplan show s@rr-phone-check"*) printf 'same => n,Playback(auth-thankyou)\n' ;;
        *127.0.0.1:9090/metrics*) printf 'ringring_database_up 1\nringring_asterisk_ami_up 1\n' ;;
        *"pjsip show transport transport-tls"*) printf 'Transport: transport-tls TLS 0.0.0.0:5061\n' ;;
      esac
      exit 0
      ;;
    logs)
      printf '%s\n' 'app-1 | {"level":"WARN","msg":"initial telephony reconcile","error":"reload Asterisk: connect to AMI: dial tcp: lookup asterisk on 127.0.0.11:53: no such host"}'
      if test "${RINGRING_TEST_UNEXPECTED_APP_WARNING:-0}" = 1; then
        printf '%s\n' 'app-1 | {"level":"WARN","msg":"unexpected fixture warning"}'
      fi
      exit 0
      ;;
  esac
fi
case "$1" in
  info|version) exit 0 ;;
esac
exit 0
EOF
  cat >"$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu
printf 'curl %s\n' "$*" >>"$RINGRING_TEST_LOG"
case "$*" in
  *"%{http_code}"*) printf '404' ;;
  *) printf '{"status":"ok"}\n' ;;
esac
EOF
  cat >"$fake_bin/fail2ban-client" <<'EOF'
#!/bin/sh
set -eu
printf 'fail2ban %s\n' "$*" >>"$RINGRING_TEST_LOG"
exit 0
EOF
  chmod 0700 "$fake_bin/docker" "$fake_bin/curl" "$fake_bin/fail2ban-client"
}

write_fixture_scripts() {
  fixture=$1
  checkout="$fixture/checkout"
  cat >"$checkout/scripts/test-firewall.sh" <<'EOF'
#!/bin/sh
set -eu
printf 'firewall %s\n' "$*" >>"$RINGRING_TEST_LOG"
EOF
  cat >"$checkout/scripts/test-tls-operator.sh" <<'EOF'
#!/bin/sh
set -eu
printf 'tls %s\n' "$*" >>"$RINGRING_TEST_LOG"
EOF
  cat >"$checkout/scripts/test-tls-probe.sh" <<'EOF'
#!/bin/sh
set -eu
printf 'tls-probe %s\n' "$*" >>"$RINGRING_TEST_LOG"
EOF
  cat >"$checkout/scripts/backup.sh" <<'EOF'
#!/bin/sh
set -eu
backup_root=$1
install -d -m 0700 "$backup_root"
short=$(git rev-parse --short HEAD)
archive="$backup_root/ringring-test-$short.tar.gz"
: >"$archive"
: >"$archive.sha256"
printf 'backup %s\n' "$archive" >>"$RINGRING_TEST_LOG"
printf 'Backup created: %s\nChecksum: %s.sha256\n' "$archive" "$archive"
EOF
  cat >"$checkout/scripts/restore-drill.sh" <<'EOF'
#!/bin/sh
set -eu
test -f "$1"
test -f "$1.sha256"
printf 'restore %s\n' "$1" >>"$RINGRING_TEST_LOG"
printf 'Restore drill passed.\n'
EOF
  chmod 0700 "$checkout/scripts/test-firewall.sh" "$checkout/scripts/test-tls-operator.sh" \
    "$checkout/scripts/test-tls-probe.sh" "$checkout/scripts/backup.sh" "$checkout/scripts/restore-drill.sh"
}

new_fixture() {
  name=$1
  fixture="$test_root/$name"
  checkout="$fixture/checkout"
  config="$fixture/etc-ringring"
  backups="$fixture/backups"
  log="$fixture/actions.log"
  install -d -m 0700 "$checkout/deploy/state" "$checkout/scripts" "$config" "$backups"
  cp "$repository/ringringctl" "$checkout/ringringctl"
  chmod 0700 "$checkout/ringringctl"
  printf 'name: ringring\nservices: {}\n' >"$checkout/compose.yaml"
  printf '.env\ndeploy/state/*\n!deploy/state/.gitkeep\n' >"$checkout/.gitignore"
  : >"$checkout/deploy/state/.gitkeep"
  write_fixture_scripts "$fixture"
  git -C "$checkout" init -q -b main
  git -C "$checkout" config user.email ringring-test@example.invalid
  git -C "$checkout" config user.name 'RingRing Test'
  git -C "$checkout" add .
  git -C "$checkout" commit -qm 'fixture base'
  write_fake_commands "$fixture"
  : >"$log"
  answers="$fixture/answers.env"
  {
    printf 'RINGRING_DOMAIN=phone.example.test\n'
    printf 'TZ=America/New_York\n'
    printf 'HOST_SIGNUP_CODE=rainbow-42\n'
    printf 'OPENAI_ADMIN_KEY=sk-admin-abcdefghijklmnop\n'
    printf 'OPENAI_PARTY_SPEND_LIMIT_CENTS=2500\n'
  } >"$answers"
  chmod 0600 "$answers"
}

run_ctl() {
  env \
    PATH="$fixture/fake-bin:$PATH" \
    RINGRINGCTL_TEST_MODE=1 \
    RINGRINGCTL_TEST_CHECKOUT="$checkout" \
    RINGRINGCTL_TEST_CONFIG_DIR="$config" \
    RINGRINGCTL_TEST_BACKUP_ROOT="$backups" \
    RINGRINGCTL_TEST_FIREWALL_INSTALLER="$checkout/scripts/test-firewall.sh" \
    RINGRINGCTL_TEST_TLS_INSTALLER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_SYNCHRONIZER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_PROBE="$checkout/scripts/test-tls-probe.sh" \
    RINGRING_TEST_LOG="$log" \
    "$checkout/ringringctl" "$@"
}

assert_private() {
  mode=$(if stat -c %a "$1" >/dev/null 2>&1; then stat -c %a "$1"; else stat -f %Lp "$1"; fi)
  case "$mode" in 600|400) ;; *) fail "$1 mode is $mode" ;; esac
}

assert_mode() {
  mode=$(if stat -c %a "$1" >/dev/null 2>&1; then stat -c %a "$1"; else stat -f %Lp "$1"; fi)
  test "$mode" = "$2" || fail "$1 mode is $mode, expected $2"
}

assert_successful_install() {
  new_fixture install-success
  output=$(run_ctl install --answers "$answers" --yes 2>&1)
  printf '%s\n' "$output" | grep -q 'RingRing is ready at https://phone.example.test/' || fail 'install success message is missing'
  for secret in rainbow-42 sk-admin-abcdefghijklmnop; do
    if printf '%s\n' "$output" | grep -q "$secret"; then
      fail 'install output exposed a supplied secret'
    fi
  done
  test ! -e "$config/install.pending" || fail 'install marker was not removed'
  for path in "$config/app.env" "$config/asterisk.env" "$checkout/.env"; do
    assert_private "$path"
  done
  grep -qx 'APP_BASE_URL=https://phone.example.test' "$config/app.env" || fail 'base URL was not rendered'
  grep -qx 'METRICS_ADDR=127.0.0.1:9090' "$config/app.env" || fail 'private metrics address was not rendered'
  grep -qx 'HOST_SIGNUP_CODE=rainbow-42' "$config/app.env" || fail 'signup code was not rendered'
  grep -qx 'OPENAI_ADMIN_KEY=sk-admin-abcdefghijklmnop' "$config/app.env" || fail 'OpenAI key was not rendered'
  grep -qx 'OPENAI_PARTY_SPEND_LIMIT_CENTS=2500' "$config/app.env" || fail 'spend ceiling was not rendered'
  grep -qx 'RINGRING_DOMAIN=phone.example.test' "$checkout/.env" || fail 'Compose domain was not rendered'
  app_ami=$(sed -n 's/^ASTERISK_AMI_SECRET=//p' "$config/app.env")
  asterisk_ami=$(sed -n 's/^ASTERISK_AMI_SECRET=//p' "$config/asterisk.env")
  test -n "$app_ami" && test "$app_ami" = "$asterisk_ami" || fail 'AMI secrets differ'
  master=$(sed -n 's/^RINGRING_MASTER_KEY=//p' "$config/app.env")
  session=$(sed -n 's/^SESSION_SECRET=//p' "$config/app.env")
  test -n "$master" && test -n "$session" && test "$master" != "$session" || fail 'application keys are empty or reused'
  test "$(printf '%s' "$master" | openssl base64 -d -A | wc -c | tr -d ' ')" -eq 32 || fail 'master key is not 32 bytes'
  test "$(printf '%s' "$session" | openssl base64 -d -A | wc -c | tr -d ' ')" -eq 32 || fail 'session key is not 32 bytes'
  for path in "$checkout/deploy/state/app" "$checkout/deploy/state/asterisk" "$checkout/deploy/state/log/asterisk"; do
    test -d "$path" || fail "state directory is missing: $path"
  done
  test -d "$config/tls" || fail 'root-only SIP TLS directory is missing'
  grep -q '^docker compose build$' "$log" || fail 'install did not build the stack'
  grep -q '^docker compose up -d --remove-orphans$' "$log" || fail 'install did not start the stack'
  grep -q '^firewall install ' "$log" || fail 'install did not configure the SIP firewall'
  firewall_line=$(grep -n '^firewall install ' "$log" | cut -d: -f1)
  up_line=$(grep -n '^docker compose up -d --remove-orphans$' "$log" | cut -d: -f1)
  test "$firewall_line" -lt "$up_line" || fail 'install exposed SIP before configuring the firewall'
  grep -q '^tls install ' "$log" || fail 'install did not install automatic SIP TLS synchronization'
  grep -q '^tls sync .* required$' "$log" || fail 'install did not require initial trusted SIP TLS synchronization'
  grep -q '^tls-probe phone.example.test$' "$log" || fail 'install did not verify the public SIP TLS endpoint'
  grep -q '^curl .*https://phone.example.test/readyz' "$log" || fail 'install did not verify public readiness'
  grep -q '^curl .*https://phone.example.test/metrics' "$log" || fail 'install did not reject public metrics exposure'
  grep -q '^docker compose exec -T app curl .*127.0.0.1:9090/metrics' "$log" || fail 'install did not verify private metrics'
  test -z "$(git -C "$checkout" status --porcelain)" || fail 'install dirtied the checkout'
  doctor_output=$(run_ctl doctor 2>&1) || fail "doctor rejected the installed fixture: $doctor_output"
  retention_output=$(run_ctl openai-retention 2>&1) || fail "retention check rejected the installed fixture: $retention_output"
  printf '%s\n' "$retention_output" | grep -q '"organization_type":"zero_data_retention"' || fail 'retention check omitted the verified organization type'
  printf '%s\n' "$retention_output" | grep -q '"projects_verified":1' || fail 'retention check omitted the verified project count'
  apns_source="$fixture/AuthKey_ABCDEFGHIJ.p8"
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$apns_source" >/dev/null 2>&1
  chmod 0600 "$apns_source"
  : >"$log"
  apns_dry_run=$(run_ctl configure-apns --key-file "$apns_source" --team-id 7PTN7E8EDS --key-id ABCDEFGHIJ --dry-run --yes 2>&1) || fail "APNs dry run failed: $apns_dry_run"
  printf '%s\n' "$apns_dry_run" | grep -q 'Dry run passed' || fail 'APNs dry run did not report success'
  test ! -e "$config/apns/AuthKey_ABCDEFGHIJ.p8" || fail 'APNs dry run copied the provider key'
  test ! -s "$log" || fail 'APNs dry run invoked a service command'
  apns_output=$(run_ctl configure-apns --key-file "$apns_source" --team-id 7PTN7E8EDS --key-id ABCDEFGHIJ --yes 2>&1) || fail "APNs configuration failed: $apns_output"
  printf '%s\n' "$apns_output" | grep -q 'background-call delivery is configured' || fail 'APNs configuration success message is missing'
  grep -qx 'APNS_TEAM_ID=7PTN7E8EDS' "$config/app.env" || fail 'APNs team ID was not rendered'
  grep -qx 'APNS_KEY_ID=ABCDEFGHIJ' "$config/app.env" || fail 'APNs key ID was not rendered'
  grep -qx 'APNS_PRIVATE_KEY_FILE=/run/secrets/ringring-apns/AuthKey_ABCDEFGHIJ.p8' "$config/app.env" || fail 'APNs key path was not rendered'
  grep -qx 'APNS_BUNDLE_ID=com.mcchord.ringring' "$config/app.env" || fail 'APNs bundle ID was not rendered'
  grep -qx 'APNS_ENVIRONMENT=production' "$config/app.env" || fail 'APNs environment was not rendered'
  assert_mode "$config/apns" 750
  assert_mode "$config/apns/AuthKey_ABCDEFGHIJ.p8" 440
  grep -q '^docker compose up -d --force-recreate app$' "$log" || fail 'APNs configuration did not recreate the app'
	: >"$log"
	doctor_output=$(run_ctl doctor 2>&1) || fail "doctor rejected the updated fixture: $doctor_output"
	if RINGRING_TEST_FAIL_RETENTION=1 run_ctl openai-retention >/dev/null 2>&1; then
		fail 'standalone retention check ignored provider verification failure'
	fi
  sed 's/^METRICS_ADDR=.*/METRICS_ADDR=0.0.0.0:9090/' "$config/app.env" >"$fixture/app.invalid-metrics.env"
  chmod 0600 "$fixture/app.invalid-metrics.env"
  mv "$fixture/app.invalid-metrics.env" "$config/app.env"
  if run_ctl doctor >/dev/null 2>&1; then
    fail 'doctor accepted a non-loopback production metrics listener'
  fi
  sed '/^METRICS_ADDR=/d' "$config/app.env" >"$fixture/app.legacy.env"
  chmod 0600 "$fixture/app.legacy.env"
  mv "$fixture/app.legacy.env" "$config/app.env"
  legacy_doctor_output=$(run_ctl doctor 2>&1) || fail "doctor rejected a legacy environment that uses the safe default: $legacy_doctor_output"
  if RINGRING_TEST_UNEXPECTED_APP_WARNING=1 run_ctl doctor >/dev/null 2>&1; then
    fail 'doctor accepted an unexpected application warning'
  fi
  if run_ctl install --answers "$answers" --yes >/dev/null 2>&1; then
    fail 'a second fresh install overwrote existing configuration'
  fi
}

assert_install_validation() {
  new_fixture dry-run
  output=$(run_ctl install --answers "$answers" --yes --dry-run 2>&1)
  printf '%s\n' "$output" | grep -q 'Dry run passed' || fail 'install dry run did not report success'
  test ! -e "$config/app.env" || fail 'install dry run created configuration'
  test ! -s "$log" || fail 'install dry run invoked a service command'

  new_fixture bad-mode
  chmod 0644 "$answers"
  if run_ctl install --answers "$answers" --yes >/dev/null 2>&1; then
    fail 'install accepted a non-private answers file'
  fi
  test ! -e "$config/app.env" || fail 'bad answers mode created configuration'

  new_fixture bad-domain
  sed 's/phone.example.test/https:\/\/phone.example.test\/path/' "$answers" >"$fixture/bad.env"
  chmod 0600 "$fixture/bad.env"
  if run_ctl install --answers "$fixture/bad.env" --yes >/dev/null 2>&1; then
    fail 'install accepted a URL instead of a hostname'
  fi
  test ! -e "$config/app.env" || fail 'bad domain created configuration'

  new_fixture bad-code
  sed 's/rainbow-42/short/' "$answers" >"$fixture/bad.env"
  chmod 0600 "$fixture/bad.env"
  if run_ctl install --answers "$fixture/bad.env" --yes >/dev/null 2>&1; then
    fail 'install accepted a short family access code'
  fi
  test ! -e "$config/app.env" || fail 'bad access code created configuration'

  new_fixture symlinked-config
  real_config="$fixture/real-config"
  mv "$config" "$real_config"
  ln -s "$real_config" "$config"
  if run_ctl install --answers "$answers" --yes >/dev/null 2>&1; then
    fail 'install accepted a symlinked managed configuration path'
  fi
  test ! -e "$real_config/app.env" || fail 'symlinked configuration path received deployment secrets'
}

assert_install_resume() {
  new_fixture install-resume
  fail_once="$fixture/fail-build-once"
  if env RINGRING_TEST_FAIL_BUILD_ONCE="$fail_once" \
    PATH="$fixture/fake-bin:$PATH" \
    RINGRINGCTL_TEST_MODE=1 \
    RINGRINGCTL_TEST_CHECKOUT="$checkout" \
    RINGRINGCTL_TEST_CONFIG_DIR="$config" \
    RINGRINGCTL_TEST_BACKUP_ROOT="$backups" \
    RINGRINGCTL_TEST_FIREWALL_INSTALLER="$checkout/scripts/test-firewall.sh" \
    RINGRINGCTL_TEST_TLS_INSTALLER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_SYNCHRONIZER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_PROBE="$checkout/scripts/test-tls-probe.sh" \
    RINGRING_TEST_LOG="$log" \
    "$checkout/ringringctl" install --answers "$answers" --yes >/dev/null 2>&1; then
    fail 'the forced install build failure unexpectedly succeeded'
  fi
  test -f "$config/install.pending" || fail 'failed install did not keep its marker'
  assert_private "$config/app.env"
  output=$(env RINGRING_TEST_FAIL_BUILD_ONCE="$fail_once" \
    PATH="$fixture/fake-bin:$PATH" \
    RINGRINGCTL_TEST_MODE=1 \
    RINGRINGCTL_TEST_CHECKOUT="$checkout" \
    RINGRINGCTL_TEST_CONFIG_DIR="$config" \
    RINGRINGCTL_TEST_BACKUP_ROOT="$backups" \
    RINGRINGCTL_TEST_FIREWALL_INSTALLER="$checkout/scripts/test-firewall.sh" \
    RINGRINGCTL_TEST_TLS_INSTALLER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_SYNCHRONIZER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_PROBE="$checkout/scripts/test-tls-probe.sh" \
    RINGRING_TEST_LOG="$log" \
    "$checkout/ringringctl" install --yes 2>&1)
  printf '%s\n' "$output" | grep -q 'Resuming the pending RingRing install' || fail 'install did not announce its resume'
  test ! -e "$config/install.pending" || fail 'resumed install kept its marker'
  for secret in rainbow-42 sk-admin-abcdefghijklmnop; do
    if printf '%s\n' "$output" | grep -q "$secret"; then
      fail 'resumed install output exposed a supplied secret'
    fi
  done
}

prepare_upgrade() {
  name=$1
  new_fixture "$name"
  run_ctl install --answers "$answers" --yes >/dev/null 2>&1
  remote="$fixture/origin.git"
  git init -q --bare --initial-branch=main "$remote"
  git -C "$checkout" remote add origin "$remote"
  git -C "$checkout" push -q -u origin main
  publisher="$fixture/publisher"
  git clone -q "$remote" "$publisher"
  git -C "$publisher" config user.email ringring-test@example.invalid
  git -C "$publisher" config user.name 'RingRing Test'
  printf 'next\n' >"$publisher/VERSION"
  git -C "$publisher" add VERSION
  git -C "$publisher" commit -qm 'fixture upgrade'
  git -C "$publisher" push -q origin main
  target_commit=$(git -C "$publisher" rev-parse HEAD)
  : >"$log"
}

assert_successful_upgrade() {
  prepare_upgrade upgrade-success
  output=$(run_ctl upgrade --yes 2>&1)
  test "$(git -C "$checkout" rev-parse HEAD)" = "$target_commit" || fail 'upgrade did not reach the target commit'
  test ! -e "$config/upgrade.pending" || fail 'upgrade marker was not removed'
  test "$(grep -c '^backup ' "$log")" -eq 2 || fail 'upgrade did not create exactly two backups'
  test "$(grep -c '^restore ' "$log")" -eq 2 || fail 'upgrade did not drill exactly two backups'
  grep -q '^firewall refresh ' "$log" || fail 'upgrade did not refresh the checked-in SIP firewall policy'
  grep -q '^tls install ' "$log" || fail 'upgrade did not refresh automatic SIP TLS synchronization'
  grep -q '^tls sync .* required$' "$log" || fail 'upgrade did not synchronize the trusted SIP TLS certificate'
  printf '%s\n' "$output" | grep -q "Upgrade complete at $target_commit" || fail 'upgrade success message is missing'
  test -z "$(git -C "$checkout" status --porcelain)" || fail 'upgrade dirtied the checkout'
}

assert_self_update_is_stable() {
  prepare_upgrade self-update
  {
    printf '#!/bin/sh\n'
    printf 'exit 97\n'
  } >"$publisher/ringringctl"
  git -C "$publisher" add ringringctl
  git -C "$publisher" commit -qm 'replace controller fixture'
  git -C "$publisher" push -q origin main
  target_commit=$(git -C "$publisher" rev-parse HEAD)
  run_ctl upgrade --yes --skip-public-check >/dev/null 2>&1 || fail 'upgrade did not survive replacement of its running controller'
  test "$(git -C "$checkout" rev-parse HEAD)" = "$target_commit" || fail 'self-update fixture did not reach the target commit'
  test ! -e "$config/upgrade.pending" || fail 'self-update fixture kept its upgrade marker'
}

assert_upgrade_dry_run() {
  prepare_upgrade upgrade-dry-run
  git -C "$checkout" fetch -q origin
  base_commit=$(git -C "$checkout" rev-parse HEAD)
  output=$(run_ctl upgrade --dry-run --skip-public-check 2>&1)
  printf '%s\n' "$output" | grep -q 'Dry run passed using existing local refs' || fail 'upgrade dry run did not report its local-ref boundary'
  test "$(git -C "$checkout" rev-parse HEAD)" = "$base_commit" || fail 'upgrade dry run moved the checkout'
  test "$(grep -c '^backup ' "$log" || true)" -eq 0 || fail 'upgrade dry run created a backup'
  test "$(grep -c '^docker compose build$' "$log" || true)" -eq 0 || fail 'upgrade dry run built the stack'
  test ! -e "$config/upgrade.pending" || fail 'upgrade dry run wrote a marker'
}

assert_upgrade_resume() {
  prepare_upgrade upgrade-resume
  fail_once="$fixture/fail-build-once"
  if env RINGRING_TEST_FAIL_BUILD_ONCE="$fail_once" \
    PATH="$fixture/fake-bin:$PATH" \
    RINGRINGCTL_TEST_MODE=1 \
    RINGRINGCTL_TEST_CHECKOUT="$checkout" \
    RINGRINGCTL_TEST_CONFIG_DIR="$config" \
    RINGRINGCTL_TEST_BACKUP_ROOT="$backups" \
    RINGRINGCTL_TEST_FIREWALL_INSTALLER="$checkout/scripts/test-firewall.sh" \
    RINGRINGCTL_TEST_TLS_INSTALLER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_SYNCHRONIZER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_PROBE="$checkout/scripts/test-tls-probe.sh" \
    RINGRING_TEST_LOG="$log" \
    "$checkout/ringringctl" upgrade --yes --skip-public-check >/dev/null 2>&1; then
    fail 'the forced build failure unexpectedly succeeded'
  fi
  test -f "$config/upgrade.pending" || fail 'failed upgrade did not keep its marker'
  test "$(git -C "$checkout" rev-parse HEAD)" = "$target_commit" || fail 'failed upgrade did not record the exact merged target'
  test "$(grep -c '^backup ' "$log")" -eq 1 || fail 'failed upgrade made an unexpected backup count'
  test "$(grep -c '^restore ' "$log")" -eq 1 || fail 'failed upgrade made an unexpected drill count'
  output=$(env RINGRING_TEST_FAIL_BUILD_ONCE="$fail_once" \
    PATH="$fixture/fake-bin:$PATH" \
    RINGRINGCTL_TEST_MODE=1 \
    RINGRINGCTL_TEST_CHECKOUT="$checkout" \
    RINGRINGCTL_TEST_CONFIG_DIR="$config" \
    RINGRINGCTL_TEST_BACKUP_ROOT="$backups" \
    RINGRINGCTL_TEST_FIREWALL_INSTALLER="$checkout/scripts/test-firewall.sh" \
    RINGRINGCTL_TEST_TLS_INSTALLER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_SYNCHRONIZER="$checkout/scripts/test-tls-operator.sh" \
    RINGRINGCTL_TEST_TLS_PROBE="$checkout/scripts/test-tls-probe.sh" \
    RINGRING_TEST_LOG="$log" \
    "$checkout/ringringctl" upgrade --yes --skip-public-check 2>&1)
  printf '%s\n' "$output" | grep -q 'Resuming the exact pending upgrade' || fail 'upgrade did not announce its exact resume'
  test ! -e "$config/upgrade.pending" || fail 'resumed upgrade kept its marker'
  test "$(grep -c '^backup ' "$log")" -eq 2 || fail 'resume repeated the pre-upgrade backup instead of adding only the post backup'
  test "$(grep -c '^restore ' "$log")" -eq 2 || fail 'resume repeated the pre-upgrade drill instead of adding only the post drill'
}

assert_successful_install
assert_install_validation
assert_install_resume
assert_successful_upgrade
assert_self_update_is_stable
assert_upgrade_dry_run
assert_upgrade_resume
printf 'ringringctl tests passed: secret-safe install, validation, doctor, verified upgrade, and exact failure resume.\n'
