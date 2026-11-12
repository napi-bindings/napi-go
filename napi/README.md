# napi — Layers 1 and 2 of napi-go

Idiomatic Go API on top of Node-API, built on Layer 0 (`sys`), in two layers that can be mixed in
the same addon:

- **Layer 2, the framework** (napi-rs style): declare Go functions and types and napi-go converts
  arguments and results, runs async functions in goroutines and backs JS classes with Go
  values. No hand-written callbacks.
- **Layer 1, the core** (node-addon-api style): typed JS values, Go errors converted into JS
  exceptions, callbacks, classes, references and buffers, for full control.

```go
package main

import (
	"context"

	"github.com/napi-bindings/napi-go/napi"
)

type User struct {
	Name  string
	Age   int     `napi:"age"`
	Email *string // nullable
}

type Counter struct{ n int }

func (c *Counter) Inc() int { c.n++; return c.n }

func init() {
	napi.Export("add", napi.Func2(func(a, b float64) float64 { return a + b }))
	napi.Export("greet", napi.Func1(func(u User) string { return "hello " + u.Name }))
	napi.Export("load", napi.Async1(func(ctx context.Context, id string) (User, error) {
		return User{Name: id}, nil // runs in a goroutine; JS gets a Promise
	}))
	napi.Export("Counter", napi.NewClass(napi.Ctor0(func() (*Counter, error) { return &Counter{}, nil })).
		Method("inc", napi.Method0((*Counter).Inc)))
}

func main() {}
```

```sh
go build -buildmode=c-shared -o addon.node .
node -e "const a = require('./addon.node'); console.log(a.add(1, 2), a.greet({ name: 'Ann', age: 3 }))"
```

The same addon with Layer 1:

```go
func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		add, err := env.NewFunction("add", func(c napi.CallContext) (napi.Value, error) {
			a, err := c.Arg(0).Float64()
			if err != nil {
				return napi.Value{}, err // TypeError ERR_NAPI_NUMBER_EXPECTED in JS
			}
			b, err := c.Arg(1).Float64()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewFloat64(a + b)
		})
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, exports.Set("add", add.Value)
	})
}
```

Complete examples: `tests/e2e/addons/framework` (Layer 2), `tests/e2e/addons/napibasic` (Layer 1),
`tests/e2e/addons/napiasync` (concurrency). The node-addon-api test suite ported to napi
(`tests/addonapisuite`) checks that Layer 1 behaves like node-addon-api.

## Layer 2: the framework

Instead of writing the registrations by hand, annotate the declarations (`//napi:export`,
`//napi:class`) and let [`napi-go gen`](../internal/codegen/README.md) generate them together with
the TypeScript declarations.

### Exports

`napi.Export(name, def)` adds an export, created in every env that loads the addon (main thread
and `worker_threads`), in registration order together with `napi.Init` functions. `def` is a
function (`FuncDef`), a class (`*ClassDef[T]`) or a constant (`napi.Const(v)`, converted with
`ToValue`). A type that cannot be converted makes `require()` fail with a TypeError naming the
export, the wrapper and the type, not the first call. `FuncDef.New(env, name)` creates a JS
function at runtime (to return a closure, for instance).

### Wrappers

Generated for 0 to 8 JS parameters (`napi/zz_wrappers.go`, `go generate ./napi`):

| Wrapper | Go function | JS |
|---|---|---|
| `FuncN` | `func(A1…AN) R` | returns the converted `R` |
| `FuncEN` | `func(A1…AN) (R, error)` | returns `R`, or throws the error |
| `ProcN` / `ProcEN` | `func(A1…AN)` / `… error` | returns `undefined` (or throws) |
| `AsyncN` | `func(context.Context, A1…AN) (R, error)` | returns a Promise; the function runs in a goroutine |
| `AsyncProcN` | `func(context.Context, A1…AN) error` | Promise resolved with `undefined` |
| `MethodN`, `MethodEN`, `MethodProcN`, `MethodProcEN` | the same with a `*T` receiver first | methods of the class of `T` |
| `CtorN` | `func(A1…AN) (*T, error)` | constructor of the class of `T` |

