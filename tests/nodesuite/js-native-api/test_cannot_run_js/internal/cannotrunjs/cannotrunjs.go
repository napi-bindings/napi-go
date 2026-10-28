// Package cannotrunjs è il porting di test/js-native-api/test_cannot_run_js/test_cannot_run_js.c,
// condiviso dai target test_cannot_run_js (NAPI_VERSION=10) e test_pending_exception
// (NAPI_VERSION=9) del binding.gyp. Il ramo #if NAPI_VERSION > 9 del C dipende da
// sys.DeclaredVersion, fissata dal build tag del target.
package cannotrunjs

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

// cannotRunJSVersion è la prima versione Node-API in cui le chiamate che eseguono JS durante lo
// spegnimento dell'ambiente restituiscono napi_cannot_run_js invece di napi_pending_exception.
const cannotRunJSVersion = 10

func finalize(env sys.Env, data, _ uintptr) {
	var global, setTimeout sys.Value
	ref := (*sys.Ref)(nt.Pointer(data))
	if !nt.BasicAssert(sys.DeleteReference(env, *ref) == sys.StatusOK,
		"napi_delete_reference(env, *ref) == napi_ok",
		"deleting reference in finalizer should succeed") {
		return
	}
	if !nt.BasicAssert(sys.GetGlobal(env, &global) == sys.StatusOK,
		"napi_get_global(env, &global) == napi_ok",
		"getting global reference in finalizer should succeed") {
		return
	}
	result := sys.GetNamedProperty(env, global, nt.CStr("setTimeout"), &setTimeout)

	// Il finalizer può girare dalle check callback (come native immediate) se l'event loop è
	// ancora attivo, e allora restituisce napi_ok, oppure durante lo spegnimento dell'ambiente,
	// dove restituisce napi_cannot_run_js o napi_pending_exception. Per l'addon non è
	// deterministico.
	if sys.DeclaredVersion >= cannotRunJSVersion {
		if !nt.BasicAssert(result == sys.StatusCannotRunJS || result == sys.StatusOK,
			"result == napi_cannot_run_js || result == napi_ok",
			"getting named property from global in finalizer should succeed "+
				"or return napi_cannot_run_js") {
			return
		}
	} else {
		if !nt.BasicAssert(result == sys.StatusPendingException || result == sys.StatusOK,
			"result == napi_pending_exception || result == napi_ok",
			"getting named property from global in finalizer should succeed "+
				"or return napi_pending_exception") {
			return
		}
	}
	nt.FreeAddr(data)
}

func createRef(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var cb sys.Value
	var valueType sys.ValueType
	ref := (*sys.Ref)(nt.Malloc(refSize))

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &cb, nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Function takes only one argument") {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, cb, &valueType)) {
		return nil
	}
	if !nt.Assert(env, valueType == sys.TypeFunction, "value_type == napi_function", "argument must be function") {
		return nil
	}
	fcb, hint := nt.Finalizer(finalize, 0)
	if !nt.Call(env, sys.AddFinalizer(env, cb, uintptr(unsafe.Pointer(ref)), fcb, hint, ref)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return cb
}

// Init è Init del C (EXTERN_C_START … EXTERN_C_END).
func Init(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("createRef", createRef),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
