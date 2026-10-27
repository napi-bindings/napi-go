// Porting di test/js-native-api/test_typedarray/test_typedarray.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func multiply(env sys.Env, info sys.CallbackInfo) sys.Value {
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
		"Wrong type of arguments. Expects a typed array as first argument.") {
		return nil
	}

	inputArray := args[0]
	var isTypedArray bool
	if !nt.Call(env, sys.IsTypedArray(env, inputArray, &isTypedArray)) {
		return nil
	}

	if !nt.Assert(env, isTypedArray, "is_typedarray",
		"Wrong type of arguments. Expects a typed array as first argument.") {
		return nil
	}

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype1 == sys.TypeNumber, "valuetype1 == napi_number",
		"Wrong type of arguments. Expects a number as second argument.") {
		return nil
	}

	var multiplier float64
	if !nt.Call(env, sys.GetValueDouble(env, args[1], &multiplier)) {
		return nil
	}

	var typ sys.TypedArrayType
	var inputBuffer sys.Value
	var byteOffset, length uint

	if !nt.Call(env, sys.GetTypedArrayInfo(env, inputArray, &typ, &length, nil, &inputBuffer, &byteOffset)) {
		return nil
	}

	var data unsafe.Pointer
	var byteLength uint
	if !nt.Call(env, sys.GetArrayBufferInfo(env, inputBuffer, &data, &byteLength)) {
		return nil
	}

	var outputBuffer sys.Value
	var outputPtr unsafe.Pointer
	if !nt.Call(env, sys.CreateArrayBuffer(env, byteLength, &outputPtr, &outputBuffer)) {
		return nil
	}

	var outputArray sys.Value
	if !nt.Call(env, sys.CreateTypedArray(env, typ, length, outputBuffer, byteOffset, &outputArray)) {
		return nil
	}

	switch typ {
	case sys.Uint8Array:
		inputBytes := unsafe.Slice((*uint8)(unsafe.Add(data, byteOffset)), length)
		outputBytes := unsafe.Slice((*uint8)(outputPtr), length)
		for i := range length {
			outputBytes[i] = uint8(float64(inputBytes[i]) * multiplier)
		}
	case sys.Float64Array:
		inputDoubles := unsafe.Slice((*float64)(unsafe.Add(data, byteOffset)), length)
		outputDoubles := unsafe.Slice((*float64)(outputPtr), length)
		for i := range length {
			outputDoubles[i] = inputDoubles[i] * multiplier
		}
	default:
		nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Typed array was of a type not expected by test.")))
		return nil
	}

	return outputArray
}

// finalizeCallback libera i dati dell'ArrayBuffer esterno, come free(finalize_data) nel C.
func finalizeCallback(_ sys.Env, finalizeData, _ uintptr) {
	nt.FreeAddr(finalizeData)
}

// externalElems è nElem del test C.
const externalElems = 3

func external(env sys.Env, _ sys.CallbackInfo) sys.Value {
	externalData := nt.Malloc(externalElems)
	if externalData == nil {
		nt.Call(env, sys.ThrowError(env, nil, nt.CStr("malloc failed")))
		return nil
	}
	values := unsafe.Slice((*int8)(externalData), externalElems)
	values[0] = 0
	values[1] = 1
	values[2] = 2

	var outputBuffer sys.Value
	cb, hint := nt.Finalizer(finalizeCallback, 0)
	if !nt.Call(env, sys.CreateExternalArrayBuffer(env, uintptr(externalData), externalElems, cb, hint, &outputBuffer)) {
		nt.DiscardFinalizer(hint)
		nt.Free(externalData)
		return nil
	}

	var outputArray sys.Value
	if !nt.Call(env, sys.CreateTypedArray(env, sys.Int8Array, externalElems, outputBuffer, 0, &outputArray)) {
		return nil
	}

	return outputArray
}

func nullArrayBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var arraybuffer sys.Value
	if !nt.Call(env, sys.CreateExternalArrayBuffer(env, 0, 0, nil, 0, &arraybuffer)) {
		return nil
	}
	return arraybuffer
}

