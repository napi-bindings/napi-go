// Command napiasync is the e2e test addon for the concurrency APIs of Layer 1 (napi): Promises
// settled from goroutines (Env.Async), the env context, typed threadsafe functions and
// Ref.DeleteLater. Counters observed by tests/e2e/async.test.mjs are process-wide atomics, since
// several envs (workers) may load the addon.
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/napi-bindings/napi-go/napi"
)

func main() {}

var counters struct {
	canceled      atomic.Int64 // waitCancel goroutines that saw the env context canceled
	tsfnFinalized atomic.Int64 // ThreadsafeFunction finalizers run
	tsfnClosing   atomic.Int64 // Calls that returned ErrClosing
	tsfnOtherErrs atomic.Int64 // Calls that failed with anything else
	finalized     atomic.Int64 // objects of trackObjects collected by V8
	deleteLater   atomic.Int64 // DeleteLater calls that returned an error
}

func init() {
	napi.Init(func(env napi.Env, exports napi.Object) (napi.Value, error) {
		fns := []struct {
			name string
			cb   napi.Callback
		}{
			{"sleepValue", sleepValue},
			{"rejectWith", rejectWith},
			{"panicInWork", panicInWork},
			{"panicInResolver", panicInResolver},
			{"resolverCalls", resolverCalls},
			{"resolveUndefined", resolveUndefined},
			{"waitCancel", waitCancel},
			{"contextAlive", contextAlive},
			{"counters", getCounters},
			{"tsfnRun", tsfnRun},
			{"tsfnQueueFull", tsfnQueueFull},
			{"tsfnAfterClose", tsfnAfterClose},
			{"tsfnThrow", tsfnThrow},
			{"tsfnIdle", tsfnIdle},
			{"tsfnFlood", tsfnFlood},
			{"trackObjects", trackObjects},
			{"deleteRefsLater", deleteRefsLater},
		}
		for _, f := range fns {
			fn, err := env.NewFunction(f.name, f.cb)
			if err != nil {
				return napi.Value{}, err
			}
			if err := exports.Set(f.name, fn.Value); err != nil {
				return napi.Value{}, err
			}
		}
		return napi.Value{}, nil
	})
}

// ---- Promises from goroutines ----

// sleepValue(ms, n): a Promise resolved with n after ms milliseconds of work in a goroutine, or
// rejected with the context error if the env is destroyed first.
func sleepValue(c napi.CallContext) (napi.Value, error) {
	ms, err := c.Arg(0).Float64()
	if err != nil {
		return napi.Value{}, err
	}
	n, err := c.Arg(1).Float64()
	if err != nil {
		return napi.Value{}, err
	}
	p, err := c.Env().Async(func(ctx context.Context) (napi.Resolver, error) {
		select {
		case <-time.After(time.Duration(ms * float64(time.Millisecond))):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return func(env napi.Env) (napi.Value, error) { return env.NewFloat64(n) }, nil
	})
	return p.Value, err
}

// rejectWith(kind, code, message): a Promise rejected by the work with an error of the given kind
// ("error", "type", "range") and code.
func rejectWith(c napi.CallContext) (napi.Value, error) {
	kind, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	code, err := c.Arg(1).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	msg, err := c.Arg(2).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	var werr error
	switch kind {
	case "type":
		werr = napi.TypeError(code, msg)
	case "range":
		werr = napi.RangeError(code, msg)
	default:
		werr = &napi.Error{Code: code, Message: msg}
	}
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) { return nil, werr })
	return p.Value, err
}

// panicInWork(): a Promise whose work panics.
func panicInWork(c napi.CallContext) (napi.Value, error) {
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) { panic("boom in work") })
	return p.Value, err
}

// panicInResolver(): a Promise whose resolver panics on the JS thread.
func panicInResolver(c napi.CallContext) (napi.Value, error) {
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) {
		return func(napi.Env) (napi.Value, error) { panic("boom in resolver") }, nil
	})
	return p.Value, err
}

