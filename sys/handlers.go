package sys

/*
#include "internal.h"
*/
import "C"

import (
	"fmt"
	"os"
	"sync/atomic"
	"unsafe"
)

// Trampolini C da passare a Node-API. Entrano in Go attraverso gli handler registrati qui sotto.
var (
	// CallbackTrampoline per CreateFunction, DefineClass, PropertyDescriptor: chiama il
	// CallbackHandler con il data della funzione.
	CallbackTrampoline = Callback(C.napi_callback(C.gn_callback))
	// FinalizeTrampoline per Wrap, AddFinalizer, CreateExternal*, SetInstanceData, ...
	FinalizeTrampoline = Finalize(C.napi_finalize(C.gn_finalize))
	// CallJSTrampoline per CreateThreadsafeFunction.
	CallJSTrampoline = ThreadsafeFunctionCallJS(C.napi_threadsafe_function_call_js(C.gn_call_js))
	// AsyncExecuteTrampoline e AsyncCompleteTrampoline per CreateAsyncWork.
	AsyncExecuteTrampoline  = AsyncExecuteCallback(C.napi_async_execute_callback(C.gn_async_execute))
	AsyncCompleteTrampoline = AsyncCompleteCallback(C.napi_async_complete_callback(C.gn_async_complete))
	// CleanupHookTrampoline per AddEnvCleanupHook / RemoveEnvCleanupHook.
	CleanupHookTrampoline = CleanupHook(C.napi_cleanup_hook(C.gn_cleanup_hook))
	// AsyncCleanupHookTrampoline per AddAsyncCleanupHook.
	AsyncCleanupHookTrampoline = AsyncCleanupHook(C.napi_async_cleanup_hook(C.gn_async_cleanup_hook))
)

// inlineArgs deve coincidere con GN_INLINE_ARGS di internal.h.
const inlineArgs = C.GN_INLINE_ARGS

type (
	// ModuleInitHandler inizializza il modulo per un env (main thread o worker) e restituisce
	// l'oggetto delle export. Senza handler require() fallisce con ERR_NAPI_GO_NO_HANDLER e il
	// messaggio di Node.js per un modulo senza entry point.
	ModuleInitHandler func(env Env, exports Value) Value
	// CallbackHandler gestisce ogni chiamata a una funzione creata con CallbackTrampoline. args
	// è valido solo durante la chiamata. Restituire nil equivale a undefined.
	CallbackHandler func(env Env, info CallbackInfo, data uintptr, this Value, args []Value) Value
	// FinalizeHandler riceve data e hint passati insieme a FinalizeTrampoline.
	FinalizeHandler func(env Env, data, hint uintptr)
	// CallJSHandler riceve il context della TSFN e il data passato a CallThreadsafeFunction.
	// env e jsCallback sono nil se la TSFN viene distrutta con elementi in coda.
	CallJSHandler func(env Env, jsCallback Value, context, data uintptr)
	// AsyncExecuteHandler gira su un thread del pool di libuv: non può chiamare Node-API.
	AsyncExecuteHandler func(env Env, data uintptr)
	// AsyncCompleteHandler gira sul thread JS dopo AsyncExecuteHandler.
	AsyncCompleteHandler func(env Env, status Status, data uintptr)
	// CleanupHookHandler riceve l'arg passato ad AddEnvCleanupHook.
	CleanupHookHandler func(arg uintptr)
	// AsyncCleanupHookHandler riceve l'handle da passare a RemoveAsyncCleanupHook.
	AsyncCleanupHookHandler func(handle AsyncCleanupHookHandle, arg uintptr)
)

var (
	moduleInitHandler       atomic.Pointer[ModuleInitHandler]
	callbackHandler         atomic.Pointer[CallbackHandler]
	finalizeHandler         atomic.Pointer[FinalizeHandler]
	callJSHandler           atomic.Pointer[CallJSHandler]
	asyncExecuteHandler     atomic.Pointer[AsyncExecuteHandler]
	asyncCompleteHandler    atomic.Pointer[AsyncCompleteHandler]
	cleanupHookHandler      atomic.Pointer[CleanupHookHandler]
	asyncCleanupHookHandler atomic.Pointer[AsyncCleanupHookHandler]
)

