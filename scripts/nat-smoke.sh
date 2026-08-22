#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
network=ringring-nat-smoke-public
containers="ringring-nat-smoke-sipp-source ringring-nat-smoke-topology ringring-nat-smoke-asterisk"

if docker network inspect "$network" >/dev/null 2>&1; then
  echo "The isolated Docker network $network already exists; another NAT smoke test may be running." >&2
  exit 1
fi
for container in $containers; do
  if test -n "$(docker ps -aq --filter "name=^/${container}$")"; then
    echo "The isolated container $container already exists; another NAT smoke test may be running." >&2
    exit 1
  fi
done

work_directory=$(mktemp -d /tmp/ringring-nat-smoke.XXXXXX)
case "$work_directory" in
  /tmp/ringring-nat-smoke.*) ;;
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

wait_for_file() {
  path=$1
  limit=$2
  description=$3
  elapsed=0
  while test ! -s "$path"; do
    if test "$elapsed" -ge "$limit"; then
      echo "$description did not finish within ${limit}s" >&2
      return 124
    fi
    sleep 1
    elapsed=$((elapsed + 1))
  done
}

media_packets() {
  source=$1
  destination=$2
  docker exec ringring-nat-smoke-topology \
    iptables -L RR_MEDIA -v -n -x | \
    awk -v source="$source" -v destination="$destination" \
      '$8 == source && $9 == destination {print $1; exit}'
}

echo "Building pinned Asterisk, SIPp, and NAT utility images..."
docker build --quiet --tag ringring-asterisk-sip-smoke:22.10.1 \
  --file "$repository/deploy/asterisk/Dockerfile" "$repository/deploy/asterisk" >/dev/null
docker build --quiet --tag ringring-sipp-smoke:3.7.7 \
  --file "$repository/deploy/sip-smoke/Dockerfile" "$repository/deploy/sip-smoke" >/dev/null
docker build --quiet --tag ringring-nat-smoke:ubuntu24 \
  "$repository/deploy/nat-smoke" >/dev/null

echo "Rendering RingRing's production endpoints and party dialplan..."
mkdir -p "$work_directory/state" "$work_directory/logs/register-a" \
  "$work_directory/logs/register-b" "$work_directory/logs/phone-a" \
  "$work_directory/logs/phone-b"
docker run --rm --network none \
  --volume "$repository:/src:ro" --volume "$work_directory:/out" --workdir /src \
  golang:1.26-bookworm go run ./scripts/render-sip-smoke-config.go /out/state
grep -q '^direct_media=no$' "$work_directory/state/pjsip.conf"
grep -q '^rtp_symmetric=yes$' "$work_directory/state/pjsip.conf"
grep -q '^force_rport=yes$' "$work_directory/state/pjsip.conf"
grep -q '^rewrite_contact=yes$' "$work_directory/state/pjsip.conf"
grep -q '^exten => 102,1,NoOp(RingRing party call)$' "$work_directory/state/extensions.conf"

# The official SIPp image is intentionally scratch-based. Extract its static
# pinned binary into the disposable topology container so each phone process
# can run inside its own nested Linux network namespace.
docker create --name ringring-nat-smoke-sipp-source \
  ringring-sipp-smoke:3.7.7 >/dev/null
docker cp ringring-nat-smoke-sipp-source:/sipp "$work_directory/sipp" >/dev/null
docker rm ringring-nat-smoke-sipp-source >/dev/null
chmod 0755 "$work_directory/sipp"

docker network create --internal --subnet 172.31.91.0/24 "$network" >/dev/null
docker run -d --name ringring-nat-smoke-asterisk \
  --network "$network" --ip 172.31.91.20 \
  --env SIP_PUBLIC_HOST=172.31.91.20 --env ASTERISK_AMI_SECRET=isolatedsmoketest \
  --volume "$work_directory/state:/var/lib/ringring/asterisk:ro" \
  --volume "$repository/scripts/sip-smoke/pjsip.conf.in:/etc/asterisk/pjsip.conf.in:ro" \
  ringring-asterisk-sip-smoke:22.10.1 >/dev/null

