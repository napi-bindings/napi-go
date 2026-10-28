// Porting di test/js-native-api/test_reference/test_reference.c (NAPI_VERSION 9, vedi
// napi-go.json).
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// testValue è static int test_value: una variabile globale, che il GC di Go non sposta, quindi
// il suo indirizzo può fare da data di un external come nel C.
var (
	testValue     int32 = 1
	finalizeCount int32
	testReference sys.Ref
)

// descriptionSize è sizeof(description) di CreateSymbolFor.
const (
	descriptionSize      = 256
	maxDescriptionLength = descriptionSize - 1
	// incorrectLength è la lunghezza passata con una descrizione NULL.
	incorrectLength = 5
)

func testValueAddr() unsafe.Pointer { return unsafe.Pointer(&testValue) }

func getFinalizeCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.CreateInt32(env, finalizeCount, &result)) {
		return nil
	}
	return result
}

func finalizeExternal(env sys.Env, data, _ uintptr) {
	actualValue := nt.Pointer(data)
	if !nt.Assert(env, actualValue == testValueAddr(), "actual_value == &test_value",
		"The correct pointer was passed to the finalizer") {
		return
	}
	finalizeCount++
}

func createExternal(env sys.Env, _ sys.CallbackInfo) sys.Value {
	data := testValueAddr()
	var result sys.Value

	if !nt.Call(env, sys.CreateExternal(env, uintptr(data), nil, 0, &result)) {
		return nil
	}

	finalizeCount = 0
	return result
}

func createSymbol(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Expect one argument only (symbol description)") {
		return nil
	}

	var resultSymbol sys.Value
	if !nt.Call(env, sys.CreateSymbol(env, args[0], &resultSymbol)) {
		return nil
	}
	return resultSymbol
}

func createSymbolFor(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	var description [descriptionSize]byte
	var descriptionLength uint

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Expect one argument only (symbol description)") {
		return nil
	}

	if !nt.Call(env, sys.GetValueStringUTF8(env, args[0], &description[0], descriptionSize,
		&descriptionLength)) {
		return nil
	}
	if !nt.Assert(env, descriptionLength <= maxDescriptionLength, "description_length <= 255",
		"Cannot accommodate descriptions longer than 255 bytes") {
		return nil
	}

	var resultSymbol sys.Value
	if !nt.Call(env, sys.SymbolFor(env, &description[0], descriptionLength, &resultSymbol)) {
		return nil
	}
	return resultSymbol
}

func createSymbolForEmptyString(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var resultSymbol sys.Value
	if !nt.Call(env, sys.SymbolFor(env, nil, 0, &resultSymbol)) {
		return nil
	}
	return resultSymbol
}

func createSymbolForIncorrectLength(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var resultSymbol sys.Value
	if !nt.Call(env, sys.SymbolFor(env, nil, incorrectLength, &resultSymbol)) {
		return nil
	}
	return resultSymbol
}

func createExternalWithFinalize(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	cb, hint := nt.Finalizer(finalizeExternal, 0)
	if !nt.Call(env, sys.CreateExternal(env, uintptr(testValueAddr()), cb, hint, &result)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	finalizeCount = 0
	return result
}

func checkExternal(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var arg sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &arg, nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 1, "argc == 1", "Expected one argument.") {
		return nil
	}

	var argtype sys.ValueType
	if !nt.Call(env, sys.Typeof(env, arg, &argtype)) {
		return nil
	}

	if !nt.Assert(env, argtype == sys.TypeExternal, "argtype == napi_external", "Expected an external value.") {
		return nil
	}

	var data unsafe.Pointer
	if !nt.Call(env, sys.GetValueExternal(env, arg, &data)) {
		return nil
	}

	if !nt.Assert(env, data != nil && *(*int32)(data) == testValue, "data != NULL && *(int*)data == test_value",
		"An external data value of 1 was expected.") {
		return nil
	}

	return nil
}

