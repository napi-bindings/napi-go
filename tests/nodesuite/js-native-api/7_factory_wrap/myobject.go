// Port of test/js-native-api/7_factory_wrap/myobject.cc and myobject.h. The C++ MyObject class
// becomes the myObject struct; the pointer passed to napi_wrap is a cgo.Handle, deleted by the
// Destructor like C++ delete.
package main

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

type myObject struct {
	counter uint32
	env     sys.Env
	wrapper sys.Ref
}

var (
	finalizeCount int32
	// constructor is MyObject::constructor.
	constructor sys.Ref
	// newData is the constructor data: registered only once.
	newData = nt.Register(newMyObject)
)

// release is equivalent to ~MyObject: status is not checked, matching C.
func (o *myObject) release() {
	sys.DeleteReference(o.env, o.wrapper)
}

// destructor is MyObject::Destructor.
func destructor(_ sys.Env, nativeObject, _ uintptr) {
	finalizeCount++
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*myObject)
	h.Delete()
	obj.release()
}

func getFinalizeCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.CreateInt32(env, finalizeCount, &result)) {
		return nil
	}
	return result
}

// initClass is MyObject::Init.
func initClass(env sys.Env) sys.Status {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("plusOne", plusOne),
	}

	var cons sys.Value
	status := sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline, newData,
		uint(len(properties)), &properties[0], &cons)
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

	var valuetype sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype)) {
		return nil
	}

	obj := &myObject{}
	if valuetype == sys.TypeUndefined {
		obj.counter = 0
	} else {
		if !nt.Call(env, sys.GetValueUint32(env, args[0], &obj.counter)) {
			return nil
		}
	}

	obj.env = env
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

func plusOne(env sys.Env, info sys.CallbackInfo) sys.Value {
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}

	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, this, &data)) {
		return nil
	}
	obj := cgo.Handle(uintptr(data)).Value().(*myObject)

	obj.counter += 1

	var num sys.Value
	if !nt.Call(env, sys.CreateUint32(env, obj.counter, &num)) {
		return nil
	}

	return num
}