docker run -d --name ringring-nat-smoke-topology --privileged \
  --network "$network" --ip 172.31.91.30 \
  --volume "$work_directory/sipp:/usr/local/bin/sipp:ro" \
  --volume "$repository/scripts/sip-smoke:/sip-scenarios:ro" \
  --volume "$repository/scripts/nat-smoke:/nat-scenarios:ro" \
  --volume "$work_directory/logs:/logs" \
  ringring-nat-smoke:ubuntu24 >/dev/null

# Give the two household paths distinct public identities, then build a private
# namespace and veth pair for each phone. The topology container performs SNAT;
# neither phone namespace has a direct interface on the server network.
docker exec ringring-nat-smoke-topology \
  ip address add 172.31.91.31/24 dev eth0
docker exec ringring-nat-smoke-topology \
  ip address add 172.31.91.32/24 dev eth0
for side in a b; do
  case "$side" in
    a) subnet=10.231.1; public_ip=172.31.91.31 ;;
    b) subnet=10.231.2; public_ip=172.31.91.32 ;;
  esac
  docker exec ringring-nat-smoke-topology ip netns add "home-$side"
  docker exec ringring-nat-smoke-topology \
    ip link add "rr-$side-main" type veth peer name "rr-$side-phone"
  docker exec ringring-nat-smoke-topology \
    ip link set "rr-$side-phone" netns "home-$side"
  docker exec ringring-nat-smoke-topology \
    ip address add "$subnet.2/24" dev "rr-$side-main"
  docker exec ringring-nat-smoke-topology ip link set "rr-$side-main" up
  docker exec ringring-nat-smoke-topology \
    ip netns exec "home-$side" ip link set lo up
  docker exec ringring-nat-smoke-topology \
    ip netns exec "home-$side" ip address add "$subnet.10/24" dev "rr-$side-phone"
  docker exec ringring-nat-smoke-topology \
    ip netns exec "home-$side" ip link set "rr-$side-phone" up
  docker exec ringring-nat-smoke-topology \
    ip netns exec "home-$side" ip route add 172.31.91.0/24 via "$subnet.2"
  docker exec ringring-nat-smoke-topology \
    iptables -t nat -A POSTROUTING -s "$subnet.0/24" -o eth0 \
      -j SNAT --to-source "$public_ip"
done
docker exec ringring-nat-smoke-topology sysctl -w net.ipv4.ip_forward=1 >/dev/null
docker exec ringring-nat-smoke-topology iptables -P FORWARD ACCEPT
docker exec ringring-nat-smoke-topology iptables -N RR_MEDIA
docker exec ringring-nat-smoke-topology iptables -I FORWARD 1 -j RR_MEDIA
docker exec ringring-nat-smoke-topology iptables -A RR_MEDIA \
  -s 10.231.1.10 -p udp ! --sport 5061 -j RETURN
docker exec ringring-nat-smoke-topology iptables -A RR_MEDIA \
  -d 10.231.1.10 -p udp ! --dport 5061 -j RETURN
docker exec ringring-nat-smoke-topology iptables -A RR_MEDIA \
  -s 10.231.2.10 -p udp ! --sport 5062 -j RETURN
docker exec ringring-nat-smoke-topology iptables -A RR_MEDIA \
  -d 10.231.2.10 -p udp ! --dport 5062 -j RETURN
docker exec ringring-nat-smoke-topology iptables -A RR_MEDIA -j RETURN

docker exec ringring-nat-smoke-topology \
  ip netns exec home-a ip route get 172.31.91.20 | grep -Fq 'via 10.231.1.2'
docker exec ringring-nat-smoke-topology \
  ip netns exec home-b ip route get 172.31.91.20 | grep -Fq 'via 10.231.2.2'

