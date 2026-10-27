// Porting di test/js-native-api/4_object_factory/4_object_factory.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func createObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var obj sys.Value
	if !nt.Call(env, sys.CreateObject(env, &obj)) {
		return nil
	}

	if !nt.Call(env, sys.SetNamedProperty(env, obj, nt.CStr("msg"), args[0])) {
		return nil
	}

	return obj
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	// La lunghezza -1 del C è SIZE_MAX, cioè NAPI_AUTO_LENGTH.
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("exports"), sys.AutoLength, sys.CallbackTrampoline,
		nt.Register(createObject), &exports)) {
		return nil
	}
	return exports
}
