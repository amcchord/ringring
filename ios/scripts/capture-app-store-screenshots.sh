#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT="$ROOT_DIR/RingRing.xcodeproj"
SCHEME="RingRing"
DEVICE_NAME="${RINGRING_SCREENSHOT_DEVICE:-iPhone 17 Pro Max}"
OUTPUT_DIR="$ROOT_DIR/fastlane/screenshots/en-US"
DERIVED_DATA="${RINGRING_SCREENSHOT_DERIVED_DATA:-$ROOT_DIR/.derived-data-screenshots}"
BUNDLE_ID="com.mcchord.ringring"

device_id="$(xcrun simctl list devices available --json | \
  jq -r --arg name "$DEVICE_NAME" '.devices[][] | select(.name == $name) | .udid' | head -n 1)"

if [[ -z "$device_id" ]]; then
  printf 'No available simulator named %s.\n' "$DEVICE_NAME" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"
for screenshot in \
  01-join.png 02-call-menu.png 03-private-call.png \
  01-join.jpg 02-call-menu.jpg 03-private-call.jpg; do
  rm -f "$OUTPUT_DIR/$screenshot"
done

xcrun simctl boot "$device_id" 2>/dev/null || true
open -g -a Simulator
xcrun simctl bootstatus "$device_id" -b
xcrun simctl status_bar "$device_id" override \
  --time 9:41 \
  --operatorName RingRing \
  --wifiBars 3 \
  --cellularBars 4 \
  --batteryState charged \
  --batteryLevel 100

xcodebuild \
  -project "$PROJECT" \
  -scheme "$SCHEME" \
  -configuration Debug \
  -destination "platform=iOS Simulator,id=$device_id" \
  -derivedDataPath "$DERIVED_DATA" \
  build

app_path="$DERIVED_DATA/Build/Products/Debug-iphonesimulator/RingRing.app"
xcrun simctl install "$device_id" "$app_path"

capture() {
  local output="$1"
  shift
  xcrun simctl terminate "$device_id" "$BUNDLE_ID" 2>/dev/null || true
  xcrun simctl launch "$device_id" "$BUNDLE_ID" "$@" >/dev/null
  sleep 2
  xcrun simctl io "$device_id" screenshot "$OUTPUT_DIR/$output" >/dev/null
}

xcrun simctl uninstall "$device_id" "$BUNDLE_ID" 2>/dev/null || true
xcrun simctl install "$device_id" "$app_path"
capture 01-join.jpg
capture 02-call-menu.jpg --preview-call-menu
capture 03-private-call.jpg --preview-active-call

for screenshot in "$OUTPUT_DIR"/*.jpg; do
  dimensions="$(sips -g pixelWidth -g pixelHeight "$screenshot" 2>/dev/null | awk '/pixelWidth|pixelHeight/ {print $2}' | paste -sd x -)"
  if [[ "$dimensions" != "1320x2868" ]]; then
    printf 'Unexpected dimensions for %s: %s (expected 1320x2868).\n' "$screenshot" "$dimensions" >&2
    exit 1
  fi
done

printf 'Captured App Store screenshots in %s\n' "$OUTPUT_DIR"
