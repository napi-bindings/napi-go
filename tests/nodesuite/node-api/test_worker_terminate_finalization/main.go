// Porting di test/node-api/test_worker_terminate_finalization/test_worker_terminate_finalization.c
// (vedi test_worker_terminate per NAPI_MODULE()).
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const bufferSize = 4

var (
	// wrappedNativeData è int wrappedNativeData: una variabile globale, il cui indirizzo fa da
	// puntatore nativo del wrap.
	wrappedNativeData int32
	ref               sys.Ref
)

func wrapFinalizer(env sys.Env, _, _ uintptr) {
	var count uint32
	if !nt.Call(env, sys.ReferenceUnref(env, ref, &count)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, ref)) {
		return
	}
}

// bufferFinalizer libera la memoria del buffer, che il C passa come hint.
func bufferFinalizer(_ sys.Env, _, hint uintptr) {
	nt.FreeAddr(hint)
}

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	var result sys.Value
	bufferData := uintptr(nt.Malloc(bufferSize))

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	cb, hint := nt.Finalizer(bufferFinalizer, bufferData)
	if !nt.Call(env, sys.CreateExternalBuffer(env, bufferSize, bufferData, cb, hint, &result)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, result, 1, &ref)) {
		return nil
	}
	cb, hint = nt.Finalizer(wrapFinalizer, 0)
	if !nt.Call(env, sys.Wrap(env, argv[0], uintptr(unsafe.Pointer(&wrappedNativeData)), cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("Test", test),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
