// Package uncaught è il porting di test/node-api/test_threadsafe_function/test_uncaught_exception.c,
// condiviso dai target test_uncaught_exception_v9 (NAPI_VERSION=9) e test_uncaught_exception
// (NAPI_EXPERIMENTAL) del binding.gyp.
package uncaught

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

// threadSafeFunctionFinalize riceve come finalize_data il napi_ref della funzione JS.
func threadSafeFunctionFinalize(env sys.Env, finalizeData, _ uintptr) {
	jsFuncRef := sys.Ref(nt.Pointer(finalizeData))
	var jsFunc, recv sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, jsFuncRef, &jsFunc)) {
		return
	}
	if !nt.Call(env, sys.GetGlobal(env, &recv)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, recv, jsFunc, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, jsFuncRef)) {
		return
	}
}

// callIntoModule verifica la chiamata a JavaScript.
func callIntoModule(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(4)
	var argv [4]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}

	var finalizeFunc sys.Ref
	if !nt.Call(env, sys.CreateReference(env, argv[3], 1, &finalizeFunc)) {
		return nil
	}

	var tsfn sys.ThreadsafeFunction
	callJS, finalize, context := nt.ThreadsafeFunction(nil, threadSafeFunctionFinalize, 0)
	if !nt.Call(env, sys.CreateThreadsafeFunction(env, argv[0], argv[1], argv[2], 0, 1,
		uintptr(unsafe.Pointer(finalizeFunc)), finalize, context, callJS, &tsfn)) {
		nt.DiscardThreadsafeFunction(context)
		return nil
	}
	if !nt.Call(env, sys.CallThreadsafeFunction(tsfn, 0, sys.TSFNBlocking)) {
		return nil
	}
	if !nt.Call(env, sys.ReleaseThreadsafeFunction(tsfn, sys.TSFNRelease)) {
		return nil
	}
	return nil
}

// Init è Init del C.
func Init(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("CallIntoModule", callIntoModule),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
