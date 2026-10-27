// Porting di test/js-native-api/test_symbol/test_symbol.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func newSymbol(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var description sys.Value
	if argc >= 1 {
		var valuetype sys.ValueType
		if !nt.Call(env, sys.Typeof(env, args[0], &valuetype)) {
			return nil
		}
		if !nt.Assert(env, valuetype == sys.TypeString, "valuetype == napi_string",
			"Wrong type of arguments. Expects a string.") {
			return nil
		}
		description = args[0]
	}

	var symbol sys.Value
	if !nt.Call(env, sys.CreateSymbol(env, description, &symbol)) {
		return nil
	}
	return symbol
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("New", newSymbol),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
