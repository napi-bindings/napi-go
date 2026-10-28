// Porting di test/node-api/test_exception/test_exception.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// bufferData è static char buffer_data[12]: una variabile globale, che il GC di Go non sposta,
// può fare da memoria di un buffer esterno come nel C.
var bufferData [12]byte

func finalizer(env sys.Env, _, _ uintptr) {
	if !nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Error during Finalize"))) {
		return
	}
}

func createExternalBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var buffer sys.Value
	cb, hint := nt.Finalizer(finalizer, 0)
	if !nt.Call(env, sys.CreateExternalBuffer(env, uint(len(bufferData)), uintptr(unsafe.Pointer(&bufferData[0])),
		cb, hint, &buffer)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return buffer
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("createExternalBuffer", createExternalBuffer),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
