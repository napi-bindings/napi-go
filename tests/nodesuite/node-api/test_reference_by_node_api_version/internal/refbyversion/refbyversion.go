// Package refbyversion è il porting di
// test/node-api/test_reference_by_node_api_version/test_reference_by_node_api_version.c,
// condiviso dai target test_reference_all_types (NAPI_VERSION=10) e test_reference_obj_only
// (NAPI_VERSION=9) del binding.gyp: lo stesso codice, con la versione dichiarata che cambia i
// tipi ammessi da napi_create_reference.
package refbyversion

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

// valueRefsLength è (int)napi_bigint + 1: un riferimento per ogni napi_valuetype.
const valueRefsLength = int(sys.TypeBigInt) + 1

const (
	refSize = uint(unsafe.Sizeof(sys.Ref(nil)))
	intSize = uint(unsafe.Sizeof(int32(0)))
	// externalValue è il 42 scritto nel data dell'external.
	externalValue = 42
)

var finalizeCount uint32

func freeData(_ sys.Env, data, _ uintptr) {
	if !nt.BasicAssert(data != 0, "data != NULL", "Expects non-NULL data.") {
		return
	}
	nt.FreeAddr(data)
}

func finalize(sys.Env, uintptr, uintptr) {
	finalizeCount++
}

func getArgValue(env sys.Env, info sys.CallbackInfo, argValue *sys.Value) sys.Status {
	argc := uint(1)
	if st := sys.GetCbInfo(env, info, &argc, argValue, nil, nil); st != sys.StatusOK {
		return st
	}
	return nt.AssertStatus(env, argc == 1, "argc == 1", "Expects one arg.")
}

func getArgValueAsIndex(env sys.Env, info sys.CallbackInfo, index *uint32) sys.Status {
	var argValue sys.Value
	if st := getArgValue(env, info, &argValue); st != sys.StatusOK {
		return st
	}
	var valueType sys.ValueType
	if st := sys.Typeof(env, argValue, &valueType); st != sys.StatusOK {
		return st
	}
	if st := nt.AssertStatus(env, valueType == sys.TypeNumber, "valueType == napi_number",
		"Argument must be a number."); st != sys.StatusOK {
		return st
	}
	return sys.GetValueUint32(env, argValue, index)
}

// valueRefs equivale a napi_get_instance_data(env, (void**)&refValues): l'array di napi_ref in
// memoria C.
func valueRefs(env sys.Env, result *[]sys.Ref) sys.Status {
	var data unsafe.Pointer
	if st := sys.GetInstanceData(env, &data); st != sys.StatusOK {
		return st
	}
	if data == nil {
		*result = nil
		return sys.StatusOK
	}
	*result = unsafe.Slice((*sys.Ref)(data), valueRefsLength)
	return sys.StatusOK
}

func getRef(env sys.Env, info sys.CallbackInfo, ref *sys.Ref) sys.Status {
	var index uint32
	if st := getArgValueAsIndex(env, info, &index); st != sys.StatusOK {
		return st
	}
	var refValues []sys.Ref
	if st := valueRefs(env, &refValues); st != sys.StatusOK {
		return st
	}
	if st := nt.AssertStatus(env, refValues != nil, "refValues != NULL", "Cannot get instance data."); st != sys.StatusOK {
		return st
	}
	*ref = refValues[index]
	return sys.StatusOK
}

func toUInt32Value(env sys.Env, value uint32) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.CreateUint32(env, value, &result)) {
		return nil
	}
	return result
}

func initRefArray(env sys.Env) sys.Status {
	// Un elemento di valueRefs per ogni napi_valuetype.
	refs := nt.Malloc(refSize * uint(valueRefsLength))
	cb, hint := nt.Finalizer(freeData, 0)
	st := sys.SetInstanceData(env, uintptr(refs), cb, hint)
	if st != sys.StatusOK {
		nt.DiscardFinalizer(hint)
	}
	return st
}

func createExternal(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	data := (*int32)(nt.Malloc(intSize))
	*data = externalValue
	cb, hint := nt.Finalizer(freeData, 0)
	if !nt.Call(env, sys.CreateExternal(env, uintptr(unsafe.Pointer(data)), cb, hint, &result)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return result
}

func createRef(env sys.Env, info sys.CallbackInfo) sys.Value {
	var argValue sys.Value
	if !nt.Call(env, getArgValue(env, info, &argValue)) {
		return nil
	}
	var valueType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argValue, &valueType)) {
		return nil
	}
	index := uint32(valueType)
	var refs []sys.Ref
	if !nt.Call(env, valueRefs(env, &refs)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argValue, 1, &refs[index])) {
		return nil
	}
	return toUInt32Value(env, index)
}

func getRefValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	var refValue sys.Ref
	if !nt.Call(env, getRef(env, info, &refValue)) {
		return nil
	}
	var value sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, refValue, &value)) {
		return nil
	}
	return value
}

func ref(env sys.Env, info sys.CallbackInfo) sys.Value {
	var refValue sys.Ref
	if !nt.Call(env, getRef(env, info, &refValue)) {
		return nil
	}
	var refCount uint32
	if !nt.Call(env, sys.ReferenceRef(env, refValue, &refCount)) {
		return nil
	}
	return toUInt32Value(env, refCount)
}

func unref(env sys.Env, info sys.CallbackInfo) sys.Value {
	var refValue sys.Ref
	if !nt.Call(env, getRef(env, info, &refValue)) {
		return nil
	}
	var refCount uint32
	if !nt.Call(env, sys.ReferenceUnref(env, refValue, &refCount)) {
		return nil
	}
	return toUInt32Value(env, refCount)
}

func deleteRef(env sys.Env, info sys.CallbackInfo) sys.Value {
	var refValue sys.Ref
	if !nt.Call(env, getRef(env, info, &refValue)) {
		return nil
	}
	if !nt.Call(env, sys.DeleteReference(env, refValue)) {
		return nil
	}
	return nil
}

func addFinalizer(env sys.Env, info sys.CallbackInfo) sys.Value {
	var obj sys.Value
	if !nt.Call(env, getArgValue(env, info, &obj)) {
		return nil
	}
	var valueType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, obj, &valueType)) {
		return nil
	}
	if !nt.Assert(env, valueType == sys.TypeObject, "valueType == napi_object", "Argument must be an object.") {
		return nil
	}
	cb, hint := nt.Finalizer(finalize, 0)
	if !nt.Call(env, sys.AddFinalizer(env, obj, 0, cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	return nil
}

func getFinalizeCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	return toUInt32Value(env, finalizeCount)
}

func initFinalizeCount(sys.Env, sys.CallbackInfo) sys.Value {
	finalizeCount = 0
	return nil
}

// Init equivale al corpo di NAPI_MODULE_INIT().
func Init(env sys.Env, exports sys.Value) sys.Value {
	finalizeCount = 0
	if !nt.Call(env, initRefArray(env)) {
		return nil
	}
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("createExternal", createExternal),
		nt.DeclareProperty("createRef", createRef),
		nt.DeclareProperty("getRefValue", getRefValue),
		nt.DeclareProperty("ref", ref),
		nt.DeclareProperty("unref", unref),
		nt.DeclareProperty("deleteRef", deleteRef),
		nt.DeclareProperty("addFinalizer", addFinalizer),
		nt.DeclareProperty("getFinalizeCount", getFinalizeCount),
		nt.DeclareProperty("initFinalizeCount", initFinalizeCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
