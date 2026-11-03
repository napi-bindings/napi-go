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

// C trampolines to pass to Node-API. They enter Go through the handlers registered below.
var (
	// CallbackTrampoline is used with CreateFunction, DefineClass, and PropertyDescriptor:
	// it invokes the registered CallbackHandler with the function's data payload.
	CallbackTrampoline = Callback(C.napi_callback(C.gn_callback))
	// FinalizeTrampoline is used with Wrap, AddFinalizer, CreateExternal*, SetInstanceData, ...
	FinalizeTrampoline = Finalize(C.napi_finalize(C.gn_finalize))
	// CallJSTrampoline is used with CreateThreadsafeFunction.
	CallJSTrampoline = ThreadsafeFunctionCallJS(C.napi_threadsafe_function_call_js(C.gn_call_js))
	// AsyncExecuteTrampoline and AsyncCompleteTrampoline are used with CreateAsyncWork.
	AsyncExecuteTrampoline  = AsyncExecuteCallback(C.napi_async_execute_callback(C.gn_async_execute))
	AsyncCompleteTrampoline = AsyncCompleteCallback(C.napi_async_complete_callback(C.gn_async_complete))
	// CleanupHookTrampoline is used with AddEnvCleanupHook / RemoveEnvCleanupHook.
	CleanupHookTrampoline = CleanupHook(C.napi_cleanup_hook(C.gn_cleanup_hook))
	// AsyncCleanupHookTrampoline is used with AddAsyncCleanupHook.
	AsyncCleanupHookTrampoline = AsyncCleanupHook(C.napi_async_cleanup_hook(C.gn_async_cleanup_hook))
)

// inlineArgs must match GN_INLINE_ARGS in internal.h.
const inlineArgs = C.GN_INLINE_ARGS

type (
	// ModuleInitHandler initializes the module for an env (main thread or worker) and returns
	// the exports object. Without a handler, require() fails with ERR_NAPI_GO_NO_HANDLER and
	// Node.js's standard message for a module without an entry point.
	ModuleInitHandler func(env Env, exports Value) Value
	// CallbackHandler handles every call to a function created with CallbackTrampoline. args
	// is valid only for the duration of the call. Returning nil is equivalent to returning undefined.
	CallbackHandler func(env Env, info CallbackInfo, data uintptr, this Value, args []Value) Value
	// FinalizeHandler receives the data and hint pointers passed alongside FinalizeTrampoline.
	FinalizeHandler func(env Env, data, hint uintptr)
	// CallJSHandler receives the TSFN context and the data passed to CallThreadsafeFunction.
	// env and jsCallback are nil when the TSFN is being torn down with queued items remaining.
	CallJSHandler func(env Env, jsCallback Value, context, data uintptr)
	// AsyncExecuteHandler runs on a libuv worker pool thread and must not call Node-API functions.
	AsyncExecuteHandler func(env Env, data uintptr)
	// AsyncCompleteHandler runs on the JS thread after AsyncExecuteHandler completes.
	AsyncCompleteHandler func(env Env, status Status, data uintptr)
	// CleanupHookHandler receives the arg passed to AddEnvCleanupHook.
	CleanupHookHandler func(arg uintptr)
	// AsyncCleanupHookHandler receives the handle to pass to RemoveAsyncCleanupHook.
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

// Handlers should be registered in init(): the Go runtime finishes initializing all packages
// before executing any entry from C. Passing a nil handler clears the registered handler.

// SetModuleInitHandler registers the ModuleInitHandler.
func SetModuleInitHandler(h ModuleInitHandler) {
	if h == nil {
		moduleInitHandler.Store(nil)
		return
	}
	moduleInitHandler.Store(&h)
}

// SetCallbackHandler registers the CallbackHandler.
func SetCallbackHandler(h CallbackHandler) {
	if h == nil {
		callbackHandler.Store(nil)
		return
	}
	callbackHandler.Store(&h)
}

// SetFinalizeHandler registers the FinalizeHandler.
func SetFinalizeHandler(h FinalizeHandler) {
	if h == nil {
		finalizeHandler.Store(nil)
		return
	}
	finalizeHandler.Store(&h)
}

// SetCallJSHandler registers the CallJSHandler.
func SetCallJSHandler(h CallJSHandler) {
	if h == nil {
		callJSHandler.Store(nil)
		return
	}
	callJSHandler.Store(&h)
}

// SetAsyncExecuteHandler registers the AsyncExecuteHandler.
func SetAsyncExecuteHandler(h AsyncExecuteHandler) {
	if h == nil {
		asyncExecuteHandler.Store(nil)
		return
	}
	asyncExecuteHandler.Store(&h)
}

// SetAsyncCompleteHandler registers the AsyncCompleteHandler.
func SetAsyncCompleteHandler(h AsyncCompleteHandler) {
	if h == nil {
		asyncCompleteHandler.Store(nil)
		return
	}
	asyncCompleteHandler.Store(&h)
}

// SetCleanupHookHandler registers the CleanupHookHandler.
func SetCleanupHookHandler(h CleanupHookHandler) {
	if h == nil {
		cleanupHookHandler.Store(nil)
		return
	}
	cleanupHookHandler.Store(&h)
}

// SetAsyncCleanupHookHandler registers the AsyncCleanupHookHandler.
func SetAsyncCleanupHookHandler(h AsyncCleanupHookHandler) {
	if h == nil {
		asyncCleanupHookHandler.Store(nil)
		return
	}
	asyncCleanupHookHandler.Store(&h)
}

// ModuleAPIVersion returns the version value reported by the module to Node.js via
// node_api_module_get_api_version_v1 (equal to DeclaredVersion).
func ModuleAPIVersion() int32 { return int32(C.gn_module_api_version()) }

// noModuleInitMessage is Node.js's standard message (ERR_DLOPEN_FAILED) for a module without an
// init function, used when no ModuleInitHandler is registered.
const noModuleInitMessage = "Module has no declared entry point."

// Error codes for JS errors thrown by sys.
const (
	CodePanic          = "GO_PANIC"
	CodeNoHandler      = "ERR_NAPI_GO_NO_HANDLER"
	CodeForeignRuntime = "ERR_NAPI_GO_FOREIGN_RUNTIME"
	CodeMissingFuncs   = "ERR_NAPI_GO_MISSING_FUNCTIONS"
	CodeInternal       = "ERR_NAPI_GO_INTERNAL"
)

// cstring returns a NUL-terminated copy of s suitable for passing to Node-API for the duration of
// a single call.
func cstring(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// throwError throws a JS Error with code and message. If an exception is already pending, it is
// preserved.
func throwError(env Env, code, message string) {
	var pending bool
	if IsExceptionPending(env, &pending) == StatusOK && pending {
		return
	}
	if st := ThrowError(env, cstring(code), cstring(message)); st != StatusOK {
		// If the error cannot be delivered to JS, at least log it to stderr.
		fmt.Fprintf(os.Stderr, "%s (napi_throw_error: %s)\n", message, st)
	}
}

func panicMessage(where string, r any) string {
	return fmt.Sprintf("napi-go: panic in %s: %v", where, r)
}

// fatal terminates the process: a panic in a context where a JS exception cannot be thrown
// behaves like an unrecovered Go panic.
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
		// Match Node.js behavior for a module without an init function (napi_module with
		// nm_register_func == NULL): require() fails instead of returning an empty exports object.
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
		// More arguments than fit in the C stack buffer: re-fetch all of them.
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
