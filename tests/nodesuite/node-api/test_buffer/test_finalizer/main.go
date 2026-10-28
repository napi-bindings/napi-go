// Porting del target test_finalizer di test/node-api/test_buffer/binding.gyp (test_finalizer.c):
// il finalizer di un buffer esterno chiama JS e lancia un'eccezione.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	theText = "Lorem ipsum dolor sit amet, consectetur adipiscing elit."
	// theTextSize è sizeof(theText): include il NUL finale.
	theTextSize = uint(len(theText) + 1)
)

// theTextC è il const char theText[] del C: memoria C condivisa e mai liberata.
func theTextC() *byte { return nt.CStr(theText) }

// malignDeleter riceve come hint il napi_ref della funzione JS.
func malignDeleter(env sys.Env, data, finalizeHint uintptr) {
	if !nt.Assert(env, data != 0 && nt.GoString((*byte)(nt.Pointer(data))) == theText,
		"data != NULL && strcmp(data, theText) == 0", "invalid data") {
		return
	}
	finalizerRef := sys.Ref(nt.Pointer(finalizeHint))
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

func malignFinalizerBuffer(env sys.Env, info sys.CallbackInfo) sys.Value {
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
	var theBuffer sys.Value
	cb, hint := nt.Finalizer(malignDeleter, uintptr(unsafe.Pointer(finalizerRef)) /* finalize_hint */)
	if !nt.Call(env, sys.CreateExternalBuffer(env, theTextSize, uintptr(unsafe.Pointer(theTextC())), cb, hint,
		&theBuffer)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return theBuffer
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	methods := []sys.PropertyDescriptor{
		nt.DeclareProperty("malignFinalizerBuffer", malignFinalizerBuffer),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, methods)) {
		return nil
	}
	return exports
}
