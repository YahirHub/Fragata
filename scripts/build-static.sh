#!/bin/sh
set -eu

VERSION="${VERSION:-dev}"
GOOS_VALUE="${GOOS:-linux}"
GOARCH_VALUE="${GOARCH:-amd64}"
OUTPUT="${OUTPUT:-dist/fragata-${GOOS_VALUE}-${GOARCH_VALUE}}"

mkdir -p "$(dirname "$OUTPUT")"
CGO_ENABLED=0 GOOS="$GOOS_VALUE" GOARCH="$GOARCH_VALUE" \
  go build -trimpath -tags netgo,osusergo \
  -ldflags="-s -w -buildid= -X main.version=${VERSION}" \
  -o "$OUTPUT" ./cmd/fragata

printf 'Binario creado: %s\n' "$OUTPUT"
if command -v file >/dev/null 2>&1; then
  file "$OUTPUT"
fi
if [ "$GOOS_VALUE" = "linux" ] && command -v ldd >/dev/null 2>&1; then
  ldd "$OUTPUT" 2>&1 || true
fi
