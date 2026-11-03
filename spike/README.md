# napi-go — Phase 0: feasibility spike

Minimal prototype that validates the architectural choices from the implementation plan (`claude/napi-go-piano-implementazione.md`
in the Project) before writing the real binding. **This is not the final API.**

## Contents

```
spike/
├── napi/                    mini-binding (Layer 0 + Layer 1 + concurrency)
│   ├── include/             Node-API headers (from Node 22)
│   ├── gonapi.h             symbol table (X-macro) + static inline wrappers + "batch" helpers
│   ├── loader.c             runtime symbol resolution: dlsym(RTLD_DEFAULT) / GetProcAddress
│   ├── module.c             NAPI_MODULE_INIT → napi_register_module_v1 + node_api_module_get_api_version_v1
│   ├── trampoline.c         single C→Go trampoline, typed trampoline, dispatcher call_js, cleanup hook
│   ├── napi.go              Env, Value, errors, zero-copy strings, scope
│   ├── callback.go          callback registry (index, not Go pointer), panic recovery
│   ├── module.go            Export, per-env state (worker_threads), cleanup
│   ├── dispatcher.go        one TSFN per env + coalescing queue, Promise from goroutines
│   └── typed.go             typed path: generic Fn1/Fn2, conversions in C
├── addons/basic             main test addon
├── addons/second            second addon (second Go runtime in the same process)
├── addons/cref              pure C addon: benchmark baseline
├── experiments/cbcost       cost of a C→Go callback by thread type
├── scripts/build.mjs        prototype of `napi-go build`
├── test/*.test.mjs          node:test suite (Q1–Q8)
├── test/bench.mjs           per-call benchmark
└── ci/spike.yml             GitHub Actions workflow (to copy into .github/workflows/)
```

## How to run

```bash
cd spike
node scripts/build.mjs                       # Windows without gcc: set CC=zig cc
node --test --test-reporter=spec test/*.test.mjs
node test/bench.mjs
cd experiments/cbcost && go run .
```

Requirements: Go ≥ 1.24 with cgo, Node ≥ 20, a C compiler (gcc/clang, or `zig cc` on Windows).

## Results

Tested environments:
- **Linux** x64 (Ubuntu 24.04, 2-core VM): Go 1.24.7, Node 22.23.4 and 20.20.0, gcc 13
- **Windows** x64 (22 logical threads): Go 1.27.1, Node 24.21.0, `zig cc` 0.16

macOS and Linux arm64 are covered by `ci/spike.yml`. Alpine/musl was run in CI: see Q7.

| # | Question | Outcome | Evidence |
|---|---|---|---|
| Q1 | `c-shared` loaded by `require()` with entry point defined in C | ✅ Linux, Windows | `napi_register_module_v1` and `node_api_module_get_api_version_v1` exported even with zig/lld on Windows. No declared dependency on the node binary |
| Q2 | Dynamic symbol resolution | ✅ Linux, Windows | 42 required symbols + 3 optional (Node-API 9/10) detected at runtime; no import library, no stubs |
| Q3 | Dispatch, arguments, panics, errors | ✅ | 12+ functions without the `static dataCopy` bug; more than 8 arguments; panic → `Error` with `code: GO_PANIC` and process alive; error codes; Go→JS→Go reentrancy; **clean under `GOEXPERIMENT=cgocheck2`** |
| Q4 | Goroutine → Promise, worker_threads | ✅ | Resolution always on the env's JS thread. 20k concurrent Promises with dispatcher coalescing. 8 concurrent workers with env cleanup; workers terminated with pending Promises without crashing |
| Q5 | **Two Go runtimes in the same process** | ✅ Linux, Windows, macOS arm64 · ❌ macOS x64 (Go limitation) | Two distinct addons: synchronous calls, async, concurrent GC and CPU, both loaded across 4 workers. Remains a scenario not officially supported by Go: must be monitored in CI. **macOS x64:** `bad sweepgen in refill` crash, because all Go runtimes share the `g` TLS slot (finding 8) |
| Q6 | Signals and process lifecycle | ✅ (macOS with the `napi/sigfix.c` fix) | Over 1000 WebAssembly traps (V8 SIGSEGV) while Go performs preemption and GC. SIGINT/SIGTERM (Linux). `child_process`. `process.exit` with in-flight goroutines. Forced `gc()`. Process exits on its own (TSFN unref) and stays alive while Promises are pending. **macOS:** SIGINT/SIGTERM without a JS listener hung the process at 100% CPU (`node::SignalExit` → `raise()` → `SignalExit` again): the Go runtime reinstalls the handler with `SA_ONSTACK` and drops `SA_RESETHAND`. Fixed in `napi/sigfix.c` (see finding 6); bug and fix also reproduced on Linux |
| Q7 | Alpine / musl | ❌ Go limitation | `dlopen` rejected by musl: *initial-exec TLS resolves to dynamic definition*. Reproduced in CI (Go 1.26.10) and locally with Go 1.24.7 and 1.27.1 (`experiments/musltls`); `LD_PRELOAD` segfaults (null argc/argv at init). Upstream fix only: golang/go#71953, CL 696635 |
| Q8 | Per-call overhead | ✅ measured | See below; two findings that change the plan |

