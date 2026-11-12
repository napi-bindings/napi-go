# How it works

## Layers

```
 your addon     //napi: annotations ──napi-go gen──▶ zz_napi.go, index.js, index.d.ts
      │
 Layer 2        napi.Export, FuncN/AsyncN/MethodN, NewClass, codecs (Go ⇄ JS conversions), JSFunc
 Layer 1        napi.Env, Value, Object, Function, errors, Ref, Async, ThreadsafeFunction
 Layer 0        sys: the Node-API C functions 1:1, C trampolines, symbol loader
      │
 Node.js        Node-API (napi_*), V8, libuv
```

Each layer only uses the one below. Layer 2 and Layer 1 are both in package `napi` and can be
mixed in one addon; Layer 0 (`sys`) is for raw ABI access.

## Loading

The addon is a shared library built by `go build -buildmode=c-shared`, renamed `.node`. When
`require()` loads it:

1. The dynamic loader runs the Go runtime initialization (the runtime starts its threads, the
   `init` functions run and register the exports).
2. Node.js calls the entry point `napi_register_module_v1` (`sys/module.c`), which resolves every
   Node-API function from the host process with `dlsym` or `GetProcAddress`. The library has no
   undefined symbols and no import library: it builds without Node.js installed, and a function
   missing from the running Node.js is reported, not a crash at load time.
3. For every env that loads the addon (the main thread, each worker thread), the registered exports
   are created.

The declared Node-API version is 8 by default, 9 or 10 with the build tags `napi_go_v9` and
`napi_go_v10` ([sys](../sys/README.md#declared-node-api-version)).

## A call

```
JS add(1, 2)
 └▶ V8 ─▶ Node-API ─▶ C trampoline (sys/typed.c): reads 1 and 2 as doubles
                       └▶ one cgo callback into Go: the Go function, by its ID in a table
                       ◀─ result 3 as a double
                      C creates the JS number
```

- **One crossing per call.** Numbers, booleans and strings up to about 500 bytes are converted in
  C, before and after the single crossing into Go, as is the `this` of methods. Other types are
  converted in Go by codecs: one per Go type, built once with reflection and cached, working on
  `unsafe` pointers at run time, with no Go allocation for primitives, `[]byte`, napi values and
  class pointers.
- **The crossing is the floor.** Node.js calls on its own thread, which is not a Go thread; entering
  Go costs about 100 ns on Linux (about 1 µs on Windows, where the Go runtime lacks an
  optimization for threads created outside Go). A call from Rust or C++ costs a few nanoseconds.
  The [benchmarks](../bench/README.md) compare the three.
- **Errors** returned by Go are thrown as JS exceptions on the way out; panics are recovered and
  thrown as `GO_PANIC`.

## Threads

V8 runs JavaScript on one thread per env, and every Node-API call must be made on it. Goroutines
run on the threads of the Go scheduler, so they never call Node-API: they hand work to the JS
thread.

- **One dispatcher per env.** Each env has one threadsafe function (unref'd: it does not keep
  Node.js alive) and a Go queue of closures. A goroutine appends to the queue, and wakes the JS
  thread only if the queue was empty: a thousand goroutines finishing together cost one wake-up.
- **Async functions** (`AsyncN`, `Env.Async`) convert the arguments on the JS thread, run the Go
  function in a goroutine, and settle the Promise through the dispatcher. Node.js stays alive while
  any is pending.
- **ThreadsafeFunction[T]** and **JSFunc[A, R]** let goroutines call JS functions (with the result
  converted back, for `JSFunc`).
- **Env context.** `Env.Context()` is canceled when the env is destroyed (worker terminated,
  process exiting); pending Promises are dropped and threadsafe functions return `ErrClosing`.

## Memory

Two garbage collectors manage the objects of an addon: V8 for JS values, Go for Go values. cgo
forbids handing Go pointers to C for C to keep, so:

- **Go values given to JS are found by ID.** Callbacks, class instances, externals and finalizers
  live in Go tables; Node-API receives an integer. When V8 collects the JS object, its finalizer
  frees the ID and the Go value becomes collectable. Reads of the tables are lock-free.
- **JS values held by Go are references** (`Value.NewRef`, napi_ref), released explicitly or from
  a goroutine with `Ref.DeleteLater`. A plain `napi.Value` is valid only during the call.
- **Buffers.** A `[]byte` parameter is a view of V8 memory, valid during the call. A returned
  `[]byte` is copied into a new Buffer. `NewExternalBuffer` hands Go memory to JS without a copy,
  pinned with `runtime.Pinner` until V8 releases it.
- **Class instances.** A JS instance holds the ID of its `*T`. In an env, the same `*T` always maps
  to the same JS object, and `Finalize` runs when the last instance is collected.

## Signals

The Go runtime and Node.js both install signal handlers in one process. Go requires every handler
to run on the alternate signal stack (`SA_ONSTACK`): a signal can arrive on a Go thread, whose
stack is small. Handlers that Node.js and libuv install after the addon has loaded
(`process.on('SIGINT')`, child processes) lack the flag; on Linux, `sys/onstack.c` adds it to
them by intercepting `sigaction` in the process. On macOS, the Go runtime reinstalls the handlers
that are already there to add `SA_ONSTACK`, and the macOS kernel does not report `SA_RESETHAND`,
so the flag is lost on the handler that Node.js uses to exit on SIGINT and SIGTERM; the handler
then calls itself forever. `sys/sigfix.c` restores the flag. The e2e
tests check WebAssembly traps, child processes, SIGINT and SIGTERM with the addon loaded and Go
busy.

## Source map

| Path | |
|---|---|
| `sys/` | Layer 0: generated bindings, loader, trampolines, signal fixes |
| `napi/` | Layers 1 and 2 |
| `internal/codegen/` | the generator of `napi-go gen` |
| `sys/internal/gen/`, `napi/internal/genwrappers/` | generators of `sys` and of the Layer 2 wrappers |
| `cmd/napi-go/` | the CLI |
| `tests/` | e2e addons and tests, the Node.js and node-addon-api test suites ported to napi-go |
| `bench/` | benchmarks |
