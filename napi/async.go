package napi

import (
	"context"
	"errors"

	"github.com/napi-bindings/napi-go/sys"
)

// Resolver converts the result of asynchronous work into the JS value that resolves its Promise.
// It runs on the JS thread, after the work has finished.
type Resolver func(env Env) (Value, error)

// Async returns a Promise settled by work, which runs in a new goroutine:
//   - work has no Env (it must not call napi): it computes, then returns a Resolver that turns the
//     result into a JS value on the JS thread. A nil Resolver resolves the Promise with
//     undefined;
//   - an error from work or from the Resolver rejects the Promise, following the same rules as
//     a callback returning the error (code, kind, ErrPendingException). A panic rejects it with
//     a GO_PANIC error;
//   - ctx is the env context (Env.Context): it is canceled when the env is destroyed, and the
//     Promise is then left unsettled, since nobody can observe it any more.
//
// While the work runs, the event loop is kept alive, as for a pending timer. This is the
// mechanism of napi-rs's AsyncTask and async fn; all Promises of an env share its dispatcher.
func (e Env) Async(work func(ctx context.Context) (Resolver, error)) (Promise, error) {
	if work == nil {
		return Promise{}, TypeError("", "Async: nil work")
	}
	st, err := e.state()
	if err != nil {
		return Promise{}, err
	}
	if st.ctx.Err() != nil {
		return Promise{}, ErrEnvClosed
	}
	p, d, err := e.NewPromise()
	if err != nil {
		return Promise{}, err
	}
	if err := st.hold(); err != nil {
		return Promise{}, err
	}
	ctx := st.ctx
	go func() {
		res, werr := runWork(ctx, work)
		// post fails with ErrEnvClosed when the env is gone together with the Promise, or when
		// the wake-up notification fails: the closure then stays queued and runs with the next
		// successful post. Neither case has anyone to report to.
		_ = st.post(func(env Env) {
			defer st.unhold()
			settle(env, d, res, werr)
		})
	}()
	return p, nil
}

// runWork calls work, turning a panic into a GO_PANIC error.
func runWork(ctx context.Context, work func(context.Context) (Resolver, error)) (res Resolver, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, err = nil, &Error{Code: sys.CodePanic, Message: panicText("async work", r)}
		}
	}()
	return work(ctx)
}

// settle resolves or rejects d on the JS thread with the outcome of the work.
func settle(env Env, d Deferred, res Resolver, err error) {
	var v Value
	if err == nil && res != nil {
		v, err = runResolver(env, res)
	}
	if err == nil {
		if v.raw == nil {
			v = env.Undefined()
		}
		settleFailed(env, d.Resolve(v))
		return
	}
	if exc, ok := env.Catch(err); ok {
		settleFailed(env, d.Reject(exc))
		return
	}
	settleFailed(env, d.RejectError(err))
}

// runResolver calls res, turning a panic into a GO_PANIC error.
func runResolver(env Env, res Resolver) (v Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = Value{}, &Error{Code: sys.CodePanic, Message: panicText("async resolver", r)}
		}
	}()
	return res(env)
}

// settleFailed handles the error of settling a Promise. While the env shuts down Node-API reports
// napi_pending_exception or napi_cannot_run_js: nobody can observe the Promise any more, so the
// error is dropped. Any other failure is thrown, so that Node reports it as uncaught.
func settleFailed(env Env, err error) {
	if err == nil || errors.Is(err, ErrPendingException) {
		return
	}
	if ne, ok := err.(*Error); ok && ne.Status == sys.StatusCannotRunJS {
		return
	}
	env.Throw(err)
}
