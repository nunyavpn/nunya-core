#!/usr/bin/env bash
#
# Builds the standalone core executable.
#
# This is the artefact the desktop client spawns as a child process. It creates its own TUN and so
# needs privilege; the Apple packet tunnel extension uses scripts/build-apple.sh instead, which
# produces a library the system hands an already-open file descriptor.
#
#   ./scripts/build.sh                    development build, parent check disabled
#   RELEASE=1 ./scripts/build.sh          release build, parent check enforced
#   GOOS=linux GOARCH=amd64 ./scripts/build.sh   cross-compile (CGO_ENABLED=0 unless you set a toolchain)

set -euo pipefail
cd "$(dirname "$0")/.."

GOOS_="$(go env GOOS)"
GOARCH_="$(go env GOARCH)"
OUT_NAME="nunya-core"
[[ "$GOOS_" == "windows" ]] && OUT_NAME="nunya-core.exe"

if [[ "${RELEASE:-0}" == "1" ]]; then
  DEST="${DEST:-build/release/${GOOS_}-${GOARCH_}}"
else
  DEST="${DEST:-build/dev/${GOOS_}-${GOARCH_}}"
fi

# Kept in scripts/tags.sh so CI vets and tests the same program this builds; see the rationale there.
#
# In release builds the core refuses to run unless its parent is a binary named `Nunya` in the same
# directory (internal/parentcheck). During development the core lives under build/ and the parent is
# cargo or the dev binary, so the check has to come off. The two are NOT interchangeable: point a
# dev run at a release core and it starts, then silently never connects.
if [[ "${RELEASE:-0}" != "1" ]]; then
  TAGS="$(./scripts/tags.sh --dev)"
  echo "==> development build: parent process check DISABLED"
else
  TAGS="$(./scripts/tags.sh)"
  echo "==> release build: parent process check enforced"
fi

# The public Go module proxy serves module zips via a storage.googleapis.com redirect. Where that
# host is unreachable, GOPROXY=direct fetches from the source repositories instead.
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"
export CGO_ENABLED="${CGO_ENABLED:-1}"
[[ "$(go env GOOS)" == "darwin" ]] && export CGO_LDFLAGS="${CGO_LDFLAGS:--weak_framework UniformTypeIdentifiers}"

./scripts/gen-proto.sh

VERSION_SINGBOX="$(go list -m -f '{{.Version}}' github.com/sagernet/sing-box)"
VERSION_CORE="${VERSION_CORE:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"

mkdir -p "$DEST"
echo "==> building nunya-core $VERSION_CORE (sing-box $VERSION_SINGBOX) for $GOOS_/$GOARCH_"
go build \
  -o "$DEST/$OUT_NAME" \
  -trimpath \
  -tags "$TAGS" \
  -ldflags "-w -s -checklinkname=0 -X 'github.com/sagernet/sing-box/constant.Version=${VERSION_SINGBOX}'" \
  .

echo "==> built $DEST/$OUT_NAME"
ls -lh "$DEST/$OUT_NAME"

if [[ "${RELEASE:-0}" != "1" ]]; then
  # DEST may already be absolute — the client's fetch-core.sh passes one — so resolve rather than
  # prefixing $PWD.
  ABS_DEST="$(cd "$DEST" && pwd)"
  echo "==> point a client dev build at it with:"
  echo "    export NUNYA_CORE_PATH=\"$ABS_DEST/$OUT_NAME\""
fi
