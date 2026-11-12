# Coming from napi-rs

napi-go follows the design of napi-rs: annotated declarations, conversions from the types of the
language, TypeScript declarations, one npm package per platform. The table maps the napi-rs
features to napi-go.

## Code

| napi-rs | napi-go |
|---|---|
| `#[napi] fn add(a: f64, b: f64) -> f64` | `//napi:export` on `func Add(a, b float64) float64` |
| `#[napi(js_name = "x")]` | `//napi:export x` |
| `#[napi(object)] struct` | any struct converts; `//napi:object` declares it in `index.d.ts` even when unused |
| `#[napi] struct` + `#[napi] impl` (class) | `//napi:class` on the type; exported methods of `*T` |
| `#[napi(constructor)]`, `#[napi(factory)]` | `//napi:constructor` (`func(…) (*T, error)`); `//napi:export Class.name` for a static factory |
| `#[napi(getter)]`, `#[napi(setter)]` | `//napi:getter`, `//napi:setter` |
| `#[napi(skip)]` | `//napi:skip` |
| `#[napi] const` | `//napi:export` on a constant |
| `#[napi(ts_type = "…")]`, `ts_return_type` | `//napi:type`, `//napi:returns` |
| `Result<T>`, `Error::new(Status::InvalidArg, msg)` | `(T, error)`; `napi.TypeError(code, msg)`, `napi.RangeError` |
| a panic | a panic becomes an Error with code `GO_PANIC` |
| `async fn` (tokio) | a first parameter `context.Context`: the function runs in a goroutine |
| `AsyncTask` with `Task` | `Env.Async(work)`: `work` runs in a goroutine and returns a `Resolver` run on the JS thread |
| `AbortSignal` of an `AsyncTask` | no counterpart: the `context.Context` is canceled only when the env is destroyed |
| `ThreadsafeFunction<T>` | `napi.NewThreadsafeFunction[T]` |
| `ThreadsafeFunction::call_async` | `napi.NewJSFunc[A, R]`, `Call(ctx, a)` |
| `Function<A, R>`, `JsFunction` | `napi.Function` |
| `Either<A, B>` | `napi.Either[A, B]` |
| `Buffer`, `&[u8]`, `Uint8Array` | `[]byte`: a view as a parameter, a copy as a result (`Env.NewBuffer` for no copy) |
| `BigInt`, `i64` | `*big.Int`, `int64` (integers are range-checked) |
| `Option<T>` | `*T` |
| `HashMap<String, T>` | `map[string]T` |
| `ObjectFinalize` | `func (*T) Finalize(env napi.Env)` |
| `Reference<T>` to a class instance | the `*T` itself: in an env the same `*T` is always the same JS object |
| `Env` | `napi.Env` (a parameter of type `napi.Env` is injected) |
| `JsObject`, `JsUnknown` | `napi.Object`, `napi.Value` |
| `#[module_exports]` | `napi.Init` (Layer 1) |

## Tooling

| `@napi-rs/cli` | `napi-go` |
|---|---|
| `napi new` | `napi-go new` |
| `napi build --release --target …` | `napi-go build -release -target …` (zig for other platforms) |
| type generation of `napi build` | `napi-go gen` (run by `build`) |
| `napi create-npm-dirs`, `napi artifacts` | `napi-go artifacts` |
| `napi prepublish` | `napi-go prepublish -publish` |
| `"napi"` in `package.json` | `"napiGo"` |
| targets `x86_64-unknown-linux-gnu`, … | targets `linux-x64-gnu`, … (the platform package suffixes, as in napi-rs) |

The CLI is a Go program run with `go tool napi-go`, at the version of the library in `go.mod`;
it does not need npm.

## Differences

- **Call overhead.** A call into Go crosses from C into the Go runtime (about 100 ns on Linux,
  about 1 µs on Windows), where napi-rs calls Rust directly. Conversions of numbers and short
  strings happen in C in the same crossing. Bulk Promises are cheaper than in napi-rs. See the
  [benchmarks](../bench/README.md).
- **Binary size.** The Go runtime is part of every addon: a small addon is about 2.5 MB stripped,
  against well under 1 MB for napi-rs.
- **Platforms.** No musl (Alpine): Go cannot build shared libraries that load with musl. On macOS
  x64 only one Go addon can be loaded per process. No WebAssembly target. See
  [platforms](platforms.md).
- **Two garbage collectors.** Go values referenced from JS live until V8 collects the JS object;
  a `[]byte` parameter must not be kept after the call.
- **Concurrency.** Goroutines replace the tokio runtime and the libuv thread pool; there is no
  runtime to configure.
