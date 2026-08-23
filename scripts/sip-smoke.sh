#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
network=ringring-sip-smoke
containers="ringring-sip-smoke-app ringring-sip-smoke-asterisk ringring-sip-smoke-register-a ringring-sip-smoke-register-b ringring-sip-smoke-register-generated ringring-sip-smoke-phone-a ringring-sip-smoke-phone-b"

if docker network inspect "$network" >/dev/null 2>&1; then
  echo "The isolated Docker network $network already exists; another smoke test may be running." >&2
  exit 1
fi
for container in $containers; do
  if test -n "$(docker ps -aq --filter "name=^/${container}$")"; then
    echo "The isolated container $container already exists; another smoke test may be running." >&2
    exit 1
  fi
done

temporary_root=${RINGRING_SMOKE_TMP_ROOT:-/tmp}
work_directory=$(mktemp -d "$temporary_root/ringring-sip-smoke.XXXXXX")
case "$work_directory" in
  "$temporary_root"/ringring-sip-smoke.*) ;;
  *) echo "Unexpected temporary directory: $work_directory" >&2; exit 1 ;;
esac

cleanup() {
  for container in $containers; do
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
  docker network rm "$network" >/dev/null 2>&1 || true
  find "$work_directory" -depth -delete >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

wait_for_container() {
  container=$1
  limit=$2
  elapsed=0
  while test "$(docker inspect --format '{{.State.Running}}' "$container" 2>/dev/null)" = true; do
    if test "$elapsed" -ge "$limit"; then
      echo "$container did not finish within ${limit}s" >&2
      docker logs "$container" >&2 || true
      docker logs --tail 200 ringring-sip-smoke-asterisk >&2 || true
      if test -d "$work_directory/logs/$container"; then
        for log_file in "$work_directory/logs/$container"/*; do
          if test -f "$log_file"; then
            echo "--- $log_file ---" >&2
            sed -n '1,400p' "$log_file" >&2
          fi
        done
      fi
      return 124
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
  code=$(docker inspect --format '{{.State.ExitCode}}' "$container")
  if test "$code" -ne 0; then
    echo "$container exited with status $code" >&2
    docker logs "$container" >&2 || true
    for service in ringring-sip-smoke-app ringring-sip-smoke-asterisk; do
      if test -n "$(docker ps -aq --filter "name=^/${service}$")"; then
        echo "--- $service ---" >&2
        docker logs --tail 200 "$service" >&2 || true
      fi
    done
    if test -d "$work_directory/logs/$container"; then
      for log_file in "$work_directory/logs/$container"/*; do
        if test -f "$log_file"; then
          echo "--- $log_file ---" >&2
          sed -n '1,400p' "$log_file" >&2
        fi
      done
    fi
  fi
  return "$code"
}

run_and_wait() {
  name=$1
  limit=$2
  shift 2
  docker run -d --name "$name" "$@" >/dev/null
  set +e
  wait_for_container "$name" "$limit"
  result=$?
  set -e
  return "$result"
}

echo "Building RingRing plus pinned Asterisk and SIPp smoke-test images..."
docker build --quiet --tag ringring-app-sip-smoke:local \
  --file "$repository/Dockerfile" "$repository" >/dev/null
docker build --quiet --target build --tag ringring-app-sip-smoke-builder:local \
  --file "$repository/Dockerfile" "$repository" >/dev/null
docker build --quiet --tag ringring-asterisk-sip-smoke:22.10.1 \
  --file "$repository/deploy/asterisk/Dockerfile" "$repository/deploy/asterisk" >/dev/null
docker build --quiet --tag ringring-sipp-smoke:3.7.7 \
  --file "$repository/deploy/sip-smoke/Dockerfile" "$repository/deploy/sip-smoke" >/dev/null

echo "Creating disposable RingRing state for two party phones..."
mkdir -p "$work_directory/app" "$work_directory/state" \
  "$work_directory/certs" \
  "$work_directory/logs/ringring-sip-smoke-register-a" \
  "$work_directory/logs/ringring-sip-smoke-register-b" \
  "$work_directory/logs/ringring-sip-smoke-register-generated" \
  "$work_directory/logs/ringring-sip-smoke-phone-a" \
  "$work_directory/logs/ringring-sip-smoke-phone-b"
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 30 \
  -subj '/CN=RingRing SIP smoke CA' \
  -keyout "$work_directory/certs/ca.key" -out "$work_directory/certs/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=ringring-sip-smoke.test' \
  -keyout "$work_directory/certs/private-key.pem" \
  -out "$work_directory/certs/server.csr" >/dev/null 2>&1
printf '%s\n' \
  'subjectAltName=DNS:ringring-sip-smoke.test,IP:172.31.89.20' \
  'extendedKeyUsage=serverAuth' >"$work_directory/certs/server.ext"
openssl x509 -req -sha256 -days 30 -in "$work_directory/certs/server.csr" \
  -CA "$work_directory/certs/ca.crt" -CAkey "$work_directory/certs/ca.key" \
  -CAcreateserial -extfile "$work_directory/certs/server.ext" \
  -out "$work_directory/certs/certificate.pem" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=RingRing SIP smoke client' \
  -keyout "$work_directory/certs/client.key" \
  -out "$work_directory/certs/client.csr" >/dev/null 2>&1
printf 'extendedKeyUsage=clientAuth\n' >"$work_directory/certs/client.ext"
openssl x509 -req -sha256 -days 30 -in "$work_directory/certs/client.csr" \
  -CA "$work_directory/certs/ca.crt" -CAkey "$work_directory/certs/ca.key" \
  -CAcreateserial -extfile "$work_directory/certs/client.ext" \
  -out "$work_directory/certs/client.crt" >/dev/null 2>&1
chmod 0444 "$work_directory/certs/ca.crt" "$work_directory/certs/certificate.pem" "$work_directory/certs/client.crt"
chmod 0400 "$work_directory/certs/ca.key" "$work_directory/certs/private-key.pem" "$work_directory/certs/client.key"
docker run --rm --network none \
  --volume "$work_directory:/out" \
  ringring-app-sip-smoke-builder:local \
  go run ./scripts/sip-smoke-state /out/app/ringring.db
chmod -R a+rwX "$work_directory/app" "$work_directory/state"

docker network create --internal --subnet 172.31.89.0/24 "$network" >/dev/null
docker run -d --name ringring-sip-smoke-app \
  --network "$network" --network-alias app --ip 172.31.89.10 \
  --env APP_ENV=development --env HTTP_ADDR=:8080 \
  --env APP_BASE_URL=http://ringring-sip-smoke.test \
  --env DATABASE_PATH=/data/ringring.db --env SIP_PUBLIC_HOST=172.31.89.20 \
  --env ASTERISK_CONFIG_DIR=/asterisk --env ASTERISK_AMI_ADDR=172.31.89.20:5038 \
  --env ASTERISK_AMI_USER=ringring --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --env FASTAGI_ADDR=:4573 --env AI_AUDIO_ADDR=:4574 \
  --env DEV_AUTH=true \
  --volume "$work_directory/app:/data" --volume "$work_directory/state:/asterisk" \
  ringring-app-sip-smoke:local >/dev/null

app_ready=0
for elapsed in $(seq 1 30); do
  if docker exec ringring-sip-smoke-app curl --fail --silent http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
    app_ready=1
    break
  fi
  sleep 1
done
if test "$app_ready" -ne 1; then
  echo "The isolated RingRing app did not become ready." >&2
  docker logs ringring-sip-smoke-app >&2 || true
  exit 1
fi
grep -q '^direct_media=no$' "$work_directory/state/pjsip.conf"
grep -q '^context=rr-party-pty_smoke$' "$work_directory/state/pjsip.conf"
grep -Fq 'exten => *10,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq ' same => n,Echo()' "$work_directory/state/extensions.conf"
grep -Fq 'exten => *15,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq "choose-extension,pty_smoke,\${CHANNEL(endpoint)}" "$work_directory/state/extensions.conf"
grep -Fq 'exten => 0,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq 'exten => *0,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq "AGI(agi://app:4573/operator,pty_smoke,help,\${CHANNEL(endpoint)})" "$work_directory/state/extensions.conf"
grep -q '^exten => 102,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"
grep -Fq 'Set(__RINGRING_CONFERENCE=rrc-pty_smoke-102)' "$work_directory/state/extensions.conf"
grep -Fq 'Dial(PJSIP/rr_smoke_b,30,G(rr-party-bridge^s^1))' "$work_directory/state/extensions.conf"
grep -Fq 'exten => *16102,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq 'join-party,pty_smoke,${CHANNEL(endpoint)},${RINGRING_CONFERENCE}' "$work_directory/state/extensions.conf"
grep -Fq 'GotoIf($["${DIALSTATUS}"="ANSWER"]?rr-phone-done:unavailable)' "$work_directory/state/extensions.conf"
grep -Fq 'exten => _X!,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq 'exten => _*X!,1,Answer()' "$work_directory/state/extensions.conf"

docker run -d --name ringring-sip-smoke-asterisk \
  --network "$network" --network-alias ringring-sip-smoke.test --ip 172.31.89.20 \
  --env SIP_PUBLIC_HOST=172.31.89.20 --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --volume "$work_directory/state:/var/lib/ringring/asterisk:ro" \
  --volume "$work_directory/certs:/run/ringring-host-tls:ro" \
  --volume "$repository/scripts/sip-smoke/pjsip.conf.in:/etc/asterisk/pjsip.conf.in:ro" \
  --volume "$repository/scripts/sip-smoke/manager.conf.in:/etc/asterisk/manager.conf.in:ro" \
  ringring-asterisk-sip-smoke:22.10.1 >/dev/null

ready=0
for elapsed in $(seq 1 30); do
  if docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show uptime' >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if test "$ready" -ne 1; then
  echo "The isolated Asterisk did not become ready." >&2
  docker logs ringring-sip-smoke-asterisk >&2 || true
  exit 1
fi
docker exec ringring-sip-smoke-asterisk sh -eu -c '
  test "$(stat -c %a /etc/asterisk/extensions.conf)" = 640
  test "$(stat -c %U:%G /etc/asterisk/extensions.conf)" = asterisk:ringring
  test "$(stat -c %a /etc/asterisk/confbridge.conf)" = 640
  test "$(stat -c %U:%G /etc/asterisk/confbridge.conf)" = asterisk:ringring
'
docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'pjsip show transport transport-tls' | grep -q 'transport-tls'
tls_report=$(docker exec ringring-sip-smoke-asterisk sh -c \
  "openssl s_client -brief -tls1_2 -verify_return_error \
    -verify_hostname ringring-sip-smoke.test -servername ringring-sip-smoke.test \
    -CAfile /run/ringring-host-tls/ca.crt -connect 127.0.0.1:5061 </dev/null 2>&1")
printf '%s\n' "$tls_report" | grep -q 'Protocol version: TLSv1.2'
printf '%s\n' "$tls_report" | grep -q 'Verification: OK'

scenario_mount="$repository/scripts/sip-smoke:/scenarios:ro"
sipp_image=ringring-sipp-smoke:3.7.7

echo "Registering disposable phone B..."
run_and_wait ringring-sip-smoke-register-b 20 \
  --network "$network" --ip 172.31.89.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-register-b:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/register.xml \
  -i 172.31.89.30 -p 5062 -s rr_smoke_b -au rr_smoke_b -ap smoke-only-b-2Kp8w6Nz \
  -key branch_tag registerb -m 1 -trace_msg -trace_err
docker rm ringring-sip-smoke-register-b >/dev/null

# Keep a receiver on the registered address while Asterisk performs its immediate
# contact qualification. It is replaced below so the measured process sees only
# the actual phone call.
docker run -d --name ringring-sip-smoke-phone-b \
  --network "$network" --ip 172.31.89.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.89.30 -p 5062 \
  -mi 172.31.89.30 -mp 6000 -aa -rtp_echo -trace_msg -trace_err >/dev/null
qualified=0
for elapsed in $(seq 1 10); do
  if docker exec ringring-sip-smoke-asterisk asterisk -rx 'pjsip show contacts' | grep -Eq 'rr_smoke_b/.*Avail'; then
    qualified=1
    break
  fi
  sleep 1
done
if test "$qualified" -ne 1; then
  echo "Disposable phone B did not answer Asterisk's contact check." >&2
  exit 1
fi

echo "Registering disposable phone A through verified TLS 1.2..."
docker run -d --name ringring-sip-smoke-register-a \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/certs:/certs:ro" \
  --volume "$work_directory/logs/ringring-sip-smoke-register-a:/logs" --workdir /logs \
  "$sipp_image" ringring-sip-smoke.test:5061 -t l1 -tls_version 1.2 \
  -tls_ca /certs/ca.crt -tls_cert /certs/client.crt -tls_key /certs/client.key \
  -sf /scenarios/register-hold.xml \
  -i 172.31.89.40 -p 5061 -s rr_smoke_a -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx \
  -key branch_tag registera -m 1 -trace_msg -trace_err >/dev/null

registered_a=0
for elapsed in $(seq 1 10); do
  contacts=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'pjsip show contacts')
  if printf '%s\n' "$contacts" | grep -q 'rr_smoke_a/'; then
    registered_a=1
    break
  fi
  if test "$(docker inspect --format '{{.State.Running}}' ringring-sip-smoke-register-a 2>/dev/null)" != true; then
    echo "Disposable phone A closed its TLS registration connection early." >&2
    wait_for_container ringring-sip-smoke-register-a 1 || true
    exit 1
  fi
  sleep 1
done
if test "$registered_a" -ne 1; then
  echo "Disposable phone A did not create an authenticated TLS contact." >&2
  exit 1
fi
docker exec ringring-sip-smoke-asterisk grep -Eq \
  'SecurityEvent="SuccessfulAuth".*AccountID="rr_smoke_a".*LocalAddress="IPV4/TLS/' \
  /var/log/asterisk/security
docker exec ringring-sip-smoke-asterisk grep -Eq \
  'SecurityEvent="SuccessfulAuth".*AccountID="rr_smoke_b".*LocalAddress="IPV4/UDP/' \
  /var/log/asterisk/security
printf '%s\n' "$contacts" | grep -q 'rr_smoke_b/'
printf '%s\n' "$contacts" | grep -Eq 'rr_smoke_b/.*Avail'
dialplan=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'dialplan show s@rr-phone-check')
printf '%s\n' "$dialplan" | grep -q 'auth-thankyou'
if printf '%s\n' "$dialplan" | grep -q 'Dial('; then
  echo "Phone-check context unexpectedly contains a Dial application" >&2
  exit 1
fi
for prompt in hello your extension is auth-thankyou; do
  docker exec ringring-sip-smoke-asterisk \
    test -s "/var/lib/asterisk/sounds/en/$prompt.gsm"
done
for prompt in sorry number-not-answering please-try-call-later cannot-complete-as-dialed please-try-again; do
  docker exec ringring-sip-smoke-asterisk \
    test -s "/var/lib/asterisk/sounds/en/$prompt.gsm"
done
invalid_number=$(docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show 222@rr-party-pty_smoke')
printf '%s\n' "$invalid_number" | grep -Fq "'_X!'"
printf '%s\n' "$invalid_number" | grep -Fq 'operator,pty_smoke,misdial'
printf '%s\n' "$invalid_number" | grep -Fq 'cannot-complete-as-dialed'
invalid_service=$(docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show *12@rr-party-pty_smoke')
printf '%s\n' "$invalid_service" | grep -Fq "'_*X!'"
printf '%s\n' "$invalid_service" | grep -Fq 'operator,pty_smoke,misdial'
printf '%s\n' "$invalid_service" | grep -Fq 'cannot-complete-as-dialed'
operator_line=$(docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show 0@rr-party-pty_smoke')
printf '%s\n' "$operator_line" | grep -Fq "'0'"
printf '%s\n' "$operator_line" | grep -Fq 'operator,pty_smoke,help'
printf '%s\n' "$operator_line" | grep -Fq 'please-try-again'
docker rm -f ringring-sip-smoke-register-a >/dev/null

echo "Sending a host-scoped incoming ring test to phone B..."
docker rm -f ringring-sip-smoke-phone-b >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-b" -type f -delete
docker run -d --name ringring-sip-smoke-phone-b \
  --network "$network" --ip 172.31.89.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.89.30 -p 5062 \
  -mi 172.31.89.30 -mp 6000 -m 1 -aa -trace_msg -trace_err >/dev/null
sleep 1
docker exec ringring-sip-smoke-app sh -c \
  'rm -f /tmp/ringring-smoke-cookies && curl --fail --silent --show-error \
    --cookie-jar /tmp/ringring-smoke-cookies --data-urlencode email=smoke@example.test \
    --output /dev/null http://127.0.0.1:8080/auth/dev'
party_page=$(docker exec ringring-sip-smoke-app curl --fail --silent --show-error \
  --cookie /tmp/ringring-smoke-cookies http://127.0.0.1:8080/parties/pty_smoke)
csrf=$(printf '%s\n' "$party_page" | sed -n 's/.*name="csrf" value="\([^"]*\)".*/\1/p' | head -n 1)
test -n "$csrf"
echo "Adding a separately credentialed phone to extension 102..."
added_phone_setup=$(docker exec ringring-sip-smoke-app sh -c \
  "curl --fail --silent --show-error --location --cookie /tmp/ringring-smoke-cookies \
    --data-urlencode csrf=$csrf --data-urlencode 'device_label=Smoke spare phone' \
    http://127.0.0.1:8080/parties/pty_smoke/members/mem_smoke_b/devices")
printf '%s\n' "$added_phone_setup" | grep -q 'Another phone ready'
printf '%s\n' "$added_phone_setup" | grep -q 'Existing phones stay connected'
added_sip_username=$(printf '%s\n' "$added_phone_setup" | \
  sed -n 's/.*id="setup-username" data-setup-value="\([^"]*\)".*/\1/p' | head -n 1)
added_sip_password=$(printf '%s\n' "$added_phone_setup" | \
  sed -n 's/.*id="setup-password" data-setup-value="\([^"]*\)".*/\1/p' | head -n 1)
printf '%s\n' "$added_sip_username" | grep -Eq '^[1-9][0-9]{5}$'
printf '%s\n' "$added_sip_password" | grep -Eq '^[1-9][0-9]{11}$'
grep -Fq "[$added_sip_username-auth]" "$work_directory/state/pjsip.conf"
shared_extension=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'dialplan show 102@rr-party-pty_smoke')
printf '%s\n' "$shared_extension" | grep -q 'PJSIP/rr_smoke_b'
printf '%s\n' "$shared_extension" | grep -q "PJSIP/$added_sip_username"
echo "Registering the generated digits-only phone credential..."
run_and_wait ringring-sip-smoke-register-generated 20 \
  --network "$network" --ip 172.31.89.50 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-register-generated:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/register.xml \
  -i 172.31.89.50 -p 5064 -s "$added_sip_username" -au "$added_sip_username" -ap "$added_sip_password" \
  -key branch_tag generated -m 1 -trace_msg -trace_err
docker rm ringring-sip-smoke-register-generated >/dev/null
docker exec ringring-sip-smoke-asterisk grep -Eq \
  "SecurityEvent=\"SuccessfulAuth\".*AccountID=\"$added_sip_username\".*LocalAddress=\"IPV4/UDP/" \
  /var/log/asterisk/security
ring_started=$(date +%s)
docker exec ringring-sip-smoke-app sh -c \
  "curl --fail --silent --show-error --location --cookie /tmp/ringring-smoke-cookies \
    --data-urlencode csrf=$csrf http://127.0.0.1:8080/parties/pty_smoke/devices/dev_smoke_b/ring-test" | \
  grep -q 'Ring test sent'
set +e
wait_for_container ringring-sip-smoke-phone-b 25
ring_result=$?
set -e
if test "$ring_result" -ne 0; then
  echo "The incoming ring-test phone did not complete cleanly." >&2
  exit 1
fi
ring_elapsed=$(($(date +%s) - ring_started))
if test "$ring_elapsed" -lt 4 || test "$ring_elapsed" -gt 21; then
  echo "The incoming ring-test prompt had an unexpected duration." >&2
  exit 1
fi
if ! grep -R -q 'RingRing setup' "$work_directory/logs/ringring-sip-smoke-phone-b"; then
  echo "The incoming ring-test INVITE did not carry the fixed caller label." >&2
  exit 1
fi
if ! docker exec ringring-sip-smoke-asterisk test ! -e /var/log/asterisk/cdr-csv/Master.csv; then
  echo "The CDR-disabled ring-test unexpectedly created a CSV call record." >&2
  exit 1
fi
channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
if ! printf '%s\n' "$channels" | grep -q '^0 active channels'; then
  echo "The incoming ring test left an active Asterisk channel." >&2
  exit 1
fi

docker rm -f ringring-sip-smoke-phone-b >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-b" -type f -delete
docker run -d --name ringring-sip-smoke-phone-b \
  --network "$network" --ip 172.31.89.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.89.30 -p 5062 \
  -mi 172.31.89.30 -mp 6000 -m 1 -aa -rtp_echo -trace_msg -trace_err >/dev/null
sleep 1

echo "Calling extension 102, publishing it live, and joining from another party phone..."
docker run -d --name ringring-sip-smoke-phone-a \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/certs:/certs:ro" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" ringring-sip-smoke.test:5061 -t l1 -tls_version 1.2 \
  -tls_ca /certs/ca.crt -tls_cert /certs/client.crt -tls_key /certs/client.key \
  -sf /scenarios/uac-hold.xml \
  -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
  -s 102 -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa -rtpcheck_debug \
  -key branch_tag calla -key sip_user rr_smoke_a -trace_msg -trace_err >/dev/null

conference_ready=0
for elapsed in $(seq 1 12); do
  conference_channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels concise')
  conference_phone_count=$(printf '%s\n' "$conference_channels" | awk -F'!' '$1 ~ /^PJSIP\// && $6 == "ConfBridge" && $7 == "rrc-pty_smoke-102,ringring_bridge,ringring_initial" {count++} END {print count+0}')
  if test "$conference_phone_count" -eq 2; then
    conference_ready=1
    break
  fi
  sleep 1
done
if test "$conference_ready" -ne 1; then
  echo "The answered call did not become a two-phone RingRing conference." >&2
  printf '%s\n' "$conference_channels" >&2
  exit 1
fi

live_phonebook=$(docker exec ringring-sip-smoke-app curl --fail --silent --show-error \
  --cookie /tmp/ringring-smoke-cookies http://127.0.0.1:8080/parties/pty_smoke/live)
printf '%s\n' "$live_phonebook" | grep -q 'Party calls'
printf '%s\n' "$live_phonebook" | grep -q '2 phones in this call'
printf '%s\n' "$live_phonebook" | grep -q '\*16102'
if printf '%s\n' "$live_phonebook" | grep -Eq 'rr_smoke|[0-9]{6}-[0-9A-Fa-f]+'; then
  echo "The live phonebook exposed an internal SIP or channel identifier." >&2
  exit 1
fi

docker rm -f ringring-sip-smoke-register-generated >/dev/null 2>&1 || true
find "$work_directory/logs/ringring-sip-smoke-register-generated" -type f -delete
docker run -d --name ringring-sip-smoke-register-generated \
  --network "$network" --ip 172.31.89.60 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-register-generated:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/friendly-failure.xml \
  -i 172.31.89.60 -p 5066 -mi 172.31.89.60 -mp 7000 \
  -s '*16102' -au "$added_sip_username" -ap "$added_sip_password" -m 1 -aa \
  -key branch_tag joincall -key sip_user "$added_sip_username" -trace_msg -trace_err >/dev/null

join_ready=0
for elapsed in $(seq 1 10); do
  conference_channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels concise')
  conference_phone_count=$(printf '%s\n' "$conference_channels" | awk -F'!' '$1 ~ /^PJSIP\// && $6 == "ConfBridge" && $7 ~ /^rrc-pty_smoke-102,/ {count++} END {print count+0}')
  if test "$conference_phone_count" -eq 3; then
    join_ready=1
    break
  fi
  sleep 1
done
if test "$join_ready" -ne 1; then
  echo "The authenticated third phone did not join the live party call." >&2
  printf '%s\n' "$conference_channels" >&2
  docker logs --tail 200 ringring-sip-smoke-app >&2 || true
  docker logs --tail 200 ringring-sip-smoke-asterisk >&2 || true
  for log_file in "$work_directory/logs/ringring-sip-smoke-register-generated"/*; do
    if test -f "$log_file"; then
      echo "--- $log_file ---" >&2
      sed -n '1,300p' "$log_file" >&2
    fi
  done
  exit 1
fi
joined_phonebook=$(docker exec ringring-sip-smoke-app curl --fail --silent --show-error \
  --cookie /tmp/ringring-smoke-cookies http://127.0.0.1:8080/parties/pty_smoke/live)
printf '%s\n' "$joined_phonebook" | grep -q '3 phones in this call'
docker exec ringring-sip-smoke-app curl --fail --silent http://127.0.0.1:9090/metrics | \
  grep -q 'ringring_voice_service_requests_total{service="conference_join",result="ready"} 1'

set +e
wait_for_container ringring-sip-smoke-register-generated 15
join_result=$?
wait_for_container ringring-sip-smoke-phone-a 20
phone_a_result=$?
set -e
if test "$join_result" -ne 0 || test "$phone_a_result" -ne 0; then
  exit 1
fi

set +e
wait_for_container ringring-sip-smoke-phone-b 10
phone_b_result=$?
set -e
if test "$phone_b_result" -ne 0; then
  exit "$phone_b_result"
fi

channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'
if ! docker exec ringring-sip-smoke-asterisk test ! -e /var/log/asterisk/cdr-csv/Master.csv; then
  echo "The private party conference unexpectedly created a CSV call record." >&2
  exit 1
fi

docker rm ringring-sip-smoke-phone-a >/dev/null
docker rm ringring-sip-smoke-register-generated >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-a" -type f -delete
for destination in 222 '*12' 0; do
  echo "Calling unavailable destination $destination and checking for an answered spoken response..."
  failure_started=$(date +%s)
  run_and_wait ringring-sip-smoke-phone-a 20 \
    --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
    --volume "$work_directory/certs:/certs:ro" \
    --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
    "$sipp_image" ringring-sip-smoke.test:5061 -t l1 -tls_version 1.2 \
    -tls_ca /certs/ca.crt -tls_cert /certs/client.crt -tls_key /certs/client.key \
    -sf /scenarios/friendly-failure.xml \
    -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
    -s "$destination" -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa \
    -key branch_tag failure -key sip_user rr_smoke_a -trace_msg -trace_err
  failure_elapsed=$(($(date +%s) - failure_started))
  if test "$failure_elapsed" -lt 2 || test "$failure_elapsed" -gt 15; then
    echo "The friendly failure prompt had an unexpected duration." >&2
    exit 1
  fi
  channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
  printf '%s\n' "$channels" | grep -q '^0 active channels'
  docker rm ringring-sip-smoke-phone-a >/dev/null
  find "$work_directory/logs/ringring-sip-smoke-phone-a" -type f -delete
done

echo "Calling *10 and checking the single-phone RTP echo..."
run_and_wait ringring-sip-smoke-phone-a 30 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/certs:/certs:ro" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" ringring-sip-smoke.test:5061 -t l1 -tls_version 1.2 \
  -tls_ca /certs/ca.crt -tls_cert /certs/client.crt -tls_key /certs/client.key \
  -sf /scenarios/uac.xml \
  -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
  -s '*10' -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa -rtpcheck_debug \
  -key branch_tag echoa -key sip_user rr_smoke_a -trace_msg -trace_err

channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'

docker rm ringring-sip-smoke-phone-a >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-a" -type f -delete
echo "Choosing extension 103 through the authenticated *15 voice flow..."
run_and_wait ringring-sip-smoke-phone-a 35 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/certs:/certs:ro" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" ringring-sip-smoke.test:5061 -t l1 -tls_version 1.2 \
  -tls_ca /certs/ca.crt -tls_cert /certs/client.crt -tls_key /certs/client.key \
  -sf /scenarios/choose-extension.xml \
  -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
  -s '*15' -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa \
  -key branch_tag choosea -trace_msg -trace_err

updated=0
for elapsed in $(seq 1 10); do
  if grep -q '^exten => 103,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"; then
    updated=1
    break
  fi
  sleep 1
done
if test "$updated" -ne 1; then
  echo "The authenticated extension selection did not update generated routing." >&2
  docker logs --tail 200 ringring-sip-smoke-app >&2 || true
  exit 1
fi
if grep -q '^exten => 101,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"; then
  echo "The previous extension remained in generated routing." >&2
  exit 1
fi
grep -q '^exten => 102,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"
grep -Fq 'callerid=RingRing 103 <103>' "$work_directory/state/pjsip.conf"
grep -Fq 'password=smoke-only-a-7Qm4s9Vx' "$work_directory/state/pjsip.conf"
docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show 103@rr-party-pty_smoke' | grep -Fq 'Dial(PJSIP/rr_smoke_a,30,G(rr-party-bridge^s^1))'
if docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show 101@rr-party-pty_smoke' | grep -Fq 'Dial(PJSIP/rr_smoke_a,30,G(rr-party-bridge^s^1))'; then
  echo "Asterisk still routed the previous extension after reload." >&2
  exit 1
fi
if docker logs ringring-sip-smoke-app 2>&1 | grep -Eq 'change extension from phone|reconcile phones after extension change'; then
  echo "The extension selection app path logged a failure." >&2
  docker logs ringring-sip-smoke-app >&2 || true
  exit 1
fi
channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'
echo "SIP smoke test passed: verified TLS 1.2 plus UDP registration, host-added same-extension routing, live three-phone party-call joining with original-caller teardown, mixed-transport extension calling, party-scoped RingRing operator routing with bundled fallback, answered spoken responses for unavailable numbers and star lines, *10 echo, bidirectional RTP, and authenticated *15 DTMF extension selection."
