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
sudo pacman -S go protobuf      # Arch/Manjaro. Debian: apt install golang protobuf-compiler
brew install go protobuf        # macOS

# Pinned, matching .github/actions/go-toolchain: these generate the wire contract, so a floating
# @latest would let two builds of the same commit emit different code.
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
export PATH="$PATH:$(go env GOPATH)/bin"

./scripts/build.sh              # -> build/dev/<goos>-<goarch>/nunya-core
RELEASE=1 ./scripts/build.sh    # -> build/release/<goos>-<goarch>/nunya-core
./scripts/build-apple.sh        # -> build/apple/NunyaCore.xcframework  (macOS + full Xcode)
```

`gen/*.pb.go` is generated and gitignored; every build script regenerates it. The build tag set
lives in [`scripts/tags.sh`](scripts/tags.sh) so the build, `go vet` and `go test` cannot drift
apart — they are not optional, and a core built without them runs fine while reporting zero traffic
forever.

Checks, as CI runs them:

```bash
gofmt -l . | grep -v '^gen/'                      # must print nothing
go vet  -tags "$(./scripts/tags.sh --dev)" ./...
go test -tags "$(./scripts/tags.sh --dev)" ./...
```

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

[`.github/workflows/release.yml`](.github/workflows/release.yml) is one staged pipeline:

```text
warmup ──> lint ──> test ──> build ────────────┬──> release ──> prune
                              └> xcframework ──┘
```

Staged rather than fanned out on purpose: lint and test cost one runner between them, so a bad
commit spends one runner instead of the four a build matrix would have started.

| Trigger | Tag | Marked | Assets |
| --- | --- | --- | --- |
| merge or push to `main` | `main-<date>-<sha>` | prerelease | binaries |
| push a `v0.x.y` tag | that tag | prerelease (beta) | binaries |
| push a `v1.x.y` tag | that tag | release | binaries + xcframework |

**`v0` is the beta line and ships binaries only.** The xcframework is only useful inside a *signed*
packet tunnel extension, and there is no Apple Developer membership to sign one with yet — so the
Apple artefact arrives with `v1`, alongside the account. The split is about signing, not about how
finished the code is.

```text
nunya-core-darwin-arm64            Apple Silicon; there is no Intel Mac build
nunya-core-linux-amd64
nunya-core-linux-arm64
nunya-core-windows-amd64.exe
NunyaCore.xcframework.zip          the Apple library — v1+ only
nunya.proto                        the contract
SHA256SUMS                         checksums over all of the above
```

The client's `scripts/fetch-core.sh` reads a pinned tag, downloads these, and verifies them against
`SHA256SUMS` before unpacking anything. Pin any build, `main-*` included:

```bash
./scripts/fetch-core.sh --update v0.1.0     # in the client repo
```

**Every published tag is immutable.** `core.lock` pins a tag *plus* the digest of that release's
`SHA256SUMS`, so a tag re-cut with different assets is reported as possible tampering — that check is
the reason the lockfile is worth having. Main builds therefore derive their tag from the commit sha
and never reuse one, and the pipeline refuses to overwrite an existing release rather than quietly
replacing its assets. To republish a commit, delete the release and its tag deliberately:

```bash
gh release delete v0.1.0 --yes --cleanup-tag
```

Old `main-*` prereleases are pruned to the newest ten after each successful publish. Pruning deletes
a build whole, so a tag that still exists still means exactly what it meant when it was cut; `v*`
releases, betas included, are never touched.

Everything published is a **release build**, betas and `main-*` prereleases included — the parent
check is what stops anything else on the machine from driving a root-privileged tunnel. A
`noparentcheck` core is never published; development cores come from `fetch-core.sh --source`.

## Scope

This core is deliberately narrower than Throne's. The build tags cover only the protocols the client
ships, which drops the cronet dependency (~500 MB of download for NaiveProxy alone) along with
everything behind the OpenVPN, OpenConnect and Tailscale tags.
