package napi

// tsfncall.go — Layer 2: calling a JS function from goroutines and receiving its result,
// converted (Napi::ThreadSafeFunction with a result, napi-rs ThreadsafeFunction::call_async).

import (
	"context"
	"reflect"
	"unsafe"
)

// JSFunc lets goroutines call a JS function with an argument of type A and wait for its result,
// converted to R. If the function returns a Promise, Call waits for it to settle. A struct{}
// argument calls the function without arguments; a struct{} result ignores the JS result. Use a
// struct or an array for several values. Neither A nor R may contain napi values (they are created
// and read on the JS thread, not in the goroutine).
//
// It is a ThreadsafeFunction: it keeps the event loop alive while open (Unref to avoid it) and is
// closed with Release or Abort. Calls return ErrClosing once it is closed and ErrEnvClosed when
// the env is destroyed while they wait.
type JSFunc[A, R any] struct {
	t      *ThreadsafeFunction[*jsCall[R]]
	envCtx context.Context
}

// jsCall is one call waiting for its result.
type jsCall[R any] struct {
	arg  unsafe.Pointer // *A, encoded on the JS thread
	done chan jsResult[R]
}

type jsResult[R any] struct {
	v   R
	err error
}

func (c *jsCall[R]) finish(v R, err error) {
	select {
	case c.done <- jsResult[R]{v, err}:
	default: // already settled (cannot happen: each call settles once)
	}
}

var emptyStruct = reflect.TypeFor[struct{}]()

// NewJSFunc creates a JSFunc calling fn. It must be called on the JS thread; Call may then be used
// from any goroutine.
func NewJSFunc[A, R any](env Env, fn Function, opts ThreadsafeFunctionOptions) (*JSFunc[A, R], error) {
	at, rt := reflect.TypeFor[A](), reflect.TypeFor[R]()
	ac := codecFor(at)
	rc := codecFor(rt)
	switch {
	case ac.encode == nil:
		return nil, definitionError("NewJSFunc: argument", ac.encodeErr)
	case ac.borrows:
		return nil, definitionError("NewJSFunc: argument", unsupported(at, "refers to JS values and cannot be used in a goroutine"))
	case rt != emptyStruct && rc.decode == nil:
		return nil, definitionError("NewJSFunc: result", rc.decodeErr)
	case rt != emptyStruct && rc.borrows:
		return nil, definitionError("NewJSFunc: result", unsupported(rt, "refers to JS values and cannot be used in a goroutine"))
	}
	st, err := env.state()
	if err != nil {
		return nil, err
	}
	callJS := func(env Env, fn Function, c *jsCall[R]) error {
		runJSCall(env, fn, c, at, ac, rt, rc)
		return nil
	}
	discard := func(c *jsCall[R]) {
		var zero R
		c.finish(zero, ErrClosing)
	}
	if opts.Name == "" {
		opts.Name = "napi-go:jsfunc"
	}
	t, err := newThreadsafeFunction(env, fn, callJS, opts, discard)
	if err != nil {
		return nil, err
	}
	return &JSFunc[A, R]{t: t, envCtx: st.ctx}, nil
}

// runJSCall runs on the JS thread: it converts the argument, calls fn and delivers the converted
// result, now or when the returned Promise settles. Every path settles c exactly once.
func runJSCall[R any](env Env, fn Function, c *jsCall[R], at reflect.Type, ac *codec, rt reflect.Type, rc *codec) {
	var zero R
	var r Value
	var err error
	if at == emptyStruct {
		r, err = fn.Call(Value{})
	} else {
		var arg Value
		if arg, err = ac.enc(env, c.arg); err != nil {
			c.finish(zero, finishPath("argument", err))
			return
		}
		r, err = fn.Call(Value{}, arg)
	}
	if err != nil {
		c.finish(zero, caughtError(env, err))
		return
	}
	if !r.IsPromise() {
		c.finish(decodeResult[R](r, rt, rc))
		return
	}
	if err := awaitPromise(env, r, func(v Value) { c.finish(decodeResult[R](v, rt, rc)) },
		func(reason Value) { c.finish(zero, jsError(reason)) }); err != nil {
		c.finish(zero, caughtError(env, err))
	}
}

func decodeResult[R any](v Value, rt reflect.Type, rc *codec) (R, error) {
	var r R
	if rt == emptyStruct {
		return r, nil
	}
	err := rc.dec(v, unsafe.Pointer(&r), modeOwn)
	return r, finishPath("result", err)
}

