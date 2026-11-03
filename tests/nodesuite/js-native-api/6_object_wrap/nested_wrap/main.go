// Port of test/js-native-api/6_object_wrap/nested_wrap.cc and nested_wrap.h (target nested_wrap,
// NAPI_VERSION=10): a napi_ref nested inside an ObjectWrap, deleted by the finalizer while its
// own finalizer is already scheduled to run.
package main

import (
	"runtime/cgo"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// nestedWrap is the C++ NestedWrap class; the pointer passed to Node-API is a cgo.Handle.
type nestedWrap struct {
	env     sys.Env
	wrapper sys.Ref
	nested  sys.Ref
}

var (
	// constructor is NestedWrap::constructor.
	constructor       sys.Ref
	finalizationCount int32
	// newData is the constructor data: registered only once.
	newData = nt.Register(newNestedWrap)
)

// release is equivalent to ~NestedWrap: it also deletes the nested reference. Statuses are not
// checked, matching C.
func (o *nestedWrap) release() {
	sys.DeleteReference(o.env, o.wrapper)
	sys.DeleteReference(o.env, o.nested)
}

// destructor is NestedWrap::Destructor. Once called, deleting the references cancels all
// pending finalizers on the object: their nt.Finalizer will never be called and remains
// allocated, just as the C test accepts.
func destructor(_ sys.Env, nativeObject, _ uintptr) {
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*nestedWrap)
	h.Delete()
	obj.release()
	finalizationCount++
}

func initClass(env sys.Env, exports sys.Value) {
	var cons sys.Value
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("NestedWrap"), sys.AutoLength, sys.CallbackTrampoline,
		newData, 0, nil, &cons)) {
		return
	}

	if !nt.Call(env, sys.CreateReference(env, cons, 1, &constructor)) {
		return
	}

	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("NestedWrap"), cons)) {
		return
	}
}

// newNestedWrap is NestedWrap::New.
func newNestedWrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	var newTarget sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTarget)) {
		return nil
	}
	isConstructor := newTarget != nil
	if !nt.BasicAssert(isConstructor, "is_constructor", "Constructor called without new") {
		return nil
	}

	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}

	obj := &nestedWrap{env: env}
	h := cgo.NewHandle(obj)
	cb, hint := nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.Wrap(env, this, uintptr(h), cb, hint, &obj.wrapper)) {
		nt.DiscardFinalizer(hint)
		h.Delete()
		return nil
	}

	// A second napi_ref to be deleted in the destructor.
	cb, hint = nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.AddFinalizer(env, this, uintptr(h), cb, hint, &obj.nested)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	return this
}

func getFinalizerCallCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.CreateInt32(env, finalizationCount, &result)) {
		return nil
	}
	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	initClass(env, exports)

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("getFinalizerCallCount", getFinalizerCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