ready=0
for elapsed in $(seq 1 30); do
  if docker exec ringring-nat-smoke-asterisk asterisk -rx 'core show uptime' >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if test "$ready" -ne 1; then
  echo "The isolated Asterisk did not become ready." >&2
  docker logs ringring-nat-smoke-asterisk >&2 || true
  exit 1
fi
docker exec ringring-nat-smoke-asterisk \
  asterisk -rx 'core set verbose 3' >/dev/null

echo "Registering extension 102 through household B's NAT..."
set +e
docker exec --workdir /logs/register-b ringring-nat-smoke-topology \
  ip netns exec home-b /usr/local/bin/sipp 172.31.91.20:5060 \
  -sf /sip-scenarios/register.xml -i 10.231.2.10 -p 5062 \
  -s rr_smoke_b -au rr_smoke_b -ap smoke-only-b-2Kp8w6Nz \
  -key branch_tag natb -m 1 -trace_err \
  >"$work_directory/logs/register-b/output.log" 2>&1
register_b_result=$?
set -e
if test "$register_b_result" -ne 0; then
  sed -n '1,240p' "$work_directory/logs/register-b/output.log" >&2 || true
  exit "$register_b_result"
fi

# Asterisk qualifies a new contact immediately. Let a disposable receiver own
# that OPTIONS transaction, then replace it before the measured call.
docker exec -d ringring-nat-smoke-topology sh -c '
  set +e
  ip netns exec home-b /usr/local/bin/sipp -sf /nat-scenarios/uas.xml \
    -i 10.231.2.10 -p 5062 -mi 10.231.2.10 -mp 6000 -aa -rtp_echo \
    -trace_err >/logs/phone-b/output.log 2>&1 &
  phone_pid=$!
  printf "%s\n" "$phone_pid" >/logs/phone-b/pid
  wait "$phone_pid"
  printf "%s\n" "$?" >/logs/phone-b/status
'
wait_for_file "$work_directory/logs/phone-b/pid" 5 \
  "Household B's qualification receiver"

qualified=0
for elapsed in $(seq 1 15); do
  if docker exec ringring-nat-smoke-asterisk asterisk -rx 'pjsip show contacts' | grep -Eq 'rr_smoke_b/.*Avail'; then
    qualified=1
    break
  fi
  sleep 1
done
if test "$qualified" -ne 1; then
  echo "Extension 102 did not answer qualification through household B's NAT." >&2
  exit 1
fi
phone_b_pid=$(sed -n '1p' "$work_directory/logs/phone-b/pid")
case "$phone_b_pid" in
  ''|*[!0-9]*) echo "Household B's receiver returned an invalid process ID." >&2; exit 1 ;;
esac
docker exec ringring-nat-smoke-topology kill "$phone_b_pid" >/dev/null 2>&1 || true
wait_for_file "$work_directory/logs/phone-b/status" 5 \
  "Household B's qualification receiver shutdown"
find "$work_directory/logs/phone-b" -type f -delete
docker exec ringring-nat-smoke-topology \
  dd if=/dev/zero of=/logs/phone-a/tone.pcm bs=8000 count=1 >/dev/null 2>&1
docker exec ringring-nat-smoke-topology \
  dd if=/dev/zero of=/logs/phone-b/tone.pcm bs=8000 count=1 >/dev/null 2>&1

docker exec -d ringring-nat-smoke-topology sh -c '
  set +e
  ip netns exec home-b /usr/local/bin/sipp -sf /nat-scenarios/uas.xml \
    -i 10.231.2.10 -p 5062 -mi 10.231.2.10 -mp 6000 -m 1 -aa \
    -trace_err >/logs/phone-b/output.log 2>&1 &
  phone_pid=$!
  printf "%s\n" "$phone_pid" >/logs/phone-b/pid
  wait "$phone_pid"
  printf "%s\n" "$?" >/logs/phone-b/status
'
wait_for_file "$work_directory/logs/phone-b/pid" 5 \
  "Household B's call receiver"

