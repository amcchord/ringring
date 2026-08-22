#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
network=ringring-sip-smoke
containers="ringring-sip-smoke-app ringring-sip-smoke-asterisk ringring-sip-smoke-register-a ringring-sip-smoke-register-b ringring-sip-smoke-phone-a ringring-sip-smoke-phone-b"

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

work_directory=$(mktemp -d /tmp/ringring-sip-smoke.XXXXXX)
case "$work_directory" in
  /tmp/ringring-sip-smoke.*) ;;
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
  "$work_directory/logs/ringring-sip-smoke-register-a" \
  "$work_directory/logs/ringring-sip-smoke-register-b" \
  "$work_directory/logs/ringring-sip-smoke-phone-a" \
  "$work_directory/logs/ringring-sip-smoke-phone-b"
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
grep -q '^context=rr-party-pty-smoke$' "$work_directory/state/pjsip.conf"
grep -Fq 'exten => *10,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq ' same => n,Echo()' "$work_directory/state/extensions.conf"
grep -Fq 'exten => *15,1,Answer()' "$work_directory/state/extensions.conf"
grep -Fq 'choose-extension,pty_smoke,${CHANNEL(endpoint)}' "$work_directory/state/extensions.conf"
grep -q '^exten => 102,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"

docker run -d --name ringring-sip-smoke-asterisk \
  --network "$network" --ip 172.31.89.20 \
  --env SIP_PUBLIC_HOST=172.31.89.20 --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --volume "$work_directory/state:/var/lib/ringring/asterisk:ro" \
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
docker exec ringring-sip-smoke-asterisk asterisk -rx 'core set verbose 3' >/dev/null

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

echo "Registering disposable phone A..."
run_and_wait ringring-sip-smoke-register-a 20 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-register-a:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/register.xml \
  -i 172.31.89.40 -p 5061 -s rr_smoke_a -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx \
  -key branch_tag registera -m 1 -trace_msg -trace_err
docker rm ringring-sip-smoke-register-a >/dev/null

contacts=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'pjsip show contacts')
printf '%s\n' "$contacts" | grep -q 'rr_smoke_a/'
printf '%s\n' "$contacts" | grep -q 'rr_smoke_b/'
printf '%s\n' "$contacts" | grep -Eq 'rr_smoke_b/.*Avail'

docker rm -f ringring-sip-smoke-phone-b >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-b" -type f -delete
docker run -d --name ringring-sip-smoke-phone-b \
  --network "$network" --ip 172.31.89.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.89.30 -p 5062 \
  -mi 172.31.89.30 -mp 6000 -m 1 -aa -rtp_echo -trace_msg -trace_err >/dev/null
sleep 1

echo "Calling extension 102 and checking the PCMU RTP round trip..."
run_and_wait ringring-sip-smoke-phone-a 30 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/uac.xml \
  -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
  -s 102 -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa -rtpcheck_debug \
  -key branch_tag calla -trace_msg -trace_err

set +e
wait_for_container ringring-sip-smoke-phone-b 10
phone_b_result=$?
set -e
if test "$phone_b_result" -ne 0; then
  exit "$phone_b_result"
fi

channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'

docker rm ringring-sip-smoke-phone-a >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-a" -type f -delete
echo "Calling *10 and checking the single-phone RTP echo..."
run_and_wait ringring-sip-smoke-phone-a 30 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/uac.xml \
  -i 172.31.89.40 -p 5061 -mi 172.31.89.40 -mp 4000 \
  -s '*10' -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx -m 1 -aa -rtpcheck_debug \
  -key branch_tag echoa -trace_msg -trace_err

channels=$(docker exec ringring-sip-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'

docker rm ringring-sip-smoke-phone-a >/dev/null
find "$work_directory/logs/ringring-sip-smoke-phone-a" -type f -delete
echo "Choosing extension 103 through the authenticated *15 voice flow..."
run_and_wait ringring-sip-smoke-phone-a 35 \
  --network "$network" --ip 172.31.89.40 --volume "$scenario_mount" \
  --volume "$work_directory/logs/ringring-sip-smoke-phone-a:/logs" --workdir /logs \
  "$sipp_image" 172.31.89.20:5060 -sf /scenarios/choose-extension.xml \
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
  asterisk -rx 'dialplan show 103@rr-party-pty-smoke' | grep -Fq 'Dial(PJSIP/rr_smoke_a,30)'
if docker exec ringring-sip-smoke-asterisk \
  asterisk -rx 'dialplan show 101@rr-party-pty-smoke' | grep -Fq 'Dial(PJSIP/rr_smoke_a,30)'; then
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
echo "SIP smoke test passed: 2 authenticated registrations, extension call, *10 echo, bidirectional RTP, and authenticated *15 DTMF extension selection."
