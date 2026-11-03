package napi

/*
#include "gonapi.h"
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Callback is a Go function callable from JavaScript. CallContext is passed by
// value: a pointer passed to a func value would escape to the heap on every call.
type Callback func(c CallContext) (Value, error)

// CallContext describes a single invocation (equivalent of napi_callback_info /
// Napi::CallbackInfo). Arguments point to the C stack of the trampoline: valid only
// during the callback.
type CallContext struct {
	env  C.napi_env
	this C.napi_value
	argc int
	argv []C.napi_value
}

func (c CallContext) Env() Env    { return Env{c.env} }
func (c CallContext) Len() int    { return c.argc }
func (c CallContext) This() Value { return Value{c.env, c.this} }

// Arg returns the i-th argument, or `undefined` if absent.
func (c CallContext) Arg(i int) Value {
	if i < len(c.argv) {
		return Value{c.env, c.argv[i]} // Node fills missing inline slots with undefined
	}
	return Env{c.env}.Undefined()
}

// ---- callback registry ----
//
// The `data` passed to napi_create_function is an index (id >= 1) into this slice,
// never a Go pointer. Lock-free reads (copy-on-write), rare writes.

var registry struct {
	mu      sync.Mutex
	entries atomic.Pointer[[]Callback]
}

func registerCallback(fn Callback) uintptr {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	var cur []Callback
	if p := registry.entries.Load(); p != nil {
		cur = *p
	}
	next := make([]Callback, len(cur), len(cur)+1)
	copy(next, cur)
	next = append(next, fn)
	registry.entries.Store(&next)
	return uintptr(len(next)) // id = index + 1
}

func lookupCallback(id uintptr) Callback {
	p := registry.entries.Load()
	if p == nil || id == 0 || id > uintptr(len(*p)) {
		return nil
	}
	return (*p)[id-1]
}

// NewFunction creates a JS function that invokes fn.
// (In the spike the registry is grow-only: Phase 2 will release slots via a
// finalizer for dynamically created functions.)
func (e Env) NewFunction(name string, fn Callback) (Value, error) {
	id := registerCallback(fn)
	var r C.napi_value
	s := C.gn_new_function(e.raw, (*C.char)(unsafe.Pointer(unsafe.StringData(name))), C.size_t(len(name)), C.uintptr_t(id), &r)
	return Value{e.raw, r}, check(e.raw, s)
}

//export goNapiCallback
func goNapiCallback(env C.napi_env, info C.napi_callback_info, id C.uintptr_t,
	argc C.size_t, argv *C.napi_value, self C.napi_value) (ret C.napi_value) {
	defer recoverToJS(env, &ret)

	fn := lookupCallback(uintptr(id))
	if fn == nil {
		Env{env}.Throw(&Error{Code: "GO_NAPI_INTERNAL", Message: "unregistered callback"})
		return nil
	}
	n := int(argc)
	var args []C.napi_value
	if n <= C.GN_INLINE_ARGS {
		args = unsafe.Slice(argv, C.GN_INLINE_ARGS) // view into the C stack of the trampoline
	} else {
		args = make([]C.napi_value, n) // rare: more arguments than the inline buffer
		count := C.size_t(n)
		if err := check(env, C.gn_napi_get_cb_info(env, info, &count, &args[0], nil, nil)); err != nil {
			Env{env}.Throw(err)
			return nil
		}
	}
	v, err := fn(CallContext{env: env, this: self, argc: n, argv: args})
	if err != nil {
		Env{env}.Throw(err)
		return nil
	}
	return v.raw
}

// recoverToJS prevents a Go panic from crossing C frames (which would abort the process):
// it converts the panic into a JS Error with code "GO_PANIC".
func recoverToJS(env C.napi_env, ret *C.napi_value) {
	if r := recover(); r != nil {
		throwPanic(env, r)
		*ret = nil
	}
}

func throwPanic(env C.napi_env, r any) {
	Env{env}.Throw(&Error{Code: "GO_PANIC", Message: fmt.Sprint(r)})
}
