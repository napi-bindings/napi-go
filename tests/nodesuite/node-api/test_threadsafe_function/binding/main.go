// Porting del target binding di test/node-api/test_threadsafe_function/binding.gyp (binding.c).
// I thread di libuv (uv_thread_create/join) diventano nt.Thread e uv_hrtime nt.HRTime.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	arrayLength  = 10000
	maxQueueSize = 2
	// busyInterval e busyDuration: ogni busyInterval valori, con coda illimitata, il thread resta
	// occupato per busyDuration nanosecondi (200 ms) per dare al thread principale il tempo di
	// interromperlo.
	busyInterval = 1000
	busyDuration = 200000000
)

type tsFnHint struct {
	blockOnFull    sys.ThreadsafeFunctionCallMode
	abort          sys.ThreadsafeFunctionReleaseMode
	startSecondary bool
	jsFinalizeCb   sys.Ref
	maxQueueSize   uint32
}

// Variabili globali come nel C: il GC di Go non le sposta, quindi i loro indirizzi possono fare
// da data, context e finalize_data della TSFN.
var (
	uvThreads [2]nt.Thread
	tsFn      sys.ThreadsafeFunction
	tsInfo    tsFnHint
	// ints sono i dati che i thread passano a JS.
	ints   [arrayLength]int32
	altRef sys.Ref
)

func tsInfoAddr() uintptr { return uintptr(unsafe.Pointer(&tsInfo)) }

func secondaryThread(tsFn sys.ThreadsafeFunction) {
	if sys.ReleaseThreadsafeFunction(tsFn, sys.TSFNRelease) != sys.StatusOK {
		sys.FatalError(nt.CStr("secondary_thread"), sys.AutoLength,
			nt.CStr("napi_release_threadsafe_function failed"), sys.AutoLength)
	}
}

// dataSourceThread è il thread che produce i dati.
func dataSourceThread(tsFn sys.ThreadsafeFunction) {
	var hint uintptr
	var status sys.Status
	queueWasFull := false
	queueWasClosing := false

	if nt.GetThreadsafeFunctionContext(tsFn, &hint) != sys.StatusOK {
		sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
			nt.CStr("napi_get_threadsafe_function_context failed"), sys.AutoLength)
	}

	tsFnInfo := (*tsFnHint)(nt.Pointer(hint))

	if hint != tsInfoAddr() {
		sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
			nt.CStr("thread-safe function hint is not as expected"), sys.AutoLength)
	}

	if tsFnInfo.startSecondary {
		if sys.AcquireThreadsafeFunction(tsFn) != sys.StatusOK {
			sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
				nt.CStr("napi_acquire_threadsafe_function failed"), sys.AutoLength)
		}

		if uvThreads[1].Create(func() { secondaryThread(tsFn) }) != 0 {
			sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
				nt.CStr("failed to start secondary thread"), sys.AutoLength)
		}
	}

	for index := arrayLength - 1; index > -1 && !queueWasClosing; index-- {
		status = sys.CallThreadsafeFunction(tsFn, uintptr(unsafe.Pointer(&ints[index])), tsFnInfo.blockOnFull)
		if tsFnInfo.maxQueueSize == 0 && (index%busyInterval == 0) {
			// Tiene il thread davvero occupato per 200 ms, così il thread principale ha modo di
			// interromperlo.
			for start := nt.HRTime(); nt.HRTime()-start < busyDuration; {
			}
		}
		switch status {
		case sys.StatusQueueFull:
			queueWasFull = true
			index++
			continue
		case sys.StatusOK:
			continue
		case sys.StatusClosing:
			queueWasClosing = true
		default:
			sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
				nt.CStr("napi_call_threadsafe_function failed"), sys.AutoLength)
		}
	}

	// Se la prova non è bloccante, almeno un accodamento deve essere stato rifiutato.
	if tsFnInfo.blockOnFull == sys.TSFNNonblocking && !queueWasFull {
		sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength, nt.CStr("queue was never full"),
			sys.AutoLength)
	}

	// Se la prova interrompe la TSFN, la coda deve essere stata in chiusura almeno una volta.
	if tsFnInfo.abort == sys.TSFNAbort && !queueWasClosing {
		sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength, nt.CStr("queue was never closing"),
			sys.AutoLength)
	}

	if !queueWasClosing && sys.ReleaseThreadsafeFunction(tsFn, sys.TSFNRelease) != sys.StatusOK {
		sys.FatalError(nt.CStr("data_source_thread"), sys.AutoLength,
			nt.CStr("napi_release_threadsafe_function failed"), sys.AutoLength)
	}
}

