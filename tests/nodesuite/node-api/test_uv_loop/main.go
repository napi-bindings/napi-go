// Port of test/node-api/test_uv_loop/test_uv_loop.cc: a uv_check_t and a uv_idle_t on the Node.js
// event loop via napitest's libuv shim. The C++ SetImmediate<T> template becomes a function
// taking a Go closure.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// dummy corresponds to static char dummy: its address is the non-null callback result.
var dummy byte

func setImmediate(env sys.Env, cb func() *byte) unsafe.Pointer {
	var loop unsafe.Pointer
	check := nt.NewUVHandle(nt.UVCheck)
	if !nt.Assert(env, sys.GetUVEventLoop(env, &loop) == sys.StatusOK,
		"napi_get_uv_event_loop(env, &loop) == napi_ok", "can get event loop") {
		return nil
	}
	// libuv return values are not checked, matching C.
	nt.UVCheckInit(loop, check)
	nt.UVCheckStart(check, func(check unsafe.Pointer) {
		nt.UVClose(check, func(handle unsafe.Pointer) { nt.Free(handle) })
		nt.CAssert(cb() != nil, "cb() != nullptr")
	})

	// The idle handle only prevents the loop from blocking in poll.
	idle := nt.NewUVHandle(nt.UVIdle)
	nt.UVIdleInit(loop, idle)
	nt.UVIdleStart(idle, func(idle unsafe.Pointer) {
		nt.UVClose(idle, func(handle unsafe.Pointer) { nt.Free(handle) })
	})

	return nil
}

func setImmediateBinding(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	var this sys.Value
	var data unsafe.Pointer
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], &this, &data)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Not enough arguments, expected 1.") {
		return nil
	}
	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argv[0], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeFunction, "t == napi_function", "Wrong first argument, function expected.") {
		return nil
	}
	var cbref sys.Ref
	if !nt.Call(env, sys.CreateReference(env, argv[0], 1, &cbref)) {
		return nil
	}

	setImmediate(env, func() *byte {
		var undefined, callback sys.Value
		var scope sys.HandleScope
		if !nt.Call(env, sys.OpenHandleScope(env, &scope)) {
			return nil
		}
		if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
			return nil
		}
		if !nt.Call(env, sys.GetReferenceValue(env, cbref, &callback)) {
			return nil
		}
		if !nt.Call(env, sys.DeleteReference(env, cbref)) {
			return nil
		}
		if !nt.Call(env, sys.CallFunction(env, undefined, callback, 0, nil, nil)) {
			return nil
		}
		if !nt.Call(env, sys.CloseHandleScope(env, scope)) {
			return nil
		}
		return &dummy
	})
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("SetImmediate", setImmediateBinding),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
