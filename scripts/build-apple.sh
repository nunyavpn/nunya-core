#!/usr/bin/env bash
#
# Builds ./mobile as NunyaCore.xcframework for Apple platforms, to be linked into the packet tunnel
# extension.
#
# A different artefact from scripts/build.sh: that one produces a standalone executable that creates
# its own TUN and therefore needs privilege. This one produces a library linked into a
# NetworkExtension provider, where the *system* creates the TUN and hands the extension a file
# descriptor — so nothing needs root.
#
# Requires full Xcode, not just the Command Line Tools: gomobile drives xcodebuild to assemble the
# xcframework. Check with `xcode-select -p`; it must point inside Xcode.app.

set -euo pipefail
cd "$(dirname "$0")/.."
DEST="${DEST:-build/apple}"

# Matches scripts/build.sh: the protocols this client ships, and nothing else.
TAGS="with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api,badlinkname,tfogo_checklinkname0"

# macOS only for now. Add ios,iossimulator here when there is an iOS target to build for.
TARGET="${TARGET:-macos}"

# xcode-select often still points at the Command Line Tools even with Xcode installed, and switching
# it needs sudo. DEVELOPER_DIR overrides it for this process only, no privilege required.
if ! xcodebuild -version >/dev/null 2>&1; then
  for candidate in /Applications/Xcode.app /Applications/Xcode-beta.app; do
    if [[ -d "$candidate/Contents/Developer" ]]; then
      export DEVELOPER_DIR="$candidate/Contents/Developer"
      echo "==> using $DEVELOPER_DIR (xcode-select points elsewhere)"
      break
    fi
  done
fi

if ! xcodebuild -version >/dev/null 2>&1; then
  cat >&2 <<'MSG'
error: full Xcode is required to build the xcframework.

  gomobile invokes xcodebuild to assemble an .xcframework, which the Command Line Tools do not
  provide.

    1. Install Xcode from the App Store
    2. sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
       (or set DEVELOPER_DIR, which needs no sudo)
    3. sudo xcodebuild -license accept
MSG
  exit 1
fi

if ! command -v gomobile >/dev/null 2>&1; then
  echo "==> installing the sagernet gomobile fork (the upstream one cannot build this core)"
  go install github.com/sagernet/gomobile/cmd/gomobile@v0.1.13
  go install github.com/sagernet/gomobile/cmd/gobind@v0.1.13
fi

# The default proxy serves module zips via a storage.googleapis.com redirect, and `direct` cannot
# resolve golang.org/x/* vanity paths. Either can be blocked depending on the network; override
# GOPROXY if neither works here.
export GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}"

./scripts/gen-proto.sh

mkdir -p "$DEST"
VERSION_SINGBOX="$(go list -m -f '{{.Version}}' github.com/sagernet/sing-box)"

echo "==> building NunyaCore.xcframework (sing-box $VERSION_SINGBOX) for $TARGET"
gomobile bind -v \
  -o "$DEST/NunyaCore.xcframework" \
  -target "$TARGET" \
  -libname=nunya \
  -trimpath \
  -ldflags "-s -w -checklinkname=0 -X github.com/sagernet/sing-box/constant.Version=${VERSION_SINGBOX}" \
  -tags "$TAGS" \
  ./mobile

echo "==> built $DEST/NunyaCore.xcframework"
du -sh "$DEST/NunyaCore.xcframework"
