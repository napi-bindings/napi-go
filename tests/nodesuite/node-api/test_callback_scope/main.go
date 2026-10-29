// Porting di test/node-api/test_callback_scope/binding.c. uv_queue_work passa per lo shim di
// libuv di napitest.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// runInCallbackScopeArgc è il numero di argomenti atteso da runInCallbackScope.
const runInCallbackScopeArgc = 3

func runInCallbackScope(env sys.Env, info sys.CallbackInfo) sys.Value {
	var argc uint
	var args [runInCallbackScopeArgc]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, nil, nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == runInCallbackScopeArgc, "argc == 3", "Wrong number of arguments") {
		return nil
	}

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var valuetype sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype)) {
		return nil
	}
	if !nt.Assert(env, valuetype == sys.TypeObject, "valuetype == napi_object",
		"Wrong type of arguments. Expects an object as first argument.") {
		return nil
	}

	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype)) {
		return nil
	}
	if !nt.Assert(env, valuetype == sys.TypeString, "valuetype == napi_string",
		"Wrong type of arguments. Expects a string as second argument.") {
		return nil
	}

	if !nt.Call(env, sys.Typeof(env, args[2], &valuetype)) {
		return nil
	}
	if !nt.Assert(env, valuetype == sys.TypeFunction, "valuetype == napi_function",
		"Wrong type of arguments. Expects a function as third argument.") {
		return nil
	}

	var context sys.AsyncContext
	if !nt.Call(env, sys.AsyncInit(env, args[0], args[1], &context)) {
		return nil
	}

	var scope sys.CallbackScope
	if !nt.Call(env, sys.OpenCallbackScope(env, args[0], context, &scope)) {
		return nil
	}

	// Un'eccezione pendente dopo la chiamata va bene: niente nt.Call, perché lo scope va chiuso
	// comunque.
	var result sys.Value
	functionCallResult := sys.CallFunction(env, args[0], args[2], 0, nil, &result)
	if functionCallResult != sys.StatusOK {
		nt.GetAndThrowLastError(env)
	}

	if !nt.Call(env, sys.CloseCallbackScope(env, scope)) {
		return nil
	}
	if !nt.Call(env, sys.AsyncDestroy(env, context)) {
		return nil
	}

	return result
}

var (
	sharedEnv sys.Env
	deferred  sys.Deferred
)

func callback(req unsafe.Pointer, _ int) {
	env := sharedEnv
	var handleScope sys.HandleScope
	if !nt.Call(env, sys.OpenHandleScope(env, &handleScope)) {
		return
	}

	var resourceName sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("test"), sys.AutoLength, &resourceName)) {
		return
	}
	var context sys.AsyncContext
	if !nt.Call(env, sys.AsyncInit(env, nil, resourceName, &context)) {
		return
	}

	var resourceObject sys.Value
	if !nt.Call(env, sys.CreateObject(env, &resourceObject)) {
		return
	}

	var undefinedValue sys.Value
	if !nt.Call(env, sys.GetUndefined(env, &undefinedValue)) {
		return
	}

	var scope sys.CallbackScope
	if !nt.Call(env, sys.OpenCallbackScope(env, resourceObject, context, &scope)) {
		return
	}

	if !nt.Call(env, sys.ResolveDeferred(env, deferred, undefinedValue)) {
		return
	}

	if !nt.Call(env, sys.CloseCallbackScope(env, scope)) {
		return
	}

	if !nt.Call(env, sys.CloseHandleScope(env, handleScope)) {
		return
	}

	if !nt.Call(env, sys.AsyncDestroy(env, context)) {
		return
	}
	nt.Free(req)
}

func noopWork(unsafe.Pointer) {}

func testResolveAsync(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var promise sys.Value
	if deferred == nil {
		sharedEnv = env
		if !nt.Call(env, sys.CreatePromise(env, &deferred, &promise)) {
			return nil
		}

		var loop unsafe.Pointer
		if !nt.Call(env, sys.GetUVEventLoop(env, &loop)) {
			return nil
		}

		req := nt.NewUVReq(nt.UVWork)
		// Il valore di ritorno non è controllato, come nel C.
		nt.UVQueueWork(loop, req, noopWork, callback)
	}

	return promise
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("runInCallbackScope", runInCallbackScope),
		nt.DeclareProperty("testResolveAsync", testResolveAsync),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