A parameter of type `napi.Env` or `napi.CallContext` is filled from the call and does not consume
a JS argument (`Func2(func(env napi.Env, s string) (napi.Value, error))` takes one JS argument).
Missing JS arguments are `undefined`, extra ones are ignored. The JS function has the Go name
given to `Export` and a `length` equal to its JS parameters.

### Type mapping

| Go | JS (from JS) | JS (to JS) |
|---|---|---|
| `bool` | boolean | boolean |
| `float64`, `float32` | number | number |
| `int8`…`int32`, `uint8`…`uint32` | number, **integer in range** (else RangeError `ERR_OUT_OF_RANGE`, never truncated) | number |
| `int`, `int64`, `uint`, `uint64`, `uintptr` | integer number or BigInt in range | number (beyond 2⁵³ precision is lost: use `*big.Int`) |
| `string` | string | string |
| `[]byte` | Buffer, Uint8Array, Uint8ClampedArray, Int8Array, ArrayBuffer, DataView: **a view** of the JS memory (a copy in `Async*`) | a new Buffer (copy) |
| `*big.Int`, `big.Int` | BigInt or integer number | BigInt |
| `time.Time` | Date (invalid Date: RangeError) | Date |
| `[]T` | array | array (`nil` → `[]`) |
| `[N]T` | array of length N | array |
| `map[K]V` (K string or integer kinds) | object (own enumerable keys) | object, keys sorted |
| struct | object: exported fields, see below | object |
| `*T` | `null`/`undefined` → `nil`, else T | `nil` → `null` |
| `*T` of a registered class | instance of the class (or `null`/`undefined` → `nil`) | its instance |
| `any` | natural type: `nil`, `bool`, `float64`, `string`, `*big.Int`, `time.Time`, `[]byte` (copy), `[]any`, `map[string]any`, `napi.Function` | conversion of the dynamic type |
| `Either[A, B]` | tries A, then B | the value that is set |
| `napi.Value`, `Object`, `Function`, `Array`, `String`, `Buffer`, … | checked and passed as is (not in `Async*`) | as is |
| type implementing `FromJS` / `ToJS` | `(*T).FromJS(v Value) error` | `T.ToJS(env Env) (Value, error)` |

Named types convert like their underlying type (`type Celsius float64`). Not convertible:
channels, functions, complex numbers, non-empty interfaces (from JS), maps with other key types.

**Structs.** The JS name of a field is its `napi` tag, else its `json` tag, else the Go name in
lowerCamelCase (`UserID` → `userID`, `URLPath` → `urlPath`). `napi:"-"` skips the field,
`omitempty` leaves zero values out and makes the field optional. Embedded structs are flattened,
as in `encoding/json`. A field that is `undefined` is an error unless it is optional: a pointer,
`any`, `napi.Value`, or tagged `omitempty`. Recursive types are supported.

**Errors.** A failed conversion throws TypeError `ERR_INVALID_ARG_TYPE`, RangeError
`ERR_OUT_OF_RANGE` or `ERR_INVALID_ARG_VALUE` (the codes Node.js uses for its own argument
checks), with the position of the value:
`argument 1: address.tags[2]: expected string, got number`. Errors returned by the Go function
follow the Layer 1 rules (table below); a panic becomes `GO_PANIC`.

### Classes

```go
napi.Export("Counter", napi.NewClass(napi.Ctor1(newCounter)).
	Method("add", napi.Method1((*Counter).Add)).
	Accessor("value", napi.Accessor(func(c *Counter) int { return c.n }, func(c *Counter, v int) { c.n = v })).
	Static("from", napi.Func1(func(n int) *Counter { return &Counter{n: n} })).
	StaticConst("MAX", 100))
```

Instances wrap a `*T`. Methods and accessors are on the prototype, statics on the constructor.
A `*T` result creates the instance of its class, or returns the existing one: in an env the same
`*T` is always the same JS object. A `*T` parameter accepts only instances of the class. If `*T`
has `Finalize(env napi.Env)`, it runs when the last instance wrapping that `*T` is collected (it
must not call JS). `napi.RegisterClass(name, def)` makes a class known without exporting it;
with the zero `CtorDef` a class cannot be constructed from JS. JS subclasses (`class Sub extends
Counter`) work.

