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

// ErrEnvClosed è restituito quando l'env di destinazione è già stato distrutto.
var ErrEnvClosed = errors.New("napi: env closed")

// post accoda fn per l'esecuzione sul thread JS dell'env. Sicura da qualsiasi goroutine.
// Le notifiche sono coalescenti: una sola chiamata alla TSFN per "ondata" di risultati.
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
	// Chiamata sotto lock: il cleanup imposta closed e rilascia la TSFN sotto lo stesso
	// lock, quindi la TSFN non può essere usata dopo il rilascio.
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
	if env == nil { // TSFN in chiusura: scarta
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
			fmt.Fprintf(os.Stderr, "napi-go: panic nel dispatcher: %v\n", r)
		}
	}()
	_ = e.Scope(func() error { fn(e); return nil })
}

// ---- Promise risolte da goroutine ----

// Resolver converte il risultato di una goroutine in un valore JS; gira sul thread JS.
type Resolver func(env Env) (Value, error)

// Promise crea una Promise JS e avvia work in una nuova goroutine. work non ha accesso
// all'Env: restituisce un Resolver che verrà eseguito sul thread JS per convertire il
// risultato. ctx viene cancellato alla distruzione dell'env.
//
// Equivalente semantico: napi_create_promise + napi_threadsafe_function in napi-rs
// (`AsyncTask`/`async fn`), ma con un'unica TSFN condivisa per env.
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
	if st.pending == 1 { // c'è lavoro in corso: tieni vivo l'event loop
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
		// Se l'env è chiuso la Promise non verrà mai risolta: l'env non esiste più.
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

// Post esegue fn sul thread JS dell'env di c, da qualsiasi goroutine (API grezza dello
// spike: la Fase 3 la sostituirà con TSFN tipizzate).
func (c CallContext) Poster() func(func(Env)) error {
	st := stateOf(c.env)
	return func(fn func(Env)) error {
		if st == nil {
			return ErrEnvClosed
		}
		return st.post(fn)
	}
}

// JSThreadID restituisce il thread OS del loop JS dell'env (diagnostica).
func (c CallContext) JSThreadID() uint64 {
	if st := stateOf(c.env); st != nil {
		return st.jsThread
	}
	return 0
}

// DispatchRuns restituisce quante volte il dispatcher dell'env è stato eseguito.
func (c CallContext) DispatchRuns() uint64 {
	if st := stateOf(c.env); st != nil {
		return st.dispatchRuns.Load()
	}
	return 0
}
