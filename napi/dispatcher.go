package napi

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/napi-bindings/napi-go/sys"
)

// ErrEnvClosed is returned by the APIs that reach the JS thread from a goroutine when the env has
// been destroyed (worker terminated, process exiting): the work can no longer run.
var ErrEnvClosed = errors.New("napi: env closed")

// dispatcher runs Go closures on an env's JS thread on behalf of goroutines. Each env has one,
// backed by a single threadsafe function: closures go into a Go queue and the threadsafe function
// is called only to wake the JS thread when the queue goes from empty to non-empty, so a burst of
// posts costs one Node-API call (coalescing). No Go pointer crosses into C: the data of the call is
// always 0.
//
// notify (napi_call_threadsafe_function, non-blocking on an unbounded queue) is called with mu
// held, and close sets closed under the same mutex: once close returns, notify is never called
// again, which is what makes it safe for Node to free the threadsafe function right after.
type dispatcher struct {
	mu        sync.Mutex
	closed    bool
	scheduled bool // notify was called and the JS thread has not taken the queue yet
	queue     []func(Env)
	notify    func() sys.Status
}

// post queues fn to run on the JS thread. It is safe from any goroutine.
func (d *dispatcher) post(fn func(Env)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrEnvClosed
	}
	d.queue = append(d.queue, fn)
	return d.scheduleLocked()
}

func (d *dispatcher) scheduleLocked() error {
	if d.scheduled {
		return nil
	}
	if st := d.notify(); st != sys.StatusOK {
		// The queue keeps fn: the next successful post delivers it. StatusClosing means the env
		// is being torn down, where the closures could not run anyway.
		if st == sys.StatusClosing {
			return ErrEnvClosed
		}
		return &Error{Status: st, Code: sys.CodeInternal, Message: "napi-go: dispatcher notification failed: " + st.String()}
	}
	d.scheduled = true
	return nil
}

// take empties the queue and returns its closures, in posting order. Called on the JS thread.
func (d *dispatcher) take() []func(Env) {
	d.mu.Lock()
	defer d.mu.Unlock()
	q := d.queue
	d.queue = nil
	d.scheduled = false
	return q
}

// requeue puts back closures that could not run yet in front of the queue and wakes the JS thread
// again. Called on the JS thread.
func (d *dispatcher) requeue(rest []func(Env)) {
	if len(rest) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.queue = append(rest[:len(rest):len(rest)], d.queue...)
	// A failure leaves the closures queued for the next post; the env is closing otherwise.
	_ = d.scheduleLocked()
}

// close discards the queue and rejects later posts. It is idempotent.
func (d *dispatcher) close() {
	d.mu.Lock()
	d.closed = true
	d.queue = nil
	d.mu.Unlock()
}

// run executes the closures taken from the queue, each in its own handle scope. A closure that
// leaves an exception pending (a panic is thrown as a GO_PANIC error) ends the batch: Node reports
// the exception as uncaught when the threadsafe function call returns, and the remaining closures
// run on the next call, with no exception pending.
func (d *dispatcher) run(env Env, q []func(Env)) {
	for i, fn := range q {
		runPosted(env, fn)
		if exceptionPending(env.raw) {
			d.requeue(q[i+1:])
			return
		}
	}
}

// panicText formats a recovered panic like the trampolines of sys do.
func panicText(where string, r any) string { return fmt.Sprintf("napi-go: panic in %s: %v", where, r) }

func runPosted(env Env, fn func(Env)) {
	err := env.WithScope(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &Error{Code: sys.CodePanic, Message: panicText("JS-thread task", r)}
			}
		}()
		fn(env)
		return nil
	})
	if err != nil {
		throwError(env.raw, err)
	}
}

// ---- env integration ----

// dispatcherResourceName is the async resource name of the dispatcher's threadsafe function, as
// seen by async_hooks.
const dispatcherResourceName = "napi-go:dispatcher"

// liveEnvs maps a raw env to its state while the env is alive, for goroutines that hold only a
// raw env (Ref.DeleteLater) and cannot call napi_get_instance_data.
var liveEnvs sync.Map // sys.Env → *envState

// threadsafe functions created by napi (dispatchers and ThreadsafeFunction), by context ID. The
// entry is removed when Node finalizes the threadsafe function.
var tsfnEntries table[tsfnEntry]

// tsfnEntry receives the calls of one threadsafe function on the JS thread, with its JS function
// (cb) and the data of the call; env is nil when Node is tearing the threadsafe function down, and
// the entry must then only release data. onFinalize runs when Node finalizes the function.
//
// The entry's ID is the context of the threadsafe function, which Node passes both to call_js
// and, as the hint, to the finalizer; the finalizer is told apart from the other napi finalizers
// by its data, tsfnFinalizeData.
type tsfnEntry struct {
	callJS     func(env sys.Env, cb sys.Value, data uintptr)
	onFinalize func(env Env)
}

