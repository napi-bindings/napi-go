// Porting di test/js-native-api/test_sharedarraybuffer/test_sharedarraybuffer.c
// (NAPI_EXPERIMENTAL, vedi napi-go.json). Le due API sperimentali passano per napitest.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// byteModulus è il 256 di bytes[i] = i % 256.
const byteModulus = 256

func testIsSharedArrayBuffer(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil
	}

	var isSharedArrayBuffer bool
	if !nt.Call(env, nt.IsSharedArrayBuffer(env, args[0], &isSharedArrayBuffer)) {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, isSharedArrayBuffer, &ret)) {
		return nil
	}
	return ret
}

func testCreateSharedArrayBuffer(env sys.Env, info sys.CallbackInfo) sys.Value {
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

	var byteLength int32
	if !nt.Call(env, sys.GetValueInt32(env, args[0], &byteLength)) {
		return nil
	}
	if !nt.Assert(env, byteLength >= 0, "byte_length >= 0",
		"Invalid byte length. Expects a non-negative integer.") {
		return nil
	}

	var ret sys.Value
	var data unsafe.Pointer
	if !nt.Call(env, nt.CreateSharedArrayBuffer(env, uint(byteLength), &data, &ret)) {
		return nil
	}
	return ret
}

func testGetSharedArrayBufferInfo(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil
	}

	var data unsafe.Pointer
	var byteLength uint
	if !nt.Call(env, sys.GetArrayBufferInfo(env, args[0], &data, &byteLength)) {
		return nil
	}

	var ret sys.Value
	// Troncamento a uint32 come la conversione implicita size_t → uint32_t del C.
	if !nt.Call(env, sys.CreateUint32(env, uint32(byteLength), &ret)) {
		return nil
	}
	return ret
}

func writeTestDataToBuffer(data unsafe.Pointer, byteLength uint) {
	if byteLength > 0 && data != nil {
		bytes := unsafe.Slice((*uint8)(data), byteLength)
		for i := range bytes {
			bytes[i] = uint8(i % byteModulus)
		}
	}
}

func testSharedArrayBufferData(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil
	}

	var data unsafe.Pointer
	var byteLength uint
	if !nt.Call(env, sys.GetArrayBufferInfo(env, args[0], &data, &byteLength)) {
		return nil
	}

	writeTestDataToBuffer(data, byteLength)

	// Restituisce la validità del puntatore ai dati.
	dataValid := data != nil && byteLength > 0
	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, dataValid, &ret)) {
		return nil
	}
	return ret
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("TestIsSharedArrayBuffer", testIsSharedArrayBuffer),
		nt.DeclareProperty("TestCreateSharedArrayBuffer", testCreateSharedArrayBuffer),
		nt.DeclareProperty("TestGetSharedArrayBufferInfo", testGetSharedArrayBufferInfo),
		nt.DeclareProperty("TestSharedArrayBufferData", testSharedArrayBufferData),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
