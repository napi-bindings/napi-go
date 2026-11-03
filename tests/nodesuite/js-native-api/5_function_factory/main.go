// Port of test/js-native-api/5_function_factory/5_function_factory.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// myFunctionData is registered only once: CreateFunction creates a new function on every
// call and the napitest registry should not grow with them.
var myFunctionData = nt.Register(myFunction)

func myFunction(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var str sys.Value
	// C's length -1 is SIZE_MAX, i.e. NAPI_AUTO_LENGTH.
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
