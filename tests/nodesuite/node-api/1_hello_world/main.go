// Porting di test/node-api/1_hello_world/binding.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const world = "world"

func method(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	// Lunghezza esplicita come strlen(str) nel test C.
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(world), uint(len(world)), &result)) {
		return nil
	}
	return result
}

// initModule equivale al corpo di NAPI_MODULE_INIT(): gira per ogni env, anche nei worker.
func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{nt.DeclareProperty("hello", method)}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
