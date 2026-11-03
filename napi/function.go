package napi

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Callback is a Go function callable from JS. Any returned error is thrown in JS (see Error);
// returning an empty Value is equivalent to returning undefined.
type Callback func(c CallContext) (Value, error)

// CallContext describes a call from JS (napi_callback_info, Napi::CallbackInfo). Arguments are
// valid only for the duration of the callback. It is passed by value: it is small and thus avoids
// escaping to the heap.
type CallContext struct {
	env  sys.Env
	info sys.CallbackInfo
	this sys.Value
	args []sys.Value
}

// Env returns the environment of the call.
func (c CallContext) Env() Env { return Env{raw: c.env} }

// This returns the this receiver of the call (or the instance being constructed for a constructor call).
func (c CallContext) This() Object { return Object{Value{env: c.env, raw: c.this}} }

// Len returns the number of arguments passed to the call.
func (c CallContext) Len() int { return len(c.args) }

// Arg returns the i-th argument, or undefined if fewer than i+1 arguments were passed.
func (c CallContext) Arg(i int) Value {
	if i >= 0 && i < len(c.args) {
		return Value{env: c.env, raw: c.args[i]}
	}
	return Env{raw: c.env}.Undefined()
}

// Args appends all call arguments to dst and returns the resulting slice.
func (c CallContext) Args(dst []Value) []Value {
	for _, a := range c.args {
		dst = append(dst, Value{env: c.env, raw: a})
	}
	return dst
}

// NewTarget returns new.target, or an empty Value if the function was not invoked with new.
func (c CallContext) NewTarget() (Value, error) {
	var r sys.Value
	if err := check(c.env, sys.GetNewTarget(c.env, c.info, &r)); err != nil {
		return Value{}, err
	}
	return Value{env: c.env, raw: r}, nil
}

// IsConstructCall reports whether the function was invoked with new.
func (c CallContext) IsConstructCall() bool {
	t, err := c.NewTarget()
	return err == nil && t.raw != nil
}

// callbacks holds the Callbacks reachable from JS: the data pointer of every function created by
// napi is an ID in this table.
var callbacks table[Callback]

func registerCallback(fn Callback) uintptr { return callbacks.add(&fn) }

func releaseCallbacks(ids []uintptr) {
	for _, id := range ids {
		callbacks.remove(id)
	}
}

// releaseCallbacksWith releases ids when o is garbage-collected (or when the env is destroyed).
// If attaching the finalizer fails, the IDs are released immediately: o is left without valid
// callbacks, avoiding a memory leak.
func (e Env) releaseCallbacksWith(o Object, ids []uintptr) error {
	if len(ids) == 0 {
		return nil
	}
	err := o.AddFinalizer(func(Env) { releaseCallbacks(ids) })
	if err != nil {
		releaseCallbacks(ids)
	}
	return err
}

// dispatchCallback is the sys CallbackHandler for all functions created by napi.
func dispatchCallback(env sys.Env, info sys.CallbackInfo, data uintptr, this sys.Value, args []sys.Value) sys.Value {
	fn := callbacks.get(data)
	if fn == nil {
		throwError(env, &Error{Code: sys.CodeInternal, Message: "napi-go: callback not registered"})
		return nil
	}
	v, err := (*fn)(CallContext{env: env, info: info, this: this, args: args})
	if err != nil {
		throwError(env, err)
		return nil
	}
	return v.raw
}

// accessor combines a getter and a setter into a single callback: Node-API invokes the getter
// with 0 arguments and the setter with the assigned value.
func accessor(get, set Callback) Callback {
	return func(c CallContext) (Value, error) {
		if c.Len() == 0 {
			if get == nil {
				return Value{}, nil
			}
			return get(c)
		}
		return set(c)
	}
}

// Function represents a JS function.
type Function struct{ Object }

// NewFunction creates a JS function with the given name that invokes fn. fn remains registered
// for as long as the function is alive.
func (e Env) NewFunction(name string, fn Callback) (Function, error) {
	if fn == nil {
		return Function{}, TypeError("", "NewFunction: nil callback")
	}
	id := registerCallback(fn)
	var r sys.Value
	st := sys.CreateFunction(e.raw, unsafe.StringData(name), uint(len(name)), sys.CallbackTrampoline, id, &r)
	v, err := e.value(r, st)
	if err != nil {
		callbacks.remove(id)
		return Function{}, err
	}
	f := Function{Object{v}}
	if err := e.releaseCallbacksWith(f.Object, []uintptr{id}); err != nil {
		return Function{}, err
	}
	return f, nil
}

// rawArgs converts args into the napi_value slice passed to Node-API. The slice escapes to the
// heap (it is passed to functions that execute JS, which cannot use nocallback), so Call and New
// allocate it only when arguments are present, and with the exact length required.
func rawArgs(args []Value) []sys.Value {
	raw := make([]sys.Value, len(args))
	for i, a := range args {
		raw[i] = a.raw
	}
	return raw
}

// Call invokes the function with the given this receiver and arguments; an empty this defaults to
// undefined. Calling with no arguments does not allocate; calling with arguments allocates the
// argument buffer. A JS exception returns ErrPendingException.
func (f Function) Call(this Value, args ...Value) (Value, error) {
	recv := this.raw
	if recv == nil {
		recv = Env{raw: f.env}.Undefined().raw
	}
	var r sys.Value
	var st sys.Status
	if len(args) == 0 {
		st = sys.CallFunction(f.env, recv, f.raw, 0, nil, &r)
	} else {
		raw := rawArgs(args)
		st = sys.CallFunction(f.env, recv, f.raw, uint(len(raw)), &raw[0], &r)
	}
	return Env{raw: f.env}.value(r, st)
}

// New invokes the function as a constructor (new f(...args)), with the same allocation behavior as Call.
func (f Function) New(args ...Value) (Object, error) {
	var r sys.Value
	var st sys.Status
	if len(args) == 0 {
		st = sys.NewInstance(f.env, f.raw, 0, nil, &r)
	} else {
		raw := rawArgs(args)
		st = sys.NewInstance(f.env, f.raw, uint(len(raw)), &raw[0], &r)
	}
	v, err := Env{raw: f.env}.value(r, st)
	return Object{v}, err
}