// callJS porta i dati in JS.
func callJS(env sys.Env, cb sys.Value, _, data uintptr) {
	if !(env == nil || cb == nil) {
		var argv, undefined sys.Value
		if !nt.Call(env, sys.CreateInt32(env, *(*int32)(nt.Pointer(data)), &argv)) {
			return
		}
		if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
			return
		}
		if !nt.Call(env, sys.CallFunction(env, undefined, cb, 1, &argv, nil)) {
			return
		}
	}
}

// callRef porta i dati in JS tramite il riferimento alternativo.
func callRef(env sys.Env, _ sys.Value, _, data uintptr) {
	if !(env == nil || altRef == nil) {
		var fn, argv, undefined sys.Value
		if !nt.Call(env, sys.GetReferenceValue(env, altRef, &fn)) {
			return
		}
		if !nt.Call(env, sys.CreateInt32(env, *(*int32)(nt.Pointer(data)), &argv)) {
			return
		}
		if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
			return
		}
		if !nt.Call(env, sys.CallFunction(env, undefined, fn, 1, &argv, nil)) {
			return
		}
	}
}

// stopThread fa la pulizia.
func stopThread(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	var valueType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argv[0], &valueType)) {
		return nil
	}
	if !nt.Assert(env, valueType == sys.TypeFunction, "value_type == napi_function",
		"StopThread argument is a function") {
		return nil
	}
	if !nt.Assert(env, tsFn != nil, "(ts_fn != NULL)", "Existing threadsafe function") {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[0], 1, &tsInfo.jsFinalizeCb)) {
		return nil
	}
	var abort bool
	if !nt.Call(env, sys.GetValueBool(env, argv[1], &abort)) {
		return nil
	}
	mode := sys.TSFNRelease
	if abort {
		mode = sys.TSFNAbort
	}
	if !nt.Call(env, sys.ReleaseThreadsafeFunction(tsFn, mode)) {
		return nil
	}
	tsFn = nil
	return nil
}

// joinTheThreads attende i thread e avvisa JS che ha finito.
func joinTheThreads(env sys.Env, data, hint uintptr) {
	theThreads := (*[2]nt.Thread)(nt.Pointer(data))
	theHint := (*tsFnHint)(nt.Pointer(hint))
	var jsCb, undefined sys.Value

	// I valori di ritorno non sono controllati, come nel C.
	theThreads[0].Join()
	if theHint.startSecondary {
		theThreads[1].Join()
	}

	if !nt.Call(env, sys.GetReferenceValue(env, theHint.jsFinalizeCb, &jsCb)) {
		return
	}
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, undefined, jsCb, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, theHint.jsFinalizeCb)) {
		return
	}
	if altRef != nil {
		if !nt.Call(env, sys.DeleteReference(env, altRef)) {
			return
		}
		altRef = nil
	}
}

func startThreadInternal(env sys.Env, info sys.CallbackInfo, cb nt.CallJSFunc, blockOnFull,
	altRefJsCb bool) sys.Value {
	argc := uint(4)
	var argv [4]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	if altRefJsCb {
		if !nt.Call(env, sys.CreateReference(env, argv[0], 1, &altRef)) {
			return nil
		}
		argv[0] = nil
	}

	tsInfo.blockOnFull = sys.TSFNNonblocking
	if blockOnFull {
		tsInfo.blockOnFull = sys.TSFNBlocking
	}

	if !nt.Assert(env, tsFn == nil, "(ts_fn == NULL)", "Existing thread-safe function") {
		return nil
	}
	var asyncName sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("Node-API Thread-safe Function Test"), sys.AutoLength,
		&asyncName)) {
		return nil
	}
	if !nt.Call(env, sys.GetValueUint32(env, argv[3], &tsInfo.maxQueueSize)) {
		return nil
	}
	callJSCb, finalize, context := nt.ThreadsafeFunction(cb, joinTheThreads, tsInfoAddr())
	if !nt.Call(env, sys.CreateThreadsafeFunction(env, argv[0], nil, asyncName, uint(tsInfo.maxQueueSize), 2,
		uintptr(unsafe.Pointer(&uvThreads)), finalize, context, callJSCb, &tsFn)) {
		nt.DiscardThreadsafeFunction(context)
		return nil
	}
	var abort bool
	if !nt.Call(env, sys.GetValueBool(env, argv[1], &abort)) {
		return nil
	}
	tsInfo.abort = sys.TSFNRelease
	if abort {
		tsInfo.abort = sys.TSFNAbort
	}
	if !nt.Call(env, sys.GetValueBool(env, argv[2], &tsInfo.startSecondary)) {
		return nil
	}

	fn := tsFn
	if !nt.Assert(env, uvThreads[0].Create(func() { dataSourceThread(fn) }) == 0,
		"(uv_thread_create(&uv_threads[0], data_source_thread, ts_fn) == 0)", "Thread creation") {
		return nil
	}

	return nil
}

