// Porting del target test_buffer di test/node-api/test_buffer/binding.gyp (test_buffer.c,
// NAPI_EXPERIMENTAL: i deleter "basic" girano durante il GC). Il define
// NODE_API_EXPERIMENTAL_NO_WARNING riguarda solo gli header.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	theText = "Lorem ipsum dolor sit amet, consectetur adipiscing elit."
	// theTextSize è sizeof(theText): include il NUL finale.
	theTextSize = uint(len(theText) + 1)
	// arrayBufferLength è byte_length di bufferFromArrayBuffer.
	arrayBufferLength = 1024
)

var (
	deleterCallCount int32
	// externalSharedArrayBufferData è static char externalSharedArrayBufferData[1]: una variabile
	// globale, che il GC di Go non sposta.
	externalSharedArrayBufferData [1]byte
)

// theTextC è il const char theText[] del C: memoria C condivisa e mai liberata.
func theTextC() *byte { return nt.CStr(theText) }

// isTheText equivale a data != NULL && strcmp(data, theText) == 0.
func isTheText(data uintptr) bool {
	return data != 0 && nt.GoString((*byte)(nt.Pointer(data))) == theText
}

func deleteTheText(_ sys.Env, data, _ uintptr) {
	if !nt.BasicAssert(isTheText(data), "data != NULL && strcmp(data, theText) == 0", "invalid data") {
		return
	}
	nt.FreeAddr(data)
	deleterCallCount++
}

func noopDeleter(_ sys.Env, data, _ uintptr) {
	if !nt.BasicAssert(isTheText(data), "data != NULL && strcmp(data, theText) == 0", "invalid data") {
		return
	}
	deleterCallCount++
}

func newBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var theBuffer sys.Value
	var theCopy unsafe.Pointer
	kBufferSize := theTextSize
	if !nt.Call(env, sys.CreateBuffer(env, theTextSize, &theCopy, &theBuffer)) {
		return nil
	}
	if !nt.Assert(env, theCopy != nil, "theCopy", "Failed to copy static text for newBuffer") {
		return nil
	}
	copy(unsafe.Slice((*byte)(theCopy), kBufferSize), unsafe.Slice(theTextC(), kBufferSize))
	return theBuffer
}

func newExternalBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var theBuffer sys.Value
	// strdup(theText)
	theCopy := nt.Malloc(theTextSize)
	if !nt.Assert(env, theCopy != nil, "theCopy", "Failed to copy static text for newExternalBuffer") {
		return nil
	}
	copy(unsafe.Slice((*byte)(theCopy), theTextSize), unsafe.Slice(theTextC(), theTextSize))
	cb, hint := nt.Finalizer(deleteTheText, 0 /* finalize_hint */)
	if !nt.Call(env, sys.CreateExternalBuffer(env, theTextSize, uintptr(theCopy), cb, hint, &theBuffer)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return theBuffer
}

// freeExternalSharedArrayBuffer è un node_api_noenv_finalize: niente env.
func freeExternalSharedArrayBuffer(data, _ uintptr) {
	if !nt.BasicAssert(data == uintptr(unsafe.Pointer(&externalSharedArrayBufferData[0])),
		"data == (void*)externalSharedArrayBufferData", "SharedArrayBuffer points to wrong data") {
		return
	}
	deleterCallCount++
}

func newExternalSharedArrayBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var sab sys.Value
	if !nt.Call(env, nt.CreateExternalSharedArrayBuffer(env,
		uintptr(unsafe.Pointer(&externalSharedArrayBufferData[0])), uint(len(externalSharedArrayBufferData)),
		freeExternalSharedArrayBuffer, 0, &sab)) {
		return nil
	}
	return sab
}

func getDeleterCallCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var callCount sys.Value
	if !nt.Call(env, sys.CreateInt32(env, deleterCallCount, &callCount)) {
		return nil
	}
	return callCount
}

func copyBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var theBuffer sys.Value
	if !nt.Call(env, sys.CreateBufferCopy(env, theTextSize, unsafe.Pointer(theTextC()), nil, &theBuffer)) {
		return nil
	}
	return theBuffer
}

