#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
network=ringring-linphone-smoke
containers="ringring-linphone-smoke-asterisk ringring-linphone-smoke-register-b ringring-linphone-smoke-phone-b ringring-linphone-smoke-client"

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
work_directory=$(mktemp -d "$temporary_root/ringring-linphone-smoke.XXXXXX")
case "$work_directory" in
  "$temporary_root"/ringring-linphone-smoke.*) ;;
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

echo "Building pinned Asterisk and official Linphone smoke-test images..."
docker build --quiet --tag ringring-asterisk-sip-smoke:22.10.1 \
  --file "$repository/deploy/asterisk/Dockerfile" "$repository/deploy/asterisk" >/dev/null
docker build --quiet --tag ringring-sipp-smoke:3.7.7 \
  --file "$repository/deploy/sip-smoke/Dockerfile" "$repository/deploy/sip-smoke" >/dev/null
docker build --quiet --tag ringring-linphone-smoke:5.5.3 \
  --file "$repository/deploy/linphone-smoke/Dockerfile" "$repository/deploy/linphone-smoke" >/dev/null

echo "Rendering Asterisk state and Linphone XML with RingRing's production code..."
mkdir -p "$work_directory/state" "$work_directory/provision" "$work_directory/client" \
  "$work_directory/certs" "$work_directory/logs/register-b" "$work_directory/logs/phone-b"
chmod 0777 "$work_directory/client"
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 30 \
  -subj '/CN=RingRing Linphone smoke CA' \
  -keyout "$work_directory/certs/ca.key" -out "$work_directory/certs/ca.crt" >/dev/null 2>&1
openssl req -newkey rsa:2048 -nodes -sha256 -subj '/CN=172.31.90.20' \
  -keyout "$work_directory/certs/private-key.pem" \
  -out "$work_directory/certs/server.csr" >/dev/null 2>&1
printf '%s\n' 'subjectAltName=IP:172.31.90.20' 'extendedKeyUsage=serverAuth' \
  >"$work_directory/certs/server.ext"
openssl x509 -req -sha256 -days 30 -in "$work_directory/certs/server.csr" \
  -CA "$work_directory/certs/ca.crt" -CAkey "$work_directory/certs/ca.key" \
  -CAcreateserial -extfile "$work_directory/certs/server.ext" \
  -out "$work_directory/certs/certificate.pem" >/dev/null 2>&1
chmod 0444 "$work_directory/certs/ca.crt" "$work_directory/certs/certificate.pem"
chmod 0400 "$work_directory/certs/ca.key" "$work_directory/certs/private-key.pem"
docker run --rm \
  --volume "$repository:/src:ro" --volume "$work_directory:/out" --workdir /src \
  golang:1.26-bookworm go run ./scripts/linphone-smoke-config /out/generated 172.31.90.20
mv "$work_directory/generated/pjsip.conf" "$work_directory/state/pjsip.conf"
mv "$work_directory/generated/extensions.conf" "$work_directory/state/extensions.conf"
mv "$work_directory/generated/linphone.xml" "$work_directory/provision/linphone.xml"
chmod 0444 "$work_directory/state/pjsip.conf" "$work_directory/state/extensions.conf" \
  "$work_directory/provision/linphone.xml"
# Exact release candidates are checked out under umask 077. Make only the
# read-only bind roots traversable to Asterisk and Linphone after they drop
# root; the TLS private key itself deliberately remains root-only.
chmod 0555 "$work_directory/state" "$work_directory/provision" "$work_directory/certs"
find "$work_directory/generated" -depth -delete
grep -Fq 'xmlns="http://www.linphone.org/xsds/lpconfig.xsd"' "$work_directory/provision/linphone.xml"
grep -Fq '&lt;sip:172.31.90.20:5061;transport=tls&gt;' "$work_directory/provision/linphone.xml"

