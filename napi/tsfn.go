package napi

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/napi-bindings/napi-go/sys"
)

// CallMode selects what ThreadsafeFunction.Call does when the queue is full.
type CallMode int

const (
	// Blocking waits until the queue has room (napi_tsfn_blocking).
	Blocking CallMode = iota
	// NonBlocking fails with ErrQueueFull instead (napi_tsfn_nonblocking).
	NonBlocking
)

var (
	// ErrClosing is returned by the methods of a ThreadsafeFunction that has been released by all
	// its threads, aborted, or destroyed with its env (napi_closing).
	ErrClosing = errors.New("napi: threadsafe function closing")
	// ErrQueueFull is returned by a NonBlocking Call when the queue is full (napi_queue_full).
	ErrQueueFull = errors.New("napi: threadsafe function queue full")
)

// ThreadsafeFunctionOptions configures NewThreadsafeFunction. The zero value is valid.
type ThreadsafeFunctionOptions struct {
	// Name identifies the threadsafe function for async_hooks; the default is "napi-go:tsfn".
	Name string
	// MaxQueueSize bounds the queue of pending calls; 0 means unbounded.
	MaxQueueSize int
	// Threads is the initial thread count: the number of Release calls that close the function.
	// The default is 1.
	Threads int
	// Finalize, if set, runs on the JS thread once the function is closed and no call will reach
	// JavaScript any more, including when the env is destroyed.
	Finalize func(env Env)
}

// defaultTSFNName is the async resource name used when ThreadsafeFunctionOptions.Name is empty.
const defaultTSFNName = "napi-go:tsfn"

// ThreadsafeFunction lets goroutines call into JavaScript (napi_threadsafe_function,
// Napi::TypedThreadSafeFunction). Each Call queues a value of type T; on the JS thread, callJS
// receives it together with the JS function given at creation and does the conversion and the
// call. Unlike a raw napi_threadsafe_function it is safe against the env being destroyed: once
// Node tears it down (worker terminated, process exiting) every method returns ErrClosing instead
// of touching freed memory, and goroutines should stop calling it.
//
// Lifetime follows Node-API: the function is open while its thread count (Threads, plus Acquire,
// minus Release) is positive, and keeps the event loop alive unless Unref is called.
type ThreadsafeFunction[T any] struct {
	raw    sys.ThreadsafeFunction
	env    sys.Env
	callJS func(env Env, fn Function, v T) error
	values table[T] // queued values, by the data ID passed to Node-API
	// discard, if set, receives the values Node drops without calling JS (Abort, env destroyed).
	discard func(v T)

	// life guards raw: calls hold the read lock, the finalizer takes the write lock and sets dead,
	// so no call can be in flight or start once Node frees the function.
	life sync.RWMutex
	dead bool

	// threads mirrors the Node-API thread count, under threadsMu, and closed is set when it
	// reaches zero or on Abort. Node itself keeps accepting calls after the last Release until the
	// JS thread notices, and reports napi_invalid_arg after an Abort: closed gives every method
	// the same answer, ErrClosing, as soon as the function is closed.
	threadsMu sync.Mutex
	threads   int
	closed    atomic.Bool

	// waitMu lets one goroutine at a time wait inside a blocking napi_call_threadsafe_function.
	// Node wakes a blocked caller with a single cond->Signal, only when it pops from a full queue
	// and when the function closes (src/node_api.cc): with several callers blocked, wake-ups are
	// lost and the others would wait forever, possibly on a condition variable Node has freed.
	waitMu sync.Mutex
}

// NewThreadsafeFunction creates a threadsafe function calling fn (which may be the zero Function
// when callJS does not need one) through callJS. It must be called on the JS thread; Call, Acquire,
// Release and Abort may then be used from any goroutine. An error returned by callJS, or a panic
// in it (as a GO_PANIC error), is thrown on the JS thread. Node reports it as an uncaught
// exception for modules declaring Node-API 10 (build tag napi_go_v10) or when node runs with
// --force-node-api-uncaught-exceptions-policy=true; otherwise it emits the DEP0168 warning and
// drops it.
func NewThreadsafeFunction[T any](env Env, fn Function, callJS func(env Env, fn Function, v T) error, opts ThreadsafeFunctionOptions) (*ThreadsafeFunction[T], error) {
	return newThreadsafeFunction(env, fn, callJS, opts, nil)
}

func newThreadsafeFunction[T any](env Env, fn Function, callJS func(env Env, fn Function, v T) error, opts ThreadsafeFunctionOptions, discard func(T)) (*ThreadsafeFunction[T], error) {
	if callJS == nil {
		return nil, TypeError("", "NewThreadsafeFunction: nil callJS")
	}
	if opts.MaxQueueSize < 0 || opts.Threads < 0 {
		return nil, RangeError("", "NewThreadsafeFunction: negative MaxQueueSize or Threads")
	}
	threads := opts.Threads
	if threads == 0 {
		threads = 1
	}
	name := opts.Name
	if name == "" {
		name = defaultTSFNName
	}
	t := &ThreadsafeFunction[T]{env: env.raw, callJS: callJS, threads: threads, discard: discard}
	entry := &tsfnEntry{callJS: t.dispatch}
	cid := tsfnEntries.add(entry)
	entry.onFinalize = func(e Env) {
		t.life.Lock()
		t.dead = true
		t.life.Unlock()
		retireEntry(e.raw, cid)
		if opts.Finalize != nil {
			opts.Finalize(e)
		}
	}
	resName, err := env.NewString(name)
	if err == nil {
		err = check(env.raw, sys.CreateThreadsafeFunction(env.raw, fn.raw, nil, resName.raw,
			uint(opts.MaxQueueSize), uint(threads), tsfnFinalizeData, sys.FinalizeTrampoline, cid, sys.CallJSTrampoline, &t.raw))
	}
	if err != nil {
		tsfnEntries.remove(cid)
		return nil, err
	}
	return t, nil
}

