# napi-go

Go bindings for [Node-API](https://nodejs.org/api/n-api.html) (N-API): build native Node.js addons
(`.node`) in Go, modeled after [napi-rs](https://napi.rs).

> **Status:** under development. Phase 0 (feasibility spike), Phase 1 (Layer 0), Phase 2
> (Layer 1), Phase 3 (concurrency: Promises from goroutines, typed threadsafe functions),
> Phase 4 (Layer 2 framework: `napi.Export`, generic wrappers, automatic conversions, classes),
> Phase 5 (code generator: `//napi:` annotations, TypeScript declarations) and Phase 6 (CLI:
> project template, cross-compilation with zig, npm platform packages) are complete; Phase 7
> (documentation, benchmarks, release 0.1) is next.

```sh
go run github.com/napi-bindings/napi-go/cmd/napi-go@latest new my-addon
cd my-addon && npm run build:debug && npm test
```

| Path | Contents |
|---|---|
| [`sys/`](sys/README.md) | Layer 0: generated 1:1 bindings for Node-API 1–10 |
| [`napi/`](napi/README.md) | Layer 2: framework (`Export`, `Func2`, `Async1`, classes, conversions); Layer 1: idiomatic Go API (values, errors, functions, classes, buffers) |
| [`cmd/napi-go/`](cmd/napi-go/README.md) | CLI: `new`, `gen`, `build` (cross-compilation with zig), `artifacts`, `prepublish` |
| [`internal/codegen/`](internal/codegen/README.md) | code generator of `napi-go gen`: `//napi:export`, `//napi:class` → registrations, `index.d.ts`, `index.js` |
| `tests/e2e/` | test addons and `node:test` suites |
| [`tests/nodesuite/`](tests/nodesuite/README.md) | Node.js `js-native-api` and `node-api` test suites ported to `sys` |
| [`tests/addonapisuite/`](tests/addonapisuite/README.md) | node-addon-api test suite ported to `napi` |
| `.github/workflows/` | CI: `sys.yml` (Layers 0–2, generator, CLI tests, Alpine/musl sentinel), `cli.yml` (projects created by the CLI, cross-compiled with zig and loaded on every platform) |

Requirements: Go ≥ 1.24 with cgo, a C compiler (gcc/clang, or `zig cc` on Windows),
Node.js ≥ 22 for tests.

```bash
go test ./sys/... ./napi/...           # unit tests, without Node
node tests/e2e/build.mjs               # Windows without gcc: set CC=zig cc
node --test tests/e2e/*.test.mjs
node tests/nodesuite/run.mjs          # Node.js test suite ported to sys
node tests/addonapisuite/run.mjs      # node-addon-api test suite ported to napi
```

Platforms: Linux glibc x64/arm64, macOS arm64/x64 (x64: one Go addon per process),
Windows x64 (arm64: cross-compiled, loaded in `cli.yml` only). Alpine/musl is not supported by current Go releases.