docker network create --internal --subnet 172.31.90.0/24 "$network" >/dev/null
docker run -d --name ringring-linphone-smoke-asterisk \
  --network "$network" --ip 172.31.90.20 \
  --env SIP_PUBLIC_HOST=172.31.90.20 --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --volume "$work_directory/state:/var/lib/ringring/asterisk:ro" \
  --volume "$work_directory/certs:/run/ringring-host-tls:ro" \
  --volume "$repository/scripts/sip-smoke/pjsip.conf.in:/etc/asterisk/pjsip.conf.in:ro" \
  ringring-asterisk-sip-smoke:22.10.1 >/dev/null

ready=0
for _ in $(seq 1 30); do
  if docker exec ringring-linphone-smoke-asterisk asterisk -rx 'core show uptime' >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if test "$ready" -ne 1; then
  echo "The isolated Asterisk did not become ready." >&2
  docker logs ringring-linphone-smoke-asterisk >&2 || true
  exit 1
fi
docker exec ringring-linphone-smoke-asterisk \
  asterisk -rx 'core set verbose 3' >/dev/null
docker exec ringring-linphone-smoke-asterisk \
  asterisk -rx 'pjsip show transport transport-tls' | grep -q 'transport-tls'

scenario_mount="$repository/scripts/sip-smoke:/scenarios:ro"
sipp_image=ringring-sipp-smoke:3.7.7

echo "Registering disposable extension 102..."
docker run --rm --name ringring-linphone-smoke-register-b \
  --network "$network" --ip 172.31.90.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/register-b:/logs" --workdir /logs \
  "$sipp_image" 172.31.90.20:5060 -sf /scenarios/register.xml \
  -i 172.31.90.30 -p 5062 -s rr_smoke_b -au rr_smoke_b -ap smoke-only-b-2Kp8w6Nz \
  -key branch_tag linphoneb -m 1 -trace_err >/dev/null

docker run -d --name ringring-linphone-smoke-phone-b \
  --network "$network" --ip 172.31.90.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.90.30 -p 5062 \
  -mi 172.31.90.30 -mp 6000 -aa -rtp_echo -trace_err >/dev/null

qualified=0
for _ in $(seq 1 15); do
  if docker exec ringring-linphone-smoke-asterisk asterisk -rx 'pjsip show contacts' | grep -Eq 'rr_smoke_b/.*Avail'; then
    qualified=1
    break
  fi
  sleep 1
done
if test "$qualified" -ne 1; then
  echo "Disposable extension 102 did not become reachable." >&2
  exit 1
fi

# Asterisk's immediate OPTIONS check is intentionally handled by a disposable
# receiver. Replace it so the measured process observes only the party call.
docker rm -f ringring-linphone-smoke-phone-b >/dev/null
find "$work_directory/logs/phone-b" -type f -delete
docker run -d --name ringring-linphone-smoke-phone-b \
  --network "$network" --ip 172.31.90.30 --volume "$scenario_mount" \
  --volume "$work_directory/logs/phone-b:/logs" --workdir /logs \
  "$sipp_image" -sf /scenarios/uas.xml -i 172.31.90.30 -p 5062 \
  -mi 172.31.90.30 -mp 6000 -m 1 -aa -rtp_echo -trace_err >/dev/null
sleep 1

echo "Asking Linphone to fetch the XML and register its provisioned account..."
docker run -d --name ringring-linphone-smoke-client \
  --network "$network" --ip 172.31.90.40 \
  --volume "$work_directory/provision:/provision:ro" \
  --volume "$work_directory/certs:/certs:ro" \
  --volume "$work_directory/client:/state" \
  ringring-linphone-smoke:5.5.3 >/dev/null

registered=0
for _ in $(seq 1 35); do
  if test -f "$work_directory/client/registered"; then
    registered=1
    break
  fi
  if test "$(docker inspect --format '{{.State.Running}}' ringring-linphone-smoke-client)" != true; then
    break
  fi
  sleep 1
done
if test "$registered" -ne 1; then
  echo "Linphone did not report a successful provisioned registration." >&2
  docker logs ringring-linphone-smoke-client >&2 || true
  docker logs --tail 200 ringring-linphone-smoke-asterisk >&2 || true
  exit 1
