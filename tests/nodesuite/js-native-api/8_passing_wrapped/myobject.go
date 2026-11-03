// Port of test/js-native-api/8_passing_wrapped/myobject.cc and myobject.h. The C++ MyObject
// class becomes the myObject struct; the pointer passed to napi_wrap is a cgo.Handle,
// deleted by the Destructor like C++ delete.
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
	// finalizeCount is size_t finalize_count, exposed as uint32 as in C.
	finalizeCount uint32
	// constructor is MyObject::constructor.
	constructor sys.Ref
	// newData is the constructor data: registered only once.
	newData = nt.Register(newMyObject)
)

// val is MyObject::Val.
func (o *myObject) val() float64 { return o.value }

// release is equivalent to ~MyObject: status is not checked, matching C.
func (o *myObject) release() {
	finalizeCount++
	sys.DeleteReference(o.env, o.wrapper)
}

// destructor is MyObject::Destructor.
func destructor(_ sys.Env, nativeObject, _ uintptr) {
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*myObject)
	h.Delete()
	obj.release()
}

// unwrap is equivalent to napi_unwrap(env, value, (void**)&obj).
func unwrap(env sys.Env, value sys.Value) (*myObject, bool) {
	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, value, &data)) {
		return nil, false
	}
	return cgo.Handle(uintptr(data)).Value().(*myObject), true
}

// initClass is MyObject::Init.
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

// newMyObject is MyObject::New.
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

	// Retrieve the resulting reference to the object via the napi_wrap out parameter to test
	// the case where a reference is destroyed by its own finalizer.
	h := cgo.NewHandle(obj)
	cb, hint := nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.Wrap(env, this, uintptr(h), cb, hint, &obj.wrapper)) {
		nt.DiscardFinalizer(hint)
		h.Delete()
		return nil
	}

	return this
}

// newInstance is MyObject::NewInstance.
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
