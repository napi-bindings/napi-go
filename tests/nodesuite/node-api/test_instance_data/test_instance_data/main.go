// Porting di test/node-api/test_instance_data/test_instance_data.c. uv_thread_create/join
// diventano nt.Thread; AddonData è una struct Go passata come instance data tramite cgo.Handle,
// così può contenere il nt.Thread.
package main

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

type addonData struct {
	jsCbRef            sys.Ref
	jsTsfnFinalizerRef sys.Ref
	tsfn               sys.ThreadsafeFunction
	thread             nt.Thread
}

// bufferBacking fa da memoria del buffer esterno di TestBufferFinalizer: il C usa l'indirizzo
// della funzione stessa con sizeof(napi_callback) byte. Una variabile globale non viene spostata
// dal GC di Go.
var bufferBacking [unsafe.Sizeof(uintptr(0))]byte

// getAddonData equivale a napi_get_instance_data(env, (void**)&data).
func getAddonData(env sys.Env, call func(sys.Env, sys.Status) bool) (*addonData, bool) {
	var data unsafe.Pointer
	if !call(env, sys.GetInstanceData(env, &data)) {
		return nil, false
	}
	return cgo.Handle(uintptr(data)).Value().(*addonData), true
}

func asyncWorkCbExecute(sys.Env, any) {}

// callCbAndDeleteRef con optionalRef nil usa data->js_cb_ref, come il C con NULL.
func callCbAndDeleteRef(env sys.Env, optionalRef *sys.Ref) {
	var jsCb, undefined sys.Value
	if optionalRef == nil {
		data, ok := getAddonData(env, nt.Call)
		if !ok {
			return
		}
		optionalRef = &data.jsCbRef
	}
	if !nt.Call(env, sys.GetReferenceValue(env, *optionalRef, &jsCb)) {
		return
	}
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, undefined, jsCb, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, *optionalRef)) {
		return
	}
	*optionalRef = nil
}

func asyncWorkCbComplete(env sys.Env, _ sys.Status, _ any) {
	callCbAndDeleteRef(env, nil)
}

func establishCallbackRef(env sys.Env, info sys.CallbackInfo) bool {
	data, ok := getAddonData(env, nt.Call)
	if !ok {
		return false
	}
	argc := uint(1)
	var jsCb sys.Value
	if !nt.Assert(env, data.jsCbRef == nil, "data->js_cb_ref == NULL", "reference must be NULL") {
		return false
	}
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &jsCb, nil, nil)) {
		return false
	}
	if !nt.Call(env, sys.CreateReference(env, jsCb, 1, &data.jsCbRef)) {
		return false
	}
	return true
}

func asyncWorkCallback(env sys.Env, info sys.CallbackInfo) sys.Value {
	if establishCallbackRef(env, info) {
		var resourceName sys.Value
		var work sys.AsyncWork
		if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("AsyncIncrement"), sys.AutoLength, &resourceName)) {
			return nil
		}
		if !nt.Call(env, sys.CreateAsyncWork(env, nil, resourceName, sys.AsyncExecuteTrampoline,
			sys.AsyncCompleteTrampoline, nt.AsyncWorkData(asyncWorkCbExecute, asyncWorkCbComplete, nil), &work)) {
			return nil
		}
		if !nt.Call(env, sys.QueueAsyncWork(env, work)) {
			return nil
		}
	}
	return nil
}

func testBufferFinalizerCallback(env sys.Env, _, _ uintptr) {
	callCbAndDeleteRef(env, nil)
}

func testBufferFinalizer(env sys.Env, info sys.CallbackInfo) sys.Value {
	var buffer sys.Value
	if establishCallbackRef(env, info) {
		cb, hint := nt.Finalizer(testBufferFinalizerCallback, 0)
		if !nt.Call(env, sys.CreateExternalBuffer(env, uint(len(bufferBacking)),
			uintptr(unsafe.Pointer(&bufferBacking[0])), cb, hint, &buffer)) {
			nt.DiscardFinalizer(hint)
			return nil
		}
	}
	return buffer
}

func threadsafeFunctionCallJS(env sys.Env, _ sys.Value, _, _ uintptr) {
	callCbAndDeleteRef(env, nil)
}

