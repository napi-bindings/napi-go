// Port of test/js-native-api/test_reference_double_free/test_reference_double_free.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// gCallCount is g_call_count: its address is passed as the hint to noDeleteDestructor, matching C.
var gCallCount uint

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

// destructor receives the napi_ref* allocated by newMyObject.
func destructor(env sys.Env, data, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(data))
	if !nt.Call(env, sys.DeleteReference(env, *ref)) {
		return
	}
	nt.FreeAddr(data)
}

func noDeleteDestructor(_ sys.Env, data, hint uintptr) {
	callCount := (*uint)(nt.Pointer(hint))
	// This destructor must be called exactly once.
	if *callCount > 0 {
		nt.Abort()
	}
	*callCount = *callCount + 1
	nt.FreeAddr(data)
}

func newMyObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var jsThis, jsDelete sys.Value
	var deleteRef bool
	ref := (*sys.Ref)(nt.Malloc(refSize))
	refAddr := uintptr(unsafe.Pointer(ref))

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &jsDelete, &jsThis, nil)) {
		return nil
	}

	if !nt.Call(env, sys.GetValueBool(env, jsDelete, &deleteRef)) {
		return nil
	}

	if deleteRef {
		cb, hint := nt.Finalizer(destructor, 0)
		if !nt.Call(env, sys.Wrap(env, jsThis, refAddr, cb, hint, ref)) {
			nt.DiscardFinalizer(hint)
			return nil
		}
	} else {
		cb, hint := nt.Finalizer(noDeleteDestructor, uintptr(unsafe.Pointer(&gCallCount)))
		if !nt.Call(env, sys.Wrap(env, jsThis, refAddr, cb, hint, ref)) {
			nt.DiscardFinalizer(hint)
			return nil
		}
	}

	if !nt.Call(env, sys.ReferenceRef(env, *ref, nil)) {
		return nil
	}

	return jsThis
}

func noopDeleter(sys.Env, uintptr, uintptr) {}

// deleteImmediately verifies that calling napi_remove_wrap followed by napi_delete_reference does
// not crash the process. It mirrors objectwrap_constructor_exception.cc in node-addon-api, where
// Napi::ObjectWrap<> is destroyed immediately after napi_wrap: because it is a subclass of
// Napi::Reference<>, the napi_remove_wrap in its destructor runs before the napi_delete_reference
// in the Napi::Reference<> destructor.
func deleteImmediately(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var jsObj sys.Value
	var ref sys.Ref
	var typ sys.ValueType

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &jsObj, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, jsObj, &typ)) {
		return nil
	}
	if !nt.Assert(env, typ == sys.TypeObject, "type == napi_object", "Expected object parameter") {
		return nil
	}

	cb, hint := nt.Finalizer(noopDeleter, 0)
	if !nt.Call(env, sys.Wrap(env, jsObj, 0, cb, hint, &ref)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.RemoveWrap(env, jsObj, nil)) {
		return nil
	}
	// After napi_remove_wrap Node-API will no longer invoke the finalizer.
	nt.DiscardFinalizer(hint)
	if !nt.Call(env, sys.DeleteReference(env, ref)) {
		return nil
	}

	return nil
}

// newData is the constructor data: registered only once.
var newData = nt.Register(newMyObject)

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var myobjCtor sys.Value
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline,
		newData, 0, nil, &myobjCtor)) {
		return nil
	}

	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("MyObject"), myobjCtor)) {
		return nil
	}

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("deleteImmediately", deleteImmediately),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