Runs: Linux 5 full runs (Node 22 ×3, Node 20, cgocheck2), Windows 4 full runs. Zero failures. Size: 1.8–2.1 MB per `.node` with `-ldflags=-s -w`.

### Benchmark (ns per call, best of 5 rounds of 1M)

| Case | JS | Pure C | Generic Go | Typed Go |
|---|---:|---:|---:|---:|
| **Linux / Node 22** | | | | |
| `noop()` | 0.5 | 12.6 | 120 | — |
| `add(1.5, 2.5)` | 2.6 | 32.6 | 355 | **131** |
| `echo(16 B)` | 2.8 | 82 | 350 | **216** |
| `echo(1 KiB)` | 2.8 | 446 | 1184 | — |
| Promise from goroutine | | | 5.6–6.5 µs | |
| **Windows / Node 24** | | | | |
| `noop()` | 0.4 | n/a | 1421–1913 | — |
| `add(1.5, 2.5)` | 4.5 | n/a | 1404–2888 | 1312–2493 |
| Promise from goroutine | | | 7.9–11.7 µs | |

Go allocations per call: 0 for numbers; 1 per string read (the resulting Go string).

Empty C→Go callback (`experiments/cbcost`):

| | Linux (Go 1.24) | Windows (Go 1.27) |
|---|---:|---:|
| Go→C (nop) | 49 | 37 |
| C→Go from Go thread | 61 | 113 |
| C→Go from C-created thread (= Node main thread) | 87 | **1001** |

### Findings that change the plan

1. **The cost model is "1 entry + N exits".** Every Node-API call made from Go inside a callback costs ~70 ns (Linux). With the typed trampoline, C reads primitive arguments, enters Go only once, and converts the result: `add` drops from 355 to 131 ns, i.e., ~100 ns above pure C, which is the theoretical minimum. **Consequence for Layer 2:** signatures with primitives (numbers, bool, strings) should be compiled into descriptors converted by C. Even with generics (`napi.Fn2`) this works without codegen; codegen will generate C glue per signature.
2. **On Windows every C→Go callback costs ~1 µs**, even with Go 1.27. The Go runtime keeps the M bound to external threads only on Unix (Go 1.21, `x_cgo_bindm` with pthread destructor). In `runtime/cgo/gcc_libinit_windows.c` the mechanism is missing ("No pthreads on Windows, these are always zero"), so every entry from Node's main thread repeats `needm`/`dropm`. **Consequences:**
   - document the cost;
   - design coarse-grained APIs (batch, async) for hot paths;
   - evaluate an upstream contribution to Go (binding the M with `FlsAlloc` and a destruction callback).
