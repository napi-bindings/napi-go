// Porting di test/js-native-api/5_function_factory/5_function_factory.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// myFunctionData si registra una volta sola: CreateFunction crea una funzione nuova a ogni
// chiamata e il registro di napitest non deve crescere con loro.
var myFunctionData = nt.Register(myFunction)

func myFunction(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var str sys.Value
	// La lunghezza -1 del C è SIZE_MAX, cioè NAPI_AUTO_LENGTH.
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("hello world"), sys.AutoLength, &str)) {
		return nil
	}
	return str
}

func createFunction(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var fn sys.Value
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("theFunction"), sys.AutoLength, sys.CallbackTrampoline,
		myFunctionData, &fn)) {
		return nil
	}
	return fn
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("exports"), sys.AutoLength, sys.CallbackTrampoline,
		nt.Register(createFunction), &exports)) {
		return nil
	}
	return exports
}
