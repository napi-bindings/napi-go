// Porting di test/js-native-api/test_number/test_null.c: le macro BINDING_FOR_CREATE e
// BINDING_FOR_GET_VALUE diventano due funzioni generiche sul tipo numerico.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

type number interface {
	~float64 | ~int32 | ~uint32 | ~int64
}

// testValue è il valore usato dalle macro C (lowercase##_t value = 42).
const testValue = 42

const invalidArgument = "Invalid argument"

type createFn[T number] func(env sys.Env, value T, result *sys.Value) sys.Status

type getValueFn[T number] func(env sys.Env, value sys.Value, result *T) sys.Status

// bindingForCreate equivale a BINDING_FOR_CREATE: chiama napi_create_<tipo> con env NULL e con
// result NULL. Lo status delle chiamate senza controllo è letto subito dopo da AddLastStatus.
func bindingForCreate[T number](create createFn[T]) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var returnValue, callResult sys.Value
		value := T(testValue)
		if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
			return nil
		}
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			create(nil, value, &callResult))
		create(env, value, nil)
		nt.AddLastStatus(env, "resultIsNull", returnValue)
		return returnValue
	}
}

// bindingForGetValue equivale a BINDING_FOR_GET_VALUE: chiama napi_get_value_<tipo> con env,
// value e result NULL a turno.
func bindingForGetValue[T number](create createFn[T], get getValueFn[T]) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var returnValue, callResult sys.Value
		value := T(testValue)
		if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
			return nil
		}
		if !nt.Call(env, create(env, value, &callResult)) {
			return nil
		}
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			get(nil, callResult, &value))
		get(env, nil, &value)
		nt.AddLastStatus(env, "valueIsNull", returnValue)
		get(env, callResult, nil)
		nt.AddLastStatus(env, "resultIsNull", returnValue)
		return returnValue
	}
}

func initTestNull(env sys.Env, exports sys.Value) {
	testNullProps := []sys.PropertyDescriptor{
		nt.DeclareProperty("createDouble", bindingForCreate(sys.CreateDouble)),
		nt.DeclareProperty("createInt32", bindingForCreate(sys.CreateInt32)),
		nt.DeclareProperty("createUint32", bindingForCreate(sys.CreateUint32)),
		nt.DeclareProperty("createInt64", bindingForCreate(sys.CreateInt64)),
		nt.DeclareProperty("getValueDouble", bindingForGetValue(sys.CreateDouble, sys.GetValueDouble)),
		nt.DeclareProperty("getValueInt32", bindingForGetValue(sys.CreateInt32, sys.GetValueInt32)),
		nt.DeclareProperty("getValueUint32", bindingForGetValue(sys.CreateUint32, sys.GetValueUint32)),
		nt.DeclareProperty("getValueInt64", bindingForGetValue(sys.CreateInt64, sys.GetValueInt64)),
	}
	var testNull sys.Value

	if !nt.Call(env, sys.CreateObject(env, &testNull)) {
		return
	}
	if !nt.Call(env, nt.DefineProperties(env, testNull, testNullProps)) {
		return
	}
	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("testNull"), testNull)) {
		return
	}
}
