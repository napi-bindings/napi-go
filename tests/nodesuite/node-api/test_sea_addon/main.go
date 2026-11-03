// Port of test/node-api/test_sea_addon/binding.c: the script packages the addon as an asset of a
// Single Executable Application and loads it with process.dlopen() from the generated executable.
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
	// Explicit length matching strlen(str) in the C test.
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(world), uint(len(world)), &result)) {
		return nil
	}
	return result
}

// initModule corresponds to the body of NAPI_MODULE_INIT().
func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{nt.DeclareProperty("hello", method)}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