### Async functions and JSFunc

`AsyncN` converts the arguments on the JS thread (so a bad argument throws synchronously), runs
the function in a goroutine (`Env.Async`, with the env context), and converts the result on the
JS thread when the Promise settles. Parameters and results must not contain napi values; `[]byte`
arguments are copied.

`NewJSFunc[A, R](env, fn, opts)` lets goroutines call a JS function and wait for the converted
result: `r, err := f.Call(ctx, arg)`. A returned Promise is awaited; JS exceptions and rejections
become an `*Error` with the JS message, code and kind. It is a threadsafe function (Release,
Abort, Ref/Unref); queued calls return `ErrClosing` when it is aborted and `ErrEnvClosed` when
the env is destroyed.

### How it works, and what it costs

- **Codecs.** Each Go type gets a codec built once with reflection and cached; at run time it
  works on `unsafe` pointers. Primitives, `[]byte`, napi values and class pointers are converted
  by direct calls, so their Go values stay on the stack: **0 Go allocations per call** for
  numbers, booleans, Buffers, methods and accessors (measured in the e2e tests).
- **Typed trampoline.** Functions, methods and accessors use a C trampoline that reads number,
  boolean and string arguments (up to 512 bytes in total), unwraps `this` for methods, and
  creates number, boolean and short string results in C: one cgo transition per call. A value
  C cannot convert (wrong type, long string) is converted in Go, with the same error messages.

  | Call (Node 22, Linux x64) | untyped path | typed path |
  |---|---|---|
  | `add(1, 2)`: `Func2(float64, float64) float64` | 628 ns | 306 ns |
  | `greet("x")`: `Func1(string) string` | 671 ns | 324 ns |
  | `c.add(1)`: `Method1(int) int` | 693 ns | 280 ns |
  | empty function (reference) | 152 ns | |

## Contents

| File | Contents |
|---|---|
| `env.go` | `Env`, `Init`, per-env state (instance data), versions, `RunScript`, pending exceptions, `ReadStats` |
| `value.go` | `Value`, `Type`, `Is*` predicates, `As*` conversions, primitive getters, coercion |
| `primitive.go` | numbers, booleans, `String` (UTF-8/UTF-16/Latin-1), `Symbol`, `BigInt` (including `*big.Int`), `Date` |
| `object.go` | `Object` and properties (string key, value, index), keys, prototype, freeze/seal, `Array`, `Property` and `DefineProperties` |
| `function.go` | `Callback`, `CallContext`, `Function` (`NewFunction`, `Call`, `New`), callback table |
| `class.go` | `DefineClass`, `Promise`/`Deferred` on the JS thread |
| `dispatcher.go` | per-env dispatcher (one threadsafe function, coalesced Go queue), `Env.Context`, `Ref.DeleteLater` |
| `async.go` | `Env.Async`, `Resolver`: Promises settled from goroutines |
| `tsfn.go` | `ThreadsafeFunction[T]`, `CallMode`, `ErrClosing`, `ErrQueueFull` |
| `lifecycle.go` | finalizers, `Wrap`/`Unwrap`/`UnwrapAs`, type tags (objects and externals), `External`, `Ref`, handle scopes, cleanup hooks |
| `buffer.go` | `Buffer`, `ArrayBuffer` (including external, zero-copy), `TypedArray`, `DataView` |
| `errors.go` | `Error`, `ErrPendingException`, `TypeError`/`RangeError`/`SyntaxError`, `Throw`, throwing rules, `Env.Check` for statuses from `sys` |
| `table.go` | ID → Go value table with lock-free reads |
| `conv.go` | Layer 2: codecs (Go ↔ JS conversions), `ToValue`, `FromValue`, `FromJS`, `ToJS` |
| `either.go` | `Either[A, B]` |
| `export.go` | Layer 2: `FuncDef`, the call engine (`arg`, `result`), `Export`, `Const` |
| `zz_wrappers.go` | generated: `FuncN`, `AsyncN`, `MethodN`, `CtorN` and variants (`internal/genwrappers`) |
| `classdef.go` | Layer 2: `NewClass`, `ClassDef`, `Accessor`, `RegisterClass`, instance identity |
| `typed.go` | Layer 2 on the typed trampoline of `sys` |
| `tsfncall.go` | `JSFunc`: calls from goroutines with a converted result |