echo "Registering extension 101 through household A's NAT..."
set +e
docker exec --workdir /logs/register-a ringring-nat-smoke-topology \
  ip netns exec home-a /usr/local/bin/sipp 172.31.91.20:5060 \
  -sf /sip-scenarios/register.xml -i 10.231.1.10 -p 5061 \
  -s rr_smoke_a -au rr_smoke_a -ap smoke-only-a-7Qm4s9Vx \
  -key branch_tag nata -m 1 -trace_err \
  >"$work_directory/logs/register-a/output.log" 2>&1
register_a_result=$?
set -e
if test "$register_a_result" -ne 0; then
  sed -n '1,240p' "$work_directory/logs/register-a/output.log" >&2 || true
  exit "$register_a_result"
fi

contacts=$(docker exec ringring-nat-smoke-asterisk asterisk -rx 'pjsip show contacts')
printf '%s\n' "$contacts" | grep -q 'rr_smoke_a/'
printf '%s\n' "$contacts" | grep -q 'rr_smoke_b/'
printf '%s\n' "$contacts" | grep -q '172.31.91.31'
printf '%s\n' "$contacts" | grep -q '172.31.91.32'
if printf '%s\n' "$contacts" | grep -Eq '10\.231\.[12]\.10'; then
  echo "Asterisk retained a phone's unreachable private SIP contact." >&2
  exit 1
fi

echo "Calling extension 102 between two household NATs and checking RTP..."
set +e
docker exec --workdir /logs/phone-a ringring-nat-smoke-topology \
  ip netns exec home-a /usr/local/bin/sipp 172.31.91.20:5060 \
  -sf /nat-scenarios/uac.xml -i 10.231.1.10 -p 5061 \
  -mi 10.231.1.10 -mp 4000 -s 102 -au rr_smoke_a \
  -ap smoke-only-a-7Qm4s9Vx -m 1 -aa \
  -key branch_tag natcall \
  >"$work_directory/logs/phone-a/output.log" 2>&1
phone_a_result=$?
set -e
if test "$phone_a_result" -ne 0; then
  sed -n '1,240p' "$work_directory/logs/phone-a/output.log" >&2 || true
  for log_file in "$work_directory/logs/phone-a"/*_errors.log; do
    if test -f "$log_file"; then
      echo "--- $(basename "$log_file") ---" >&2
      sed -n '1,240p' "$log_file" >&2
    fi
  done
  sed -n '1,240p' "$work_directory/logs/phone-b/output.log" >&2 || true
  docker exec ringring-nat-smoke-topology \
    iptables -L RR_MEDIA -v -n -x >&2 || true
  exit "$phone_a_result"
fi

wait_for_file "$work_directory/logs/phone-b/status" 15 \
  "Household B's measured call"
phone_b_result=$(sed -n '1p' "$work_directory/logs/phone-b/status")
if test "$phone_b_result" -ne 0; then
  sed -n '1,240p' "$work_directory/logs/phone-b/output.log" >&2 || true
  exit "$phone_b_result"
fi

a_sent=$(media_packets 10.231.1.10 0.0.0.0/0)
a_received=$(media_packets 0.0.0.0/0 10.231.1.10)
b_sent=$(media_packets 10.231.2.10 0.0.0.0/0)
b_received=$(media_packets 0.0.0.0/0 10.231.2.10)
for count in "$a_sent" "$a_received" "$b_sent" "$b_received"; do
  if test -z "$count" || test "$count" -lt 100; then
    echo "The NAT media path did not carry enough packets in every direction." >&2
    exit 1
  fi
done

channels=$(docker exec ringring-nat-smoke-asterisk asterisk -rx 'core show channels count')
printf '%s\n' "$channels" | grep -q '^0 active channels'
echo "NAT smoke test passed: 2 authenticated registrations, rewritten contacts, party calling, and bidirectional RTP across separate household networks."
