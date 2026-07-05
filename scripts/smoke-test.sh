#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
FRAGATA_BIN="${FRAGATA_BIN:-./dist/fragata}"

"$FRAGATA_BIN" -healthcheck "$BASE_URL/healthz"
printf 'PASS: %s/healthz responde correctamente\n' "$BASE_URL"
