// Porting di test/js-native-api/test_conversions/test_conversions.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// stringBufferSize è la dimensione di char value[100] in AsString.
const stringBufferSize = 100

// firstArg legge il primo argomento, come napi_get_cb_info con argc = 1 nel test C.
func firstArg(env sys.Env, info sys.CallbackInfo) (sys.Value, bool) {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil, false
	}
	return args[0], true
}

func asBool(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value bool
	if !nt.Call(env, sys.GetValueBool(env, arg, &value)) {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.GetBoolean(env, value, &output)) {
		return nil
	}
	return output
}

func asInt32(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value int32
	if !nt.Call(env, sys.GetValueInt32(env, arg, &value)) {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.CreateInt32(env, value, &output)) {
		return nil
	}
	return output
}

func asUInt32(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value uint32
	if !nt.Call(env, sys.GetValueUint32(env, arg, &value)) {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.CreateUint32(env, value, &output)) {
		return nil
	}
	return output
}

func asInt64(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value int64
	if !nt.Call(env, sys.GetValueInt64(env, arg, &value)) {
		return nil
	}
	var output sys.Value
	// Come nel C: napi_create_int64(env, (double)value, …) passa per un double.
	if !nt.Call(env, sys.CreateInt64(env, int64(float64(value)), &output)) {
		return nil
	}
	return output
}

func asDouble(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value float64
	if !nt.Call(env, sys.GetValueDouble(env, arg, &value)) {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.CreateDouble(env, value, &output)) {
		return nil
	}
	return output
}

func asString(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := firstArg(env, info)
	if !ok {
		return nil
	}
	var value [stringBufferSize]byte
	if !nt.Call(env, sys.GetValueStringUTF8(env, arg, &value[0], uint(len(value)), nil)) {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, &value[0], sys.AutoLength, &output)) {
		return nil
	}
	return output
}

// coerceFn equivale a napi_coerce_to_bool, napi_coerce_to_number, …
type coerceFn func(env sys.Env, value sys.Value, result *sys.Value) sys.Status

// coercer restituisce la funzione JS che applica coerce al primo argomento.
func coercer(coerce coerceFn) nt.Func {
	return func(env sys.Env, info sys.CallbackInfo) sys.Value {
		arg, ok := firstArg(env, info)
		if !ok {
			return nil
		}
		var output sys.Value
		if !nt.Call(env, coerce(env, arg, &output)) {
			return nil
		}
		return output
	}
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("asBool", asBool),
		nt.DeclareProperty("asInt32", asInt32),
		nt.DeclareProperty("asUInt32", asUInt32),
		nt.DeclareProperty("asInt64", asInt64),
		nt.DeclareProperty("asDouble", asDouble),
		nt.DeclareProperty("asString", asString),
		nt.DeclareProperty("toBool", coercer(sys.CoerceToBool)),
		nt.DeclareProperty("toNumber", coercer(sys.CoerceToNumber)),
		nt.DeclareProperty("toObject", coercer(sys.CoerceToObject)),
		nt.DeclareProperty("toString", coercer(sys.CoerceToString)),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	initTestNull(env, exports)

	return exports
}
