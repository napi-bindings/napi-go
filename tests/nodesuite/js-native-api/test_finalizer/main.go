// Port of test/js-native-api/test_finalizer/test_finalizer.c (NAPI_EXPERIMENTAL):
// "basic" finalizers run during GC, while those that touch JS are deferred via
// node_api_post_finalizer.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// finalizerData is C's FinalizerData struct, allocated in C memory as instance data.
type finalizerData struct {
	finalizeCount int32
	jsFunc        sys.Ref
}

const finalizerDataSize = uint(unsafe.Sizeof(finalizerData{}))

// instanceData is equivalent to napi_get_instance_data(env, (void**)&data).
func instanceData(env sys.Env, call func(env sys.Env, status sys.Status) bool) (*finalizerData, bool) {
	var data unsafe.Pointer
	if !call(env, sys.GetInstanceData(env, &data)) {
		return nil, false
	}
	return (*finalizerData)(data), true
}

func finalizerOnlyCallback(env sys.Env, finalizeData, _ uintptr) {
	data := (*finalizerData)(nt.Pointer(finalizeData))
	data.finalizeCount++
	count := data.finalizeCount

	// Instance data is accessible even during GC.
	data, ok := instanceData(env, nt.BasicCall)
	if !ok {
		return
	}
	// C uses an assignment (count = data->finalize_count) as a condition: it is true when the
	// count is non-zero. The port preserves the same check.
	count = data.finalizeCount
	if !nt.BasicAssert(count != 0, "count = data->finalize_count", "Expected to be the same FinalizerData") {
		return
	}
}

func finalizerCallingJSCallback(env sys.Env, finalizeData, _ uintptr) {
	var jsFunc, undefined sys.Value
	data := (*finalizerData)(nt.Pointer(finalizeData))
	if !nt.Call(env, sys.GetReferenceValue(env, data.jsFunc, &jsFunc)) {
		return
	}
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, undefined, jsFunc, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, data.jsFunc)) {
		return
	}
	data.jsFunc = nil
	data.finalizeCount++
}

// finalizerWithJSCallback defers code that touches JS until after GC.
func finalizerWithJSCallback(env sys.Env, finalizeData, finalizeHint uintptr) {
	cb, hint := nt.Finalizer(finalizerCallingJSCallback, finalizeHint)
	if !nt.BasicCall(env, nt.PostFinalizer(env, cb, finalizeData, hint)) {
		nt.DiscardFinalizer(hint)
		return
	}
}

// finalizerWithFailedJSCallback intentionally calls a JS-touching API during GC: Node.js
// terminates the process ("Finalizer is calling a function that may affect GC state").
func finalizerWithFailedJSCallback(env sys.Env, finalizeData, _ uintptr) {
	var obj sys.Value
	data := (*finalizerData)(nt.Pointer(finalizeData))
	data.finalizeCount++
	if !nt.Call(env, sys.CreateObject(env, &obj)) {
		return
	}
}

// addFinalizerWith corresponds to the shared part of addFinalizer, addFinalizerWithJS, and
// addFinalizerFailOnJS: napi_add_finalizer on argv[0] with instance data as data.
func addFinalizerWith(env sys.Env, object sys.Value, data *finalizerData, finalizer nt.FinalizeFunc) {
	cb, hint := nt.Finalizer(finalizer, 0)
	if !nt.Call(env, sys.AddFinalizer(env, object, uintptr(unsafe.Pointer(data)), cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return
	}
}

func addFinalizer(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	data, ok := instanceData(env, nt.Call)
	if !ok {
		return nil
	}
	addFinalizerWith(env, argv[0], data, finalizerOnlyCallback)
	return nil
}

// addFinalizerWithJS adds a finalizer that calls JS and succeeds.
func addFinalizerWithJS(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	var argType sys.ValueType

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	data, ok := instanceData(env, nt.Call)
	if !ok {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, argv[1], &argType)) {
		return nil
	}
	if !nt.Assert(env, argType == sys.TypeFunction, "arg_type == napi_function",
		"Expected function as the second arg") {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[1], 1, &data.jsFunc)) {
		return nil
	}
	addFinalizerWith(env, argv[0], data, finalizerWithJSCallback)
	return nil
}

// addFinalizerFailOnJS adds a finalizer that calls JS and fails.
func addFinalizerFailOnJS(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	data, ok := instanceData(env, nt.Call)
	if !ok {
		return nil
	}
	addFinalizerWith(env, argv[0], data, finalizerWithFailedJSCallback)
	return nil
}

func getFinalizerCallCount(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	var result sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	data, ok := instanceData(env, nt.Call)
	if !ok {
		return nil
	}
	if !nt.Call(env, sys.CreateInt32(env, data.finalizeCount, &result)) {
		return nil
	}
	return result
}

func finalizeData(_ sys.Env, data, _ uintptr) {
	nt.FreeAddr(data)
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	data := (*finalizerData)(nt.Malloc(finalizerDataSize))
	if !nt.Assert(env, data != nil, "data != NULL", "Failed to allocate memory") {
		return nil
	}
	*data = finalizerData{} // memset(data, 0, sizeof(FinalizerData))
	cb, hint := nt.Finalizer(finalizeData, 0)
	if !nt.Call(env, sys.SetInstanceData(env, uintptr(unsafe.Pointer(data)), cb, hint)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("addFinalizer", addFinalizer),
		nt.DeclareProperty("addFinalizerWithJS", addFinalizerWithJS),
		nt.DeclareProperty("addFinalizerFailOnJS", addFinalizerFailOnJS),
		nt.DeclareProperty("getFinalizerCallCount", getFinalizerCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
