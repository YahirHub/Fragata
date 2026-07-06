#!/bin/sh
set -eu

run_uid="${FRAGATA_UID:-65532}"
run_gid="${FRAGATA_GID:-65532}"
port="${FRAGATA_LISTEN_PORT:-8080}"
url="http://127.0.0.1:${port}/healthz"

if [ "$(id -u)" -eq 0 ] && command -v su-exec >/dev/null 2>&1; then
  exec su-exec "${run_uid}:${run_gid}" /usr/local/bin/fragata -healthcheck "$url"
fi
exec /usr/local/bin/fragata -healthcheck "$url"
