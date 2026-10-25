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

// Callback è una funzione Go invocabile da JavaScript. CallContext è passato per
// valore: un puntatore passato a una func value sfuggirebbe sull'heap a ogni chiamata.
type Callback func(c CallContext) (Value, error)

// CallContext descrive una singola invocazione (equivalente di napi_callback_info /
// Napi::CallbackInfo). Gli argomenti puntano allo stack C del trampolino: validi solo
// durante la callback.
type CallContext struct {
	env  C.napi_env
	this C.napi_value
	argc int
	argv []C.napi_value
}

func (c CallContext) Env() Env    { return Env{c.env} }
func (c CallContext) Len() int    { return c.argc }
func (c CallContext) This() Value { return Value{c.env, c.this} }

// Arg restituisce l'i-esimo argomento, `undefined` se assente.
func (c CallContext) Arg(i int) Value {
	if i < len(c.argv) {
		return Value{c.env, c.argv[i]} // Node riempie gli slot inline mancanti con undefined
	}
	return Env{c.env}.Undefined()
}

// ---- registro delle callback ----
//
// Il `data` passato a napi_create_function è un indice (id >= 1) in questo slice,
// mai un puntatore Go. Lettura lock-free (copy-on-write), scrittura rara.

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
	return uintptr(len(next)) // id = indice + 1
}

func lookupCallback(id uintptr) Callback {
	p := registry.entries.Load()
	if p == nil || id == 0 || id > uintptr(len(*p)) {
		return nil
	}
	return (*p)[id-1]
}

// NewFunction crea una funzione JS che invoca fn.
// (Nello spike il registro è solo in crescita: la Fase 2 aggiungerà il rilascio
// dello slot tramite finalizer per le funzioni create dinamicamente.)
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
		Env{env}.Throw(&Error{Code: "GO_NAPI_INTERNAL", Message: "callback non registrata"})
		return nil
	}
	n := int(argc)
	var args []C.napi_value
	if n <= C.GN_INLINE_ARGS {
		args = unsafe.Slice(argv, C.GN_INLINE_ARGS) // vista sullo stack C del trampolino
	} else {
		args = make([]C.napi_value, n) // raro: più argomenti del buffer inline
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

// recoverToJS impedisce che un panic Go attraversi i frame C (abort del processo):
// lo trasforma in un Error JS con code "GO_PANIC".
func recoverToJS(env C.napi_env, ret *C.napi_value) {
	if r := recover(); r != nil {
		throwPanic(env, r)
		*ret = nil
	}
}

func throwPanic(env C.napi_env, r any) {
	Env{env}.Throw(&Error{Code: "GO_PANIC", Message: fmt.Sprint(r)})
}
