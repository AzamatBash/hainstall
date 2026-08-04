#!/bin/sh
# Named volume on /var/run/haproxy is empty + root-owned; official image runs as
# non-root and cannot create admin.sock. Prepare the dir, then run as root (MVP).
set -e
mkdir -p /var/run/haproxy
chmod 777 /var/run/haproxy
exec haproxy "$@"