// Gli handler vanno registrati in init(): il runtime Go completa l'inizializzazione dei pacchetti
// prima di eseguire qualsiasi ingresso da C. Un handler nil rimuove quello registrato.

// SetModuleInitHandler registra il ModuleInitHandler.
func SetModuleInitHandler(h ModuleInitHandler) {
	if h == nil {
		moduleInitHandler.Store(nil)
		return
	}
	moduleInitHandler.Store(&h)
}

// SetCallbackHandler registra il CallbackHandler.
func SetCallbackHandler(h CallbackHandler) {
	if h == nil {
		callbackHandler.Store(nil)
		return
	}
	callbackHandler.Store(&h)
}

// SetFinalizeHandler registra il FinalizeHandler.
func SetFinalizeHandler(h FinalizeHandler) {
	if h == nil {
		finalizeHandler.Store(nil)
		return
	}
	finalizeHandler.Store(&h)
}

// SetCallJSHandler registra il CallJSHandler.
func SetCallJSHandler(h CallJSHandler) {
	if h == nil {
		callJSHandler.Store(nil)
		return
	}
	callJSHandler.Store(&h)
}

// SetAsyncExecuteHandler registra il AsyncExecuteHandler.
func SetAsyncExecuteHandler(h AsyncExecuteHandler) {
	if h == nil {
		asyncExecuteHandler.Store(nil)
		return
	}
	asyncExecuteHandler.Store(&h)
}

// SetAsyncCompleteHandler registra il AsyncCompleteHandler.
func SetAsyncCompleteHandler(h AsyncCompleteHandler) {
	if h == nil {
		asyncCompleteHandler.Store(nil)
		return
	}
	asyncCompleteHandler.Store(&h)
}

// SetCleanupHookHandler registra il CleanupHookHandler.
func SetCleanupHookHandler(h CleanupHookHandler) {
	if h == nil {
		cleanupHookHandler.Store(nil)
		return
	}
	cleanupHookHandler.Store(&h)
}

// SetAsyncCleanupHookHandler registra il AsyncCleanupHookHandler.
func SetAsyncCleanupHookHandler(h AsyncCleanupHookHandler) {
	if h == nil {
		asyncCleanupHookHandler.Store(nil)
		return
	}
	asyncCleanupHookHandler.Store(&h)
}

// ModuleAPIVersion restituisce il valore che il modulo dichiara a Node.js tramite
// node_api_module_get_api_version_v1 (uguale a DeclaredVersion).
func ModuleAPIVersion() int32 { return int32(C.gn_module_api_version()) }

// noModuleInitMessage è il messaggio di Node.js (ERR_DLOPEN_FAILED) per un modulo senza funzione
// di init, usato quando non è registrato un ModuleInitHandler.
const noModuleInitMessage = "Module has no declared entry point."

// Codici degli errori JS lanciati da sys.
const (
	CodePanic          = "GO_PANIC"
	CodeNoHandler      = "ERR_NAPI_GO_NO_HANDLER"
	CodeForeignRuntime = "ERR_NAPI_GO_FOREIGN_RUNTIME"
	CodeMissingFuncs   = "ERR_NAPI_GO_MISSING_FUNCTIONS"
	CodeInternal       = "ERR_NAPI_GO_INTERNAL"
)

