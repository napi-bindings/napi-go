// Porting di test/js-native-api/2_function_arguments/2_function_arguments.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func add(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc >= 2, "argc >= 2", "Wrong number of arguments") {
		return nil
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil
	}

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype0 == sys.TypeNumber && valuetype1 == sys.TypeNumber,
		"valuetype0 == napi_number && valuetype1 == napi_number",
		"Wrong argument type. Numbers expected.") {
		return nil
	}

	var value0 float64
	if !nt.Call(env, sys.GetValueDouble(env, args[0], &value0)) {
		return nil
	}

	var value1 float64
	if !nt.Call(env, sys.GetValueDouble(env, args[1], &value1)) {
		return nil
	}

	var sum sys.Value
	if !nt.Call(env, sys.CreateDouble(env, value0+value1, &sum)) {
		return nil
	}

	return sum
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{nt.DeclareProperty("add", add)}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
