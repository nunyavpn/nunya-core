# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this
repository.

## What this is

`nunya-core` is the network engine behind [Nunya](https://github.com/nunyavpn/nunya), the desktop
VPN client in the sibling `../nunya` checkout. It wraps **sing-box** (the tunnel) and **Xray-core**
(a sidecar for protocols and configs sing-box cannot run) behind a small framed-RPC surface, and it
is built and released on its own so the client pins a *version* of it rather than vendoring its
source.

This repository produces binaries. It has **no user interface** and is not useful on its own.

Both repos are GPL-3.0 forks of [Throne](https://github.com/throneproj/Throne) (itself descended
from NekoRay). This one begins with Throne's `core/` directory, extracted with its history by
`git subtree`, so the attribution chain stays intact. The `replace` directives in `go.mod` still
point at `throneproj/*` forks of sing-box, Xray, sing and wireguard-go: those carry patches this
core depends on (auto-selector groups, `SetEgress`/`SetOutboundDNS` on the Xray instance, masque,
amnezia fixes) and are **dependency forks, not branding** — do not "clean them up".

`README.md` is the long-form rationale for the build/release model. Read it before changing
anything about artefacts, tags or the release workflow, and keep it current.

### Product goals, in priority order

Simple, reliable, safe, fast, power-efficient — for **macOS (Apple silicon), Linux and Windows**.
Every design decision below is downstream of those, in that order. When a change trades reliability
or safety for a feature, the feature loses.

Longer term the intent is to replace the pre-built upstream engines with an own implementation in
Rust or C, for performance, predictability and battery. Nothing in this repo should make that
harder: `proto/nunya.proto` is the seam that a future engine has to satisfy, so keep the contract
describable in terms of *what the tunnel does*, not in terms of sing-box internals.

## Setup and commands (Linux is the current dev machine)

Development moved from macOS to Linux. Everything except the Apple artefact builds here.

```bash
# Manjaro/Arch. Debian: apt install golang protobuf-compiler
sudo pacman -S go protobuf
# Pinned to match .github/actions/go-toolchain — these generate the wire contract, so a floating
# @latest lets two builds of the same commit emit different code.
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
export PATH="$PATH:$(go env GOPATH)/bin"     # protoc cannot find the plugins otherwise

./scripts/build.sh                            # -> build/dev/linux-amd64/nunya-core
```

Verified on this machine: Go 1.27, protoc from `pacman`, a ~59 MB binary reporting
`sing-box v1.14.1-…` and `Xray-core 26.9.9`. Run bare (no `NUNYA_CORE_SOCKET`) it prints both
versions and exits — a cheap way to confirm a build is alive without a client.

| Task | Command |
| --- | --- |
| Dev build (parent check off) | `./scripts/build.sh` |
| Release build (parent check on) | `RELEASE=1 ./scripts/build.sh` |
| Regenerate `gen/*.pb.go` only | `./scripts/gen-proto.sh` |
| Test | `go test -tags "$(./scripts/tags.sh --dev)" ./...` — three packages have tests (`internal/rpc`, `internal/xray`, `internal/xraydns`) and none need privilege or network |
| Vet, as CI runs it | `go vet -tags "$(./scripts/tags.sh --dev)" ./...` |
| Apple xcframework | `./scripts/build-apple.sh` — **macOS with full Xcode only** |

The build tags are not optional decoration. `with_clash_api` in particular is not about exposing a
control port: its presence is what makes sing-box construct the traffic manager that `QueryStats`
and `QueryConnections` read (`needClashAPI` in `internal/boxbox/box.go`). The client's generated
config leaves `external_controller` unset. Build without the tags and the core compiles, runs, and
reports zero traffic forever.

`scripts/tags.sh` is the single source of truth for that set — `build.sh` builds with it and CI vets
and tests with it, so a vet run cannot type-check a different program from the one that ships. Add a
tag there, never inline. (`build-apple.sh` keeps its own shorter list: the packet tunnel extension is
handed a TUN descriptor and never configures an interface, so `with_dhcp` buys it nothing.)

`gen/*.pb.go` is generated and gitignored; every build script regenerates it. Never commit it, and
never hand-edit it.

### Wiring a dev core into the client

```bash
./scripts/build.sh
export NUNYA_CORE_PATH="$PWD/build/dev/linux-amd64/nunya-core"   # the script prints this line
# or, from ../nunya:  ./scripts/fetch-core.sh --source ../nunya-core
```

The client runs a real tunnel under `../nunya/scripts/dev-linux.sh`, which keeps **both** the Rust
side and the core inside one container — peer identity is a pid over a unix socket, so splitting
GUI-here/core-there breaks verification rather than merely being inconvenient.

### If module downloads fail

The public Go proxy serves module zips through a `storage.googleapis.com` redirect. Where that host
is blocked, `GOPROXY=direct ./scripts/build.sh` fetches from the source repositories instead. The
first build on a fresh machine downloads a large dependency graph (sing-box + Xray + gVisor) and
takes a while; that is normal, not a hang.

### Dev and release builds are not interchangeable

A release core refuses to start unless its parent process is a binary named `Nunya` in the same
directory (`internal/parentcheck`). That check is the whole reason a root-privileged tunnel binary
can sit on disk safely: without it, anything local could drive it. Development builds add the
`noparentcheck` tag because there the parent is `cargo`.

Point a release core at a dev client and you get a core that starts and then **silently never
connects**. Keep them in separate directories, which the build script does for you. A `--source`
core is never published, and the client's `build-app.sh` refuses to bundle one.

## Architecture

### Two artefacts, two privilege models

| Artefact | Built by | Privilege | Consumer |
| --- | --- | --- | --- |
| `nunya-core` executable | `scripts/build.sh` | creates its own TUN, so needs root / `CAP_NET_ADMIN`+ | the client's subprocess transport (Linux, Windows, and macOS until the extension is signed) |
| `NunyaCore.xcframework` | `scripts/build-apple.sh` | none — the system opens the TUN and passes an fd | the macOS `NEPacketTunnelProvider` |

The xcframework is the one that matters on macOS: it lets the tunnel run inside a packet tunnel
extension, so Nunya appears in System Settings beside every other VPN and never asks for root
through a mechanism of its own. It binds `mobile/` via gomobile (the **sagernet fork** — upstream
gomobile cannot build this core), which is also the surface an Android library would use.

### The RPC is not gRPC

`proto/nunya.proto` is the contract, and it ships as a release asset. The Go handlers generate from
it here; the client generates its Rust bindings from the very same file, so a client pinned to a tag
can never be built against a proto it did not pin.

Despite `service NunyaCoreService`, **nothing on the wire is gRPC**. The service block exists to
generate message types and to keep the Go handler table honest — the last line of
`internal/rpc/dispatch.go` is `var _ gen.NunyaCoreServiceServer = globalServer`, which is what makes
a missing handler a compile error. The framing is two little-endian frames over a unix socket (a
named pipe on Windows):

```text
request   [u32 id][u16 method_len][method][u32 payload_len][payload]
response  [u32 id][u8  status    ][u32 payload_len][payload or error text]
```

Two things invert the usual expectations:

- **The GUI listens and the core dials in.** The core is a *child* of the app; `main.go` reads the
  socket from `NUNYA_CORE_SOCKET`, retries the connect ten times, and exits when the parent dies.
- **Both ends verify each other.** `internal/parentcheck` checks the parent is `Nunya` beside us;
  `internal/ipc` checks the socket's *owning process* is that same parent (`SO_PEERCRED` on Linux,
  `LOCAL_PEERPID` on Darwin). The client performs the symmetric check. A local process that won the
  race to the socket must not be able to impersonate the core and report a tunnel that is not there.

