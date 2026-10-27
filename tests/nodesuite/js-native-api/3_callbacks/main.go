// Porting di test/js-native-api/3_callbacks/3_callbacks.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const helloWorld = "hello world"

func runCallback(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments. Expects a single argument.") {
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

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}
	if !nt.Assert(env, valuetype1 == sys.TypeUndefined, "valuetype1 == napi_undefined",
		"Additional arguments should be undefined.") {
		return nil
	}

	var argv [1]sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(helloWorld), uint(len(helloWorld)), &argv[0])) {
		return nil
	}

	var global sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return nil
	}

	cb := args[0]
	if !nt.Call(env, sys.CallFunction(env, global, cb, 1, &argv[0], nil)) {
		return nil
	}

	return nil
}

func runCallbackWithRecv(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	cb := args[0]
	recv := args[1]
	if !nt.Call(env, sys.CallFunction(env, recv, cb, 0, nil, nil)) {
		return nil
	}
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{
		nt.DeclareProperty("RunCallback", runCallback),
		nt.DeclareProperty("RunCallbackWithRecv", runCallbackWithRecv),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
