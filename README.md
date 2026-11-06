# napi-go

Go bindings for [Node-API](https://nodejs.org/api/n-api.html) (N-API): build native Node.js addons
(`.node`) in Go, modeled after [napi-rs](https://napi.rs).

> **Status:** under development. Phase 0 (feasibility spike), Phase 1 (Layer 0) and Phase 2
> (Layer 1) are complete; Phase 3 (concurrency) is next.

| Path | Contents |
|---|---|
| [`sys/`](sys/README.md) | Layer 0: generated 1:1 bindings for Node-API 1–10 |
| [`napi/`](napi/README.md) | Layer 1: idiomatic Go API (values, errors, functions, classes, buffers) |
| `tests/e2e/` | test addons and `node:test` suites |
| [`tests/nodesuite/`](tests/nodesuite/README.md) | Node.js `js-native-api` and `node-api` test suites ported to `sys` |
| [`tests/addonapisuite/`](tests/addonapisuite/README.md) | node-addon-api test suite ported to `napi` |
| `.github/workflows/` | CI: `sys.yml` (Layer 0 and 1, including the Alpine/musl sentinel) |

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
Windows x64. Alpine/musl is not supported by current Go releases.
