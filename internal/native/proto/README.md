# Proto Files

This directory contains the Protocol Buffer definitions for the native service.

## Generating Code

To generate the Go code from the proto files, run:

```bash
./scripts/generate_proto.sh
```

Or manually:

```bash
protoc \
    --go_out=. \
    --go_opt=paths=source_relative \
    --go-grpc_out=. \
    --go-grpc_opt=paths=source_relative \
    internal/native/proto/native.proto
```

## Prerequisites

- `protoc` - Protocol Buffer compiler
- `protoc-gen-go` - Go plugin for protoc (install with: `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`)
- `protoc-gen-go-grpc` - gRPC Go plugin for protoc (install with: `go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest`)

## Note

The current `native.pb.go` and `native_grpc.pb.go` files are placeholder/stub files. They should be regenerated from `native.proto` using the commands above.

