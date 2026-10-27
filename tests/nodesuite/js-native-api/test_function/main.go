// Porting di test/js-native-api/test_function/test_function.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

// Dati delle funzioni create più volte: registrati una volta sola in init() (registrarli in una
// dichiarazione var creerebbe un ciclo di inizializzazione con la funzione stessa).
var (
	testCreateFunctionParametersData uintptr
	testFunctionNameData             uintptr
)

func init() {
	testCreateFunctionParametersData = nt.Register(testCreateFunctionParameters)
	testFunctionNameData = nt.Register(testFunctionName)
	nt.ModuleInit(initModule)
}

// maxArgs è la capacità di napi_value args[10] in TestCallFunction.
const maxArgs = 10

const trackedFunction = "TrackedFunction"

func testCreateFunctionParameters(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result, returnValue sys.Value

	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}

	// Lo status delle chiamate senza controllo è letto subito dopo da AddLastStatus, come nel C.
	status := sys.CreateFunction(nil, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		testCreateFunctionParametersData, &result)

	nt.AddReturnedStatus(env, "envIsNull", returnValue, "Invalid argument", sys.StatusInvalidArg, status)

	sys.CreateFunction(env, nil, sys.AutoLength, sys.CallbackTrampoline, testCreateFunctionParametersData, &result)

	nt.AddLastStatus(env, "nameIsNull", returnValue)

	sys.CreateFunction(env, nt.CStr(trackedFunction), sys.AutoLength, nil, 0, &result)

	nt.AddLastStatus(env, "cbIsNull", returnValue)

	sys.CreateFunction(env, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		testCreateFunctionParametersData, nil)

	nt.AddLastStatus(env, "resultIsNull", returnValue)

	return returnValue
}

func testCallFunction(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(maxArgs)
	var args [maxArgs]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc > 0, "argc > 0", "Wrong number of arguments") {
		return nil
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil
	}

	if !nt.Assert(env, valuetype0 == sys.TypeFunction, "valuetype0 == napi_function",
		"Wrong type of arguments. Expects a function as first argument.") {
		return nil
	}

	// argv = args + 1: con argc == 1 punta comunque dentro args, come in C.
	argv := &args[1]
	argc--

	var global sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.CallFunction(env, global, args[0], argc, argv, &result)) {
		return nil
	}

	return result
}

func testFunctionName(sys.Env, sys.CallbackInfo) sys.Value {
	return nil
}

// finalizeFunction riceve come data il napi_ref passato a napi_wrap.
func finalizeFunction(env sys.Env, data, _ uintptr) {
	ref := sys.Ref(nt.Pointer(data))

	// Valore undefined di JavaScript.
	var undefined sys.Value
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}

	// La funzione JavaScript da chiamare.
	var jsFunction sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, ref, &jsFunction)) {
		return
	}

	// La si chiama per segnalare che la funzione generata sta per essere raccolta.
	if !nt.Call(env, sys.CallFunction(env, undefined, jsFunction, 0, nil, nil)) {
		return
	}

	// Si distrugge il riferimento persistente alla funzione appena chiamata.
	if !nt.Call(env, sys.DeleteReference(env, ref)) {
		return
	}
}

func makeTrackedFunction(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var jsFinalizeCb sys.Value
	var argType sys.ValueType

	// Funzione che segnala a JavaScript la raccolta della funzione creata qui sotto.
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &jsFinalizeCb, nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, jsFinalizeCb, &argType)) {
		return nil
	}
	if !nt.Assert(env, argType == sys.TypeFunction, "arg_type == napi_function", "Argument must be a function") {
		return nil
	}

	// Funzione creata dinamicamente.
	var result sys.Value
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		testFunctionNameData, &result)) {
		return nil
	}

	// Riferimento forte alla funzione da chiamare quando quella creata viene raccolta.
	var jsFinalizeCbRef sys.Ref
	if !nt.Call(env, sys.CreateReference(env, jsFinalizeCb, 1, &jsFinalizeCbRef)) {
		return nil
	}

	// Finalizer sulla funzione creata, con il riferimento come data.
	cb, hint := nt.Finalizer(finalizeFunction, 0)
	if !nt.Call(env, sys.Wrap(env, result, uintptr(unsafe.Pointer(jsFinalizeCbRef)), cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	return result
}

func testBadReturnExceptionPending(env sys.Env, _ sys.CallbackInfo) sys.Value {
	nt.Call(env, sys.ThrowError(env, nt.CStr("throwing exception"), nt.CStr("throwing exception")))
	// Un addon dovrebbe restituire sempre un napi_value valido, ma con un'eccezione pendente Node
	// deve ignorare il valore restituito: 0xFFFFFFFFF non è mai un napi_value valido.
	return nt.InvalidValue()
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var fn1, fn2, fn3, fn4, fn5, fn6 sys.Value
	if !nt.Call(env, sys.CreateFunction(env, nil, sys.AutoLength, sys.CallbackTrampoline,
		nt.Register(testCallFunction), &fn1)) {
		return nil
	}
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("Name"), sys.AutoLength, sys.CallbackTrampoline,
		testFunctionNameData, &fn2)) {
		return nil
	}
	// Lunghezza 5: il nome diventa "Name_".
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("Name_extra"), 5, sys.CallbackTrampoline,
		testFunctionNameData, &fn3)) {
		return nil
	}
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("MakeTrackedFunction"), sys.AutoLength,
		sys.CallbackTrampoline, nt.Register(makeTrackedFunction), &fn4)) {
		return nil
	}
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("TestCreateFunctionParameters"), sys.AutoLength,
		sys.CallbackTrampoline, testCreateFunctionParametersData, &fn5)) {
		return nil
	}
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("TestBadReturnExceptionPending"), sys.AutoLength,
		sys.CallbackTrampoline, nt.Register(testBadReturnExceptionPending), &fn6)) {
		return nil
	}

	exportsByName := []struct {
		name  string
		value sys.Value
	}{
		{"TestCall", fn1},
		{"TestName", fn2},
		{"TestNameShort", fn3},
		{"MakeTrackedFunction", fn4},
		{"TestCreateFunctionParameters", fn5},
		{"TestBadReturnExceptionPending", fn6},
	}
	for _, e := range exportsByName {
		if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr(e.name), e.value)) {
			return nil
		}
	}

	return exports
}