// cstring restituisce una copia di s terminata da NUL, da passare a Node-API per la durata di una
// chiamata.
func cstring(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// throwError lancia un Error JS con code e message. Se un'eccezione è già pendente resta quella.
func throwError(env Env, code, message string) {
	var pending bool
	if IsExceptionPending(env, &pending) == StatusOK && pending {
		return
	}
	if st := ThrowError(env, cstring(code), cstring(message)); st != StatusOK {
		// L'errore non arriva a JS: resta almeno su stderr.
		fmt.Fprintf(os.Stderr, "%s (napi_throw_error: %s)\n", message, st)
	}
}

func panicMessage(where string, r any) string {
	return fmt.Sprintf("napi-go: panic in %s: %v", where, r)
}

// fatal termina il processo: un panic dove non si può lanciare un'eccezione JS si comporta come
// un panic Go non recuperato.
func fatal(where string, r any) {
	msg := panicMessage(where, r)
	FatalError(cstring("napi-go"), AutoLength, cstring(msg), AutoLength)
}

//export goNapiRegisterModule
func goNapiRegisterModule(env C.napi_env, exports C.napi_value) (ret C.napi_value) {
	e := Env(env)
	defer func() {
		if r := recover(); r != nil {
			throwError(e, CodePanic, panicMessage("module init", r))
			ret = nil
		}
	}()
	h := moduleInitHandler.Load()
	if h == nil {
		// Come Node.js per un modulo senza funzione di init (napi_module con nm_register_func
		// NULL): require() fallisce invece di restituire un oggetto vuoto.
		throwError(e, CodeNoHandler, noModuleInitMessage)
		return nil
	}
	return C.napi_value((*h)(e, Value(exports)))
}

//export goNapiCallback
func goNapiCallback(env C.napi_env, info C.napi_callback_info, data C.uintptr_t, argc C.size_t,
	argv *C.napi_value, this C.napi_value) (ret C.napi_value) {
	e := Env(env)
	defer func() {
		if r := recover(); r != nil {
			throwError(e, CodePanic, panicMessage("callback", r))
			ret = nil
		}
	}()
	h := callbackHandler.Load()
	if h == nil {
		throwError(e, CodeNoHandler, "napi-go: no callback handler registered (sys.SetCallbackHandler)")
		return nil
	}
	n := uint(argc)
	var args []Value
	if n <= inlineArgs {
		args = unsafe.Slice((*Value)(unsafe.Pointer(argv)), n)
	} else {
		// Più argomenti del buffer letto in C: si rileggono tutti.
		args = make([]Value, n)
		if st := GetCbInfo(e, CallbackInfo(info), &n, &args[0], nil, nil); st != StatusOK {
			throwError(e, CodeInternal, "napi-go: napi_get_cb_info failed: "+st.String())
			return nil
		}
		args = args[:n]
	}
	return C.napi_value((*h)(e, CallbackInfo(info), uintptr(data), Value(this), args))
}

//export goNapiFinalize
func goNapiFinalize(env C.napi_env, data, hint C.uintptr_t) {
	defer func() {
		if r := recover(); r != nil {
			fatal("finalizer", r)
		}
	}()
	if h := finalizeHandler.Load(); h != nil {
		(*h)(Env(env), uintptr(data), uintptr(hint))
	}
}

//export goNapiCallJS
func goNapiCallJS(env C.napi_env, jsCallback C.napi_value, context, data C.uintptr_t) {
	e := Env(env)
	defer func() {
		if r := recover(); r != nil {
			if e == nil {
				fatal("threadsafe function", r)
			}
			throwError(e, CodePanic, panicMessage("threadsafe function", r))
		}
	}()
	if h := callJSHandler.Load(); h != nil {
		(*h)(e, Value(jsCallback), uintptr(context), uintptr(data))
	}
}

//export goNapiAsyncExecute
func goNapiAsyncExecute(env C.napi_env, data C.uintptr_t) {
	defer func() {
		if r := recover(); r != nil {
			fatal("async work", r)
		}
	}()
	if h := asyncExecuteHandler.Load(); h != nil {
		(*h)(Env(env), uintptr(data))
	}
}

//export goNapiAsyncComplete
func goNapiAsyncComplete(env C.napi_env, status C.napi_status, data C.uintptr_t) {
	e := Env(env)
	defer func() {
		if r := recover(); r != nil {
			throwError(e, CodePanic, panicMessage("async work completion", r))
		}
	}()
	if h := asyncCompleteHandler.Load(); h != nil {
		(*h)(e, Status(status), uintptr(data))
	}
}

//export goNapiCleanupHook
func goNapiCleanupHook(arg C.uintptr_t) {
	defer func() {
		if r := recover(); r != nil {
			fatal("cleanup hook", r)
		}
	}()
	if h := cleanupHookHandler.Load(); h != nil {
		(*h)(uintptr(arg))
	}
}

//export goNapiAsyncCleanupHook
func goNapiAsyncCleanupHook(handle C.napi_async_cleanup_hook_handle, arg C.uintptr_t) {
	defer func() {
		if r := recover(); r != nil {
			fatal("async cleanup hook", r)
		}
	}()
	if h := asyncCleanupHookHandler.Load(); h != nil {
		(*h)(AsyncCleanupHookHandle(handle), uintptr(arg))
	}
}
