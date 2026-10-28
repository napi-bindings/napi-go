// Porting di test/js-native-api/test_reference_double_free/test_reference_double_free.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// gCallCount è g_call_count: il suo indirizzo è l'hint di noDeleteDestructor, come nel C.
var gCallCount uint

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

// destructor riceve il napi_ref* allocato da newMyObject.
func destructor(env sys.Env, data, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(data))
	if !nt.Call(env, sys.DeleteReference(env, *ref)) {
		return
	}
	nt.FreeAddr(data)
}

func noDeleteDestructor(_ sys.Env, data, hint uintptr) {
	callCount := (*uint)(nt.Pointer(hint))
	// Questo distruttore va chiamato esattamente una volta.
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

// deleteImmediately verifica che napi_remove_wrap seguita da napi_delete_reference non faccia
// cadere il processo. È l'analogo di objectwrap_constructor_exception.cc di node-addon-api, dove
// Napi::ObjectWrap<> viene distrutto subito dopo napi_wrap: essendo una sottoclasse di
// Napi::Reference<>, la napi_remove_wrap del suo distruttore precede la napi_delete_reference
// del distruttore di Napi::Reference<>.
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
	// Dopo napi_remove_wrap Node-API non chiama più il finalizer.
	nt.DiscardFinalizer(hint)
	if !nt.Call(env, sys.DeleteReference(env, ref)) {
		return nil
	}

	return nil
}

// newData è il data del costruttore: si registra una volta sola.
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
