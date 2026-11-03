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

// Export registers a function to be exposed on the module's `exports` object.
// Must be called from init() in the addon's main package: init functions run during
// Go runtime initialization, which completes before napi_register_module_v1.
// Semantic equivalent of `#[napi] fn` in napi-rs (registry + per-env materialization).
func Export(name string, fn Callback) {
	exportDefs = append(exportDefs, exportDef{name: name, id: registerCallback(fn)})
}

// ---- per-env state (main thread and each worker_thread) ----

type envState struct {
	id           uintptr
	jsThread     uint64 // OS thread ID of this env's JS event loop
	ctx          context.Context
	cancel       context.CancelFunc
	pending      int // in-flight async operations (JS thread only)
	dispatchRuns atomic.Uint64

	mu        sync.Mutex // protects the fields below
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

// stateOf retrieves the current env's state via napi_get_instance_data.
func stateOf(env C.napi_env) *envState {
	return envStateByID(uintptr(C.gn_get_instance_id(env)))
}

// EnvStats returns (live envs, total envs created) — used by worker tests.
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
	// Dispatcher: one TSFN per env, with no JS function, unreferenced as long as
	// there are no in-flight async operations (does not keep the event loop alive).
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

// goNapiEnvCleanup is called by Node when the env is destroyed (process exit or
// worker termination), on the env's JS thread.
//
//export goNapiEnvCleanup
func goNapiEnvCleanup(id C.uintptr_t) {
	st := envStateByID(uintptr(id))
	if st == nil {
		return
	}
	st.cancel() // async goroutines observe ctx.Done()
	st.mu.Lock()
	st.closed = true
	st.queue = nil
	tsfn := st.tsfn
	st.tsfn = nil
	st.mu.Unlock()
	if tsfn != nil {
		C.gn_abort_tsfn(tsfn) // after this no one calls the TSFN anymore (closed under lock)
	}
	envs.mu.Lock()
	delete(envs.byID, st.id)
	envs.mu.Unlock()
	envs.live.Add(-1)
}