// retireEntry frees the context ID of a finalized threadsafe function. Right after the finalizer,
// Node still calls call_js (with a NULL env) for the values left in the queue, with this ID: the
// entry stays, and keeps discarding them, until a later task on the JS thread frees the ID, so
// that a threadsafe function created in the meantime cannot receive them.
func retireEntry(env sys.Env, cid uintptr) {
	v, ok := liveEnvs.Load(env)
	if ok && v.(*envState).post(func(Env) { tsfnEntries.remove(cid) }) == nil {
		return
	}
	// The env is being destroyed: no threadsafe function can be created any more.
	tsfnEntries.remove(cid)
}

// dispatch receives one queued value on the JS thread (env nil: Node is discarding the queue).
func (t *ThreadsafeFunction[T]) dispatch(raw sys.Env, cb sys.Value, data uintptr) {
	p := t.values.remove(data)
	if p == nil {
		return
	}
	if raw == nil {
		if t.discard != nil {
			t.discard(*p)
		}
		return
	}
	env := Env{raw: raw}
	err := env.WithScope(func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = &Error{Code: sys.CodePanic, Message: panicText("threadsafe function callJS", r)}
			}
		}()
		return t.callJS(env, Function{Object{Value{env: raw, raw: cb}}}, *p)
	})
	if err != nil {
		throwError(raw, err)
	}
}

// with runs f on the raw function unless Node has finalized it.
func (t *ThreadsafeFunction[T]) with(f func() sys.Status) error {
	t.life.RLock()
	defer t.life.RUnlock()
	if t.dead || t.closed.Load() {
		return ErrClosing
	}
	return tsfnError(f())
}

func tsfnError(st sys.Status) error {
	switch st {
	case sys.StatusOK:
		return nil
	case sys.StatusClosing:
		return ErrClosing
	case sys.StatusQueueFull:
		return ErrQueueFull
	}
	// These functions do not set the env's last error: the status is all there is.
	return &Error{Status: st, Kind: statusKind(st), Message: st.String()}
}

// Call queues v for callJS. It is safe from any goroutine; with Blocking it waits while the queue
// is full (goroutines waiting at the same time queue up in Go, since Node-API wakes only one
// waiting thread at a time). It returns ErrClosing once the function is closing or destroyed with
// its env.
func (t *ThreadsafeFunction[T]) Call(v T, mode CallMode) error {
	id := t.values.add(&v)
	// Fast path: no wait. A Blocking call that finds the queue full waits in Node-API, one
	// goroutine at a time (see waitMu).
	err := t.with(func() sys.Status { return sys.CallThreadsafeFunction(t.raw, id, sys.TSFNNonblocking) })
	if errors.Is(err, ErrQueueFull) && mode == Blocking {
		t.waitMu.Lock()
		err = t.with(func() sys.Status { return sys.CallThreadsafeFunction(t.raw, id, sys.TSFNBlocking) })
		t.waitMu.Unlock()
	}
	if err != nil {
		t.values.remove(id)
	}
	return err
}

// Acquire adds a thread to the function: one more Release is needed to close it.
func (t *ThreadsafeFunction[T]) Acquire() error {
	t.threadsMu.Lock()
	defer t.threadsMu.Unlock()
	err := t.with(func() sys.Status { return sys.AcquireThreadsafeFunction(t.raw) })
	if err == nil {
		t.threads++
	}
	return err
}

// Release removes a thread; when none is left the function closes (every method then returns
// ErrClosing) and is finalized once its queue is drained. A thread must not use the function after
// its Release.
func (t *ThreadsafeFunction[T]) Release() error {
	t.threadsMu.Lock()
	defer t.threadsMu.Unlock()
	err := t.with(func() sys.Status { return sys.ReleaseThreadsafeFunction(t.raw, sys.TSFNRelease) })
	if err == nil {
		t.threads--
		if t.threads == 0 {
			t.closed.Store(true)
		}
	}
	return err
}

// Abort closes the function at once, whatever the thread count: queued values are discarded and
// every method then returns ErrClosing.
func (t *ThreadsafeFunction[T]) Abort() error {
	t.threadsMu.Lock()
	defer t.threadsMu.Unlock()
	err := t.with(func() sys.Status { return sys.ReleaseThreadsafeFunction(t.raw, sys.TSFNAbort) })
	if err == nil {
		t.closed.Store(true)
	}
	return err
}

// Ref makes the function keep the event loop alive (the default). JS thread only.
func (t *ThreadsafeFunction[T]) Ref() error {
	return t.with(func() sys.Status { return sys.RefThreadsafeFunction(t.env, t.raw) })
}

// Unref lets the event loop exit while the function is open. JS thread only.
func (t *ThreadsafeFunction[T]) Unref() error {
	return t.with(func() sys.Status { return sys.UnrefThreadsafeFunction(t.env, t.raw) })
}
