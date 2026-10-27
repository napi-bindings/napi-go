// Porting di test/js-native-api/test_object/test_null.c.
//
// In tutte le funzioni lo status delle chiamate senza controllo è letto subito dopo da
// AddLastStatus, come nel C.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

var nullDefinePropertiesData uintptr

func init() {
	nullDefinePropertiesData = nt.Register(nullDefineProperties)
}

const (
	invalidArgument = "Invalid argument"
	someString      = "someString"
	keyName         = "key"
)

// newObjects crea return_value e object, come all'inizio di quasi tutte le funzioni del C.
func newObjects(env sys.Env) (returnValue, object sys.Value, ok bool) {
	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil, nil, false
	}
	if !nt.Call(env, sys.CreateObject(env, &object)) {
		return nil, nil, false
	}
	return returnValue, object, true
}

// newObjectsAndKey aggiunge la chiave "someString".
func newObjectsAndKey(env sys.Env) (returnValue, object, key sys.Value, ok bool) {
	returnValue, object, ok = newObjects(env)
	if !ok {
		return nil, nil, nil, false
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(someString), sys.AutoLength, &key)) {
		return nil, nil, nil, false
	}
	return returnValue, object, key, true
}

func nullSetProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, key, ok := newObjectsAndKey(env)
	if !ok {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.SetProperty(nil, object, key, object))
	sys.SetProperty(env, nil, key, object)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.SetProperty(env, object, nil, object)
	nt.AddLastStatus(env, "keyIsNull", returnValue)
	sys.SetProperty(env, object, key, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullGetProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, key, ok := newObjectsAndKey(env)
	if !ok {
		return nil
	}
	var prop sys.Value
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetProperty(nil, object, key, &prop))
	sys.GetProperty(env, nil, key, &prop)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetProperty(env, object, nil, &prop)
	nt.AddLastStatus(env, "keyIsNull", returnValue)
	sys.GetProperty(env, object, key, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

type boolValuedPropAPI func(env sys.Env, object, key sys.Value, result *bool) sys.Status

// testBoolValuedPropAPI equivale a TestBoolValuedPropApi.
func testBoolValuedPropAPI(api boolValuedPropAPI) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		returnValue, object, key, ok := newObjectsAndKey(env)
		if !ok {
			return nil
		}
		var result bool
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			api(nil, object, key, &result))
		api(env, nil, key, &result)
		nt.AddLastStatus(env, "objectIsNull", returnValue)
		api(env, object, nil, &result)
		nt.AddLastStatus(env, "keyIsNull", returnValue)
		api(env, object, key, nil)
		nt.AddLastStatus(env, "valueIsNull", returnValue)
		return returnValue
	}
}

func nullSetNamedProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, ok := newObjects(env)
	if !ok {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.SetNamedProperty(nil, object, nt.CStr(keyName), object))
	sys.SetNamedProperty(env, nil, nt.CStr(keyName), object)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.SetNamedProperty(env, object, nil, object)
	nt.AddLastStatus(env, "keyIsNull", returnValue)
	sys.SetNamedProperty(env, object, nt.CStr(keyName), nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullGetNamedProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, ok := newObjects(env)
	if !ok {
		return nil
	}
	var prop sys.Value
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetNamedProperty(nil, object, nt.CStr(keyName), &prop))
	sys.GetNamedProperty(env, nil, nt.CStr(keyName), &prop)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetNamedProperty(env, object, nil, &prop)
	nt.AddLastStatus(env, "keyIsNull", returnValue)
	sys.GetNamedProperty(env, object, nt.CStr(keyName), nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullHasNamedProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, ok := newObjects(env)
	if !ok {
		return nil
	}
	var result bool
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.HasNamedProperty(nil, object, nt.CStr(keyName), &result))
	sys.HasNamedProperty(env, nil, nt.CStr(keyName), &result)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.HasNamedProperty(env, object, nil, &result)
	nt.AddLastStatus(env, "keyIsNull", returnValue)
	sys.HasNamedProperty(env, object, nt.CStr(keyName), nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullSetElement(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, ok := newObjects(env)
	if !ok {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.SetElement(nil, object, 0, object))
	sys.SetElement(env, nil, 0, object)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	// Come nel C: napi_set_property con chiave 0 (NULL), non napi_set_element.
	sys.SetProperty(env, object, nil, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullGetElement(env sys.Env, _ sys.CallbackInfo) sys.Value {
	returnValue, object, ok := newObjects(env)
	if !ok {
		return nil
	}
	var prop sys.Value
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetElement(nil, object, 0, &prop))
	// Come nel C: napi_get_property con chiave 0 (NULL), non napi_get_element.
	sys.GetProperty(env, nil, nil, &prop)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetProperty(env, object, nil, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

type boolValuedElementAPI func(env sys.Env, object sys.Value, index uint32, result *bool) sys.Status

// testBoolValuedElementAPI equivale a TestBoolValuedElementApi.
func testBoolValuedElementAPI(api boolValuedElementAPI) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		returnValue, object, ok := newObjects(env)
		if !ok {
			return nil
		}
		var result bool
		nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
			api(nil, object, 0, &result))
		api(env, nil, 0, &result)
		nt.AddLastStatus(env, "objectIsNull", returnValue)
		api(env, object, 0, nil)
		nt.AddLastStatus(env, "valueIsNull", returnValue)
		return returnValue
	}
}

func nullDefineProperties(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var object, returnValue sys.Value
	desc := sys.PropertyDescriptor{
		Utf8Name:   nt.CStr("prop"),
		Method:     sys.CallbackTrampoline,
		Attributes: sys.PropertyEnumerable,
		Data:       nullDefinePropertiesData,
	}

	if !nt.Call(env, sys.CreateObject(env, &object)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}

	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.DefineProperties(nil, object, 1, &desc))

	sys.DefineProperties(env, nil, 1, &desc)
	nt.AddLastStatus(env, "objectIsNull", returnValue)

	sys.DefineProperties(env, object, 1, nil)
	nt.AddLastStatus(env, "descriptorListIsNull", returnValue)

	// Come nel C le varianti seguenti modificano desc ma passano comunque NULL come lista.
	desc.Utf8Name = nil
	sys.DefineProperties(env, object, 1, nil)
	nt.AddLastStatus(env, "utf8nameIsNull", returnValue)
	desc.Utf8Name = nt.CStr("prop")

	desc.Method = nil
	sys.DefineProperties(env, object, 1, nil)
	nt.AddLastStatus(env, "methodIsNull", returnValue)
	desc.Method = sys.CallbackTrampoline

	return returnValue
}

