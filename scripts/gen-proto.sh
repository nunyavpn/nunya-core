#!/usr/bin/env bash
#
# Generates the Go bindings from proto/nunya.proto into gen/.
#
# proto/nunya.proto is the contract this repository publishes: the client generates its own
# bindings from the very same file, shipped as a release asset. Nothing on the wire is gRPC — see
# internal/rpc/dispatch.go for the framing — but the service block keeps the Go handler table
# honest and gives both sides their message types.

set -euo pipefail
cd "$(dirname "$0")/.."

for tool in protoc protoc-gen-go protoc-gen-go-grpc; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "error: $tool not found on PATH" >&2
    echo "  brew install protobuf" >&2
    echo "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" >&2
    echo "  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest" >&2
    echo "  (and make sure \$(go env GOPATH)/bin is on PATH)" >&2
    exit 1
  }
done

mkdir -p gen
protoc -I proto \
  --go_out=gen --go_opt=paths=source_relative \
  --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
  proto/nunya.proto

echo "==> generated gen/nunya.pb.go, gen/nunya_grpc.pb.go"