func createTypedArray(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(4)
	var args [4]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 2 || argc == 4, "argc == 2 || argc == 4", "Wrong number of arguments") {
		return nil
	}

	inputArray := args[0]

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, inputArray, &valuetype0)) {
		return nil
	}

	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects a typed array as first argument.") {
		return nil
	}

	var isTypedArray bool
	if !nt.Call(env, sys.IsTypedArray(env, inputArray, &isTypedArray)) {
		return nil
	}

	if !nt.Assert(env, isTypedArray, "is_typedarray",
		"Wrong type of arguments. Expects a typed array as first argument.") {
		return nil
	}

	var valuetype1 sys.ValueType
	inputBuffer := args[1]
	if !nt.Call(env, sys.Typeof(env, inputBuffer, &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype1 == sys.TypeObject, "valuetype1 == napi_object",
		"Wrong type of arguments. Expects an array buffer as second argument.") {
		return nil
	}

	var isArrayBuffer bool
	if !nt.Call(env, sys.IsArrayBuffer(env, inputBuffer, &isArrayBuffer)) {
		return nil
	}

	if !nt.Assert(env, isArrayBuffer, "is_arraybuffer",
		"Wrong type of arguments. Expects an array buffer as second argument.") {
		return nil
	}

	var typ sys.TypedArrayType
	var inArrayBuffer sys.Value
	var byteOffset, length uint

	if !nt.Call(env, sys.GetTypedArrayInfo(env, inputArray, &typ, &length, nil, &inArrayBuffer, &byteOffset)) {
		return nil
	}

	if argc == 4 {
		var valuetype2 sys.ValueType
		if !nt.Call(env, sys.Typeof(env, args[2], &valuetype2)) {
			return nil
		}

		if !nt.Assert(env, valuetype2 == sys.TypeNumber, "valuetype2 == napi_number",
			"Wrong type of arguments. Expects a number as third argument.") {
			return nil
		}

		var uint32Length uint32
		if !nt.Call(env, sys.GetValueUint32(env, args[2], &uint32Length)) {
			return nil
		}
		length = uint(uint32Length)

		var valuetype3 sys.ValueType
		if !nt.Call(env, sys.Typeof(env, args[3], &valuetype3)) {
			return nil
		}

		if !nt.Assert(env, valuetype3 == sys.TypeNumber, "valuetype3 == napi_number",
			"Wrong type of arguments. Expects a number as third argument.") {
			return nil
		}

		var uint32ByteOffset uint32
		if !nt.Call(env, sys.GetValueUint32(env, args[3], &uint32ByteOffset)) {
			return nil
		}
		byteOffset = uint(uint32ByteOffset)
	}

	var outputArray sys.Value
	if !nt.Call(env, sys.CreateTypedArray(env, typ, length, inputBuffer, byteOffset, &outputArray)) {
		return nil
	}

	return outputArray
}

func detach(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments.") {
		return nil
	}

	var isTypedArray bool
	if !nt.Call(env, sys.IsTypedArray(env, args[0], &isTypedArray)) {
		return nil
	}
	if !nt.Assert(env, isTypedArray, "is_typedarray",
		"Wrong type of arguments. Expects a typedarray as first argument.") {
		return nil
	}

	var arraybuffer sys.Value
	if !nt.Call(env, sys.GetTypedArrayInfo(env, args[0], nil, nil, nil, &arraybuffer, nil)) {
		return nil
	}
	if !nt.Call(env, sys.DetachArrayBuffer(env, arraybuffer)) {
		return nil
	}
	return nil
}

func isDetached(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments.") {
		return nil
	}

	arrayBuffer := args[0]
	var isArrayBuffer bool
	if !nt.Call(env, sys.IsArrayBuffer(env, arrayBuffer, &isArrayBuffer)) {
		return nil
	}
	if !nt.Assert(env, isArrayBuffer, "is_arraybuffer",
		"Wrong type of arguments. Expects an array buffer as first argument.") {
		return nil
	}

	var isDetached bool
	if !nt.Call(env, sys.IsDetachedArrayBuffer(env, arrayBuffer, &isDetached)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.GetBoolean(env, isDetached, &result)) {
		return nil
	}

	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("Multiply", multiply),
		nt.DeclareProperty("External", external),
		nt.DeclareProperty("NullArrayBuffer", nullArrayBuffer),
		nt.DeclareProperty("CreateTypedArray", createTypedArray),
		nt.DeclareProperty("Detach", detach),
		nt.DeclareProperty("IsDetached", isDetached),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