func nullGetPropertyNames(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var returnValue, props sys.Value
	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetPropertyNames(nil, returnValue, &props))
	sys.GetPropertyNames(env, nil, &props)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetPropertyNames(env, returnValue, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullGetAllPropertyNames(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var returnValue, props sys.Value
	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetAllPropertyNames(nil, returnValue, sys.KeyOwnOnly, sys.KeyWritable, sys.KeyKeepNumbers, &props))
	sys.GetAllPropertyNames(env, nil, sys.KeyOwnOnly, sys.KeyWritable, sys.KeyKeepNumbers, &props)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetAllPropertyNames(env, returnValue, sys.KeyOwnOnly, sys.KeyWritable, sys.KeyKeepNumbers, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func nullGetPrototype(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var returnValue, proto sys.Value
	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}
	nt.AddReturnedStatus(env, "envIsNull", returnValue, invalidArgument, sys.StatusInvalidArg,
		sys.GetPrototype(nil, returnValue, &proto))
	sys.GetPrototype(env, nil, &proto)
	nt.AddLastStatus(env, "objectIsNull", returnValue)
	sys.GetPrototype(env, returnValue, nil)
	nt.AddLastStatus(env, "valueIsNull", returnValue)
	return returnValue
}

func initTestNull(env sys.Env, exports sys.Value) {
	var testNull sys.Value
	testNullProps := []sys.PropertyDescriptor{
		nt.DeclareProperty("setProperty", nullSetProperty),
		nt.DeclareProperty("getProperty", nullGetProperty),
		nt.DeclareProperty("hasProperty", testBoolValuedPropAPI(sys.HasProperty)),
		nt.DeclareProperty("hasOwnProperty", testBoolValuedPropAPI(sys.HasOwnProperty)),
		nt.DeclareProperty("deleteProperty", testBoolValuedPropAPI(sys.DeleteProperty)),
		nt.DeclareProperty("setNamedProperty", nullSetNamedProperty),
		nt.DeclareProperty("getNamedProperty", nullGetNamedProperty),
		nt.DeclareProperty("hasNamedProperty", nullHasNamedProperty),
		nt.DeclareProperty("setElement", nullSetElement),
		nt.DeclareProperty("getElement", nullGetElement),
		nt.DeclareProperty("hasElement", testBoolValuedElementAPI(sys.HasElement)),
		nt.DeclareProperty("deleteElement", testBoolValuedElementAPI(sys.DeleteElement)),
		{
			Utf8Name:   nt.CStr("defineProperties"),
			Method:     sys.CallbackTrampoline,
			Attributes: sys.PropertyDefault,
			Data:       nullDefinePropertiesData,
		},
		nt.DeclareProperty("getPropertyNames", nullGetPropertyNames),
		nt.DeclareProperty("getAllPropertyNames", nullGetAllPropertyNames),
		nt.DeclareProperty("getPrototype", nullGetPrototype),
	}

	if !nt.Call(env, sys.CreateObject(env, &testNull)) {
		return
	}
	if !nt.Call(env, nt.DefineProperties(env, testNull, testNullProps)) {
		return
	}

	testNullSet := []sys.PropertyDescriptor{nt.Value("testNull", testNull, sys.PropertyEnumerable)}
	if !nt.Call(env, nt.DefineProperties(env, exports, testNullSet)) {
		return
	}
}
