// Porting di test/js-native-api/test_constructor/test_null.c.
//
// Il data &some_data del C diventa il data di napitest della funzione TestConstructor: con
// sys.CallbackTrampoline il data identifica la funzione Go da chiamare.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

var testConstructorData uintptr

func init() {
	testConstructorData = nt.Register(testConstructor)
}

const testClass = "TestClass"

func testConstructor(sys.Env, sys.CallbackInfo) sys.Value {
	return nil
}

func testNullDefineClass(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var returnValue, cons sys.Value

	prop := sys.PropertyDescriptor{
		Utf8Name:   nt.CStr("testConstructor"),
		Method:     sys.CallbackTrampoline,
		Attributes: sys.PropertyDefault,
		Data:       testConstructorData,
	}

	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}

	// Lo status delle chiamate senza controllo è letto subito dopo da AddLastStatus, come nel C.
	nt.AddReturnedStatus(env, "envIsNull", returnValue, "Invalid argument", sys.StatusInvalidArg,
		sys.DefineClass(nil, nt.CStr(testClass), sys.AutoLength, sys.CallbackTrampoline, testConstructorData,
			1, &prop, &cons))

	sys.DefineClass(env, nil, sys.AutoLength, sys.CallbackTrampoline, testConstructorData, 1, &prop, &cons)
	nt.AddLastStatus(env, "nameIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(testClass), 0, sys.CallbackTrampoline, testConstructorData, 1, &prop, &cons)
	nt.AddLastStatus(env, "lengthIsZero", returnValue)

	sys.DefineClass(env, nt.CStr(testClass), sys.AutoLength, nil, testConstructorData, 1, &prop, &cons)
	nt.AddLastStatus(env, "nativeSideIsNull", returnValue)

	// data NULL: la classe non viene istanziata, quindi il trampolino non la usa mai.
	sys.DefineClass(env, nt.CStr(testClass), sys.AutoLength, sys.CallbackTrampoline, 0, 1, &prop, &cons)
	nt.AddLastStatus(env, "dataIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(testClass), sys.AutoLength, sys.CallbackTrampoline, testConstructorData,
		0, &prop, &cons)
	nt.AddLastStatus(env, "propsLengthIsZero", returnValue)

	sys.DefineClass(env, nt.CStr(testClass), sys.AutoLength, sys.CallbackTrampoline, testConstructorData,
		1, nil, &cons)
	nt.AddLastStatus(env, "propsIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(testClass), sys.AutoLength, sys.CallbackTrampoline, testConstructorData,
		1, &prop, nil)
	nt.AddLastStatus(env, "resultIsNull", returnValue)

	return returnValue
}

func initTestNull(env sys.Env, exports sys.Value) {
	var testNull sys.Value
	testNullProps := []sys.PropertyDescriptor{
		nt.DeclareProperty("testDefineClass", testNullDefineClass),
	}

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