## Layer 1: design choices

**Value carries its env.** `Value` is `{env, napi_value}`: two words passed by value, like
`Napi::Value`. Specialized types (`Object`, `Function`, `String`, `Buffer`, ...) embed
`Value`, so they inherit all its methods and `x.Value` returns the generic value without
conversions. Arguments to `Set`, `Call`, and similar methods are `Value` rather than an interface:
converting a struct to an interface would allocate.

**Errors.** Every fallible operation returns `error`. A status other than `napi_ok`
becomes a `*napi.Error` with the message from `napi_get_last_error_info`, copied immediately (Node
overwrites it on the next call). In JS:

| Error returned by a callback | Thrown as |
|---|---|
| `*napi.Error` from a `*_expected` status | `TypeError`, code `ERR_NAPI_<STATUS>` (e.g. `ERR_NAPI_NUMBER_EXPECTED`) |
| `*napi.Error` from another status | `Error`, code `ERR_NAPI_<STATUS>` |
| `napi.TypeError(code, msg)`, `RangeError`, `SyntaxError` | the corresponding constructor, with code |
| error implementing `JSCode() string` | `Error` with that code |
| any other `error` | `Error` with message `err.Error()` |
| `napi.Throw(v)` | the value `v` as-is |
| `ErrPendingException` | the already-pending exception, unchanged |
| panic | `Error` with code `GO_PANIC` (handled by `sys`) |

Wrapped errors (`fmt.Errorf("...: %w", err)`, `errors.Join`) preserve their type and code. If
an exception is already pending, the returned error is ignored: JS receives the first exception.
`Env.Catch(err)` catches the exception signaled by `ErrPendingException` and clears it.

**IDs instead of pointers.** The cgo rules forbid passing Go pointers to C. Callbacks,
values associated via `Wrap` and `NewExternal`, finalizers, and cleanup hooks are stored in two tables
(`callbacks`, `handles`) and Node-API receives an ID as `data`/`hint`. Reads are lock-free (atomic
loads on 256-slot segments); IDs are freed in the finalizer of the JS object holding them and
reused. A function created with `NewFunction` or a class from `DefineClass` keeps its callbacks
alive until V8 garbage-collects it. `ReadStats` exposes the counters (for leak tests).

**Per-env state.** `Init` runs for every env that loads the addon (main thread and `worker_threads`).
The napi state for an env is stored via `napi_set_instance_data` and released when the env is
destroyed; `Env.SetInstanceData` stores the user's value within it. Therefore, an addon using napi
must not call `sys.SetInstanceData` or register `sys` handlers directly.

**Allocations.** Measured in e2e tests: 0 Go allocations per call for a callback,
reading/writing numbers, `typeof`, reading a property by string key (key created
with `node_api_create_property_key_utf8` when available), and `Function.Call` with no arguments.
`GoString` reads strings up to 250 bytes into a stack buffer with a single cgo crossing:
the only allocation is the Go string. `Call` and `New` with arguments allocate the argument buffer
(functions that execute JS cannot be marked `nocallback`, so cgo moves the buffer to the heap).

**Zero-copy buffers.** `Bytes`, `NewBuffer`, `NewArrayBuffer`, and `TypedArray.Info` return
a `[]byte` backed by V8 memory, valid as long as the object is alive and not detached: copy it before
retaining it or passing it to a goroutine. `NewExternalBuffer`/`NewExternalArrayBuffer` hand JS
Go memory pinned with `runtime.Pinner` until the finalizer runs; where external buffers are not
allowed (`napi_no_external_buffers_allowed`: V8 sandbox, Electron) they fall back to a copy.

## Threading

