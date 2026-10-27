// Porting di test/js-native-api/test_general/test_general.c (NAPI_EXPERIMENTAL dal binding.gyp).
//
// node_api_set_prototype e node_api_post_finalizer sono sperimentali e mancano dagli header di
// sys: si chiamano tramite napitest.SetPrototype e napitest.PostFinalizer.
package main

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func testStrictEquals(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var boolResult bool
	var result sys.Value
	if !nt.Call(env, sys.StrictEquals(env, args[0], args[1], &boolResult)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, boolResult, &result)) {
		return nil
	}
	return result
}

func testSetPrototype(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Call(env, nt.SetPrototype(env, args[0], args[1])) {
		return nil
	}
	return nil
}

func testGetPrototype(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	var result sys.Value
	if !nt.Call(env, sys.GetPrototype(env, args[0], &result)) {
		return nil
	}
	return result
}

func testGetVersion(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var version uint32
	var result sys.Value
	if !nt.Call(env, sys.GetVersion(env, &version)) {
		return nil
	}
	if !nt.Call(env, sys.CreateUint32(env, version, &result)) {
		return nil
	}
	return result
}

func doInstanceOf(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var instanceOf bool
	if !nt.Call(env, sys.Instanceof(env, args[0], args[1], &instanceOf)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.GetBoolean(env, instanceOf, &result)) {
		return nil
	}
	return result
}

func getNull(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.GetNull(env, &result)) {
		return nil
	}
	return result
}

func getUndefined(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.GetUndefined(env, &result)) {
		return nil
	}
	return result
}

func createNapiError(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var value sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("xyz"), 3, &value)) {
		return nil
	}

	var doubleValue float64
	status := sys.GetValueDouble(env, value, &doubleValue)

	if !nt.Assert(env, status != sys.StatusOK, "status != napi_ok", "Failed to produce error condition") {
		return nil
	}

	var errorInfo *sys.ExtendedErrorInfo
	if !nt.Call(env, sys.GetLastErrorInfo(env, &errorInfo)) {
		return nil
	}

	if !nt.Assert(env, errorInfo.ErrorCode == status, "error_info->error_code == status",
		"Last error info code should match last status") {
		return nil
	}
	if !nt.Assert(env, errorInfo.ErrorMessage != nil, "error_info->error_message",
		"Last error info message should not be null") {
		return nil
	}

	return nil
}

func testNapiErrorCleanup(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var errorInfo *sys.ExtendedErrorInfo
	if !nt.Call(env, sys.GetLastErrorInfo(env, &errorInfo)) {
		return nil
	}

	var result sys.Value
	isOK := errorInfo.ErrorCode == sys.StatusOK
	if !nt.Call(env, sys.GetBoolean(env, isOK, &result)) {
		return nil
	}
	return result
}

// typeNames sono i nomi restituiti da testNapiTypeof; per gli altri tipi restituisce NULL.
var typeNames = map[sys.ValueType]string{
	sys.TypeNumber:    "number",
	sys.TypeString:    "string",
	sys.TypeFunction:  "function",
	sys.TypeObject:    "object",
	sys.TypeBoolean:   "boolean",
	sys.TypeUndefined: "undefined",
	sys.TypeSymbol:    "symbol",
	sys.TypeNull:      "null",
}

func testNapiTypeof(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var argumentType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &argumentType)) {
		return nil
	}

	var result sys.Value
	if name, ok := typeNames[argumentType]; ok {
		if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(name), sys.AutoLength, &result)) {
			return nil
		}
	}
	return result
}

var derefItemCalled bool

// derefItemAddr è &deref_item_called, il data atteso dal finalizer.
func derefItemAddr() uintptr { return uintptr(unsafe.Pointer(&derefItemCalled)) }

func derefItem(env sys.Env, data, _ uintptr) {
	if !nt.Assert(env, data == derefItemAddr(), "data == &deref_item_called",
		"Finalize callback was called with the correct pointer") {
		return
	}
	derefItemCalled = true
}

func derefItemWasCalled(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var itWasCalled sys.Value
	if !nt.Call(env, sys.GetBoolean(env, derefItemCalled, &itWasCalled)) {
		return nil
	}
	return itWasCalled
}

// wrapFirstArg equivale a wrap_first_arg: fa napi_wrap del primo argomento con finalizer e data.
func wrapFirstArg(env sys.Env, info sys.CallbackInfo, finalizer nt.FinalizeFunc, data uintptr) sys.Value {
	argc := uint(1)
	var toWrap sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &toWrap, nil, nil)) {
		return nil
	}
	cb, hint := nt.Finalizer(finalizer, 0)
	if !nt.Call(env, sys.Wrap(env, toWrap, data, cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	return toWrap
}

func wrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	derefItemCalled = false
	return wrapFirstArg(env, info, derefItem, derefItemAddr())
}

func unwrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var wrapped sys.Value
	var data unsafe.Pointer

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &wrapped, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.Unwrap(env, wrapped, &data)) {
		return nil
	}
	return nil
}

func removeWrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var wrapped sys.Value
	var data unsafe.Pointer

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &wrapped, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.RemoveWrap(env, wrapped, &data)) {
		return nil
	}
	return nil
}

var finalizeCalled bool

func testFinalize(sys.Env, uintptr, uintptr) {
	finalizeCalled = true
}

func testFinalizeWrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	return wrapFirstArg(env, info, testFinalize, 0)
}

func finalizeWasCalled(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var itWasCalled sys.Value
	if !nt.Call(env, sys.GetBoolean(env, finalizeCalled, &itWasCalled)) {
		return nil
	}
	return itWasCalled
}

func testAdjustExternalMemory(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	var adjustedValue int64

	if !nt.Call(env, sys.AdjustExternalMemory(env, 1, &adjustedValue)) {
		return nil
	}
	if !nt.Call(env, sys.CreateDouble(env, float64(adjustedValue), &result)) {
		return nil
	}
	return result
}

func testNapiRun(env sys.Env, info sys.CallbackInfo) sys.Value {
	var script, result sys.Value
	argc := uint(1)

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &script, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.RunScript(env, script, &result)) {
		return nil
	}
	return result
}

// finalizerOnlyCallback gira dopo il GC (node_api_post_finalizer) e può chiamare JS. data è il
// napi_ref del callback JS.
func finalizerOnlyCallback(env sys.Env, data, _ uintptr) {
	jsCbRef := sys.Ref(nt.Pointer(data))
	var jsCb, undefined sys.Value

	if !nt.Call(env, sys.GetReferenceValue(env, jsCbRef, &jsCb)) {
		return
	}
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, undefined, jsCb, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, jsCbRef)) {
		return
	}
}

// scheduleFinalizerOnlyCallback gira durante il GC e rimanda finalizerOnlyCallback.
func scheduleFinalizerOnlyCallback(env sys.Env, data, _ uintptr) {
	cb, hint := nt.Finalizer(finalizerOnlyCallback, 0)
	if !nt.Call(env, nt.PostFinalizer(env, cb, data, hint)) {
		nt.DiscardFinalizer(hint)
		return
	}
}

func addFinalizerOnly(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	var jsCbRef sys.Ref

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[1], 1, &jsCbRef)) {
		return nil
	}
	cb, hint := nt.Finalizer(scheduleFinalizerOnlyCallback, 0)
	if !nt.Call(env, sys.AddFinalizer(env, argv[0], uintptr(unsafe.Pointer(jsCbRef)), cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return nil
}

// envCleanupFinalizerMessages è env_cleanup_finalizer_messages del C.
var envCleanupFinalizerMessages = [...]string{
	"simple wrap",
	"wrap, removeWrap",
	"first wrap",
	"second wrap",
}

// cleanupEnvFinalizer scrive su stdout, come il printf del C: lo script controlla le righe.
func cleanupEnvFinalizer(_ sys.Env, data, _ uintptr) {
	if data >= uintptr(len(envCleanupFinalizerMessages)) {
		fmt.Fprintf(os.Stderr, "napitest: invalid finalizer message index %d\n", data)
		return
	}
	if _, err := fmt.Fprintf(os.Stdout, "finalize at env cleanup for %s\n", envCleanupFinalizerMessages[data]); err != nil {
		fmt.Fprintf(os.Stderr, "napitest: cannot write to stdout: %v\n", err)
	}
}

func envCleanupWrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	var value uint32

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.GetValueUint32(env, argv[1], &value)) {
		return nil
	}

	return wrapFirstArg(env, info, cleanupEnvFinalizer, uintptr(value))
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("testStrictEquals", testStrictEquals),
		nt.DeclareProperty("testSetPrototype", testSetPrototype),
		nt.DeclareProperty("testGetPrototype", testGetPrototype),
		nt.DeclareProperty("testGetVersion", testGetVersion),
		nt.DeclareProperty("testNapiRun", testNapiRun),
		nt.DeclareProperty("doInstanceOf", doInstanceOf),
		nt.DeclareProperty("getUndefined", getUndefined),
		nt.DeclareProperty("getNull", getNull),
		nt.DeclareProperty("createNapiError", createNapiError),
		nt.DeclareProperty("testNapiErrorCleanup", testNapiErrorCleanup),
		nt.DeclareProperty("testNapiTypeof", testNapiTypeof),
		nt.DeclareProperty("wrap", wrap),
		nt.DeclareProperty("envCleanupWrap", envCleanupWrap),
		nt.DeclareProperty("unwrap", unwrap),
		nt.DeclareProperty("removeWrap", removeWrap),
		nt.DeclareProperty("addFinalizerOnly", addFinalizerOnly),
		nt.DeclareProperty("testFinalizeWrap", testFinalizeWrap),
		nt.DeclareProperty("finalizeWasCalled", finalizeWasCalled),
		nt.DeclareProperty("derefItemWasCalled", derefItemWasCalled),
		nt.DeclareProperty("testAdjustExternalMemory", testAdjustExternalMemory),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
