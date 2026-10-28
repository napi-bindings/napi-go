// Package myobject è il porting di test/js-native-api/6_object_wrap/myobject.cc e myobject.h,
// condiviso dai target myobject e myobject_basic_finalizer del binding.gyp: nel C sono lo stesso
// sorgente compilato due volte, il secondo con NAPI_EXPERIMENTAL (Destructor diventa un
// finalizer "basic" che gira durante il GC).
//
// La classe C++ MyObject diventa la struct myObject; il puntatore passato a napi_wrap è un
// cgo.Handle, cancellato dal Destructor come la delete del C.
package myobject

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

// finalizerData è FinalizerData (int32_t) del C: il contatore dei Destructor, nell'instance data.
type finalizerData = int32

const finalizerDataSize = uint(unsafe.Sizeof(finalizerData(0)))

type myObject struct {
	value   float64
	env     sys.Env
	wrapper sys.Ref
}

// constructor è MyObject::constructor; danglingRef è dangling_ref.
var (
	constructor sys.Ref
	danglingRef sys.Ref
)

// release equivale a ~MyObject: lo status non è controllato, come nel C.
func (o *myObject) release() {
	sys.DeleteReference(o.env, o.wrapper)
}

// unwrap equivale a napi_unwrap(env, this, (void**)&obj).
func unwrap(env sys.Env, this sys.Value) (*myObject, bool) {
	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, this, &data)) {
		return nil, false
	}
	return cgo.Handle(uintptr(data)).Value().(*myObject), true
}

// destructor è MyObject::Destructor.
func destructor(env sys.Env, nativeObject, _ uintptr) {
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*myObject)
	h.Delete()
	obj.release()

	var data unsafe.Pointer
	if !nt.BasicCall(env, sys.GetInstanceData(env, &data)) {
		return
	}
	*(*finalizerData)(data) += 1
}

func initClass(env sys.Env, exports sys.Value) {
	properties := []sys.PropertyDescriptor{
		nt.Accessor("value", getValue, setValue, sys.PropertyDefault),
		nt.Accessor("valueReadonly", getValue, nil, sys.PropertyDefault),
		nt.DeclareProperty("plusOne", plusOne),
		nt.DeclareProperty("multiply", multiply),
	}

	var cons sys.Value
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline,
		newData, uint(len(properties)), &properties[0], &cons)) {
		return
	}

	if !nt.Call(env, sys.CreateReference(env, cons, 1, &constructor)) {
		return
	}

	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("MyObject"), cons)) {
		return
	}
}

// newData è il data del costruttore: si registra una volta sola.
var newData = nt.Register(newMyObject)

// newMyObject è MyObject::New.
func newMyObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	var newTarget sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTarget)) {
		return nil
	}
	isConstructor := newTarget != nil

	argc := uint(1)
	var args [1]sys.Value
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], &this, nil)) {
		return nil
	}

	if isConstructor {
		// Invocato come costruttore: `new MyObject(...)`.
		value := 0.0
		var valuetype sys.ValueType
		if !nt.Call(env, sys.Typeof(env, args[0], &valuetype)) {
			return nil
		}

		if valuetype != sys.TypeUndefined {
			if !nt.Call(env, sys.GetValueDouble(env, args[0], &value)) {
				return nil
			}
		}

		obj := &myObject{value: value, env: env}
		h := cgo.NewHandle(obj)
		cb, hint := nt.Finalizer(destructor, 0)
		if !nt.Call(env, sys.Wrap(env, this, uintptr(h), cb, hint, &obj.wrapper)) {
			nt.DiscardFinalizer(hint)
			h.Delete()
			return nil
		}

		return this
	}

	// Invocato come funzione `MyObject(...)`: lo si trasforma in una chiamata al costruttore.
	argc = 1
	argv := [1]sys.Value{args[0]}

	var cons sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, constructor, &cons)) {
		return nil
	}

	var instance sys.Value
	if !nt.Call(env, sys.NewInstance(env, cons, argc, &argv[0], &instance)) {
		return nil
	}

	return instance
}

func getValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}

	obj, ok := unwrap(env, this)
	if !ok {
		return nil
	}

	var num sys.Value
	if !nt.Call(env, sys.CreateDouble(env, obj.value, &num)) {
		return nil
	}

	return num
}

func setValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], &this, nil)) {
		return nil
	}

	obj, ok := unwrap(env, this)
	if !ok {
		return nil
	}

	if !nt.Call(env, sys.GetValueDouble(env, args[0], &obj.value)) {
		return nil
	}

	return nil
}

func plusOne(env sys.Env, info sys.CallbackInfo) sys.Value {
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}

	obj, ok := unwrap(env, this)
	if !ok {
		return nil
	}

	obj.value += 1

	var num sys.Value
	if !nt.Call(env, sys.CreateDouble(env, obj.value, &num)) {
		return nil
	}

	return num
}

func multiply(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], &this, nil)) {
		return nil
	}

	multiple := 1.0
	if argc >= 1 {
		if !nt.Call(env, sys.GetValueDouble(env, args[0], &multiple)) {
			return nil
		}
	}

	obj, ok := unwrap(env, this)
	if !ok {
		return nil
	}

	var cons sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, constructor, &cons)) {
		return nil
	}

	const argCount = 1
	var argv [argCount]sys.Value
	if !nt.Call(env, sys.CreateDouble(env, obj.value*multiple, &argv[0])) {
		return nil
	}

	var instance sys.Value
	if !nt.Call(env, sys.NewInstance(env, cons, argCount, &argv[0], &instance)) {
		return nil
	}

	return instance
}

// objectWrapDanglingReferenceFinalizer non deve mai essere chiamato.
func objectWrapDanglingReferenceFinalizer(sys.Env, uintptr, uintptr) {
	nt.CAssert(false, `0 && "unreachable"`)
}

func objectWrapDanglingReference(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	// Crea un napi_wrap e lo rimuove subito, lasciando pendente (non cancellato) il ref
	// restituito.
	cb, hint := nt.Finalizer(objectWrapDanglingReferenceFinalizer, 0)
	if !nt.Call(env, sys.Wrap(env, args[0], 0, cb, hint, &danglingRef)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.RemoveWrap(env, args[0], nil)) {
		return nil
	}
	// Dopo napi_remove_wrap Node-API non chiama più il finalizer.
	nt.DiscardFinalizer(hint)

	return args[0]
}

func objectWrapDanglingReferenceTest(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var out, ret sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, danglingRef, &out)) {
		return nil
	}

	if out == nil {
		// Il napi_ref è stato invalidato: lo si cancella.
		if !nt.Call(env, sys.DeleteReference(env, danglingRef)) {
			return nil
		}
		if !nt.Call(env, sys.GetBoolean(env, true, &ret)) {
			return nil
		}
	} else {
		// Il napi_ref pendente è ancora valido.
		if !nt.Call(env, sys.GetBoolean(env, false, &ret)) {
			return nil
		}
	}

	return ret
}

func getFinalizerCallCount(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}

	var data unsafe.Pointer
	if !nt.Call(env, sys.GetInstanceData(env, &data)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.CreateInt32(env, *(*finalizerData)(data), &result)) {
		return nil
	}

	return result
}

func finalizeData(_ sys.Env, data, _ uintptr) {
	nt.FreeAddr(data)
}

// Init è Init del C (EXTERN_C_START … EXTERN_C_END).
func Init(env sys.Env, exports sys.Value) sys.Value {
	// Il C non controlla il risultato di new; qui un'allocazione fallita lancia un errore invece
	// di scrivere su NULL.
	data := (*finalizerData)(nt.Malloc(finalizerDataSize))
	if !nt.Assert(env, data != nil, "data != NULL", "Failed to allocate memory") {
		return nil
	}
	*data = 0
	cb, hint := nt.Finalizer(finalizeData, 0)
	if !nt.Call(env, sys.SetInstanceData(env, uintptr(unsafe.Pointer(data)), cb, hint)) {
		nt.DiscardFinalizer(hint)
		nt.Free(unsafe.Pointer(data))
		return nil
	}

	initClass(env, exports)

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("objectWrapDanglingReference", objectWrapDanglingReference),
		nt.DeclareProperty("objectWrapDanglingReferenceTest", objectWrapDanglingReferenceTest),
		nt.DeclareProperty("getFinalizerCallCount", getFinalizerCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
