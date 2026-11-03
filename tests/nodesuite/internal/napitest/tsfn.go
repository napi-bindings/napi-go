package napitest

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// CallJSFunc corresponds to napi_threadsafe_function_call_js. env and jsCallback are nil when the
// TSFN is torn down with items still queued.
type CallJSFunc func(env sys.Env, jsCallback sys.Value, context, data uintptr)

type threadsafeFunction struct {
	callJS   CallJSFunc
	finalize FinalizeFunc
	context  uintptr
}

// ThreadsafeFunction prepares the three napi_create_threadsafe_function arguments that in C are
// functions or the context: call_js_cb, thread_finalize_cb, and context. callJS and finalize may
// be nil as in C; context is the C test's context, which callJS, finalize, and
// GetThreadsafeFunctionContext receive unchanged.
//
// thread_finalize_cb is always sys.FinalizeTrampoline, even when finalize is nil (in which case it
// is a no-op). The token returned as context is never released: Node-API calls call_js_cb with a
// NULL env for items remaining in the queue even after thread_finalize_cb and does not signal when
// it is done, so there is no safe point to release it. That is one handle per TSFN, which is
// acceptable in tests. If napi_create_threadsafe_function fails, release it with
// DiscardThreadsafeFunction.
//
// When callJS is nil, call_js_cb is NULL and Node-API calls the JS function with no arguments.
func ThreadsafeFunction(callJS CallJSFunc, finalize FinalizeFunc,
	context uintptr) (sys.ThreadsafeFunctionCallJS, sys.Finalize, uintptr) {
	token := uintptr(cgo.NewHandle(&threadsafeFunction{callJS: callJS, finalize: finalize, context: context}))
	var callJSCb sys.ThreadsafeFunctionCallJS
	if callJS != nil {
		callJSCb = sys.CallJSTrampoline
	}
	return callJSCb, sys.FinalizeTrampoline, token
}

// DiscardThreadsafeFunction releases the ThreadsafeFunction context for a TSFN that was never created.
func DiscardThreadsafeFunction(token uintptr) {
	cgo.Handle(token).Delete()
}

// GetThreadsafeFunctionContext corresponds to napi_get_threadsafe_function_context: returns the
// original context passed to ThreadsafeFunction.
func GetThreadsafeFunctionContext(fn sys.ThreadsafeFunction, result *uintptr) sys.Status {
	var p unsafe.Pointer
	if st := sys.GetThreadsafeFunctionContext(fn, &p); st != sys.StatusOK {
		return st
	}
	*result = cgo.Handle(uintptr(p)).Value().(*threadsafeFunction).context
	return sys.StatusOK
}

func dispatchCallJS(env sys.Env, jsCallback sys.Value, context, data uintptr) {
	t := cgo.Handle(context).Value().(*threadsafeFunction)
	t.callJS(env, jsCallback, t.context, data)
}
