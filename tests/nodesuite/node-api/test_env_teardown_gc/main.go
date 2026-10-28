// Porting di test/node-api/test_env_teardown_gc/binding.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

var myObjectData = nt.Register(myObject)

func myObjectFini(env sys.Env, data, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(data))
	var global, cleanup sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return
	}
	if !nt.Call(env, sys.GetNamedProperty(env, global, nt.CStr("cleanup"), &cleanup)) {
		return
	}
	status := sys.CallFunction(env, global, cleanup, 0, nil, nil)
	// Potrebbe non essere permesso chiamare JS: allora lo status è napi_pending_exception.
	if !nt.Assert(env, status == sys.StatusOK || status == sys.StatusPendingException,
		"status == napi_ok || status == napi_pending_exception", "Unexpected status for napi_call_function") {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, *ref)) {
		return
	}
	nt.FreeAddr(data)
}

func myObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	var jsThis sys.Value
	ref := (*sys.Ref)(nt.Malloc(refSize))
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &jsThis, nil)) {
		return nil
	}
	cb, hint := nt.Finalizer(myObjectFini, 0)
	if !nt.Call(env, sys.Wrap(env, jsThis, uintptr(unsafe.Pointer(ref)), cb, hint, ref)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return nil
}

// initModule equivale al corpo di NAPI_MODULE_INIT().
func initModule(env sys.Env, exports sys.Value) sys.Value {
	var ctor sys.Value
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline,
		myObjectData, 0, nil, &ctor)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("MyObject"), ctor)) {
		return nil
	}
	return exports
}