func unref(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, tsFn != nil, "ts_fn != NULL", "No existing thread-safe function") {
		return nil
	}
	if !nt.Call(env, sys.UnrefThreadsafeFunction(env, tsFn)) {
		return nil
	}
	return nil
}

func release(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, tsFn != nil, "ts_fn != NULL", "No existing thread-safe function") {
		return nil
	}
	if !nt.Call(env, sys.ReleaseThreadsafeFunction(tsFn, sys.TSFNRelease)) {
		return nil
	}
	return nil
}

// Avvio.

func startThread(env sys.Env, info sys.CallbackInfo) sys.Value {
	return startThreadInternal(env, info, callJS, true /* block_on_full */, false /* alt_ref_js_cb */)
}

func startThreadNonblocking(env sys.Env, info sys.CallbackInfo) sys.Value {
	return startThreadInternal(env, info, callJS, false /* block_on_full */, false /* alt_ref_js_cb */)
}

func startThreadNoNative(env sys.Env, info sys.CallbackInfo) sys.Value {
	return startThreadInternal(env, info, nil, true /* block_on_full */, false /* alt_ref_js_cb */)
}

func startThreadNoJsFunc(env sys.Env, info sys.CallbackInfo) sys.Value {
	return startThreadInternal(env, info, callRef, true /* block_on_full */, true /* alt_ref_js_cb */)
}

// threadSafeFunctionFinalize riceve come finalize_data il napi_ref della funzione JS.
func threadSafeFunctionFinalize(env sys.Env, finalizeData, _ uintptr) {
	jsFuncRef := sys.Ref(nt.Pointer(finalizeData))
	var jsFunc, recv sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, jsFuncRef, &jsFunc)) {
		return
	}
	if !nt.Call(env, sys.GetGlobal(env, &recv)) {
		return
	}
	if !nt.Call(env, sys.CallFunction(env, recv, jsFunc, 0, nil, nil)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, jsFuncRef)) {
		return
	}
}

// callIntoModule verifica la chiamata a JavaScript.
func callIntoModule(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(4)
	var argv [4]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}

	var finalizeFunc sys.Ref
	if !nt.Call(env, sys.CreateReference(env, argv[3], 1, &finalizeFunc)) {
		return nil
	}

	var tsfn sys.ThreadsafeFunction
	callJSCb, finalize, context := nt.ThreadsafeFunction(nil, threadSafeFunctionFinalize, 0)
	if !nt.Call(env, sys.CreateThreadsafeFunction(env, argv[0], argv[1], argv[2], 0, 1,
		uintptr(unsafe.Pointer(finalizeFunc)), finalize, context, callJSCb, &tsfn)) {
		nt.DiscardThreadsafeFunction(context)
		return nil
	}
	if !nt.Call(env, sys.CallThreadsafeFunction(tsfn, 0, sys.TSFNBlocking)) {
		return nil
	}
	if !nt.Call(env, sys.ReleaseThreadsafeFunction(tsfn, sys.TSFNRelease)) {
		return nil
	}
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	for index := range ints {
		ints[index] = int32(index)
	}

	var jsArrayLength, jsMaxQueueSize sys.Value
	// Gli status non sono controllati, come nel C.
	sys.CreateUint32(env, arrayLength, &jsArrayLength)
	sys.CreateUint32(env, maxQueueSize, &jsMaxQueueSize)

	properties := []sys.PropertyDescriptor{
		nt.Value("ARRAY_LENGTH", jsArrayLength, sys.PropertyEnumerable),
		nt.Value("MAX_QUEUE_SIZE", jsMaxQueueSize, sys.PropertyEnumerable),
		nt.DeclareProperty("StartThread", startThread),
		nt.DeclareProperty("StartThreadNoNative", startThreadNoNative),
		nt.DeclareProperty("StartThreadNonblocking", startThreadNonblocking),
		nt.DeclareProperty("StartThreadNoJsFunc", startThreadNoJsFunc),
		nt.DeclareProperty("StopThread", stopThread),
		nt.DeclareProperty("Unref", unref),
		nt.DeclareProperty("Release", release),
		nt.DeclareProperty("CallIntoModule", callIntoModule),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
