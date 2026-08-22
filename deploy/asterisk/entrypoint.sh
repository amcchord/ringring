#!/bin/sh
set -eu

case "${ASTERISK_AMI_SECRET:-}" in
  *[!A-Za-z0-9_-]*|'')
    echo "ASTERISK_AMI_SECRET must contain only URL-safe characters" >&2
    exit 1
    ;;
esac

case "${SIP_PUBLIC_HOST:-}" in
  *[!A-Za-z0-9.:-]*|'')
    echo "SIP_PUBLIC_HOST contains unsupported characters" >&2
    exit 1
    ;;
esac

sed "s|__ASTERISK_AMI_SECRET__|${ASTERISK_AMI_SECRET}|g" /etc/asterisk/manager.conf.in > /etc/asterisk/manager.conf
sed "s|__SIP_PUBLIC_HOST__|${SIP_PUBLIC_HOST}|g" /etc/asterisk/pjsip.conf.in > /etc/asterisk/pjsip.conf
chmod 0640 /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf
chown asterisk:ringring /etc/asterisk/manager.conf /etc/asterisk/pjsip.conf
install -d -o asterisk -g ringring /var/run/asterisk /var/log/asterisk /var/spool/asterisk

exec /usr/sbin/asterisk -f -U asterisk -G ringring
