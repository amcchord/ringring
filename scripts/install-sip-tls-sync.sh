#!/bin/sh
set -eu
umask 077

if test "$(id -u)" -ne 0; then
  echo "Run this script as root." >&2
  exit 1
fi
if test "$#" -gt 1; then
  echo "usage: $0 [CHECKOUT]" >&2
  exit 2
fi
checkout=${1:-/opt/ringring}
case "$checkout" in
  /*) ;;
  *) echo "The checkout path must be absolute." >&2; exit 1 ;;
esac
test "$checkout" = /opt/ringring || {
  echo "The production SIP TLS synchronizer requires /opt/ringring." >&2
  exit 1
}
printf '%s\n' "$checkout" | grep -Eq '^/[A-Za-z0-9._/-]+$' || {
  echo "The checkout path contains unsupported characters." >&2
  exit 1
}
for source in \
  "$checkout/deploy/systemd/ringring-sip-tls-sync.service.in" \
  "$checkout/deploy/systemd/ringring-sip-tls-sync.timer" \
  "$checkout/scripts/sync-sip-tls.sh"; do
  test -f "$source" && test ! -L "$source" || {
    echo "Required SIP TLS synchronization file is missing or unsafe." >&2
    exit 1
  }
done
for target in \
  /etc/systemd/system/ringring-sip-tls-sync.service \
  /etc/systemd/system/ringring-sip-tls-sync.timer; do
  test ! -L "$target" || {
    echo "Refusing to replace a symlinked systemd unit: $target" >&2
    exit 1
  }
done

install -d -m 0700 /etc/ringring/tls
temporary=$(mktemp /etc/systemd/system/.ringring-sip-tls-sync.XXXXXX)
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  find "$temporary" -maxdepth 0 -type f -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM
sed "s|__RINGRING_CHECKOUT__|$checkout|g" \
  "$checkout/deploy/systemd/ringring-sip-tls-sync.service.in" >"$temporary"
chmod 0644 "$temporary"
install -m 0644 "$temporary" /etc/systemd/system/ringring-sip-tls-sync.service
install -m 0644 "$checkout/deploy/systemd/ringring-sip-tls-sync.timer" \
  /etc/systemd/system/ringring-sip-tls-sync.timer
systemctl daemon-reload
systemctl enable --now ringring-sip-tls-sync.timer
systemctl is-enabled --quiet ringring-sip-tls-sync.timer
systemctl is-active --quiet ringring-sip-tls-sync.timer
echo "SIP TLS certificate synchronization timer installed."
