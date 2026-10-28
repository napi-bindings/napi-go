// Porting di test/js-native-api/test_reference/test_finalizer.c: un finalizer che chiama JS e
// lancia un'eccezione, consegnata a uncaughtException.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// testValue è static int test_value (vedi test_reference); finalizeCount è azzerato come nel C
// anche se nessuno lo legge.
var (
	testValue     int32 = 1
	finalizeCount int32
)

func testValueAddr() unsafe.Pointer { return unsafe.Pointer(&testValue) }

// finalizeExternalCallJs riceve come hint il napi_ref della funzione JS.
func finalizeExternalCallJs(env sys.Env, data, hint uintptr) {
	actualValue := nt.Pointer(data)
	if !nt.Assert(env, actualValue == testValueAddr(), "actual_value == &test_value",
		"The correct pointer was passed to the finalizer") {
		return
	}

	finalizerRef := sys.Ref(nt.Pointer(hint))
	var jsFinalizer, recv sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, finalizerRef, &jsFinalizer)) {
		return
	}
	if !nt.Call(env, sys.GetGlobal(env, &recv)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, recv, jsFinalizer, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, finalizerRef)) {
		return
	}
}

func createExternalWithJsFinalize(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	finalizer := args[0]
	var finalizerValuetype sys.ValueType
	if !nt.Call(env, sys.Typeof(env, finalizer, &finalizerValuetype)) {
		return nil
	}
	if !nt.Assert(env, finalizerValuetype == sys.TypeFunction, "finalizer_valuetype == napi_function",
		"Wrong type of first argument") {
		return nil
	}
	var finalizerRef sys.Ref
	if !nt.Call(env, sys.CreateReference(env, finalizer, 1, &finalizerRef)) {
		return nil
	}

	var result sys.Value
	cb, hint := nt.Finalizer(finalizeExternalCallJs, uintptr(unsafe.Pointer(finalizerRef)))
	if !nt.Call(env, sys.CreateExternal(env, uintptr(testValueAddr()), cb, hint, &result)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	finalizeCount = 0
	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("createExternalWithJsFinalize", createExternalWithJsFinalize),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
