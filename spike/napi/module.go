package napi

/*
#include "gonapi.h"
*/
import "C"

import (
	"context"
	"sync"
	"sync/atomic"
	"unsafe"
)

type exportDef struct {
	name string
	id   uintptr
}

var exportDefs []exportDef

// Export registra una funzione da esporre sull'oggetto `exports` del modulo.
// Va chiamata nelle init() del package main dell'addon: le init girano durante
// l'inizializzazione del runtime Go, che termina prima di napi_register_module_v1.
// Equivalente semantico di `#[napi] fn` in napi-rs (registro + materializzazione per env).
func Export(name string, fn Callback) {
	exportDefs = append(exportDefs, exportDef{name: name, id: registerCallback(fn)})
}

// ---- stato per env (main thread e ogni worker_thread) ----

type envState struct {
	id           uintptr
	jsThread     uint64 // thread OS del loop JS di questo env
	ctx          context.Context
	cancel       context.CancelFunc
	pending      int // operazioni async in corso (solo thread JS)
	dispatchRuns atomic.Uint64

	mu        sync.Mutex // protegge i campi sotto
	tsfn      C.napi_threadsafe_function
	closed    bool
	scheduled bool
	queue     []func(Env)
}

var envs struct {
	mu     sync.Mutex
	nextID uintptr
	byID   map[uintptr]*envState
	live   atomic.Int64
	total  atomic.Int64
}

func envStateByID(id uintptr) *envState {
	envs.mu.Lock()
	defer envs.mu.Unlock()
	return envs.byID[id]
}

// stateOf recupera lo stato dell'env corrente tramite napi_get_instance_data.
func stateOf(env C.napi_env) *envState {
	return envStateByID(uintptr(C.gn_get_instance_id(env)))
}

// EnvStats restituisce (env vivi, env creati in totale) — usato dai test sui worker.
func EnvStats() (live, total int64) { return envs.live.Load(), envs.total.Load() }

//export goNapiRegisterModule
func goNapiRegisterModule(env C.napi_env, exports C.napi_value) (ret C.napi_value) {
	defer recoverToJS(env, &ret)
	e := Env{env}

	envs.mu.Lock()
	if envs.byID == nil {
		envs.byID = make(map[uintptr]*envState)
	}
	envs.nextID++
	st := &envState{id: envs.nextID, jsThread: ThreadID()}
	envs.byID[st.id] = st
	envs.mu.Unlock()
	st.ctx, st.cancel = context.WithCancel(context.Background())
	envs.live.Add(1)
	envs.total.Add(1)

	if err := check(env, C.gn_set_instance_id(env, C.uintptr_t(st.id))); err != nil {
		e.Throw(err)
		return nil
	}
	// Dispatcher: una TSFN per env, senza funzione JS, non referenziata finché non
	// ci sono operazioni async in corso (non tiene vivo l'event loop).
	if err := check(env, C.gn_new_dispatcher_tsfn(env, C.uintptr_t(st.id), &st.tsfn)); err != nil {
		e.Throw(err)
		return nil
	}
	if err := check(env, C.gn_napi_unref_threadsafe_function(env, st.tsfn)); err != nil {
		e.Throw(err)
		return nil
	}
	if err := check(env, C.gn_add_cleanup_hook(env, C.uintptr_t(st.id))); err != nil {
		e.Throw(err)
		return nil
	}

	ex := Value{env, exports}
	for _, d := range exportDefs {
		var fn C.napi_value
		s := C.gn_new_function(env, (*C.char)(unsafe.Pointer(unsafe.StringData(d.name))), C.size_t(len(d.name)), C.uintptr_t(d.id), &fn)
		if err := check(env, s); err != nil {
			e.Throw(err)
			return nil
		}
		if err := ex.Set(d.name, Value{env, fn}); err != nil {
			e.Throw(err)
			return nil
		}
	}
	if err := materializeTyped(env, ex); err != nil {
		e.Throw(err)
		return nil
	}
	return exports
}

// goNapiEnvCleanup viene chiamata da Node alla distruzione dell'env (uscita del
// processo o terminazione di un worker), sul thread JS dell'env.
//
//export goNapiEnvCleanup
func goNapiEnvCleanup(id C.uintptr_t) {
	st := envStateByID(uintptr(id))
	if st == nil {
		return
	}
	st.cancel() // le goroutine async vedono ctx.Done()
	st.mu.Lock()
	st.closed = true
	st.queue = nil
	tsfn := st.tsfn
	st.tsfn = nil
	st.mu.Unlock()
	if tsfn != nil {
		C.gn_abort_tsfn(tsfn) // dopo questo nessuno chiama più la TSFN (closed sotto lock)
	}
	envs.mu.Lock()
	delete(envs.byID, st.id)
	envs.mu.Unlock()
	envs.live.Add(-1)
}
