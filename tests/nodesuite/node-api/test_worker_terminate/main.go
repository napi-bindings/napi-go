// Porting di test/node-api/test_worker_terminate/test_worker_terminate.c. Il C usa NAPI_MODULE()
// per verificare i worker con la registrazione classica: in napi-go il modulo si registra
// sempre con napi_register_module_v1, chiamata per ogni env.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var recv sys.Value
	var argv [1]sys.Value
	var status sys.Status

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], &recv, nil)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Not enough arguments, expected 1.") {
		return nil
	}
	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argv[0], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeFunction, "t == napi_function", "Wrong first argument, function expected.") {
		return nil
	}

	status = sys.CallFunction(env, recv, argv[0], 0, nil, nil)
	nt.CAssert(status == sys.StatusPendingException, "status == napi_pending_exception")

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
