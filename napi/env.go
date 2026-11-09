package napi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Env represents the Node-API environment of a callback (napi_env, Napi::Env). It is valid only on
// the JavaScript thread that provided it: do not pass it to goroutines. An Env remains the same
// for the entire lifetime of its environment (main thread or worker_thread), so it may be stored
// alongside a Ref, provided it is only used from callbacks on that same thread.
type Env struct{ raw sys.Env }

// EnvFromRaw wraps a raw sys.Env (for code that mixes napi and sys).
func EnvFromRaw(raw sys.Env) Env { return Env{raw: raw} }

// Raw returns the underlying sys.Env.
func (e Env) Raw() sys.Env { return e.raw }

// InitFunc initializes the module for an env and returns the exports value. Returning an empty
// Value leaves exports unchanged; returning any other value replaces it (like module.exports = ...).
type InitFunc func(env Env, exports Object) (Value, error)

var inits struct {
	mu  sync.Mutex
	fns []InitFunc
}

// Init registers a module initialization function. Call it from init(): Node invokes the
// registered functions in registration order for every env that loads the addon (including in
// worker_threads). Each function receives the exports returned by the previous one. Returning an
// error causes require() to fail. If no function is registered, require() fails with Node.js's
// message for a module without an entry point.
func Init(fn InitFunc) {
	if fn == nil {
		panic("napi: Init(nil)")
	}
	inits.mu.Lock()
	inits.fns = append(inits.fns, fn)
	inits.mu.Unlock()
}

// noEntryPointMessage is Node.js's error message for a module with no init function.
const noEntryPointMessage = "Module has no declared entry point."

// envState holds the per-env state for napi, reachable from Node-API via napi_instance_data.
type envState struct {
	env               sys.Env
	instanceData      any
	instanceFinalizer FinalizeFunc

	// Concurrency (dispatcher.go): the dispatcher and its threadsafe function, the env context
	// and the number of holds keeping the event loop alive (JS thread only).
	dispatcher *dispatcher
	tsfn       sys.ThreadsafeFunction
	ctx        context.Context
	cancel     context.CancelFunc
	holds      int
}

var envStates table[envState]

func registerModule(env sys.Env, exports sys.Value) sys.Value {
	inits.mu.Lock()
	fns := inits.fns
	inits.mu.Unlock()
	if len(fns) == 0 {
		throwError(env, &Error{Code: sys.CodeNoHandler, Message: noEntryPointMessage})
		return nil
	}
	if err := attachEnvState(env); err != nil {
		throwError(env, err)
		return nil
	}
	e := Env{raw: env}
	ex := Value{env: env, raw: exports}
	for _, fn := range fns {
		v, err := fn(e, Object{ex})
		if err != nil {
			throwError(env, err)
			return nil
		}
		if v.raw != nil {
			ex = v
		}
	}
	return ex.raw
}

// attachEnvState creates the env state and attaches it via napi_set_instance_data: Node invokes
// the finalizer when the env is destroyed (on process exit or worker termination).
func attachEnvState(env sys.Env) error {
	st := &envState{env: env}
	if err := startDispatcher(env, st); err != nil {
		return err
	}
	id := envStates.add(st)
	fid := handles.add(&handle{onFinalize: func(e Env, _ any) {
		s := envStates.remove(id)
		if s == nil {
			return
		}
		s.closeEnv(env)
		if s.instanceFinalizer != nil {
			s.instanceFinalizer(e, s.instanceData)
		}
	}})
	if err := check(env, sys.SetInstanceData(env, id, sys.FinalizeTrampoline, fid)); err != nil {
		handles.remove(fid)
		envStates.remove(id)
		// Node finalizes the dispatcher's threadsafe function with the env.
		st.closeEnv(env)
		return err
	}
	return nil
}

// state returns the env's state.
func (e Env) state() (*envState, error) {
	var p unsafe.Pointer
	if err := check(e.raw, sys.GetInstanceData(e.raw, &p)); err != nil {
		return nil, err
	}
	st := envStates.get(uintptr(p))
	if st == nil {
		return nil, &Error{Code: sys.CodeInternal, Message: "env state not found (instance data replaced through sys?)"}
	}
	return st, nil
}

// SetInstanceData associates v with the env (napi_set_instance_data). Each env (main thread,
// worker) has its own instance data. If fin is non-nil, it is called when the env is destroyed
// with the last value set; as in Node-API, replacing the value does not invoke the previous
// finalizer.
func (e Env) SetInstanceData(v any, fin FinalizeFunc) error {
	st, err := e.state()
	if err != nil {
		return err
	}
	st.instanceData, st.instanceFinalizer = v, fin
	return nil
}

// InstanceData returns the value set with SetInstanceData (or nil if none was set).
func (e Env) InstanceData() (any, error) {
	st, err := e.state()
	if err != nil {
		return nil, err
	}
	return st.instanceData, nil
}

// handle represents a Go value passed to Node-API via an ID (napi_wrap, external, finalizer,
// cleanup hook). onFinalize, if non-nil, is called when Node releases the ID.
type handle struct {
	value      any
	onFinalize FinalizeFunc
}

// FinalizeFunc is called when Node-API releases the Go value v (when the JS object is garbage
// collected or the env is destroyed). It runs on the JS thread; with the napi_go_experimental
// build tag it may run during V8's GC, in which case it must not call functions that execute JS.
type FinalizeFunc func(env Env, v any)

var handles table[handle]

func init() {
	sys.SetModuleInitHandler(registerModule)
	sys.SetCallbackHandler(dispatchCallback)
	sys.SetFinalizeHandler(dispatchFinalize)
	sys.SetCleanupHookHandler(dispatchCleanupHook)
}