// resolverCalls(fn): a Promise resolved with fn(41) as called by the resolver on the JS thread;
// if fn throws, the Promise is rejected with what it threw.
func resolverCalls(c napi.CallContext) (napi.Value, error) {
	fn, err := c.Arg(0).AsFunction()
	if err != nil {
		return napi.Value{}, err
	}
	ref, err := fn.NewRef(1)
	if err != nil {
		return napi.Value{}, err
	}
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) {
		return func(env napi.Env) (v napi.Value, err error) {
			defer func() {
				if derr := ref.Delete(); err == nil {
					err = derr
				}
			}()
			v, err = ref.Value()
			if err != nil {
				return napi.Value{}, err
			}
			f, err := v.AsFunction()
			if err != nil {
				return napi.Value{}, err
			}
			var arg napi.Value
			arg, err = env.NewFloat64(41)
			if err != nil {
				return napi.Value{}, err
			}
			return f.Call(napi.Value{}, arg)
		}, nil
	})
	if err != nil {
		// The Async error is the one to report; the reference is released anyway.
		err = errors.Join(err, ref.Delete())
	}
	return p.Value, err
}

// resolveUndefined(): a Promise whose work returns a nil Resolver.
func resolveUndefined(c napi.CallContext) (napi.Value, error) {
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) { return nil, nil })
	return p.Value, err
}

// waitCancel(): starts work that only ends when the env context is canceled, counting it.
func waitCancel(c napi.CallContext) (napi.Value, error) {
	p, err := c.Env().Async(func(ctx context.Context) (napi.Resolver, error) {
		<-ctx.Done()
		counters.canceled.Add(1)
		return nil, ctx.Err()
	})
	return p.Value, err
}

// contextAlive(): whether the env context is still live.
func contextAlive(c napi.CallContext) (napi.Value, error) {
	ctx, err := c.Env().Context()
	if err != nil {
		return napi.Value{}, err
	}
	return c.Env().NewBoolean(ctx.Err() == nil)
}

func getCounters(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	for k, v := range map[string]int64{
		"canceled":      counters.canceled.Load(),
		"tsfnFinalized": counters.tsfnFinalized.Load(),
		"tsfnClosing":   counters.tsfnClosing.Load(),
		"tsfnOtherErrs": counters.tsfnOtherErrs.Load(),
		"finalized":     counters.finalized.Load(),
		"deleteLater":   counters.deleteLater.Load(),
	} {
		n, err := env.NewFloat64(float64(v))
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.Set(k, n); err != nil {
			return napi.Value{}, err
		}
	}
	return o.Value, nil
}

// ---- typed threadsafe functions ----

// call is the value queued by the tsfn exports: which goroutine sent it and its sequence number.
type call struct{ goroutine, seq int }

// callWithPair calls fn(goroutine, seq) on the JS thread.
func callWithPair(env napi.Env, fn napi.Function, v call) error {
	g, err := env.NewInt32(int32(v.goroutine))
	if err != nil {
		return err
	}
	s, err := env.NewInt32(int32(v.seq))
	if err != nil {
		return err
	}
	_, err = fn.Call(napi.Value{}, g, s)
	return err
}

func countFinalize(napi.Env) { counters.tsfnFinalized.Add(1) }

// release releases t for a goroutine, counting a failure like a failed Call.
func release(t interface{ Release() error }) {
	if err := t.Release(); err != nil {
		countCallError(err)
	}
}

// countCallError records why a goroutine stopped calling.
func countCallError(err error) {
	if errors.Is(err, napi.ErrClosing) {
		counters.tsfnClosing.Add(1)
	} else {
		counters.tsfnOtherErrs.Add(1)
	}
}

