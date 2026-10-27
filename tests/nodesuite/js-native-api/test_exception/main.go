// Porting di test/js-native-api/test_exception/test_exception.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

var exceptionWasPending bool

// num è il dato dell'External; come nel C se ne passa solo l'indirizzo (una variabile globale
// non viene mai spostata dal GC).
var num int32 = 0x23432

func returnException(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var global sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return nil
	}

	var result sys.Value
	status := sys.CallFunction(env, global, args[0], 0, nil, &result)
	if status == sys.StatusPendingException {
		var ex sys.Value
		if !nt.Call(env, sys.GetAndClearLastException(env, &ex)) {
			return nil
		}
		return ex
	}

	return nil
}

func constructReturnException(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var result sys.Value
	status := sys.NewInstance(env, args[0], 0, nil, &result)
	if status == sys.StatusPendingException {
		var ex sys.Value
		if !nt.Call(env, sys.GetAndClearLastException(env, &ex)) {
			return nil
		}
		return ex
	}

	return nil
}

func allowException(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var global sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return nil
	}

	var result sys.Value
	// Lo status viene ignorato di proposito: il test controlla napi_is_exception_pending().
	_ = sys.CallFunction(env, global, args[0], 0, nil, &result)
	nt.Call(env, sys.IsExceptionPending(env, &exceptionWasPending))
	return nil
}

func constructAllowException(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var result sys.Value
	// Lo status viene ignorato di proposito: il test controlla napi_is_exception_pending().
	_ = sys.NewInstance(env, args[0], 0, nil, &result)
	nt.Call(env, sys.IsExceptionPending(env, &exceptionWasPending))
	return nil
}

func wasPending(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.GetBoolean(env, exceptionWasPending, &result)) {
		return nil
	}
	return result
}

func finalizer(env sys.Env, _, _ uintptr) {
	nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Error during Finalize")))
}

func createExternal(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var external sys.Value
	cb, hint := nt.Finalizer(finalizer, 0)
	if !nt.Call(env, sys.CreateExternal(env, uintptr(unsafe.Pointer(&num)), cb, hint, &external)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return external
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("returnException", returnException),
		nt.DeclareProperty("allowException", allowException),
		nt.DeclareProperty("constructReturnException", constructReturnException),
		nt.DeclareProperty("constructAllowException", constructAllowException),
		nt.DeclareProperty("wasPending", wasPending),
		nt.DeclareProperty("createExternal", createExternal),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	// Init lancia un errore che porta con sé le export nella proprietà "binding".
	var err, code, message sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("Error during Init"), sys.AutoLength, &message)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(""), sys.AutoLength, &code)) {
		return nil
	}
	if !nt.Call(env, sys.CreateError(env, code, message, &err)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, err, nt.CStr("binding"), exports)) {
		return nil
	}
	if !nt.Call(env, sys.Throw(env, err)) {
		return nil
	}
	return exports
}