func createReference(env sys.Env, info sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, testReference == nil, "test_reference == NULL",
		"The test allows only one reference at a time.") {
		return nil
	}

	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 2, "argc == 2", "Expected two arguments.") {
		return nil
	}

	var initialRefcount uint32
	if !nt.Call(env, sys.GetValueUint32(env, args[1], &initialRefcount)) {
		return nil
	}

	if !nt.Call(env, sys.CreateReference(env, args[0], initialRefcount, &testReference)) {
		return nil
	}

	if !nt.Assert(env, testReference != nil, "test_reference != NULL",
		"A reference should have been created.") {
		return nil
	}

	return nil
}

func deleteReference(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, testReference != nil, "test_reference != NULL",
		"A reference must have been created.") {
		return nil
	}

	if !nt.Call(env, sys.DeleteReference(env, testReference)) {
		return nil
	}
	testReference = nil
	return nil
}

func incrementRefcount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, testReference != nil, "test_reference != NULL",
		"A reference must have been created.") {
		return nil
	}

	var refcount uint32
	if !nt.Call(env, sys.ReferenceRef(env, testReference, &refcount)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.CreateUint32(env, refcount, &result)) {
		return nil
	}
	return result
}

func decrementRefcount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, testReference != nil, "test_reference != NULL",
		"A reference must have been created.") {
		return nil
	}

	var refcount uint32
	if !nt.Call(env, sys.ReferenceUnref(env, testReference, &refcount)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.CreateUint32(env, refcount, &result)) {
		return nil
	}
	return result
}

func getReferenceValue(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, testReference != nil, "test_reference != NULL",
		"A reference must have been created.") {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, testReference, &result)) {
		return nil
	}
	return result
}

// deleteBeforeFinalizeFinalizer: le assert() del C restano attive (nt.CAssert).
func deleteBeforeFinalizeFinalizer(env sys.Env, finalizeData, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(finalizeData))
	var value sys.Value
	nt.CAssert(sys.GetReferenceValue(env, *ref, &value) == sys.StatusOK,
		"napi_get_reference_value(env, *ref, &value) == napi_ok")
	nt.CAssert(value == nil, "value == NULL")
	// Lo status non è controllato, come nel C.
	sys.DeleteReference(env, *ref)
	nt.FreeAddr(finalizeData)
}

func validateDeleteBeforeFinalize(env sys.Env, info sys.CallbackInfo) sys.Value {
	var wrapObject sys.Value
	argc := uint(1)
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &wrapObject, nil, nil)) {
		return nil
	}

	refT := (*sys.Ref)(nt.Malloc(uint(unsafe.Sizeof(sys.Ref(nil)))))
	cb, hint := nt.Finalizer(deleteBeforeFinalizeFinalizer, 0)
	if !nt.Call(env, sys.Wrap(env, wrapObject, uintptr(unsafe.Pointer(refT)), cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	// Crea un riferimento che diventa raccoglibile insieme all'oggetto avvolto, passando lo
	// stesso wrapObject: il finalizer del wrap può quindi girare prima di quello (interno) del
	// riferimento, e chiama napi_delete_reference, verificando che sia ammesso prima che il
	// finalizer del riferimento sia eseguito.
	if !nt.Call(env, sys.CreateReference(env, wrapObject, 0, refT)) {
		return nil
	}
	return wrapObject
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareGetter("finalizeCount", getFinalizeCount),
		nt.DeclareProperty("createExternal", createExternal),
		nt.DeclareProperty("createExternalWithFinalize", createExternalWithFinalize),
		nt.DeclareProperty("checkExternal", checkExternal),
		nt.DeclareProperty("createReference", createReference),
		nt.DeclareProperty("createSymbol", createSymbol),
		nt.DeclareProperty("createSymbolFor", createSymbolFor),
		nt.DeclareProperty("createSymbolForEmptyString", createSymbolForEmptyString),
		nt.DeclareProperty("createSymbolForIncorrectLength", createSymbolForIncorrectLength),
		nt.DeclareProperty("deleteReference", deleteReference),
		nt.DeclareProperty("incrementRefcount", incrementRefcount),
		nt.DeclareProperty("decrementRefcount", decrementRefcount),
		nt.DeclareGetter("referenceValue", getReferenceValue),
		nt.DeclareProperty("validateDeleteBeforeFinalize", validateDeleteBeforeFinalize),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
