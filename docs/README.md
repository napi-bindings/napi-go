# napi-go documentation

| Document | For |
|---|---|
| [Getting started](getting-started.md) | creating, building, testing and publishing an addon |
| [Writing addons](guide.md) | functions, conversions, errors, async functions, classes, calling JS from goroutines, Layer 1 |
| [How it works](architecture.md) | the layers, loading, the call path, threads, memory, signals, costs |
| [Coming from napi-rs](napi-rs.md) | napi-rs features and their napi-go counterparts |
| [Platforms and limitations](platforms.md) | supported systems and versions, known limits, troubleshooting |
| [Benchmarks](../bench/README.md) | napi-go against napi-rs and node-addon-api |
| [Releasing napi-go](releasing.md) | the release checklist (maintainers) |

Reference:

| Package | |
|---|---|
| [`napi`](../napi/README.md) | Layer 2 (framework) and Layer 1 (core): type mapping, wrappers, threading rules |
| [`cmd/napi-go`](../cmd/napi-go/README.md) | the CLI: configuration, targets, cross-compilation, publishing |
| [`internal/codegen`](../internal/codegen/README.md) | the `//napi:` annotations and the TypeScript mapping |
| [`sys`](../sys/README.md) | Layer 0: the raw Node-API bindings |

The API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/napi-bindings/napi-go/napi).
