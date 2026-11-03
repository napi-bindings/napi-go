// Port of test/node-api/test_threadsafe_function_shutdown/binding.cc. Detached std::threads
// become unjoined nt.Threads; the C++ call<> template becomes the call function.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// call corresponds to the C++ call<> template / NAPI_CALL: if status is not napi_ok, it prints
// "<name>: <status>" to stderr and calls abort().
func call(name string, status sys.Status) {
	if status == sys.StatusOK {
		return
	}
	// Write error is not checked: the process aborts immediately after.
	fmt.Fprintf(os.Stderr, "%s: %d\n", name, int(status))
	nt.Abort()
}

const (
	nThreads = 32
	// callInterval is the sleep_for(1ms) between calls.
	callInterval = time.Millisecond
)

var runData = nt.Register(run)

// logf writes to stderr like C's fprintf; a write error does not affect the test outcome,
// which only checks the exit code.
func logf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		return
	}
}

func threadFunc(tsfn sys.ThreadsafeFunction) {
	logf("thread_func: starting\n")
	status := sys.CallThreadsafeFunction(tsfn, 0, sys.TSFNBlocking)
	for status == sys.StatusOK {
		time.Sleep(callInterval)
		status = sys.CallThreadsafeFunction(tsfn, 0, sys.TSFNBlocking)
	}
	logf("thread_func: Got status %d, exiting...\n", int(status))
}

func tsfnCallback(env sys.Env, _ sys.Value, _, _ uintptr) {
	if env == nil {
		logf("tsfn_callback: env=%p\n", env)
	}
}

func tsfnFinalize(env sys.Env, _, _ uintptr) {
	logf("tsfn_finalize: env=%p\n", env)
}

func run(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var global, undefined sys.Value
	call("napi_get_global", sys.GetGlobal(env, &global))
	call("napi_get_undefined", sys.GetUndefined(env, &undefined))
	callJS, finalize, context := nt.ThreadsafeFunction(tsfnCallback, tsfnFinalize, 0)
	var tsfn sys.ThreadsafeFunction
	call("napi_create_threadsafe_function", sys.CreateThreadsafeFunction(env, nil, global, undefined, 0, nThreads,
		0, finalize, context, callJS, &tsfn))
	for i := 0; i < nThreads; i++ {
		var t nt.Thread
		t.Create(func() { threadFunc(tsfn) })
	}
	call("napi_unref_threadsafe_function", sys.UnrefThreadsafeFunction(env, tsfn))
	var result sys.Value
	call("napi_get_undefined", sys.GetUndefined(env, &result))
	return result
}

func initModule(env sys.Env, _ sys.Value) sys.Value {
	var fn sys.Value
	call("napi_create_function", sys.CreateFunction(env, nil, 0, sys.CallbackTrampoline, runData, &fn))
	return fn
}
