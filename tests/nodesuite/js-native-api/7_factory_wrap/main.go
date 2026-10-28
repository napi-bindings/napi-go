// Porting di test/js-native-api/7_factory_wrap/7_factory_wrap.cc.
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

	var instance sys.Value
	if !nt.Call(env, newInstance(env, args[0], &instance)) {
		return nil
	}

	return instance
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	if !nt.Call(env, initClass(env)) {
		return nil
	}

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareGetter("finalizeCount", getFinalizeCount),
		nt.DeclareProperty("createObject", createObject),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
