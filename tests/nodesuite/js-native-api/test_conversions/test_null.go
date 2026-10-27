// Porting di test/js-native-api/test_conversions/test_null.c: le macro GEN_NULL_CHECK_BINDING e
// GEN_NULL_CHECK_STRING_BINDING diventano due funzioni generiche.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

const invalidArgument = "Invalid argument"

// stringBufLen e stringLength sono arg_type buf[4] e size_t length = 3 della macro C.
const (
	stringBufLen = 4
	stringLength = 3
)

type getFn[T any] func(env sys.Env, value sys.Value, result *T) sys.Status

// genNullCheckBinding equivale a GEN_NULL_CHECK_BINDING: chiama api con env, value e result NULL
// a turno, poi con un oggetto al posto del tipo atteso. Lo status delle chiamate senza controllo
// è letto subito dopo da AddLastStatus, come nella macro C.
func genNullCheckBinding[T any](api getFn[T]) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var returnValue sys.Value
		var result T
		if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
			return nil
		}
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			api(nil, returnValue, &result))
		api(env, nil, &result)
		nt.AddLastStatus(env, "valueIsNull", returnValue)
		api(env, returnValue, nil)
		nt.AddLastStatus(env, "resultIsNull", returnValue)
		api(env, returnValue, &result)
		nt.AddLastStatus(env, "inputTypeCheck", returnValue)
		return returnValue
	}
}

type getStringFn[T byte | uint16] func(env sys.Env, value sys.Value, buf *T, bufsize uint, result *uint) sys.Status

// genNullCheckStringBinding equivale a GEN_NULL_CHECK_STRING_BINDING.
func genNullCheckStringBinding[T byte | uint16](api getStringFn[T]) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var returnValue sys.Value
		if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
			return nil
		}
		var buf1 [stringBufLen]T
		length1 := uint(stringLength)
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			api(nil, returnValue, &buf1[0], length1, &length1))

		var buf2 [stringBufLen]T
		length2 := uint(stringLength)
		api(env, nil, &buf2[0], length2, &length2)
		nt.AddLastStatus(env, "valueIsNull", returnValue)

		api(env, returnValue, nil, stringLength, nil)
		nt.AddLastStatus(env, "wrongTypeIn", returnValue)

		var str sys.Value
		if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("Something"), sys.AutoLength, &str)) {
			return nil
		}

		api(env, str, nil, stringLength, nil)
		nt.AddLastStatus(env, "bufAndOutLengthIsNull", returnValue)

		return returnValue
	}
}

func initTestNull(env sys.Env, exports sys.Value) {
	var testNull sys.Value
	testNullProps := []sys.PropertyDescriptor{
		nt.DeclareProperty("getValueBool", genNullCheckBinding(sys.GetValueBool)),
		nt.DeclareProperty("getValueInt32", genNullCheckBinding(sys.GetValueInt32)),
		nt.DeclareProperty("getValueUint32", genNullCheckBinding(sys.GetValueUint32)),
		nt.DeclareProperty("getValueInt64", genNullCheckBinding(sys.GetValueInt64)),
		nt.DeclareProperty("getValueDouble", genNullCheckBinding(sys.GetValueDouble)),
		nt.DeclareProperty("coerceToBool", genNullCheckBinding(sys.CoerceToBool)),
		nt.DeclareProperty("coerceToObject", genNullCheckBinding(sys.CoerceToObject)),
		nt.DeclareProperty("coerceToString", genNullCheckBinding(sys.CoerceToString)),
		nt.DeclareProperty("getValueStringUtf8", genNullCheckStringBinding(sys.GetValueStringUTF8)),
		nt.DeclareProperty("getValueStringLatin1", genNullCheckStringBinding(sys.GetValueStringLatin1)),
		nt.DeclareProperty("getValueStringUtf16", genNullCheckStringBinding(sys.GetValueStringUTF16)),
	}

	if !nt.Call(env, sys.CreateObject(env, &testNull)) {
		return
	}
	if !nt.Call(env, nt.DefineProperties(env, testNull, testNullProps)) {
		return
	}

	testNullSet := []sys.PropertyDescriptor{{
		Utf8Name:   nt.CStr("testNull"),
		Value:      testNull,
		Attributes: sys.PropertyEnumerable,
	}}
	if !nt.Call(env, nt.DefineProperties(env, exports, testNullSet)) {
		return
	}
}
