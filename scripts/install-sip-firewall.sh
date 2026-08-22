#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "Run this script as root." >&2
  exit 1
fi

install_packages=1
if [ "${1:-}" = --skip-packages ]; then
  install_packages=0
  shift
fi
if [ "$#" -gt 1 ]; then
  echo "Usage: $0 [--skip-packages] [CHECKOUT]" >&2
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
if [ ! -f "$jail_source" ] || [ -L "$jail_source" ]; then
  echo "Could not find $jail_source" >&2
  exit 1
fi
if [ "$install_packages" -eq 1 ]; then
  apt-get update
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends fail2ban iptables
fi

for command_name in fail2ban-client iptables systemctl install; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Required command is missing: $command_name" >&2
    exit 1
  fi
done
if ! iptables -n -L DOCKER-USER >/dev/null 2>&1; then
  echo "Docker's DOCKER-USER firewall chain is required." >&2
  exit 1
fi
if [ -L /etc/fail2ban/jail.d/ringring-sip.local ]; then
  echo "Refusing to replace a symlinked RingRing Fail2Ban jail." >&2
  exit 1
fi
install -m 0644 "$jail_source" /etc/fail2ban/jail.d/ringring-sip.local
fail2ban-client -t
systemctl enable --now fail2ban
fail2ban-client reload
fail2ban-client status ringring-sip
