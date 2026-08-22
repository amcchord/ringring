#!/bin/sh
set -eu
umask 077

runtime_dir=/run/ringring-tls
source_dir=/run/ringring-host-tls
runtime_certificate="$runtime_dir/certificate.pem"
runtime_key="$runtime_dir/private-key.pem"
source_certificate="$source_dir/certificate.pem"
source_key="$source_dir/private-key.pem"

case "${1:-}" in
  --prepare) mode=prepare ;;
  --reload) mode=reload ;;
  *) echo "usage: $0 --prepare|--reload" >&2; exit 2 ;;
esac

case "${SIP_PUBLIC_HOST:-}" in
  *[!A-Za-z0-9.:-]*|'')
    echo "SIP_PUBLIC_HOST contains unsupported characters" >&2
    exit 1
    ;;
esac

install -d -o asterisk -g ringring -m 0750 "$runtime_dir"
temporary=$(mktemp -d "$runtime_dir/.refresh.XXXXXX")
cleanup() {
  exit_code=$?
  trap - EXIT HUP INT TERM
  find "$temporary" -depth -delete >/dev/null 2>&1
  exit "$exit_code"
}
trap cleanup EXIT HUP INT TERM

certificate_matches_host() {
  certificate=$1
  case "$SIP_PUBLIC_HOST" in
    *:*) openssl x509 -in "$certificate" -noout -checkip "$SIP_PUBLIC_HOST" >/dev/null 2>&1 ;;
    *[!0-9.]*) openssl x509 -in "$certificate" -noout -checkhost "$SIP_PUBLIC_HOST" >/dev/null 2>&1 ;;
    *) openssl x509 -in "$certificate" -noout -checkip "$SIP_PUBLIC_HOST" >/dev/null 2>&1 ;;
  esac
}

valid_pair() {
  certificate=$1
  key=$2
  test -f "$certificate" && test ! -L "$certificate" && test -f "$key" && test ! -L "$key" || return 1
  certificate_matches_host "$certificate" || return 1
  openssl x509 -in "$certificate" -noout -checkend 3600 >/dev/null 2>&1 || return 1
  certificate_public=$(openssl x509 -in "$certificate" -pubkey -noout 2>/dev/null | sha256sum | awk '{print $1}')
  key_public=$(openssl pkey -in "$key" -pubout 2>/dev/null | sha256sum | awk '{print $1}')
  test -n "$certificate_public" && test "$certificate_public" = "$key_public"
}

install_pair() {
  certificate=$1
  key=$2
  install -o asterisk -g ringring -m 0644 "$certificate" "$temporary/certificate.pem"
  install -o asterisk -g ringring -m 0600 "$key" "$temporary/private-key.pem"
  if test -f "$runtime_certificate" && test -f "$runtime_key" && \
    cmp -s "$temporary/certificate.pem" "$runtime_certificate" && \
    cmp -s "$temporary/private-key.pem" "$runtime_key"; then
    changed=0
    return
  fi
  mv "$temporary/certificate.pem" "$runtime_certificate"
  mv "$temporary/private-key.pem" "$runtime_key"
  changed=1
}

generate_fallback() {
  case "$SIP_PUBLIC_HOST" in
    *:*) subject_alt_name="IP:$SIP_PUBLIC_HOST" ;;
    *[!0-9.]*) subject_alt_name="DNS:$SIP_PUBLIC_HOST" ;;
    *) subject_alt_name="IP:$SIP_PUBLIC_HOST" ;;
  esac
  openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 2 \
    -subj "/CN=$SIP_PUBLIC_HOST" -addext "subjectAltName=$subject_alt_name" \
    -keyout "$temporary/fallback.key" -out "$temporary/fallback.crt" >/dev/null 2>&1
  install_pair "$temporary/fallback.crt" "$temporary/fallback.key"
  echo "A trusted SIP TLS certificate is not available yet; using a temporary self-signed certificate." >&2
}

changed=0
if valid_pair "$source_certificate" "$source_key"; then
  install_pair "$source_certificate" "$source_key"
elif test "$mode" = prepare; then
  if ! valid_pair "$runtime_certificate" "$runtime_key"; then
    generate_fallback
  fi
else
  echo "The synchronized SIP TLS certificate or key is missing, invalid, mismatched, or near expiry." >&2
  exit 1
fi

if test "$mode" = reload && test "$changed" -eq 1; then
  channel_count=$(asterisk -rx 'core show channels count' | sed -n 's/^\([0-9][0-9]*\) active channels.*$/\1/p')
  test -n "$channel_count" || {
    echo "Could not determine whether calls are active; deferring the SIP TLS reload." >&2
    exit 75
  }
  if test "$channel_count" -ne 0; then
    echo "A call is active; deferring the SIP TLS certificate reload." >&2
    exit 75
  fi
  asterisk -rx 'module reload res_pjsip.so' >/dev/null
  asterisk -rx 'pjsip show transport transport-tls' | grep -q 'transport-tls'
  echo "SIP TLS certificate refreshed."
fi