func threadsafeFunctionTestThread(data *addonData) {
	// Non serve napi_acquire_threadsafe_function: il thread principale ha impostato il
	// conteggio a 1 e c'è un solo thread secondario. Il data della chiamata nel C è il puntatore
	// a ThreadsafeFunctionCallJS, mai letto: qui è 0.
	status := sys.CallThreadsafeFunction(data.tsfn, 0, sys.TSFNNonblocking)
	if status != sys.StatusOK {
		sys.FatalError(nt.CStr("ThreadSafeFunctionTestThread"), sys.AutoLength, nt.CStr("Failed to call TSFN"),
			sys.AutoLength)
	}

	status = sys.ReleaseThreadsafeFunction(data.tsfn, sys.TSFNRelease)
	if status != sys.StatusOK {
		sys.FatalError(nt.CStr("ThreadSafeFunctionTestThread"), sys.AutoLength, nt.CStr("Failed to release TSFN"),
			sys.AutoLength)
	}
}

func finalizeThreadsafeFunction(env sys.Env, _, _ uintptr) {
	data, ok := getAddonData(env, nt.Call)
	if !ok {
		return
	}
	if !nt.Assert(env, data.thread.Join() == 0, "uv_thread_join(&data->thread) == 0", "Failed to join the thread") {
		return
	}
	callCbAndDeleteRef(env, &data.jsTsfnFinalizerRef)
	data.tsfn = nil
}

// testThreadsafeFunction riceve due argomenti: la callback JS e la callback di finalizzazione,
// che fa proseguire il test.
func testThreadsafeFunction(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	var resourceName sys.Value

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	data, ok := getAddonData(env, nt.Call)
	if !ok {
		return nil
	}
	if !nt.Assert(env, data.jsCbRef == nil, "data->js_cb_ref == NULL", "reference must be NULL") {
		return nil
	}
	if !nt.Assert(env, data.jsTsfnFinalizerRef == nil, "data->js_tsfn_finalizer_ref == NULL",
		"tsfn finalizer reference must be NULL") {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[0], 1, &data.jsCbRef)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[1], 1, &data.jsTsfnFinalizerRef)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("TSFN instance data test"), sys.AutoLength,
		&resourceName)) {
		return nil
	}
	callJS, finalize, context := nt.ThreadsafeFunction(threadsafeFunctionCallJS, finalizeThreadsafeFunction, 0)
	if !nt.Call(env, sys.CreateThreadsafeFunction(env, nil, nil, resourceName, 0, 1, 0, finalize, context, callJS,
		&data.tsfn)) {
		nt.DiscardThreadsafeFunction(context)
		return nil
	}
	if !nt.Assert(env, data.thread.Create(func() { threadsafeFunctionTestThread(data) }) == 0,
		"uv_thread_create(&data->thread, ThreadsafeFunctionTestThread, data) == 0", "uv_thread_create failed") {
		return nil
	}
	return nil
}

func deleteAddonData(env sys.Env, rawData, _ uintptr) {
	h := cgo.Handle(rawData)
	data := h.Value().(*addonData)
	if data.jsCbRef != nil {
		if !nt.Call(env, sys.DeleteReference(env, data.jsCbRef)) {
			return
		}
	}
	if data.jsTsfnFinalizerRef != nil {
		if !nt.Call(env, sys.DeleteReference(env, data.jsTsfnFinalizerRef)) {
			return
		}
	}
	h.Delete()
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	h := cgo.NewHandle(&addonData{})
	cb, hint := nt.Finalizer(deleteAddonData, 0)
	if !nt.Call(env, sys.SetInstanceData(env, uintptr(h), cb, hint)) {
		nt.DiscardFinalizer(hint)
		h.Delete()
		return nil
	}

	props := []sys.PropertyDescriptor{
		nt.DeclareProperty("asyncWorkCallback", asyncWorkCallback),
		nt.DeclareProperty("testBufferFinalizer", testBufferFinalizer),
		nt.DeclareProperty("testThreadsafeFunction", testThreadsafeFunction),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, props)) {
		return nil
	}
	return exports
}
