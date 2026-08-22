#!/bin/sh
set -eu

repository=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
app_image=ringring-radio-smoke-app
asterisk_image=ringring-radio-smoke-asterisk

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker is required for the radio compatibility check." >&2
  exit 1
fi
for image in "$app_image" "$asterisk_image"; do
  if docker image inspect "$image" >/dev/null 2>&1; then
    echo "Refusing to replace an existing test image: $image" >&2
    exit 1
  fi
done

cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  set +e
  docker image rm "$app_image" "$asterisk_image" >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

docker build --tag "$app_image" --file "$repository/Dockerfile" "$repository" >/dev/null
docker build --tag "$asterisk_image" --file "$repository/deploy/asterisk/Dockerfile" "$repository/deploy/asterisk" >/dev/null

docker run --rm --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --entrypoint /usr/local/bin/ringring \
  "$app_image" radio-catalog | \
docker run --rm --interactive --network bridge --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user 10002:10001 \
  --tmpfs /tmp:rw,noexec,nosuid,size=1m --entrypoint /bin/sh "$asterisk_image" -c '
    set -eu
    tab=$(printf "\t")
    count=0
    while IFS="$tab" read -r station_id stream_url; do
      case "$station_id" in
        ""|*[!a-z0-9-]*) echo "Unsafe station identifier." >&2; exit 1 ;;
      esac
      case "$stream_url" in
        http://ice5.somafm.com/*-128-mp3) ;;
        *) echo "Unsafe station URL." >&2; exit 1 ;;
      esac
      output="/tmp/$station_id.log"
      set +e
      timeout --signal=TERM --kill-after=2 5 mpg123 -t "$stream_url" >"$output" 2>&1
      status=$?
      set -e
      if test "$status" -ne 124 || ! grep -Eq "MPEG [0-9.]+ L III" "$output"; then
        echo "Station did not deliver decodable MP3 audio: $station_id" >&2
        exit 1
      fi
      count=$((count + 1))
      echo "Station playable: $station_id"
    done
    test "$count" -eq 3
  '

echo "Radio compatibility check passed: the complete code-controlled catalog delivered decodable MP3 audio in the production Asterisk image."