fi

available=0
for _ in $(seq 1 15); do
  if docker exec ringring-linphone-smoke-asterisk asterisk -rx 'pjsip show contacts' | grep -Eq 'rr_smoke_a/.*Avail'; then
    available=1
    break
  fi
  sleep 1
done
if test "$available" -ne 1; then
  echo "Asterisk did not find the Linphone contact reachable." >&2
  exit 1
fi
docker exec ringring-linphone-smoke-asterisk grep -Eq \
  'SecurityEvent="SuccessfulAuth".*AccountID="rr_smoke_a".*LocalAddress="IPV4/TLS/' \
  /var/log/asterisk/security

echo "Calling extension 102 from provisioned Linphone and checking echoed RTP..."
touch "$work_directory/client/call"
call_complete=0
for _ in $(seq 1 35); do
  if test -f "$work_directory/client/call-complete"; then
    call_complete=1
    break
  fi
  if test "$(docker inspect --format '{{.State.Running}}' ringring-linphone-smoke-client)" != true; then
    break
  fi
  sleep 1
done
if test "$call_complete" -ne 1; then
  echo "Linphone did not complete the extension call and media assertions." >&2
  docker logs ringring-linphone-smoke-client >&2 || true
  docker logs ringring-linphone-smoke-asterisk 2>&1 | \
    grep -E '(<--- Received SIP request|<--- Transmitting SIP response|^INVITE sip:|^SIP/2.0 |^From:|^To:|^CSeq:|^c=IN |^m=audio |^a=rtpmap:|^a=(sendrecv|inactive)|No matching endpoint|Executing \[)' | \
    tail -n 120 >&2 || true
  exit 1
fi

phone_b_done=0
for _ in $(seq 1 15); do
  if test "$(docker inspect --format '{{.State.Running}}' ringring-linphone-smoke-phone-b)" != true; then
    phone_b_done=1
    break
  fi
  sleep 1
done
if test "$phone_b_done" -ne 1; then
  echo "Disposable extension 102 did not finish the Linphone call." >&2
  exit 1
fi
phone_b_result=$(docker inspect --format '{{.State.ExitCode}}' ringring-linphone-smoke-phone-b)
if test "$phone_b_result" -ne 0; then
  docker logs ringring-linphone-smoke-phone-b >&2 || true
  docker logs ringring-linphone-smoke-client 2>&1 | \
    grep -Ei '(ACK sip:|BYE sip:|CallSession|call state|streams running|error)' | \
    tail -n 160 >&2 || true
  docker logs ringring-linphone-smoke-asterisk 2>&1 | \
    grep -E '(<--- Received SIP request|<--- Transmitting SIP response|^INVITE sip:|^ACK sip:|^BYE sip:|^SIP/2.0 |^From:|^To:|^CSeq:|No matching endpoint|Executing \[)' | \
    tail -n 160 >&2 || true
  exit "$phone_b_result"
fi

channels=$(docker exec ringring-linphone-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'

touch "$work_directory/client/stop"
client_done=0
for _ in $(seq 1 20); do
  if test "$(docker inspect --format '{{.State.Running}}' ringring-linphone-smoke-client)" != true; then
    client_done=1
    break
  fi
  sleep 1
done
if test "$client_done" -ne 1; then
  echo "Linphone did not shut down after the registrar assertion." >&2
  docker logs ringring-linphone-smoke-client >&2 || true
  exit 1
fi
client_result=$(docker inspect --format '{{.State.ExitCode}}' ringring-linphone-smoke-client)
if test "$client_result" -ne 0; then
  docker logs ringring-linphone-smoke-client >&2 || true
  exit "$client_result"
fi
docker logs ringring-linphone-smoke-client 2>&1 | \
  grep '^Linphone provisioning smoke passed:'
echo "Asterisk confirmed both contacts were reachable and routed Linphone's extension call."
