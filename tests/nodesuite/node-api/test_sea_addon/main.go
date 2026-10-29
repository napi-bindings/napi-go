// Porting di test/node-api/test_sea_addon/binding.c: lo script impacchetta l'addon come asset di
// una Single Executable Application e lo carica con process.dlopen() dall'eseguibile generato.
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

// initModule equivale al corpo di NAPI_MODULE_INIT().
func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{nt.DeclareProperty("hello", method)}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