// tsfnRun(fn, goroutines, calls, maxQueue, nonBlocking): each goroutine calls fn(g, 0..calls-1)
// through one threadsafe function (Threads = goroutines) and releases it when done. NonBlocking
// calls retry on ErrQueueFull.
func tsfnRun(c napi.CallContext) (napi.Value, error) {
	fn, err := c.Arg(0).AsFunction()
	if err != nil {
		return napi.Value{}, err
	}
	var n [3]uint32
	for i := range n {
		if n[i], err = c.Arg(i + 1).Uint32(); err != nil {
			return napi.Value{}, err
		}
	}
	goroutines, calls, maxQueue := int(n[0]), int(n[1]), int(n[2])
	nonBlocking, err := c.Arg(4).Bool()
	if err != nil {
		return napi.Value{}, err
	}
	if goroutines == 0 {
		return napi.Value{}, napi.RangeError("", "tsfnRun: at least one goroutine")
	}
	mode := napi.Blocking
	if nonBlocking {
		mode = napi.NonBlocking
	}
	t, err := napi.NewThreadsafeFunction(c.Env(), fn, callWithPair, napi.ThreadsafeFunctionOptions{
		Name: "tsfnRun", MaxQueueSize: maxQueue, Threads: goroutines, Finalize: countFinalize,
	})
	if err != nil {
		return napi.Value{}, err
	}
	for g := 0; g < goroutines; g++ {
		go func() {
			defer release(t)
			for i := 0; i < calls; i++ {
				err := t.Call(call{g, i}, mode)
				for errors.Is(err, napi.ErrQueueFull) {
					time.Sleep(time.Microsecond)
					err = t.Call(call{g, i}, mode)
				}
				if err != nil {
					countCallError(err)
					return
				}
			}
		}()
	}
	return napi.Value{}, nil
}

// tsfnQueueFull(): with a queue of one value and the JS thread busy in this very call, a second
// NonBlocking Call fails with ErrQueueFull. Returns the two outcomes as strings.
func tsfnQueueFull(c napi.CallContext) (napi.Value, error) {
	t, err := napi.NewThreadsafeFunction(c.Env(), napi.Function{}, func(napi.Env, napi.Function, int) error { return nil },
		napi.ThreadsafeFunctionOptions{MaxQueueSize: 1, Finalize: countFinalize})
	if err != nil {
		return napi.Value{}, err
	}
	results := make(chan error, 2)
	go func() {
		results <- t.Call(1, napi.NonBlocking)
		results <- t.Call(2, napi.NonBlocking)
		release(t)
	}()
	return outcomes(c.Env(), <-results, <-results)
}

// tsfnAfterClose(abort): after Release (or Abort) closes the function, Call, Acquire and Release
// fail with ErrClosing.
func tsfnAfterClose(c napi.CallContext) (napi.Value, error) {
	abort, err := c.Arg(0).Bool()
	if err != nil {
		return napi.Value{}, err
	}
	t, err := napi.NewThreadsafeFunction(c.Env(), napi.Function{}, func(napi.Env, napi.Function, int) error { return nil },
		napi.ThreadsafeFunctionOptions{Finalize: countFinalize})
	if err != nil {
		return napi.Value{}, err
	}
	var closeErr error
	if abort {
		closeErr = t.Abort()
	} else {
		closeErr = t.Release()
	}
	return outcomes(c.Env(), closeErr, t.Call(1, napi.NonBlocking), t.Acquire(), t.Release())
}

// outcomes returns errs as an array of "ok", "closing", "queueFull" or the error text.
func outcomes(env napi.Env, errs ...error) (napi.Value, error) {
	a, err := env.NewArrayWithLength(len(errs))
	if err != nil {
		return napi.Value{}, err
	}
	for i, e := range errs {
		s := "ok"
		switch {
		case errors.Is(e, napi.ErrClosing):
			s = "closing"
		case errors.Is(e, napi.ErrQueueFull):
			s = "queueFull"
		case e != nil:
			s = e.Error()
		}
		v, err := env.NewString(s)
		if err != nil {
			return napi.Value{}, err
		}
		if err := a.SetIndex(uint32(i), v.Value); err != nil {
			return napi.Value{}, err
		}
	}
	return a.Value, nil
}

