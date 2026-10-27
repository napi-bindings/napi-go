// Porting di test/js-native-api/test_string/test_null.c: la macro DECLARE_TEST diventa una
// funzione generica sul tipo di carattere.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

const invalidArgument = "Invalid argument"

// testStringValue è la stringa "something" del test C.
const testStringValue = "something"

// declareTest equivale a DECLARE_TEST(charset, str_arg). Lo status delle chiamate senza
// controllo è letto subito dopo da AddLastStatus, come nella macro C.
func declareTest[T char](create createAPI[T], strArg *T) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var returnValue, result sys.Value
		if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
			return nil
		}

		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			create(nil, strArg, sys.AutoLength, &result))

		create(env, nil, sys.AutoLength, &result)
		nt.AddLastStatus(env, "stringIsNullNonZeroLength", returnValue)

		create(env, nil, 0, &result)
		nt.AddLastStatus(env, "stringIsNullZeroLength", returnValue)

		create(env, strArg, sys.AutoLength, nil)
		nt.AddLastStatus(env, "resultIsNull", returnValue)

		return returnValue
	}
}

func initTestNull(env sys.Env, exports sys.Value) {
	var testNull sys.Value
	testNullProps := []sys.PropertyDescriptor{
		nt.DeclareProperty("test_create_utf8", declareTest(sys.CreateStringUTF8, nt.CStr(testStringValue))),
		nt.DeclareProperty("test_create_latin1", declareTest(sys.CreateStringLatin1, nt.CStr(testStringValue))),
		nt.DeclareProperty("test_create_utf16", declareTest(sys.CreateStringUTF16, nt.UTF16(testStringValue))),
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