// dispatchFinalize receives the hint (an ID in handles) for all napi finalizers, except those of
// threadsafe functions, whose hint is their context (see tsfnEntry).
func dispatchFinalize(env sys.Env, data, hint uintptr) {
	if data == tsfnFinalizeData {
		finalizeTSFN(env, hint)
		return
	}
	h := handles.remove(hint)
	if h != nil && h.onFinalize != nil {
		h.onFinalize(Env{raw: env}, h.value)
	}
}

// Undefined returns the JS undefined value.
func (e Env) Undefined() Value {
	var v sys.Value
	// napi_get_undefined only fails with an invalid env.
	sys.GetUndefined(e.raw, &v)
	return Value{env: e.raw, raw: v}
}

// Null returns the JS null value.
func (e Env) Null() Value {
	var v sys.Value
	sys.GetNull(e.raw, &v)
	return Value{env: e.raw, raw: v}
}

// Global returns the JS global object.
func (e Env) Global() Object {
	var v sys.Value
	sys.GetGlobal(e.raw, &v)
	return Object{Value{env: e.raw, raw: v}}
}

// RunScript compiles and executes a string of JavaScript code (napi_run_script).
func (e Env) RunScript(source string) (Value, error) {
	s, err := e.NewString(source)
	if err != nil {
		return Value{}, err
	}
	var r sys.Value
	st := sys.RunScript(e.raw, s.raw, &r)
	return e.value(r, st)
}

// NapiVersion returns the highest Node-API version supported by the Node.js runtime.
func (e Env) NapiVersion() (uint32, error) {
	var v uint32
	st := sys.GetVersion(e.raw, &v)
	return v, check(e.raw, st)
}

// NodeVersion represents the Node.js runtime version (napi_node_version).
type NodeVersion struct {
	Major, Minor, Patch uint32
	Release             string
}

// NodeVersion returns the Node.js runtime version.
func (e Env) NodeVersion() (NodeVersion, error) {
	var v *sys.NodeVersion
	if err := check(e.raw, sys.GetNodeVersion(e.raw, &v)); err != nil {
		return NodeVersion{}, err
	}
	return NodeVersion{Major: v.Major, Minor: v.Minor, Patch: v.Patch, Release: goStringFromC(v.Release)}, nil
}

// ModuleFileName returns the file:// URL of the addon (Node-API 9; returns StatusUnavailable on earlier versions).
func (e Env) ModuleFileName() (string, error) {
	var p *byte
	if err := check(e.raw, sys.GetModuleFileName(e.raw, &p)); err != nil {
		return "", err
	}
	return goStringFromC(p), nil
}

// AdjustExternalMemory reports external memory kept alive by JS objects to V8 (positive when
// allocated, negative when freed) and returns the adjusted total.
func (e Env) AdjustExternalMemory(delta int64) (int64, error) {
	var total int64
	st := sys.AdjustExternalMemory(e.raw, delta, &total)
	return total, check(e.raw, st)
}

// FatalError terminates the process with a fatal error message (napi_fatal_error).
func FatalError(location, message string) {
	sys.FatalError(unsafe.StringData(location), uint(len(location)), unsafe.StringData(message), uint(len(message)))
}

// IsExceptionPending reports whether a JS exception is currently pending.
func (e Env) IsExceptionPending() bool { return exceptionPending(e.raw) }

// GetAndClearLastException returns and clears the pending exception, or returns undefined if none
// is pending.
func (e Env) GetAndClearLastException() (Value, error) {
	var v sys.Value
	st := sys.GetAndClearLastException(e.raw, &v)
	return e.value(v, st)
}

// Catch intercepts the JS exception signaled by err (ErrPendingException): it clears and returns
// the exception. For any other err, it returns false and leaves any pending exception untouched.
func (e Env) Catch(err error) (Value, bool) {
	if !errors.Is(err, ErrPendingException) {
		return Value{}, false
	}
	v, gerr := e.GetAndClearLastException()
	if gerr != nil {
		return Value{}, false
	}
	return v, true
}

// Throw throws err in JS just as a callback returning err would. If an exception is already
// pending, it does nothing. After calling Throw, the callback should return without executing
// further JS.
func (e Env) Throw(err error) {
	if err != nil {
		throwError(e.raw, err)
	}
}

// ThrowValue throws v as-is in JS (napi_throw).
func (e Env) ThrowValue(v Value) error { return check(e.raw, sys.Throw(e.raw, v.raw)) }

// value completes value creation: it returns v if st is StatusOK, or the corresponding error
// otherwise. The status must be evaluated before calling value (st := sys.X(..., &v);
// e.value(v, st)), because Go does not guarantee whether v would be read before or after sys.X if
// passed inline as an argument.
func (e Env) value(v sys.Value, st sys.Status) (Value, error) {
	if st != sys.StatusOK {
		return Value{}, statusError(e.raw, st)
	}
	return Value{env: e.raw, raw: v}, nil
}

// logf writes errors that cannot be surfaced to JS to stderr.
func logf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		// stderr is already the last-resort channel (used when throwing in JS is not possible);
		// there is nowhere else to report a failed write.
		return
	}
}

// Stats counts the Go values retained by napi on behalf of Node-API, for diagnostics and memory
// leak testing: callbacks of live functions and classes, handles (values associated via Wrap,
// externals, finalizers, cleanup hooks, external buffers), and active envs.
type Stats struct {
	Callbacks int
	Handles   int
	Envs      int
}

// ReadStats returns the current counters (safe to call from any goroutine).
func ReadStats() Stats {
	return Stats{Callbacks: callbacks.len(), Handles: handles.len(), Envs: envStates.len()}
}
