# nunya-core

The network core behind [Nunya](https://github.com/nunyavpn/nunya) — a [sing-box](https://github.com/SagerNet/sing-box) and
[Xray](https://github.com/XTLS/Xray-core) engine wrapped in a small RPC surface, built and released
on its own so the client can pin a version of it rather than vendor its source.

This repository produces binaries. It has no user interface and is not useful on its own.

## Lineage and licence

Forked from [Throne](https://github.com/throneproj/Throne) (itself descended from NekoRay), whose
`core/` directory this repository's history begins with. It is **GPL-3.0**, like its ancestors — see
[LICENSE](LICENSE). The `replace` directives in [go.mod](go.mod) still point at the `throneproj/*`
forks of sing-box, Xray and sing, because those carry patches this core depends on.

## Artefacts

Two, for two different privilege models:

| Artefact | Built by | Privilege | Used by |
| --- | --- | --- | --- |
| `nunya-core` executable | `scripts/build.sh` | creates its own TUN, so needs root | the client's child-process transport |
| `NunyaCore.xcframework` | `scripts/build-apple.sh` | none — the system opens the TUN and passes an fd | the macOS packet tunnel extension |

The xcframework is the one that matters on macOS: it lets the tunnel run inside a
`NEPacketTunnelProvider`, so Nunya appears in System Settings beside every other VPN and never asks
for root through a mechanism of its own.

## The contract

[`proto/nunya.proto`](proto/nunya.proto) is the interface, and it ships as a release asset. The Go
handlers generate from it here; the client generates its Rust bindings from the very same file, so a
client pinned to a tag can never be built against a proto it did not pin.

Nothing on the wire is gRPC. The service block exists to generate message types and keep the Go
handler table honest — [`internal/rpc/dispatch.go`](internal/rpc/dispatch.go) reads and writes two
little-endian frames over a unix socket (or a named pipe on Windows):

```text
request   [u32 id][u16 method_len][method][u32 payload_len][payload]
response  [u32 id][u8  status    ][u32 payload_len][payload]
```

## Building

```bash
brew install go protobuf                                             # macOS
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
export PATH="$PATH:$(go env GOPATH)/bin"

./scripts/build.sh              # -> build/dev/<goos>-<goarch>/nunya-core
RELEASE=1 ./scripts/build.sh    # -> build/release/<goos>-<goarch>/nunya-core
./scripts/build-apple.sh        # -> build/apple/NunyaCore.xcframework  (needs full Xcode)
```

`gen/*.pb.go` is generated and gitignored; every build script regenerates it.

### Dev and release builds are not interchangeable

A release core refuses to start unless its parent process is a binary named `Nunya` in the same
directory ([`internal/parentcheck`](internal/parentcheck)). That check is what stops anything else
on the machine from driving a root-privileged tunnel. Development builds add the `noparentcheck`
tag, because there the parent is `cargo`.

Point a release core at a dev client and you get a core that starts and then silently never
connects. Keep them in separate directories, which the build script does for you.

### If module downloads fail

The public Go proxy serves module zips through a `storage.googleapis.com` redirect. Where that host
is blocked, `GOPROXY=direct ./scripts/build.sh` fetches from the source repositories instead.

## Releasing

Push a tag. [`.github/workflows/release.yml`](.github/workflows/release.yml) builds every platform
natively (CGO is on, so there is no cross-compilation), then publishes:

```text
nunya-core-<goos>-<goarch>[.exe]   one per platform
NunyaCore.xcframework.zip          the Apple library
nunya.proto                        the contract
SHA256SUMS                         checksums over all of the above
```

The client's `scripts/fetch-core.sh` reads a pinned tag, downloads these, and verifies them against
`SHA256SUMS` before unpacking anything.

## Scope

This core is deliberately narrower than Throne's. The build tags cover only the protocols the client
ships, which drops the cronet dependency (~500 MB of download for NaiveProxy alone) along with
everything behind the OpenVPN, OpenConnect and Tailscale tags.