Method names are string keys into the Go handler map, so a typo is a runtime `unknown method`, never
a compile error on the client side. Adding an RPC means: proto → `scripts/gen-proto.sh` → handler →
**an entry in the `handlers` map** → the client's `rpc::method`. Forgetting the map entry is the
usual mistake.

The proto is **proto2**: every scalar is optional with a default, which is why handlers are full of
`To(...)` and `in.GetX()` rather than plain field access, and why the Rust side sees `Option<T>`.

Every request gets its own goroutine, each with its own `recover()` — `main()`'s recover only covers
the main goroutine, so without it one malformed request takes the whole core down and the tunnel
with it. `CheckConfig` recovers separately because sing-box's option parsing panics on some
malformed input.

### Two engines, one tunnel

sing-box is always the tunnel: it owns the TUN, the router, DNS and the connection tracker. Xray,
when present, is a **sidecar reached over loopback SOCKS** — never a second tunnel. The client's
generated sing-box config contains a socks outbound pointed at an Xray inbound; `LoadConfigReq`
carries the Xray side alongside.

Three shapes, and they do not mix:

- `xray_config` — one sidecar instance the client composed, started eagerly.
- `xray_config` + `xray_lazy_start` — the same, behind a **gate** (below).
- `xray_full_configs` — *opaque* whole configs (what a subscription hands over verbatim), each its
  own gated instance backing its own socks outbound. **Never merged into `xray_config`**; merging
  opaque configs is how you get tag collisions and a config the user cannot reason about.