// tsfnFinalizeData is the finalize_data of the threadsafe functions created by napi: never a
// valid pointer nor a table ID, so dispatchFinalize recognizes them.
const tsfnFinalizeData = ^uintptr(0)

// finalizeTSFN runs the finalizer of the threadsafe function whose context is cid.
func finalizeTSFN(env sys.Env, cid uintptr) {
	if e := tsfnEntries.get(cid); e != nil && e.onFinalize != nil {
		e.onFinalize(Env{raw: env})
	}
}

func init() { sys.SetCallJSHandler(dispatchCallJS) }

func dispatchCallJS(env sys.Env, cb sys.Value, context, data uintptr) {
	if e := tsfnEntries.get(context); e != nil {
		e.callJS(env, cb, data)
	}
}

// startDispatcher creates the env's dispatcher and its context, canceled when the env is
// destroyed. Its threadsafe function is unref'd: it keeps the event loop alive only while hold
// is in effect.
func startDispatcher(env sys.Env, st *envState) error {
	st.ctx, st.cancel = context.WithCancel(context.Background())
	d := &dispatcher{}
	entry := &tsfnEntry{callJS: func(raw sys.Env, _ sys.Value, _ uintptr) {
		if raw == nil {
			return // torn down: the queue was discarded by closeEnv
		}
		d.run(Env{raw: raw}, d.take())
	}}
	cid := tsfnEntries.add(entry)
	entry.onFinalize = func(Env) {
		// The env is being destroyed: no threadsafe function can be created any more, so the ID
		// can be freed at once (call_js ignores the data of the leftover calls).
		tsfnEntries.remove(cid)
		st.closeEnv(env)
	}
	name, err := Env{raw: env}.NewString(dispatcherResourceName)
	if err == nil {
		err = check(env, sys.CreateThreadsafeFunction(env, nil, nil, name.raw, 0, 1, tsfnFinalizeData,
			sys.FinalizeTrampoline, cid, sys.CallJSTrampoline, &st.tsfn))
	}
	if err == nil {
		err = check(env, sys.UnrefThreadsafeFunction(env, st.tsfn))
	}
	if err != nil {
		tsfnEntries.remove(cid)
		st.cancel()
		return err
	}
	d.notify = func() sys.Status { return sys.CallThreadsafeFunction(st.tsfn, 0, sys.TSFNNonblocking) }
	st.dispatcher = d
	liveEnvs.Store(env, st)
	return nil
}

// closeEnv marks the env destroyed: the dispatcher rejects new work, the env context is canceled.
// It runs when Node finalizes the dispatcher's threadsafe function or the instance data, whichever
// comes first, and is idempotent.
func (st *envState) closeEnv(env sys.Env) {
	if st.dispatcher != nil {
		st.dispatcher.close()
	}
	if st.cancel != nil {
		st.cancel()
	}
	liveEnvs.CompareAndDelete(env, st)
}

// hold keeps the event loop alive until the matching unhold, for work that will settle from a
// goroutine. Both run on the JS thread.
func (st *envState) hold() error {
	st.holds++
	if st.holds == 1 {
		if err := check(st.env, sys.RefThreadsafeFunction(st.env, st.tsfn)); err != nil {
			st.holds--
			return err
		}
	}
	return nil
}

func (st *envState) unhold() {
	st.holds--
	if st.holds == 0 {
		// Cannot fail: with a non-NULL function Node only calls uv_unref and returns napi_ok
		// (src/node_api.cc).
		_ = sys.UnrefThreadsafeFunction(st.env, st.tsfn)
	}
}

// post runs fn on the env's JS thread; safe from any goroutine.
func (st *envState) post(fn func(Env)) error { return st.dispatcher.post(fn) }

// Context returns a context canceled when the env is destroyed (process exit, worker
// termination). Goroutines started on behalf of the env should stop when it is done. Like every
// Env method it must be called on the JS thread; the context itself may be used anywhere.
func (e Env) Context() (context.Context, error) {
	st, err := e.state()
	if err != nil {
		return nil, err
	}
	return st.ctx, nil
}

// DeleteLater deletes the reference on its env's JS thread. Unlike Delete it is safe from any
// goroutine: the deletion is queued (like the "custom GC" threadsafe function of napi-rs). If the
// env is already destroyed it does nothing, since Node frees the references of a destroyed env.
// As with Delete, a Ref must not be used after its env is destroyed.
func (r Ref) DeleteLater() error {
	v, ok := liveEnvs.Load(r.env)
	if !ok {
		return nil
	}
	err := v.(*envState).post(func(env Env) {
		// Fails only for a NULL env or ref (src/js_native_api_v8.cc): the env is alive and the
		// ref was valid when DeleteLater was called.
		_ = sys.DeleteReference(env.raw, r.raw)
	})
	if errors.Is(err, ErrEnvClosed) {
		return nil
	}
	return err
}
