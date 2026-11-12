# Platforms and limitations

## Supported

| Target | Built from | Tested in CI |
|---|---|---|
| `linux-x64-gnu` | any host (zig: glibc 2.17 or later) | Node 22, 24, 26, current; Go 1.24 and stable |
| `linux-arm64-gnu` | any host (zig: glibc 2.17 or later) | Node 22, 24, 26; Go 1.24 and stable |
| `darwin-arm64` | macOS, or any host with zig | Node 22, 24, 26; Go 1.24 and stable |
| `darwin-x64` | macOS, or any host with zig | Node 22, 24, 26; Go 1.24 and stable |
| `win32-x64-gnu` | Windows (MinGW gcc or zig), or any host with zig | Node 22, 24, 26; Go 1.24 and stable |
| `win32-arm64-gnu` | any host with zig | Node 24: binaries built with zig, loaded and tested |

- **Go** 1.24 or later (the `tool` directive of `go.mod` used by the template needs 1.24).
- **Node.js** 22 or later is tested. Addons declare Node-API 8 by default (Node.js 12.22 and later
  provide it); the build tags `napi_go_v9` and `napi_go_v10` declare 9 and 10 for their functions
  and behaviors.
- **Worker threads**: supported; each worker loads the addon in its own env.
- **Electron**: not tested. Where external buffers are not allowed (the V8 sandbox), napi-go copies
  instead.

## Known limitations

- **musl (Alpine Linux) is not supported.** No Go release builds a shared library that loads with
  musl: `dlopen` rejects the initial-exec TLS of the Go runtime (golang/go#71953; the Go change
  CL 696635 fixes it and is tracked in CI). The loader in `index.js` explains it on musl.
- **macOS x64: one Go addon per process.** On darwin/amd64 every Go runtime uses the same TLS slot
  for the current goroutine (golang/go#23617). A second napi-go addon (or any other Go c-shared
  library) is rejected with `ERR_NAPI_GO_FOREIGN_RUNTIME` instead of corrupting memory. macOS arm64,
  Linux and Windows load any number of Go addons.
- **Windows: a call costs about 1 µs**, against about 100 ns on Linux: the Go runtime lacks an
  optimization for threads created outside Go on Windows (`x_cgo_bindm`).
- **Binary size**: the Go runtime is in every addon, about 2.5 MB for a small one (stripped).
- **Not wrapped yet**: async hooks (`napi_async_init`, `napi_make_callback`), the libuv thread pool
  (`napi_async_work`) and SharedArrayBuffer are only in `sys`; asynchronous class methods are not
  supported by Layer 2 (call `Env.Async` in a method). See the
  [current limitations](../napi/README.md#current-limitations).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `cannot load the native addon … for linux-x64-musl` | musl is not supported: use a glibc-based image (Debian, Ubuntu, Red Hat, `node:*-slim`) |
| `cannot load the native addon … package not installed` | the platform package was not installed: `npm install` with optional dependencies enabled (not `--omit=optional`), on the same platform as the one that runs the code |
| `GLIBC_2.xx not found` | the binary was built against a newer glibc: build with zig (`napi-go build -release`, the default when zig is installed) |
| `ERR_NAPI_GO_FOREIGN_RUNTIME` | macOS x64: a second Go addon in the process (see above) |
| `ERR_NAPI_GO_MISSING_FUNCTIONS` | the Node.js version lacks Node-API functions of the declared version: drop the `napi_go_v9`/`napi_go_v10` tag or update Node.js |
| `cross-compiling for … needs zig` | install [zig](https://ziglang.org/download/) or build on that platform |
| `cgo: C compiler "gcc" not found` (Windows) | install MinGW gcc, or use `napi-go build -zig always` with zig |
| a TypeError `ERR_INVALID_ARG_TYPE` from an addon function | a JS argument of the wrong type: the message names the argument and the path inside it |
| a `GO_PANIC` error | a panic in the Go function; its message is the panic value |

To see what `index.js` tried, read the error: it lists every candidate (local file, platform
package) with the reason it failed. `NAPI_GO_NATIVE_LIBRARY_PATH=/path/to/addon.node` loads a
specific binary.
