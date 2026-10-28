// Porting di test/node-api/test_fatal_exception/test_fatal_exception.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	var err sys.Value
	argc := uint(1)

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &err, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.FatalException(env, err)) {
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
