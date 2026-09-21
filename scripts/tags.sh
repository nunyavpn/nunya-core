#!/usr/bin/env bash
#
# Prints the build tag set for the desktop core. Single source of truth: build.sh builds with it and
# CI vets and tests with it, so the two cannot drift.
#
# The tags are not decoration. with_clash_api in particular is not about exposing a control port —
# its mere presence is what makes sing-box construct the traffic manager QueryStats and
# QueryConnections read (see needClashAPI in internal/boxbox/box.go), and the client's generated
# config leaves external_controller unset. Vetting without it type-checks a different program from
# the one that ships.
#
#   TAGS="$(./scripts/tags.sh)"         what a release core is built with
#   TAGS="$(./scripts/tags.sh --dev)"   the same, plus noparentcheck
#
# scripts/build-apple.sh keeps its own, shorter list: the packet tunnel extension is handed a TUN
# descriptor and never configures an interface, so with_dhcp buys it nothing.

set -euo pipefail

TAGS="with_clash_api,with_gvisor,with_quic,with_wireguard,with_utls,with_dhcp,badlinkname,tfogo_checklinkname0"

if [[ "${1:-}" == "--dev" ]]; then
  TAGS="$TAGS,noparentcheck"
fi

printf '%s\n' "$TAGS"