// tsfnThrow(code): one call whose callJS fails with an error with the given code (reported by
// Node as an uncaught exception), then a second call that panics.
func tsfnThrow(c napi.CallContext) (napi.Value, error) {
	code, err := c.Arg(0).GoString()
	if err != nil {
		return napi.Value{}, err
	}
	t, err := napi.NewThreadsafeFunction(c.Env(), napi.Function{}, func(_ napi.Env, _ napi.Function, n int) error {
		if n == 0 {
			return &napi.Error{Code: code, Message: "from callJS"}
		}
		panic(fmt.Sprintf("callJS panic %d", n))
	}, napi.ThreadsafeFunctionOptions{Finalize: countFinalize})
	if err != nil {
		return napi.Value{}, err
	}
	go func() {
		defer release(t)
		for i := 0; i < 2; i++ {
			if err := t.Call(i, napi.Blocking); err != nil {
				countCallError(err)
			}
		}
	}()
	return napi.Value{}, nil
}

// idle keeps the threadsafe functions of tsfnIdle reachable until their env is destroyed.
var idle sync.Map

// tsfnIdle(unref): an open threadsafe function nobody calls; with unref it does not keep the
// event loop alive.
func tsfnIdle(c napi.CallContext) (napi.Value, error) {
	unref, err := c.Arg(0).Bool()
	if err != nil {
		return napi.Value{}, err
	}
	t, err := napi.NewThreadsafeFunction(c.Env(), napi.Function{}, func(napi.Env, napi.Function, int) error { return nil },
		napi.ThreadsafeFunctionOptions{Finalize: countFinalize})
	if err != nil {
		return napi.Value{}, err
	}
	idle.Store(t, struct{}{})
	if unref {
		return napi.Value{}, t.Unref()
	}
	return napi.Value{}, nil
}

// tsfnFlood(goroutines, maxQueue): goroutines call a no-op threadsafe function in a tight
// Blocking loop until it closes (env destroyed): they must stop with ErrClosing, never touching
// freed memory. With a bounded queue several of them wait for room at the same time.
func tsfnFlood(c napi.CallContext) (napi.Value, error) {
	goroutines, err := c.Arg(0).Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	maxQueue, err := c.Arg(1).Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	t, err := napi.NewThreadsafeFunction(c.Env(), napi.Function{}, func(napi.Env, napi.Function, int) error { return nil },
		napi.ThreadsafeFunctionOptions{Threads: int(goroutines), MaxQueueSize: int(maxQueue), Finalize: countFinalize})
	if err != nil {
		return napi.Value{}, err
	}
	for g := 0; g < int(goroutines); g++ {
		go func() {
			for i := 0; ; i++ {
				if err := t.Call(i, napi.Blocking); err != nil {
					countCallError(err)
					return
				}
			}
		}()
	}
	return napi.Value{}, nil
}

// ---- references released from goroutines ----

// refs holds the references of trackObjects until deleteRefsLater.
var refs struct {
	mu   sync.Mutex
	list []napi.Ref
}

// trackObjects(n): creates n objects, each counted in finalized when V8 collects it, and keeps
// them alive with strong references.
func trackObjects(c napi.CallContext) (napi.Value, error) {
	n, err := c.Arg(0).Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	refs.mu.Lock()
	defer refs.mu.Unlock()
	for i := uint32(0); i < n; i++ {
		o, err := c.Env().NewObject()
		if err != nil {
			return napi.Value{}, err
		}
		if err := o.AddFinalizer(func(napi.Env) { counters.finalized.Add(1) }); err != nil {
			return napi.Value{}, err
		}
		r, err := o.NewRef(1)
		if err != nil {
			return napi.Value{}, err
		}
		refs.list = append(refs.list, r)
	}
	return napi.Value{}, nil
}

// deleteRefsLater(): a goroutine deletes the references of trackObjects with DeleteLater; the
// returned Promise resolves when it has queued them all.
func deleteRefsLater(c napi.CallContext) (napi.Value, error) {
	refs.mu.Lock()
	list := refs.list
	refs.list = nil
	refs.mu.Unlock()
	p, err := c.Env().Async(func(context.Context) (napi.Resolver, error) {
		for _, r := range list {
			if err := r.DeleteLater(); err != nil {
				counters.deleteLater.Add(1)
			}
		}
		return nil, nil
	})
	return p.Value, err
}
