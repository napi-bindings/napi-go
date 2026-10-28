// Porting di test/node-api/test_threadsafe_function_abort/binding.cc. La classe C++ Context
// diventa la struct tsfnContext, passata come context della TSFN tramite cgo.Handle; il template
// call<> del C++ diventa la funzione call.
package main

import (
	"fmt"
	"os"
	"runtime/cgo"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// call equivale al template call<> / NAPI_CALL del C++: se status non è napi_ok stampa
// "<nome>: <status>" su stderr e chiama abort().
func call(name string, status sys.Status) {
	if status == sys.StatusOK {
		return
	}
	// L'errore della scrittura non è controllato: il processo termina subito dopo.
	fmt.Fprintf(os.Stderr, "%s: %d\n", name, int(status))
	nt.Abort()
}

type contextState int

const (
	stateCreated contextState = iota
	stateCalled
)

type tsfnContext struct {
	state contextState
}

// initialThreadCount è l'initial_thread_count della TSFN.
const initialThreadCount = 1

var runData = nt.Register(run)

func tsfnCallback(_ sys.Env, _ sys.Value, ctxP, _ uintptr) {
	ctx := cgo.Handle(ctxP).Value().(*tsfnContext)
	nt.CAssert(ctx.state == stateCreated, "ctx->state == Context::State::kCreated")
	ctx.state = stateCalled
}

func tsfnFinalize(_ sys.Env, _, finalizeHint uintptr) {
	h := cgo.Handle(finalizeHint)
	ctx := h.Value().(*tsfnContext)
	nt.CAssert(ctx.state == stateCalled, "ctx->state == Context::State::kCalled")
	h.Delete()
}

func run(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var global, undefined sys.Value
	call("napi_get_global", sys.GetGlobal(env, &global))
	call("napi_get_undefined", sys.GetUndefined(env, &undefined))
	ctx := cgo.NewHandle(&tsfnContext{state: stateCreated})
	callJS, finalize, context := nt.ThreadsafeFunction(tsfnCallback, tsfnFinalize, uintptr(ctx))
	var tsfn sys.ThreadsafeFunction
	call("napi_create_threadsafe_function", sys.CreateThreadsafeFunction(env, nil, global, undefined, 0,
		initialThreadCount, 0, finalize, context, callJS, &tsfn))
	call("napi_call_threadsafe_function", sys.CallThreadsafeFunction(tsfn, 0, sys.TSFNBlocking))
	call("napi_unref_threadsafe_function", sys.UnrefThreadsafeFunction(env, tsfn))
	call("napi_release_threadsafe_function", sys.ReleaseThreadsafeFunction(tsfn, sys.TSFNAbort))
	var result sys.Value
	call("napi_get_undefined", sys.GetUndefined(env, &result))
	return result
}

func initModule(env sys.Env, _ sys.Value) sys.Value {
	var fn sys.Value
	call("napi_create_function", sys.CreateFunction(env, nil, 0, sys.CallbackTrampoline, runData, &fn))
	return fn
}
