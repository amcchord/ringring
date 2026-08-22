#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this script as root." >&2
  exit 1
fi

ringring_checkout=${1:-/opt/ringring}
case "$ringring_checkout" in
  /*) ;;
  *)
    echo "The checkout path must be absolute." >&2
    exit 1
    ;;
esac

jail_source="$ringring_checkout/deploy/fail2ban/ringring-sip.local"
if [ ! -f "$jail_source" ]; then
  echo "Could not find $jail_source" >&2
  exit 1
fi
if ! iptables -n -L DOCKER-USER >/dev/null 2>&1; then
  echo "Docker's DOCKER-USER firewall chain is required." >&2
  exit 1
fi

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends fail2ban iptables
install -m 0644 "$jail_source" /etc/fail2ban/jail.d/ringring-sip.local
fail2ban-client -t
systemctl enable --now fail2ban
fail2ban-client reload
fail2ban-client status ringring-sip
