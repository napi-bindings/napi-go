// Porting di test/js-native-api/test_typedarray/test_typedarray_sharedarraybuffer.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func createTypedArray(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(4)
	var args [4]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 2 || argc == 4, "argc == 2 || argc == 4", "Wrong number of arguments") {
		return nil
	}

	var isTypedArray bool
	if !nt.Call(env, sys.IsTypedArray(env, args[0], &isTypedArray)) {
		return nil
	}
	if !nt.Assert(env, isTypedArray, "is_typedarray",
		"Wrong type of arguments. Expects a typed array as first argument.") {
		return nil
	}

	var typ sys.TypedArrayType
	var length, byteOffset uint
	if !nt.Call(env, sys.GetTypedArrayInfo(env, args[0], &typ, &length, nil, nil, &byteOffset)) {
		return nil
	}

	if argc == 4 {
		var uint32Length uint32
		if !nt.Call(env, sys.GetValueUint32(env, args[2], &uint32Length)) {
			return nil
		}
		length = uint(uint32Length)

		var uint32ByteOffset uint32
		if !nt.Call(env, sys.GetValueUint32(env, args[3], &uint32ByteOffset)) {
			return nil
		}
		byteOffset = uint(uint32ByteOffset)
	}

	var typedarray sys.Value
	if !nt.Call(env, sys.CreateTypedArray(env, typ, length, args[1], byteOffset, &typedarray)) {
		return nil
	}

	return typedarray
}

func getArrayBuffer(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}

	var arraybuffer sys.Value
	if !nt.Call(env, sys.GetTypedArrayInfo(env, args[0], nil, nil, nil, &arraybuffer, nil)) {
		return nil
	}

	return arraybuffer
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("CreateTypedArray", createTypedArray),
		nt.DeclareProperty("GetArrayBuffer", getArrayBuffer),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
