package napi

/*
#include "gonapi.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ErrEnvClosed is returned when the target env has already been destroyed.
var ErrEnvClosed = errors.New("napi: env closed")

// post enqueues fn for execution on the env's JS thread. Safe from any goroutine.
// Notifications are coalesced: at most one TSFN call per batch of queued items.
func (st *envState) post(fn func(Env)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return ErrEnvClosed
	}
	st.queue = append(st.queue, fn)
	if st.scheduled {
		return nil
	}
	st.scheduled = true
	// Called under lock: cleanup sets closed and releases the TSFN under the same
	// lock, so the TSFN can never be called after release.
	if s := C.gn_call_tsfn(st.tsfn); s != C.napi_ok {
		st.scheduled = false
		return &Error{Status: Status(s), Message: "napi_call_threadsafe_function failed"}
	}
	return nil
}

//export goNapiDispatch
func goNapiDispatch(env C.napi_env, id C.uintptr_t) {
	st := envStateByID(uintptr(id))
	if st == nil {
		return
	}
	st.mu.Lock()
	q := st.queue
	st.queue = nil
	st.scheduled = false
	st.mu.Unlock()
	if env == nil { // TSFN closing: discard
		return
	}
	st.dispatchRuns.Add(1)
	e := Env{env}
	for _, fn := range q {
		runOnJSThread(e, fn)
	}
}

func runOnJSThread(e Env, fn func(Env)) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "napi-go: panic in dispatcher: %v\n", r)
		}
	}()
	_ = e.Scope(func() error { fn(e); return nil })
}

// ---- Promises resolved from goroutines ----

// Resolver converts the result of a goroutine into a JS value; runs on the JS thread.
type Resolver func(env Env) (Value, error)

// Promise creates a JS Promise and runs work in a new goroutine. work does not have
// access to Env: it returns a Resolver that will run on the JS thread to convert the
// result. ctx is canceled when the env is destroyed.
//
// Semantic equivalent: napi_create_promise + napi_threadsafe_function in napi-rs
// (`AsyncTask`/`async fn`), but using a single shared TSFN per env.
func (c CallContext) Promise(work func(ctx context.Context) (Resolver, error)) (Value, error) {
	env := c.env
	st := stateOf(env)
	if st == nil {
		return Value{}, &Error{Code: "GO_NAPI_INTERNAL", Message: "env state not found"}
	}
	var deferred C.napi_deferred
	var promise C.napi_value
	if err := check(env, C.gn_napi_create_promise(env, &deferred, &promise)); err != nil {
		return Value{}, err
	}
	st.pending++
	if st.pending == 1 { // work in flight: keep the event loop alive
		C.gn_napi_ref_threadsafe_function(env, st.tsfn)
	}
	go func() {
		res, err := safeWork(st.ctx, work)
		_ = st.post(func(e Env) {
			settle(e, deferred, res, err)
			st.pending--
			if st.pending == 0 {
				C.gn_napi_unref_threadsafe_function(e.raw, st.tsfn)
			}
		})
		// If the env is closed the Promise will never settle: the env no longer exists.
	}()
	return Value{env, promise}, nil
}

func safeWork(ctx context.Context, work func(context.Context) (Resolver, error)) (res Resolver, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &Error{Code: "GO_PANIC", Message: fmt.Sprint(r)}
		}
	}()
	return work(ctx)
}

func settle(e Env, d C.napi_deferred, res Resolver, err error) {
	if err == nil && res != nil {
		var v Value
		v, err = res(e)
		if err == nil {
			C.gn_napi_resolve_deferred(e.raw, d, v.raw)
			return
		}
	} else if err == nil {
		C.gn_napi_resolve_deferred(e.raw, d, e.Undefined().raw)
		return
	}
	ev, cerr := e.NewError(err)
	if cerr != nil {
		ev = e.Undefined()
	}
	C.gn_napi_reject_deferred(e.raw, d, ev.raw)
}

// Poster returns a function that schedules fn on c's env JS thread from any goroutine
// (raw spike API: Phase 3 will replace this with typed TSFNs).
func (c CallContext) Poster() func(func(Env)) error {
	st := stateOf(c.env)
	return func(fn func(Env)) error {
		if st == nil {
			return ErrEnvClosed
		}
		return st.post(fn)
	}
}

// JSThreadID returns the OS thread ID of the env's JS event loop (diagnostics).
func (c CallContext) JSThreadID() uint64 {
	if st := stateOf(c.env); st != nil {
		return st.jsThread
	}
	return 0
}

// DispatchRuns returns how many times the env's dispatcher has run.
func (c CallContext) DispatchRuns() uint64 {
	if st := stateOf(c.env); st != nil {
		return st.dispatchRuns.Load()
	}
	return 0
}