3. The race detector (TSan) does not initialize inside a `.node` loaded by Node. Concurrent logic (dispatcher, registries) must be tested with `go test -race` using a fake TSFN.
4. `-Wl,-z,nodelete` is unnecessary: Go already sets `NODELETE` on `c-shared` builds (and it is outside the cgo flag allowlist anyway).
5. **Alpine/musl cannot be supported with any current Go release (≤ 1.27).** The Go runtime in `c-shared` builds uses initial-exec TLS (`R_X86_64_TPOFF64`) and expects `argc`/`argv` in init functions: musl rejects `dlopen` and segfaults with `LD_PRELOAD`. **Consequences:**
   - musl is dropped from the supported platforms for the initial release;
   - the Alpine job remains in CI as a sentinel: a `dlopen` probe emits a warning (green job, tests skipped) if the error matches the known issue, runs all tests if loading succeeds, and fails the job for any other outcome;
   - track proposal golang/go#71953 and CL 696635, which introduce the "general dynamic" TLS model and `argv` checks. The CL is tested in CI (`go cl696635` variant of the Alpine job, Go built from the latest patchset): there, the known error fails the job.
6. **On macOS the Go runtime breaks Node's default SIGINT/SIGTERM handling.** At `c-shared` startup, `initsig` → `setsigstack` reads back each pre-existing handler and reinstalls it with `SA_ONSTACK`. The macOS kernel enforces `SA_RESETHAND` when set, but does not report it back when queried (verified in CI with `experiments/sigflags`), so reinstalling drops it. Node installs `node::SignalExit` with `SA_RESETHAND` and then calls `raise()`: without that flag the handler re-invokes itself endlessly (confirmed with `sample` in CI: `_sigtramp → SignalExit → raise → _sigtramp …`). **Fix:** after runtime startup, `napi/sigfix.c` restores `SA_RESETHAND` solely on `node::SignalExit` (identified via `dladdr`) for SIGINT and SIGTERM. **Fix verified in CI on macOS 26 arm64** (Go 1.27.3, Node 22): all SIGINT/SIGTERM variants pass. To be reported upstream to Go: on Darwin `setsigstack` cannot trust read-back flags.
7. `#cgo noescape`/`nocallback` (Go 1.24) work: zero allocations for numeric getters/setters and stack-allocated string read buffers.
8. **On macOS x64 two Go addons in the same process corrupt memory.** On darwin/amd64 the Go runtime stores `g` in a fixed TLS slot, `0x30(GS)`, reserved by Apple for Go (golang/go#23617; `cmd/link` `computeTLSOffset`, `runtime/sys_darwin_amd64.s`). The slot is process-wide, so all Go runtimes share it. On other systems each runtime has its own slot: darwin/arm64 allocates one via `pthread_key_create` (`tlsinit`), Linux uses a per-module ELF TLS variable, Windows uses `TlsAlloc`.
   - **Mechanism:** `basic`'s init leaves its `g` bound to the main thread (`x_cgo_bindm`, Go 1.21). When Node enters `second`, its `cgocallback` finds the slot non-null and uses `basic`'s `g` and M as its own. In CI (macOS 15 x64, Go 1.24.13) a goroutine from `basic` runs with `second`'s M and mcache → `fatal error: bad sweepgen in refill`.
   - **Deterministic check:** `__goTlsSlot()` (darwin/amd64 only, in `napi/diag.c`) reads the slot. After `second`'s init, the slot still holds `basic`'s `g`.
   - **Consequences:**
     - on macOS x64 only one napi-go addon per process is supported;
     - in Phase 1 napi-go must reject a second runtime with a clear error instead of corrupting memory;
     - the signal issue remains: the second runtime's handlers also receive signals from the first runtime's threads. This must be investigated before declaring the rejection safe;
     - limited impact: macOS 26 is the final macOS release for Intel Macs.
