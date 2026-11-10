package napi

import (
	"errors"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// DefineClass defines a JS class (napi_define_class). constructor is invoked with new: the
// CallContext's this is the new instance, which typically wraps a Go value via Wrap. Properties
// with the Static attribute are defined on the constructor; all others are defined on the
// prototype. Callbacks remain registered for as long as the constructor is alive.
//
// Calling the class without new throws a TypeError, matching standard JS class semantics.
func (e Env) DefineClass(name string, constructor Callback, props ...Property) (Function, error) {
	if constructor == nil {
		return Function{}, TypeError("", "DefineClass: nil constructor")
	}
	descs, res, err := e.descriptors(props)
	if err != nil {
		return Function{}, err
	}
	ctorID := registerCallback(requireNew(name, constructor))
	res.cbs = append(res.cbs, ctorID)
	var descp *sys.PropertyDescriptor
	if len(descs) > 0 {
		descp = &descs[0]
	}
	var r sys.Value
	st := sys.DefineClass(e.raw, unsafe.StringData(name), uint(len(name)), sys.CallbackTrampoline, ctorID,
		uint(len(descs)), descp, &r)
	v, err := e.value(r, st)
	if err != nil {
		res.release()
		return Function{}, err
	}
	f := Function{Object{v}}
	if err := e.releaseWith(f.Object, res); err != nil {
		return Function{}, err
	}
	return f, nil
}

// requireNew rejects calls without new: unlike JS classes, Node-API would otherwise allow them
// with an unexpected this receiver.
func requireNew(name string, fn Callback) Callback {
	return func(c CallContext) (Value, error) {
		if !c.IsConstructCall() {
			return Value{}, TypeError("", "Class constructor "+name+" cannot be invoked without 'new'")
		}
		return fn(c)
	}
}

// ---- Promises settled on the JS thread ----

// Promise represents a JS Promise.
type Promise struct{ Object }

// Deferred resolves or rejects the Promise created by NewPromise (napi_deferred). It may be used
// only once and only on the env's JS thread; to settle a Promise from a goroutine, use Env.Async.
type Deferred struct {
	env sys.Env
	raw sys.Deferred
}

// NewPromise creates a pending Promise and the Deferred that settles it.
func (e Env) NewPromise() (Promise, Deferred, error) {
	var d sys.Deferred
	var r sys.Value
	st := sys.CreatePromise(e.raw, &d, &r)
	v, err := e.value(r, st)
	if err != nil {
		return Promise{}, Deferred{}, err
	}
	return Promise{Object{v}}, Deferred{env: e.raw, raw: d}, nil
}

// Resolve resolves the Promise with v.
func (d Deferred) Resolve(v Value) error {
	return check(d.env, sys.ResolveDeferred(d.env, d.raw, v.raw))
}

// Reject rejects the Promise with v.
func (d Deferred) Reject(v Value) error {
	return check(d.env, sys.RejectDeferred(d.env, d.raw, v.raw))
}

// RejectError rejects the Promise with the JS Error representing err (following the same rules as
// a callback returning err).
func (d Deferred) RejectError(err error) error {
	if err == nil {
		return TypeError("", "RejectError: nil error")
	}
	var thrown *Thrown
	if errors.As(err, &thrown) && thrown.Value.raw != nil {
		return d.Reject(thrown.Value)
	}
	v, cerr := newErrorValue(d.env, err)
	if cerr != nil {
		return cerr
	}
	return check(d.env, sys.RejectDeferred(d.env, d.raw, v))
}
