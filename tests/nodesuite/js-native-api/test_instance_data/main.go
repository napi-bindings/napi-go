// Porting di test/js-native-api/test_instance_data/test_instance_data.c.
package main

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// addonData è la struct AddonData del C, allocata in memoria C come instance data.
type addonData struct {
	value   uint
	print   bool
	jsCbRef sys.Ref
}

const (
	addonDataSize = uint(unsafe.Sizeof(addonData{}))
	initialValue  = 41
	// deleteMessage è la riga che lo script padre si aspetta su stdout.
	deleteMessage = "deleting addon data"
)

// getAddonData equivale a napi_get_instance_data(env, (void**)&data).
func getAddonData(env sys.Env) (*addonData, bool) {
	var data unsafe.Pointer
	if !nt.Call(env, sys.GetInstanceData(env, &data)) {
		return nil, false
	}
	return (*addonData)(data), true
}

func increment(env sys.Env, _ sys.CallbackInfo) sys.Value {
	data, ok := getAddonData(env)
	if !ok {
		return nil
	}
	var result sys.Value
	data.value++
	if !nt.Call(env, sys.CreateUint32(env, uint32(data.value), &result)) {
		return nil
	}
	return result
}

func deleteAddonData(env sys.Env, rawData, _ uintptr) {
	data := (*addonData)(nt.Pointer(rawData))
	if data.print {
		// Come il printf del C: lo script padre legge la prima riga di stdout.
		if _, err := fmt.Fprintln(os.Stdout, deleteMessage); err != nil {
			fmt.Fprintf(os.Stderr, "napitest: cannot write to stdout: %v\n", err)
		}
	}
	if data.jsCbRef != nil {
		if !nt.Call(env, sys.DeleteReference(env, data.jsCbRef)) {
			return
		}
	}
	nt.FreeAddr(rawData)
}

func setPrintOnDelete(env sys.Env, _ sys.CallbackInfo) sys.Value {
	data, ok := getAddonData(env)
	if !ok {
		return nil
	}
	data.print = true
	return nil
}

func testFinalizer(env sys.Env, _, _ uintptr) {
	data, ok := getAddonData(env)
	if !ok {
		return
	}
	var jsCb, undefined sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, data.jsCbRef, &jsCb)) {
		return
	}
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, undefined, jsCb, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, data.jsCbRef)) {
		return
	}
	data.jsCbRef = nil
}

func objectWithFinalizer(env sys.Env, info sys.CallbackInfo) sys.Value {
	var result, jsCb sys.Value
	argc := uint(1)

	data, ok := getAddonData(env)
	if !ok {
		return nil
	}
	if !nt.Assert(env, data.jsCbRef == nil, "data->js_cb_ref == NULL", "reference must be NULL") {
		return nil
	}
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &jsCb, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &result)) {
		return nil
	}
	cb, hint := nt.Finalizer(testFinalizer, 0)
	if !nt.Call(env, sys.AddFinalizer(env, result, 0, cb, hint, nil)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, jsCb, 1, &data.jsCbRef)) {
		return nil
	}

	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	// Il C non controlla il risultato di malloc; qui un'allocazione fallita lancia un errore
	// invece di scrivere su NULL.
	data := (*addonData)(nt.Malloc(addonDataSize))
	if !nt.Assert(env, data != nil, "data != NULL", "Failed to allocate memory") {
		return nil
	}
	*data = addonData{value: initialValue}
	cb, hint := nt.Finalizer(deleteAddonData, 0)
	if !nt.Call(env, sys.SetInstanceData(env, uintptr(unsafe.Pointer(data)), cb, hint)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	props := []sys.PropertyDescriptor{
		nt.DeclareProperty("increment", increment),
		nt.DeclareProperty("setPrintOnDelete", setPrintOnDelete),
		nt.DeclareProperty("objectWithFinalizer", objectWithFinalizer),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, props)) {
		return nil
	}

	return exports
}
