// Porting di test/js-native-api/8_passing_wrapped/myobject.cc e myobject.h. La classe C++
// MyObject diventa la struct myObject; il puntatore passato a napi_wrap è un cgo.Handle,
// cancellato dal Destructor come la delete del C.
package main

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

type myObject struct {
	value   float64
	env     sys.Env
	wrapper sys.Ref
}

var (
	// finalizeCount è size_t finalize_count, esposto come uint32 come nel C.
	finalizeCount uint32
	// constructor è MyObject::constructor.
	constructor sys.Ref
	// newData è il data del costruttore: si registra una volta sola.
	newData = nt.Register(newMyObject)
)

// val è MyObject::Val.
func (o *myObject) val() float64 { return o.value }

// release equivale a ~MyObject: lo status non è controllato, come nel C.
func (o *myObject) release() {
	finalizeCount++
	sys.DeleteReference(o.env, o.wrapper)
}

// destructor è MyObject::Destructor.
func destructor(_ sys.Env, nativeObject, _ uintptr) {
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*myObject)
	h.Delete()
	obj.release()
}

// unwrap equivale a napi_unwrap(env, value, (void**)&obj).
func unwrap(env sys.Env, value sys.Value) (*myObject, bool) {
	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, value, &data)) {
		return nil, false
	}
	return cgo.Handle(uintptr(data)).Value().(*myObject), true
}

// initClass è MyObject::Init.
func initClass(env sys.Env) sys.Status {
	var cons sys.Value
	status := sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline, newData,
		0, nil, &cons)
	if status != sys.StatusOK {
		return status
	}

	status = sys.CreateReference(env, cons, 1, &constructor)
	if status != sys.StatusOK {
		return status
	}

	return sys.StatusOK
}

// newMyObject è MyObject::New.
func newMyObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], &this, nil)) {
		return nil
	}

	obj := &myObject{}

	var valuetype sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype)) {
		return nil
	}

	if valuetype == sys.TypeUndefined {
		obj.value = 0
	} else {
		if !nt.Call(env, sys.GetValueDouble(env, args[0], &obj.value)) {
			return nil
		}
	}

	obj.env = env

	// La chiamata a napi_wrap deve chiedere il riferimento all'oggetto con il parametro di
	// uscita: così si verifica il caso di un riferimento distrutto dal proprio finalizer.
	h := cgo.NewHandle(obj)
	cb, hint := nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.Wrap(env, this, uintptr(h), cb, hint, &obj.wrapper)) {
		nt.DiscardFinalizer(hint)
		h.Delete()
		return nil
	}

	return this
}

// newInstance è MyObject::NewInstance.
func newInstance(env sys.Env, arg sys.Value, instance *sys.Value) sys.Status {
	const argc = 1
	argv := [argc]sys.Value{arg}

	var cons sys.Value
	status := sys.GetReferenceValue(env, constructor, &cons)
	if status != sys.StatusOK {
		return status
	}

	status = sys.NewInstance(env, cons, argc, &argv[0], instance)
	if status != sys.StatusOK {
		return status
	}

	return sys.StatusOK
}