func bufferHasInstance(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	theBuffer := args[0]
	var hasInstance bool
	var theType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, theBuffer, &theType)) {
		return nil
	}
	if !nt.Assert(env, theType == sys.TypeObject, "theType == napi_object",
		"bufferHasInstance: instance is not an object") {
		return nil
	}
	if !nt.Call(env, sys.IsBuffer(env, theBuffer, &hasInstance)) {
		return nil
	}
	if !nt.Assert(env, hasInstance, "hasInstance", "bufferHasInstance: instance is not a buffer") {
		return nil
	}
	var returnValue sys.Value
	if !nt.Call(env, sys.GetBoolean(env, hasInstance, &returnValue)) {
		return nil
	}
	return returnValue
}

func bufferInfo(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	theBuffer := args[0]
	var bufferData unsafe.Pointer
	var returnValue sys.Value
	var bufferLength uint
	if !nt.Call(env, sys.GetBufferInfo(env, theBuffer, &bufferData, &bufferLength)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env,
		nt.GoString((*byte)(bufferData)) == theText && bufferLength == theTextSize, &returnValue)) {
		return nil
	}
	return returnValue
}

func staticBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var theBuffer sys.Value
	cb, hint := nt.Finalizer(noopDeleter, 0 /* finalize_hint */)
	if !nt.Call(env, sys.CreateExternalBuffer(env, theTextSize, uintptr(unsafe.Pointer(theTextC())), cb, hint,
		&theBuffer)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return theBuffer
}

func invalidObjectAsBuffer(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	notTheBuffer := args[0]
	status := sys.GetBufferInfo(env, notTheBuffer, nil, nil)
	if !nt.Assert(env, status == sys.StatusInvalidArg, "status == napi_invalid_arg",
		"napi_get_buffer_info: should fail with napi_invalid_arg when passed non buffer") {
		return nil
	}
	return notTheBuffer
}

func bufferFromArrayBuffer(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var status sys.Status
	var arraybuffer, buffer sys.Value
	byteLength := uint(arrayBufferLength)
	var data unsafe.Pointer
	var bufferLength uint
	var bufferData unsafe.Pointer

	status = sys.CreateArrayBuffer(env, byteLength, &data, &arraybuffer)
	if !nt.Assert(env, status == sys.StatusOK, "status == napi_ok", "Failed to create arraybuffer") {
		return nil
	}

	status = sys.CreateBufferFromArrayBuffer(env, arraybuffer, 0, byteLength, &buffer)
	if !nt.Assert(env, status == sys.StatusOK, "status == napi_ok", "Failed to create buffer from arraybuffer") {
		return nil
	}

	status = sys.GetBufferInfo(env, buffer, &bufferData, &bufferLength)
	if !nt.Assert(env, status == sys.StatusOK, "status == napi_ok", "Failed to get buffer info") {
		return nil
	}

	if !nt.Assert(env, bufferLength == byteLength, "buffer_length == byte_length", "Buffer length mismatch") {
		return nil
	}

	return buffer
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var theValue sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, theTextC(), theTextSize, &theValue)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("theText"), theValue)) {
		return nil
	}
	methods := []sys.PropertyDescriptor{
		nt.DeclareProperty("newBuffer", newBuffer),
		nt.DeclareProperty("newExternalBuffer", newExternalBuffer),
		nt.DeclareProperty("newExternalSharedArrayBuffer", newExternalSharedArrayBuffer),
		nt.DeclareProperty("getDeleterCallCount", getDeleterCallCount),
		nt.DeclareProperty("copyBuffer", copyBuffer),
		nt.DeclareProperty("bufferHasInstance", bufferHasInstance),
		nt.DeclareProperty("bufferInfo", bufferInfo),
		nt.DeclareProperty("staticBuffer", staticBuffer),
		nt.DeclareProperty("invalidObjectAsBuffer", invalidObjectAsBuffer),
		nt.DeclareProperty("bufferFromArrayBuffer", bufferFromArrayBuffer),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, methods)) {
		return nil
	}
	return exports
}