// awaitPromise calls onResolve or onReject when p settles (p.then(onResolve, onReject)).
func awaitPromise(env Env, p Value, onResolve, onReject func(Value)) error {
	then, err := Object{p}.Get("then")
	if err != nil {
		return err
	}
	thenFn, err := then.AsFunction()
	if err != nil {
		return err
	}
	res, err := env.NewFunction("", func(c CallContext) (Value, error) { onResolve(c.Arg(0)); return Value{}, nil })
	if err != nil {
		return err
	}
	rej, err := env.NewFunction("", func(c CallContext) (Value, error) { onReject(c.Arg(0)); return Value{}, nil })
	if err != nil {
		return err
	}
	_, err = thenFn.Call(p, res.Value, rej.Value)
	return err
}

// caughtError turns a pending JS exception into a Go error (and clears it); other errors pass
// through.
func caughtError(env Env, err error) error {
	if exc, ok := env.Catch(err); ok {
		return jsError(exc)
	}
	return err
}

// jsError converts a thrown JS value or a rejection reason into an *Error: the message and code
// of an Error object, or the string conversion of any other value. Reading these properties may
// fail (a throwing getter, an env shutting down): the error then says the message could not be
// read, and keeps the kind and code read so far.
func jsError(v Value) error {
	e := &Error{Kind: KindError, Message: unreadableMessage}
	if v.IsError() {
		o := Object{v}
		if n, err := readString(o, "name"); err == nil {
			switch n {
			case "TypeError":
				e.Kind = KindTypeError
			case "RangeError":
				e.Kind = KindRangeError
			case "SyntaxError":
				e.Kind = KindSyntaxError
			}
		}
		if c, err := o.Get("code"); err != nil {
			discardPending(v, err)
		} else if c.IsString() {
			if code, err := c.GoString(); err == nil {
				e.Code = code
			}
		}
		if m, err := readString(o, "message"); err == nil {
			e.Message = m
		}
		return e
	}
	s, err := v.ToString()
	if err != nil {
		discardPending(v, err)
		return e
	}
	if m, err := s.GoString(); err == nil {
		e.Message = m
	}
	return e
}

// discardPending clears the JS exception left by a failed read while building an error: it is
// replaced by unreadableMessage and must not surface later as an uncaught exception.
func discardPending(v Value, err error) {
	_, _ = Env{raw: v.env}.Catch(err)
}

// unreadableMessage is the message of a JS error whose message could not be read.
const unreadableMessage = "JavaScript error (message unreadable)"

// readString reads the string property key of o, converting other values with String(). A JS
// exception thrown while reading is cleared (discardPending).
func readString(o Object, key string) (string, error) {
	v, err := o.Get(key)
	if err == nil {
		var s String
		if s, err = v.ToString(); err == nil {
			return s.GoString()
		}
	}
	discardPending(o.Value, err)
	return "", err
}

// Call calls the JS function with a and returns its converted result. It blocks while the queue
// is full (MaxQueueSize) and until the result is available, ctx is done (the call may still run;
// its result is dropped) or the env is destroyed. A JS exception or rejection is returned as an
// *Error with the message and code of the JS error.
func (f *JSFunc[A, R]) Call(ctx context.Context, a A) (R, error) {
	var zero R
	c := &jsCall[R]{arg: unsafe.Pointer(&a), done: make(chan jsResult[R], 1)}
	if err := f.t.Call(c, Blocking); err != nil {
		return zero, err
	}
	select {
	case r := <-c.done:
		return r.v, r.err
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-f.envCtx.Done():
		return zero, ErrEnvClosed
	}
}

// Acquire, Release, Abort, Ref and Unref behave as for ThreadsafeFunction.
func (f *JSFunc[A, R]) Acquire() error { return f.t.Acquire() }

// Release removes a thread; when none is left the function closes.
func (f *JSFunc[A, R]) Release() error { return f.t.Release() }

// Abort closes the function at once; waiting calls return ErrClosing.
func (f *JSFunc[A, R]) Abort() error { return f.t.Abort() }

// Ref makes the function keep the event loop alive (the default). JS thread only.
func (f *JSFunc[A, R]) Ref() error { return f.t.Ref() }

// Unref lets the event loop exit while the function is open. JS thread only.
func (f *JSFunc[A, R]) Unref() error { return f.t.Unref() }
