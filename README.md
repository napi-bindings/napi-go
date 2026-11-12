# napi-go

Native Node.js addons (`.node`) written in Go, on top of
[Node-API](https://nodejs.org/api/n-api.html): annotated Go functions, structs and classes become
a typed npm package, built for every platform from one machine. The design follows
[napi-rs](https://napi.rs).

```go
// Greet returns a greeting for name.
//
//napi:export
func Greet(name string) string { return "hello " + name }

// Download fetches url in a goroutine; JS gets a Promise.
//
//napi:export
func Download(ctx context.Context, url string) ([]byte, error) { … }
```

```ts
import { greet, download } from 'my-addon'   // typed by the generated index.d.ts
greet('Ann')
const body = await download('https://example.com')   // a Buffer
```

```sh
go run github.com/napi-bindings/napi-go/cmd/napi-go@latest new my-addon
cd my-addon && npm run build:debug && npm test
```

- **Conversions from Go types**: numbers, strings, `[]byte`, slices, maps, structs, pointers,
  `*big.Int`, `time.Time`; wrong JS arguments throw a TypeError naming the argument.
- **Goroutines**: functions with a `context.Context` return Promises; goroutines call JS functions
  with `JSFunc`; Promises settled in bulk cost one wake-up of the JS thread.
- **Classes** backed by Go values, with methods, getters, setters and finalizers.
- **TypeScript declarations** generated from the Go code, Go comments as JSDoc.
- **One machine builds all platforms**: Linux, macOS and Windows on x64 and arm64, with zig;
  one npm package per platform, as napi-rs.
- **Worker threads**, Node-API 8 to 10, and the Node.js and node-addon-api test suites ported to
  napi-go.

> **Status:** 0.1, the first release ([CHANGELOG](CHANGELOG.md)). Before 1.0 the API may change
> between minor versions.

## Documentation

| | |
|---|---|
| [Getting started](docs/getting-started.md) | create, build, test and publish an addon |
| [Writing addons](docs/guide.md) | functions, types, errors, async, classes, calling JS |
| [How it works](docs/architecture.md) | layers, threads, memory, signals, costs |
| [Coming from napi-rs](docs/napi-rs.md) | the napi-rs features in napi-go |
| [Platforms and limitations](docs/platforms.md) | support, known limits, troubleshooting |
| [Benchmarks](bench/README.md) | against napi-rs and node-addon-api |
| [API reference](https://pkg.go.dev/github.com/napi-bindings/napi-go/napi) | pkg.go.dev |

## Platforms

Linux (glibc) x64 and arm64, macOS arm64 and x64, Windows x64 and arm64, with Go 1.24 or later and
Node.js 22 or later. Not supported: musl (Alpine), which no Go release can load as a shared
library. On macOS x64, one Go addon per process. See [platforms](docs/platforms.md).

## Repository

| Path | Contents |
|---|---|
| [`napi/`](napi/README.md) | Layer 2 (framework: `Export`, wrappers, conversions, classes) and Layer 1 (core: values, errors, functions, buffers, threads) |
| [`sys/`](sys/README.md) | Layer 0: the Node-API 1–10 functions, generated 1:1 |
| [`cmd/napi-go/`](cmd/napi-go/README.md) | the CLI: `new`, `gen`, `build`, `artifacts`, `prepublish` |
| [`internal/codegen/`](internal/codegen/README.md) | the code generator: annotations and TypeScript mapping |
| [`bench/`](bench/README.md) | benchmarks |
| `tests/e2e/` | test addons and `node:test` suites |
| [`tests/nodesuite/`](tests/nodesuite/README.md), [`tests/addonapisuite/`](tests/addonapisuite/README.md) | the Node.js and node-addon-api test suites, ported |
| `.github/workflows/` | CI: `sys.yml` (layers, generator, CLI tests, Alpine sentinel), `cli.yml` (projects created by the CLI, cross-compiled and loaded on every platform), `bench.yml`, `release.yml` |

Developing napi-go itself:

```bash
go test ./sys/... ./napi/... ./internal/... ./cmd/...
node tests/e2e/build.mjs               # Windows without gcc: set CC=zig cc
node --test tests/e2e/*.test.mjs
node tests/nodesuite/run.mjs          # Node.js test suite ported to sys
node tests/addonapisuite/run.mjs      # node-addon-api test suite ported to napi
```

Releases: [docs/releasing.md](docs/releasing.md). License: [Apache 2.0](LICENSE).
