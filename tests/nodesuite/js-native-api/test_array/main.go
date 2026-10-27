// Porting di test/js-native-api/test_array/test_array.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func testGetElement(env sys.Env, info sys.CallbackInfo) sys.Value {
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

	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects an array as first argument.") {
		return nil
	}

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype1 == sys.TypeNumber, "valuetype1 == napi_number",
		"Wrong type of arguments. Expects an integer as second argument.") {
		return nil
	}

	array := args[0]
	var index int32
	if !nt.Call(env, sys.GetValueInt32(env, args[1], &index)) {
		return nil
	}

	if !nt.Assert(env, index >= 0, "index >= 0", "Invalid index. Expects a positive integer.") {
		return nil
	}

	var isarray bool
	if !nt.Call(env, sys.IsArray(env, array, &isarray)) {
		return nil
	}

	if !isarray {
		return nil
	}

	var length uint32
	if !nt.Call(env, sys.GetArrayLength(env, array, &length)) {
		return nil
	}

	if !nt.Assert(env, uint32(index) < length, "((uint32_t)index < length)", "Index out of bounds!") {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetElement(env, array, uint32(index), &ret)) {
		return nil
	}

	return ret
}

func testHasElement(env sys.Env, info sys.CallbackInfo) sys.Value {
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

	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects an array as first argument.") {
		return nil
	}

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype1 == sys.TypeNumber, "valuetype1 == napi_number",
		"Wrong type of arguments. Expects an integer as second argument.") {
		return nil
	}

	array := args[0]
	var index int32
	if !nt.Call(env, sys.GetValueInt32(env, args[1], &index)) {
		return nil
	}

	var isarray bool
	if !nt.Call(env, sys.IsArray(env, array, &isarray)) {
		return nil
	}

	if !isarray {
		return nil
	}

	var hasElement bool
	// Come in C, l'indice int32 passa a uint32_t senza controlli.
	if !nt.Call(env, sys.HasElement(env, array, uint32(index), &hasElement)) {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, hasElement, &ret)) {
		return nil
	}

	return ret
}

func testDeleteElement(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 2, "argc == 2", "Wrong number of arguments") {
		return nil
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil
	}
	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects an array as first argument.") {
		return nil
	}

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}
	if !nt.Assert(env, valuetype1 == sys.TypeNumber, "valuetype1 == napi_number",
		"Wrong type of arguments. Expects an integer as second argument.") {
		return nil
	}

	array := args[0]
	var index int32
	var result bool
	var ret sys.Value

	if !nt.Call(env, sys.GetValueInt32(env, args[1], &index)) {
		return nil
	}
	if !nt.Call(env, sys.IsArray(env, array, &result)) {
		return nil
	}

	if !result {
		return nil
	}

	if !nt.Call(env, sys.DeleteElement(env, array, uint32(index), &result)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, result, &ret)) {
		return nil
	}

	return ret
}

func newArray(env sys.Env, info sys.CallbackInfo) sys.Value {
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

	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects an array as first argument.") {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.CreateArray(env, &ret)) {
		return nil
	}

	var length uint32
	if !nt.Call(env, sys.GetArrayLength(env, args[0], &length)) {
		return nil
	}

	for i := uint32(0); i < length; i++ {
		var e sys.Value
		if !nt.Call(env, sys.GetElement(env, args[0], i, &e)) {
			return nil
		}
		if !nt.Call(env, sys.SetElement(env, ret, i, e)) {
			return nil
		}
	}

	return ret
}

func newWithLength(env sys.Env, info sys.CallbackInfo) sys.Value {
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
		"Wrong type of arguments. Expects an integer the first argument.") {
		return nil
	}

	var arrayLength int32
	if !nt.Call(env, sys.GetValueInt32(env, args[0], &arrayLength)) {
		return nil
	}

	var ret sys.Value
	// Come la conversione implicita int32_t → size_t del C: 4294967295 arriva come -1 e diventa
	// SIZE_MAX, che Node-API riduce alla lunghezza massima di un array.
	if !nt.Call(env, sys.CreateArrayWithLength(env, uint(arrayLength), &ret)) {
		return nil
	}

	return ret
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("TestGetElement", testGetElement),
		nt.DeclareProperty("TestHasElement", testHasElement),
		nt.DeclareProperty("TestDeleteElement", testDeleteElement),
		nt.DeclareProperty("New", newArray),
		nt.DeclareProperty("NewWithLength", newWithLength),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
