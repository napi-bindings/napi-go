# napi — Layer 1 of napi-go

Idiomatic Go API on top of Node-API, built on Layer 0 (`sys`). It is the Go equivalent of
node-addon-api for C++: typed values, Go errors converted into JS exceptions, callbacks, classes,
references, and buffers, without passing Go pointers to C.

```go
package main

import "github.com/napi-bindings/napi-go/napi"

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

func main() {}
```

```sh
go build -buildmode=c-shared -o add.node .
node -e "console.log(require('./add.node').add(1, 2))"
```

Complete example covering all parts of the API: `tests/e2e/addons/napibasic`. The node-addon-api
test suite ported to napi (`tests/addonapisuite`) checks that napi behaves like node-addon-api.

## Contents

| File | Contents |
|---|---|
| `env.go` | `Env`, `Init`, per-env state (instance data), versions, `RunScript`, pending exceptions, `ReadStats` |
| `value.go` | `Value`, `Type`, `Is*` predicates, `As*` conversions, primitive getters, coercion |
| `primitive.go` | numbers, booleans, `String` (UTF-8/UTF-16/Latin-1), `Symbol`, `BigInt` (including `*big.Int`), `Date` |
| `object.go` | `Object` and properties (string key, value, index), keys, prototype, freeze/seal, `Array`, `Property` and `DefineProperties` |
| `function.go` | `Callback`, `CallContext`, `Function` (`NewFunction`, `Call`, `New`), callback table |
| `class.go` | `DefineClass`, `Promise`/`Deferred` on the JS thread |
| `lifecycle.go` | finalizers, `Wrap`/`Unwrap`/`UnwrapAs`, type tags (objects and externals), `External`, `Ref`, handle scopes, cleanup hooks |
| `buffer.go` | `Buffer`, `ArrayBuffer` (including external, zero-copy), `TypedArray`, `DataView` |
| `errors.go` | `Error`, `ErrPendingException`, `TypeError`/`RangeError`/`SyntaxError`, `Throw`, throwing rules, `Env.Check` for statuses from `sys` |
| `table.go` | ID → Go value table with lock-free reads |

## Design choices

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

Everything must be called on the env's JS thread, inside a callback or `Init`. `Env`, `Value`, and `Ref`
must not be used from other goroutines. APIs for working from goroutines (Promises resolved from
goroutines, typed threadsafe functions, releasing Refs from other threads) are planned for
Phase 3.

## Current limitations

- With `napi_go_experimental`, finalizers may run inside V8's GC: `FinalizeFunc` callbacks
  must not call functions that execute JS (`node_api_post_finalizer` is not exposed yet).
- `DefineClass` creates base classes: inheriting from a native class works from JS
  (`class Sub extends Counter`), not between classes defined in Go.
- Async context, `MakeCallback`, async work, and threadsafe functions are currently available only in `sys`.
- A class defined with `DefineClass` always throws a TypeError when called without `new`; there
  is no hook to make it act as a factory (node-addon-api's `ObjectWrap::OnCalledAsFunction`).
- `NewSymbol` always sets a description: `Symbol()` without one has no counterpart.
- SharedArrayBuffer creation (experimental Node-API) is not wrapped.
