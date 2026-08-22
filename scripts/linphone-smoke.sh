#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
network=ringring-linphone-smoke
containers="ringring-linphone-smoke-asterisk ringring-linphone-smoke-client"

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

work_directory=$(mktemp -d /tmp/ringring-linphone-smoke.XXXXXX)
case "$work_directory" in
  /tmp/ringring-linphone-smoke.*) ;;
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
docker build --quiet --tag ringring-linphone-smoke:5.5.3 \
  --file "$repository/deploy/linphone-smoke/Dockerfile" "$repository/deploy/linphone-smoke" >/dev/null

echo "Rendering Asterisk state and Linphone XML with RingRing's production code..."
mkdir -p "$work_directory/state" "$work_directory/provision" "$work_directory/client"
chmod 0777 "$work_directory/client"
docker run --rm \
  --volume "$repository:/src:ro" --volume "$work_directory:/out" --workdir /src \
  golang:1.26-bookworm go run ./scripts/linphone-smoke-config /out/generated 172.31.90.20
mv "$work_directory/generated/pjsip.conf" "$work_directory/state/pjsip.conf"
mv "$work_directory/generated/extensions.conf" "$work_directory/state/extensions.conf"
mv "$work_directory/generated/linphone.xml" "$work_directory/provision/linphone.xml"
chmod 0444 "$work_directory/state/pjsip.conf" "$work_directory/state/extensions.conf" \
  "$work_directory/provision/linphone.xml"
find "$work_directory/generated" -depth -delete
grep -Fq 'xmlns="http://www.linphone.org/xsds/lpconfig.xsd"' "$work_directory/provision/linphone.xml"
grep -Fq '&lt;sip:172.31.90.20:5060;transport=udp&gt;' "$work_directory/provision/linphone.xml"

docker network create --internal --subnet 172.31.90.0/24 "$network" >/dev/null
docker run -d --name ringring-linphone-smoke-asterisk \
  --network "$network" --ip 172.31.90.20 \
  --env SIP_PUBLIC_HOST=172.31.90.20 --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --volume "$work_directory/state:/var/lib/ringring/asterisk:ro" \
  --volume "$repository/scripts/sip-smoke/pjsip.conf.in:/etc/asterisk/pjsip.conf.in:ro" \
  ringring-asterisk-sip-smoke:22.10.1 >/dev/null

ready=0
for elapsed in $(seq 1 30); do
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

echo "Asking Linphone to fetch the XML and register its provisioned account..."
docker run -d --name ringring-linphone-smoke-client \
  --network "$network" --ip 172.31.90.30 \
  --volume "$work_directory/provision:/provision:ro" \
  --volume "$work_directory/client:/state" \
  ringring-linphone-smoke:5.5.3 >/dev/null

registered=0
for elapsed in $(seq 1 35); do
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
for elapsed in $(seq 1 15); do
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

touch "$work_directory/client/stop"
client_done=0
for elapsed in $(seq 1 20); do
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
docker logs ringring-linphone-smoke-client
echo "Asterisk confirmed the provisioned Linphone contact was registered and reachable."