Everything except the APIs listed below must be called on the env's JS thread, inside a callback,
`Init`, or code those run (a `Resolver`, a `callJS`, a finalizer). `Env`, `Value`, and `Ref` must
not be used from other goroutines. To work from goroutines:

| API | Use |
|---|---|
| `Env.Async(work)` | returns a Promise settled by `work`, which runs in a goroutine without `Env` and returns a `Resolver` that builds the result on the JS thread (napi-rs `AsyncTask` / `async fn`). Errors and panics reject it; the event loop stays alive until it settles |
| `Env.Context()` | a `context.Context` canceled when the env is destroyed (worker terminated, process exiting): goroutines started for the env should stop |
| `NewThreadsafeFunction[T](env, fn, callJS, opts)` | goroutines `Call(v T, Blocking/NonBlocking)`; `callJS` converts `v` and calls `fn` on the JS thread (`Napi::TypedThreadSafeFunction`). `Acquire`/`Release`/`Abort` from any goroutine, `Ref`/`Unref` on the JS thread |
| `Ref.DeleteLater()` | deletes a reference from any goroutine (queued on the JS thread) |
| `AsyncN` (Layer 2) | `Env.Async` with automatic conversion of arguments and result |
| `NewJSFunc[A, R]` (Layer 2) | goroutines call a JS function and receive its converted result (`Call(ctx, a)`) |

How it works and what it guarantees:

- **One dispatcher per env.** Each env (main thread, worker) has one `napi_threadsafe_function`,
  unref'd, and a Go queue of closures: a burst of results from goroutines costs one Node-API call
  (the call is made only when the queue goes from empty to non-empty). `Async` keeps the event
  loop alive while work is pending. A closure that throws ends its batch, and the remaining
  closures run on the next call.
- **Destroyed envs.** When the env is destroyed, `Env.Context()` is canceled and pending Promises
  are dropped (nobody can observe them). The methods of a `ThreadsafeFunction` then return
  `ErrClosing`: calls hold a read lock that the finalizer takes for writing, so a goroutine can
  never call a function Node has already freed.
- **Closed functions.** After the last `Release`, or after `Abort`, every method returns
  `ErrClosing`. Node-API itself keeps accepting calls until the JS thread notices, and reports
  `napi_invalid_arg` after an abort.
- **Blocking calls.** Node wakes a thread blocked on a full queue with a single
  `cond->Signal`, and only when it pops from a full queue or closes the function
  (`src/node_api.cc`): with several threads blocked at once, wake-ups are lost. `Call` first tries
  without blocking. If the queue is full, goroutines wait for room one at a time.
- **Errors in `callJS`.** An error or panic in `callJS` is thrown on the JS thread. Node reports
  it as an uncaught exception for modules declaring Node-API 10 (`napi_go_v10`) or with
  `--force-node-api-uncaught-exceptions-policy=true`. Otherwise it emits the DEP0168 warning and
  drops it.

## Current limitations

- With `napi_go_experimental`, finalizers may run inside V8's GC: `FinalizeFunc` callbacks
  must not call functions that execute JS (`node_api_post_finalizer` is not exposed yet).
- `DefineClass` creates base classes: inheriting from a native class works from JS
  (`class Sub extends Counter`), not between classes defined in Go.
- Async context, `MakeCallback` and `napi_async_work` (the libuv thread pool) are available only in
  `sys`; `Env.Async` runs work in goroutines.
- A `ThreadsafeFunction` call does not return the JS result: use `JSFunc` for that.
- Layer 2: no async methods (use `Env.Async` in a method), no variadic Go functions (inject
  `CallContext`), class methods have `length` 0, classes defined in Go cannot extend each other,
  and a `*T` converts to the class registered for `T` only (one class per Go type).
- A class defined with `DefineClass` always throws a TypeError when called without `new`; there
  is no hook to make it act as a factory (node-addon-api's `ObjectWrap::OnCalledAsFunction`).
- `NewSymbol` always sets a description: `Symbol()` without one has no counterpart.
- SharedArrayBuffer creation (experimental Node-API) is not wrapped.
