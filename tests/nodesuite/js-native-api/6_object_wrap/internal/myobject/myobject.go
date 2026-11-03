// Package myobject is the port of test/js-native-api/6_object_wrap/myobject.cc and myobject.h,
// shared by the myobject and myobject_basic_finalizer targets in binding.gyp: in C they are the
// same source compiled twice, the second time with NAPI_EXPERIMENTAL (Destructor becomes a
// "basic" finalizer that runs during GC).
//
// The C++ MyObject class becomes the myObject struct; the pointer passed to napi_wrap is a
// cgo.Handle, deleted by the Destructor like C++ delete.
package myobject

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

// finalizerData is FinalizerData (int32_t) from C: the Destructor counter stored in instance data.
type finalizerData = int32

const finalizerDataSize = uint(unsafe.Sizeof(finalizerData(0)))

type myObject struct {
	value   float64
	env     sys.Env
	wrapper sys.Ref
}

// constructor is MyObject::constructor; danglingRef is dangling_ref.
var (
	constructor sys.Ref
	danglingRef sys.Ref
)

// release is equivalent to ~MyObject: status is not checked, matching C.
func (o *myObject) release() {
	sys.DeleteReference(o.env, o.wrapper)
}

// unwrap is equivalent to napi_unwrap(env, this, (void**)&obj).
func unwrap(env sys.Env, this sys.Value) (*myObject, bool) {
	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, this, &data)) {
		return nil, false
	}
	return cgo.Handle(uintptr(data)).Value().(*myObject), true
}

// destructor is MyObject::Destructor.
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

// newData is the constructor data: registered only once.
var newData = nt.Register(newMyObject)

// newMyObject is MyObject::New.
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
		// Invoked as constructor: `new MyObject(...)`.
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

	// Invoked as plain function `MyObject(...)`: turn into a constructor call.
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

// objectWrapDanglingReferenceFinalizer must never be called.
func objectWrapDanglingReferenceFinalizer(sys.Env, uintptr, uintptr) {
	nt.CAssert(false, `0 && "unreachable"`)
}

func objectWrapDanglingReference(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	// Create a napi_wrap and remove it immediately, leaving the returned ref
	// dangling (not deleted).
	cb, hint := nt.Finalizer(objectWrapDanglingReferenceFinalizer, 0)
	if !nt.Call(env, sys.Wrap(env, args[0], 0, cb, hint, &danglingRef)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.RemoveWrap(env, args[0], nil)) {
		return nil
	}
	// After napi_remove_wrap Node-API will no longer invoke the finalizer.
	nt.DiscardFinalizer(hint)

	return args[0]
}

func objectWrapDanglingReferenceTest(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var out, ret sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, danglingRef, &out)) {
		return nil
	}

	if out == nil {
		// If the napi_ref has been invalidated, delete it.
		if !nt.Call(env, sys.DeleteReference(env, danglingRef)) {
			return nil
		}
		if !nt.Call(env, sys.GetBoolean(env, true, &ret)) {
			return nil
		}
	} else {
		// The dangling reference is still valid.
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

// Init is C's Init (EXTERN_C_START ... EXTERN_C_END).
func Init(env sys.Env, exports sys.Value) sys.Value {
	// C does not check the result of new; here a failed allocation throws an error instead
	// of writing to NULL.
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
