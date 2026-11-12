# Changelog

All notable changes to napi-go. The format follows [Keep a Changelog](https://keepachangelog.com/),
and versions follow [Semantic Versioning](https://semver.org/): before 1.0.0, a minor version may
change the API (the changes are listed here).

## [0.1.0] - unreleased

The first release: native Node.js addons written in Go, with the tooling of napi-rs.

### Framework (Layer 2, package `napi`)

- `napi.Export` of functions, classes and constants, with generic wrappers (`FuncN`, `FuncEN`,
  `ProcN`, `ProcEN`, `AsyncN`, `AsyncProcN`, `MethodN`, `CtorN`, …) for up to 8 parameters.
- Automatic conversions from the Go types: numbers (integers range-checked), strings, `[]byte`
  (views of JS memory), slices, arrays, maps, structs (`napi`/`json` tags), pointers, `any`,
  `*big.Int`, `time.Time`, `Either[A, B]`, napi values, and types implementing `FromJS`/`ToJS`.
  Conversion errors name the argument and the path inside it.
- Asynchronous functions: a `context.Context` first parameter runs the function in a goroutine and
  returns a Promise.
- Classes backed by Go values (`NewClass`), with methods, accessors, statics, `Finalize`, and the
  same JS object for the same `*T`.
- `JSFunc[A, R]`: goroutines call JS functions and receive the converted result.
- A C trampoline converts numbers, booleans and short strings and unwraps `this` with one cgo
  crossing per call; no Go allocation per call for primitives, Buffers, methods and accessors.

### Core (Layer 1, package `napi`)

- Typed JS values (`Value`, `Object`, `Function`, `String`, `BigInt`, `Buffer`, `TypedArray`, …),
  Go errors thrown as JS exceptions with kind and code, panics as `GO_PANIC`.
- Functions, classes (`DefineClass`), properties, references, finalizers, `Wrap`, type tags,
  externals, handle scopes, cleanup hooks, instance data per env, zero-copy and external buffers.
- Concurrency: `Env.Async` (Promises settled from goroutines), `ThreadsafeFunction[T]`,
  `Env.Context` (canceled when the env is destroyed), `Ref.DeleteLater`; one dispatcher per env
  wakes the JS thread once for a batch of results.
- Worker threads: the addon loads in every env, with separate state.

### Node-API bindings (Layer 0, package `sys`)

- The 154 functions of Node-API 1–10, generated 1:1, resolved at run time from the Node.js process
  (no import library, builds without Node.js).
- Declared Node-API version 8 by default, 9 and 10 with build tags.
- Coexistence of the Go runtime with Node.js signals: `SA_ONSTACK` added to the handlers Node.js
  installs later (Linux), `SA_RESETHAND` restored for SIGINT and SIGTERM (macOS), and a guard
  against a second Go runtime on macOS x64.

### Code generator (`napi-go gen`)

- `//napi:export`, `//napi:class`, `//napi:constructor`, `//napi:getter`, `//napi:setter`,
  `//napi:skip`, `//napi:object`, `//napi:type`, `//napi:returns`.
- Generates the registrations (`zz_napi.go`), `index.d.ts` with the Go comments as JSDoc, and
  `index.js`; reports unsupported types with file and line; `-check` for CI.

### CLI (`cmd/napi-go`)

- `napi-go new`: a project with an annotated example, tests and a GitHub Actions workflow that
  builds, tests and publishes every target.
- `napi-go build`: targets `linux-x64-gnu`, `linux-arm64-gnu`, `darwin-x64`, `darwin-arm64`,
  `win32-x64-gnu`, `win32-arm64-gnu`; cross-compilation from any host with zig (Linux binaries for
  glibc 2.17); format check of each binary.
- `napi-go artifacts` and `napi-go prepublish`: one npm package per platform and
  `optionalDependencies`, as napi-rs; `index.js` loads the binary of the running platform.

### Known limitations

- musl (Alpine Linux) is not supported by any Go release (golang/go#71953).
- macOS x64: one Go addon per process (golang/go#23617).
- A call into Go costs about 100 ns on Linux and about 1 µs on Windows; see `bench/README.md`.
- Intermittent failure, under investigation, of one end-to-end signal test on Linux arm64 CI
  runners (a hang or a SIGBUS while the test floods every thread with signals).

[0.1.0]: https://github.com/napi-bindings/napi-go/releases/tag/v0.1.0
