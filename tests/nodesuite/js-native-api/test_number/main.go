// Porting di test/js-native-api/test_number/test_number.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// numberArg legge il primo argomento con le verifiche comuni alle funzioni del test C (stesse
// asserzioni, stesso ordine).
func numberArg(env sys.Env, info sys.CallbackInfo) (sys.Value, bool) {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil, false
	}

	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil, false
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil, false
	}

	if !nt.Assert(env, valuetype0 == sys.TypeNumber, "valuetype0 == napi_number",
		"Wrong type of arguments. Expects a number as first argument.") {
		return nil, false
	}
	return args[0], true
}

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := numberArg(env, info)
	if !ok {
		return nil
	}

	var input float64
	if !nt.Call(env, sys.GetValueDouble(env, arg, &input)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateDouble(env, input, &output)) {
		return nil
	}

	return output
}

func testUint32Truncation(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := numberArg(env, info)
	if !ok {
		return nil
	}

	var input uint32
	if !nt.Call(env, sys.GetValueUint32(env, arg, &input)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateUint32(env, input, &output)) {
		return nil
	}

	return output
}

func testInt32Truncation(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := numberArg(env, info)
	if !ok {
		return nil
	}

	var input int32
	if !nt.Call(env, sys.GetValueInt32(env, arg, &input)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateInt32(env, input, &output)) {
		return nil
	}

	return output
}

func testInt64Truncation(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := numberArg(env, info)
	if !ok {
		return nil
	}

	var input int64
	if !nt.Call(env, sys.GetValueInt64(env, arg, &input)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateInt64(env, input, &output)) {
		return nil
	}

	return output
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("Test", test),
		nt.DeclareProperty("TestInt32Truncation", testInt32Truncation),
		nt.DeclareProperty("TestUint32Truncation", testUint32Truncation),
		nt.DeclareProperty("TestInt64Truncation", testInt64Truncation),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	initTestNull(env, exports)

	return exports
}