`xrayPreparer` in `internal/rpc/xray.go` runs between `core.New` and `Start` on **every** instance,
eager or gated, and applies two settings that keep the sidecar off the active TUN: `SetEgress`
(bind to the real default interface, plus the auto-redirect fwmark on Linux) and `SetOutboundDNS`
(resolve through the box's `dns-direct` transport). An instance that skips this resolves through the
tunnel it is supposed to be providing, and the result is a routing loop rather than an error.

### The Xray gate (`internal/xray/gate.go`)

Auto-selector profiles can hold dozens of Xray members that are probed once a bench cycle and
otherwise idle. Starting them all eagerly costs memory and CPU for nothing.

The gate **owns the loopback ports for the whole life of the config** and builds the instance on the
first connection, tearing it down again after `idle` seconds without traffic. Owning the port
unconditionally is the point: a dial landing while the instance is down must queue, not hit a closed
port and surface as a dead server. `remapInbounds` moves the real inbounds behind the originals so
the gate can sit in front.

There is deliberately **no UDP gate**: Xray serves SOCKS5 UDP ASSOCIATE from a per-session ephemeral
hub, and the association's TCP control connection is what keeps the instance from being swept.

`startXrayFullGates` starts the *first* config alone before fanning the rest out across
`NumCPU` workers — the first instance loads the geo tables the others share, and parallelising it
races several loaders against each other.

### Lifecycle and state (`internal/rpc/`)

- `lifecycleMu` serialises `Start` against `Stop`; the dispatcher hands every request its own
  goroutine, so without it two clicks race.
- `stateMu` guards only the instance *pointers* and is **never held across a Create/Start**, so a
  stats poll cannot block behind a profile start.
- Teardown order is load-bearing: `Stop` unpublishes the box *before* closing it, so a poll arriving
  mid-teardown sees "no instance" rather than a dying one. `Start`'s failure path closes Xray gates,
  the extra process and the box in reverse order and clears `autoRedirectMark` — the nftables rules
  went down with the TUN, so a later probe box must not carry the exemption mark.
- `boxContextHolder` is created **per start, never as a package global**: a probe box must not
  answer for the running instance.

### Probing, testing, measuring

`internal/probe` and the `Test`/`SpeedTest`/`IPTest` RPCs build a **separate short-lived box** from
a config the client supplies (`prepareTestEnv` in `internal/rpc/testenv.go`), or measure the running
one when `test_current` is set. `ErrTestAborted`'s exact wording is matched by the GUI, so it is
part of the contract.

The core's geo lookups (`IPTest`, `SpeedTest` with `only_country`) hit endpoints compiled in —
`api.ip2location.io`, `speedtest.net` — both Cloudflare-fronted and therefore unreachable from a
Cloudflare Workers proxy, which is the shape most free subscriptions take. **This is why the client
locates servers itself** in two steps rather than asking the core. Do not "fix" the client by
routing it back through these RPCs without first giving the messages a URL field.

### Platform seams

Everything OS-specific is behind a build-tagged file pair, and the stub is always the `!` case, so a
new platform compiles before it works:

| Concern | Files |
| --- | --- |
| Parent identity | `internal/parentcheck/parentcheck_{linux,darwin,windows}.go`, `_noop` (tag `noparentcheck`) |
| Socket peer pid | `internal/ipc/ipc_peer_{linux,darwin}.go`, `ipc_windows.go` (named pipe) |
| Privilege probe | `internal/rpc/privilege_linux.go` (the **full** capability set — `NET_ADMIN` alone opens the TUN and then fails part-way), `privilege_other.go` |
| System DNS | `internal/sysdns/sysdns_darwin.go` (+ stub), `internal/rpc/systemdns_windows.go`, `internal/boxdns/dns_manager_windows.go` |
| Windows interface config | `internal/winipcfg/` — vendored, syscall-generated; do not hand-edit `zwinipcfg_windows.go` |

On Linux the system-DNS path is a **stub that returns an error**, because the TUN's `hijack-dns`
rule does the work instead; `Start` only calls `sysdns.SetSystemDNS` on darwin. If you find yourself
wanting resolv.conf edits on Linux, check first whether the route rule already covers it.

`internal/boxdns` runs an interface monitor at `init()` time, independent of the box lifecycle —
which is exactly why `internal/distro/all` must **not** be imported from `mobile/` (see below).

### The memory watchdog (`main.go`)

A soft `SetMemoryLimit` of 2 GiB with a watchdog that panics at 1.5 GiB **live** heap. Two details
worth not undoing:

- It samples `/gc/heap/live:bytes`, not `HeapAlloc`, which counts unswept garbage and sawtooths up
  to the GC target — a bare threshold on `HeapAlloc` fires on a perfectly healthy heap.
- It forces a GC and re-reads before panicking, then backs off 30 s, because `FreeOSMemory` is
  stop-the-world and a busy core can sit near the threshold for minutes.

A panic exits with code 2; the exit code is all the GUI has to tell a crash from a clean stop. Heap
profiles go to `os.CreateTemp`, never a clock-derived name — the core runs privileged, and a
guessable path lets a planted symlink turn a profile dump into a root-owned write anywhere.

### `mobile/` is a parallel implementation, not a wrapper

`mobile/` is the gomobile-bound surface. It owns its own sing-box instance and Xray instances,
mirroring `internal/rpc` and `internal/boxmain` **without** their IPC, signal and desktop-only
wiring. `internal/rpc`, `internal/boxmain` and `internal/distro/all` may **not** be imported there —
`distro/all` starts the desktop netlink monitors at `init()`. The duplication is deliberate: the
desktop core creates its TUN, the mobile one is handed a file descriptor, and the lifecycles differ
enough that sharing the code means branching it everywhere.

When you change behaviour in `internal/rpc`, check whether `mobile/` needs the same change. Nothing
enforces it.

## Conventions

- **Comments explain why, not what.** Nearly every non-obvious line here carries the decision and
  the alternative that was rejected (`// Not HeapAlloc: …`, `// One per box, never shared: …`). This
  is the codebase's defining characteristic across both repos — a change that arrives without it
  reads as foreign. Match the density.
- **Fail loudly at the boundary, never silently degrade.** A config the core cannot run is an error
  string the GUI can show, not a config quietly rewritten into something startable.
- **Error text the GUI matches on is API.** `ErrTestAborted`, `"Instance is not running"` and
  friends cannot be reworded without changing the client.
- **Idempotent queries must stay idempotent.** `QueryStats` clears core-side counters;
  `QueryAutoSelectors` deliberately does not, so more than one consumer can poll it. Keep new
  `Query*` RPCs in the second camp.
- No `gofmt` exemptions — unlike the client, this tree *is* format-clean. `go vet` with the full tag
  set is the CI gate.
- Tests live beside the code (`gate_test.go`, `egress_test.go`, `resolver_test.go`) and run without
  privilege or network; anything needing a real TUN belongs in the client's container harness.

## Current state

Working: the framed RPC and its handler table, bidirectional peer verification, sing-box start/stop
with a TUN, the Xray sidecar in all three shapes (eager, gated, opaque-full), the gate's port
takeover and idle sweep, traffic stats and the connection table (live + recently-closed ring),
URL/IP/speed/country probes, auto-selector status and pinning, WARP registration (WireGuard and
masque) and WG keypair generation, rule-set updates, the diagnostics capture, the Linux capability
probe, the Windows DNS manager, and the gomobile surface under `mobile/`.

### The CI pipeline

`.github/workflows/release.yml` and `ci.yml` share one staged flow; `ci.yml` is the same thing with
the publishing half cut off, and runs on **pull requests only** (a main push is already covered by
`release.yml`, and running both would build the same commit twice).

```text
warmup ──> lint ──> test ──> build ────────────┬──> release ──> prune
                              └> xcframework ──┘
```

Staged rather than fanned out deliberately: lint and test cost one ubuntu runner between them, so a
bad commit spends one runner instead of the four a matrix would have started. Only the xcframework
runs beside `build`, because it is the slowest job and nothing in the matrix needs it.

- **warmup** decides the release identity *once* (`tag`, `prerelease`, `apple` outputs) and primes
  the module cache the later ubuntu stages restore. Three jobs consume that identity; a duplicated
  `if:` is how you get a release marked stable that skipped the xcframework.
- **lint** is `gofmt -l` (excluding generated `gen/`) plus `go vet`. The tree is gofmt-clean and the
  gate keeps it that way.
- `.github/actions/go-toolchain` is a composite action every stage uses, with the protoc plugin
  versions **pinned**. They generate the wire contract, so `@latest` would mean two builds of one
  commit can emit different code.

| Trigger | Tag | Marked | xcframework |
| --- | --- | --- | --- |
| merge or push to `main` | `main-<date>-<sha>` | prerelease | no |
| push a `v0.x.y` tag | that tag | prerelease (beta) | no |
| push a `v1.x.y` tag | that tag | release | yes |

**`v0` is the beta line and ships binaries only.** The gate is the Apple Developer account, not code
maturity: an xcframework is only useful inside a *signed* packet tunnel extension, and there is no
membership to sign one with yet. The Apple artefact arrives with `v1`. A `v1.x.y-rc1` gets the full
asset set but stays marked prerelease.

**Four binaries: `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`.** Nunya does not
support Intel Macs, so there is no `darwin/amd64`, and `build-apple.sh` pins `TARGET=macos/arm64` —
a bare `macos` makes gomobile build amd64 too and fold both into a universal framework. The
ecosystem agrees: GitHub retired the `macos-13` Intel runner in December 2025, and `macos-15-intel`
is the last Intel image, going away Fall 2027. Do not add an Intel leg back; with CGO on there is no
cross-compilation to fall back on either.

Everything published is built `RELEASE=1`, betas included. A downloadable core with its parent check
off is a root-capable binary anything local could drive; development cores come from
`fetch-core.sh --source` and are never uploaded.

Three properties protect the client's `core.lock`, and each is load-bearing:

- **Published tags are immutable.** The lock pins a tag plus the digest of that release's
  `SHA256SUMS`; a re-cut tag is reported to the user as possible tampering. Main tags carry the
  commit sha and are never reused, and `release` **fails** on an existing release rather than
  replacing its assets. Republishing means deleting the release and tag on purpose
  (`gh release delete <tag> --yes --cleanup-tag`).
- **Pruning deletes builds, never rewrites them.** The newest ten `main-*` prereleases survive; the
  rest go whole, so a tag that still exists still means what it meant when cut. The filter keys on
  the `main-` prefix, not the prerelease flag, so `v0` betas and `v1.0.0-rc1` are safe.
- **The asset list is asserted before publishing.** The four binaries and the proto are the floor;
  only the xcframework is conditional. A silently short list would reach the client as a 404
  mid-fetch, after `fetch-core.sh` had already verified `SHA256SUMS`.

`release` spells out its `if:` (`needs.xcframework.result == 'success' || == 'skipped'`) rather than
using `always()`, because the xcframework is skipped for main and v0 and a skipped dependency would
otherwise skip the publish — while `always()` would publish straight through a genuine failure.

`../nunya/core.lock` still holds the placeholder `v0.0.0-unreleased`. Once a `v0` beta is cut, a
client pins it with `./scripts/fetch-core.sh --update v0.1.0` instead of building from `--source`.

**Known gap:** `fetch-core.sh` appends `NunyaCore.xcframework.zip` to its asset list on any macOS
host, unconditionally. A `v0` release has no such asset, so fetching a beta *on a Mac* fails on a
bare curl 404. Fetching on Linux and Windows is unaffected. Fix that in the client before anyone
pins a beta from a Mac.

Deliberately narrower than Throne: the build tags cover only the protocols the client ships, which
drops cronet (~500 MB of download for NaiveProxy alone) along with everything behind the OpenVPN,
OpenConnect and Tailscale tags. The VPN-challenge messages in the proto (`VPNChallenge`,
`SubmitVPNChallenge`) are inherited from that path and are **currently dead weight** — keep them
reserved rather than deleting fields, but do not build on them.

Not started: the own-engine rewrite, an Android artefact (`mobile/` is bound only for Apple by
`build-apple.sh`), and iOS targets (`TARGET=macos` is hardcoded as the default — add
`ios,iossimulator` when there is something to build for).

## The other repository

`../nunya` is the Tauri v2 client: TypeScript frontend (no framework — `src/dom.ts` is the whole
abstraction), Rust shell, Swift packet tunnel extension on macOS. It has its own, longer `CLAUDE.md`
— read it before changing anything that crosses the seam.

Rules that hold across both:

- **This repo never knows about the client's UI; the client never builds this repo.** The only
  coupling is `proto/nunya.proto` plus a pinned tag.
- Changing the proto is a **two-repo, two-commit** operation: land it here, cut a tag, then
  `./scripts/fetch-core.sh --update <tag>` in the client and commit the lockfile.
- The client generates sing-box JSON today (`src-tauri/src/config.rs`). The long-term plan is to
  move generation *into this core* behind a `GenerateConfig` RPC, so config shape stops being a
  thing two languages have to agree on. Keep that direction in mind when tempted to add a field to
  `LoadConfigReq` that only exists to describe config the client already built.
