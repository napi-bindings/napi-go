// Porting di test/js-native-api/test_date/test_date.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func createDate(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil
	}

	if !nt.Assert(env, valuetype0 == sys.TypeNumber, "valuetype0 == napi_number",
		"Wrong type of arguments. Expects a number as first argument.") {
		return nil
	}

	var time float64
	if !nt.Call(env, sys.GetValueDouble(env, args[0], &time)) {
		return nil
	}

	var date sys.Value
	if !nt.Call(env, sys.CreateDate(env, time, &date)) {
		return nil
	}

	return date
}

func isDate(env sys.Env, info sys.CallbackInfo) sys.Value {
	var date, result sys.Value
	argc := uint(1)
	var isDate bool
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &date, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.IsDate(env, date, &isDate)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, isDate, &result)) {
		return nil
	}

	return result
}

func getDateValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	var date, result sys.Value
	argc := uint(1)
	var value float64
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &date, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.GetDateValue(env, date, &value)) {
		return nil
	}
	if !nt.Call(env, sys.CreateDouble(env, value, &result)) {
		return nil
	}

	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("createDate", createDate),
		nt.DeclareProperty("isDate", isDate),
		nt.DeclareProperty("getDateValue", getDateValue),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
