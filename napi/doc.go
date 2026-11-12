// Package napi provides Layers 1 and 2 of napi-go, for writing native Node.js addons (.node) in
// Go on top of Node-API (package sys).
//
// # Layer 2: the framework
//
// Export declares Go functions, classes and constants; the generic wrappers convert arguments
// and results automatically:
//
//	func init() {
//		napi.Export("add", napi.Func2(func(a, b float64) float64 { return a + b }))
//		napi.Export("load", napi.Async1(func(ctx context.Context, id string) (User, error) { ... }))
//		napi.Export("Counter", napi.NewClass(napi.Ctor1(newCounter)).
//			Method("add", napi.Method1((*Counter).Add)))
//	}
//
// The wrappers are FuncN, FuncEN, ProcN, ProcEN (synchronous), AsyncN, AsyncProcN (Promises
// settled from goroutines), MethodN and its variants, and CtorN, for 0 to 8 JS parameters. A
// napi.Env or napi.CallContext parameter is injected. Conversions follow the Go type: numbers
// (integers are range-checked), strings, []byte (Buffer), slices, arrays, maps, structs (napi or
// json tags, else lowerCamelCase names), pointers (nullable), any, *big.Int, time.Time,
// Either[A, B], the napi value types, and types implementing FromJS and ToJS. ToValue and
// FromValue convert directly. JSFunc calls JS from goroutines and returns the converted result.
// See README.md for the type mapping and the costs.
//
// # Layer 1: the core
//
// Layer 1 gives full control over JS values:
//
//	package main
//
//	import "github.com/napi-bindings/napi-go/napi"
//
//	func init() {
//		napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
//			hello, err := env.NewFunction("hello", func(c napi.CallContext) (napi.Value, error) {
//				s, err := c.Env().NewString("world")
//				return s.Value, err
//			})
//			if err != nil {
//				return napi.Value{}, err
//			}
//			return napi.Value{}, exports.Set("hello", hello.Value)
//		})
//	}
//
//	func main() {}
//
// Build with go build -buildmode=c-shared -o hello.node.
//
// # Values
//
// Env and Value wrap napi_env and napi_value. Value carries its env (two words, zero
// allocations) and specialized types (Object, Array, Function, String, BigInt, Buffer, ...)
// embed Value: Value methods work on all of them and the promoted Value field returns the
// generic value (obj.Set("s", str.Value)). Checked conversions are provided by Value.AsObject,
// Value.AsString, and so on.
//
// A Value is valid only on its env's JS thread and within the current handle scope: to retain it
// beyond a callback, use Value.NewRef. In loops that create many values, Env.WithScope releases
// them at the end of each iteration.
//
// # Errors and exceptions
//
// Every fallible operation returns error. A Node-API failure is returned as an *Error with the
// Status and message from Node. If a JS exception is pending (JS called from Go threw),
// ErrPendingException is returned: returning it from a callback lets the exception propagate;
// Env.Catch intercepts it.
//
// A Callback that returns an error throws it in JS: an *Error using its Kind constructor
// (TypeError, RangeError, ...) and code, an error implementing Coder as an Error with that code,
// or any other error as an Error with message err.Error(). Throw throws an arbitrary JS value.
// A panic inside a callback becomes a JS Error with code "GO_PANIC".
//
// # Threading
//
// All napi functions must be called on the env's JS thread, inside a callback or during
// module initialization; Env and Value must not be passed to goroutines. Goroutines reach the
// JS thread through Env.Async (a Promise settled by work running in a goroutine), typed
// threadsafe functions (NewThreadsafeFunction) and Ref.DeleteLater; Env.Context is canceled
// when the env is destroyed.
//
// # Go values passed to JS
//
// Callbacks, values associated via Object.Wrap and Env.NewExternal, and finalizers are stored
// in Go tables while Node-API receives only an ID, in compliance with cgo pointer rules. They are
// released when V8 garbage-collects the JS object holding them (or when the env is destroyed).
// A function created with Env.NewFunction or a class defined with Env.DefineClass keeps its
// callbacks alive for as long as it lives.
//
// # Worker threads
//
// Functions registered with Init run for every env that loads the addon (main thread and
// worker_threads). Each env has its own state: Env.SetInstanceData, Env.AddCleanupHook.
//
// napi registers the sys handlers (SetModuleInitHandler, SetCallbackHandler, SetFinalizeHandler,
// SetCleanupHookHandler) and uses napi_set_instance_data: an addon that uses napi must not call
// the corresponding functions in sys.
package napi

//go:generate go run ./internal/genwrappers
