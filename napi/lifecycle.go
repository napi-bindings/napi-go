package napi

import (
	"errors"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// ---- finalizer ----

// AddFinalizer registers fn to be called when o is garbage-collected by V8 or when the env is
// destroyed (napi_add_finalizer). See FinalizeFunc for restrictions on what fn may do.
func (o Object) AddFinalizer(fn func(env Env)) error {
	if fn == nil {
		return TypeError("", "AddFinalizer: nil function")
	}
	id := handles.add(&handle{onFinalize: func(e Env, _ any) { fn(e) }})
	if err := check(o.env, sys.AddFinalizer(o.env, o.raw, 0, sys.FinalizeTrampoline, id, nil)); err != nil {
		handles.remove(id)
		return err
	}
	return nil
}

// ---- wrap ----

// ErrNotWrapped is returned by Unwrap and RemoveWrap when an object does not wrap a Go value.
var ErrNotWrapped = &Error{Status: StatusInvalidArg, Kind: KindTypeError, Message: "object does not wrap a Go value"}

// Wrap associates the Go value v with the JS object o (napi_wrap): Unwrap retrieves it for as
// long as o lives. fin, if non-nil, is called when o is garbage-collected. An object may wrap at
// most one value.
func (o Object) Wrap(v any, fin FinalizeFunc) error {
	id := handles.add(&handle{value: v, onFinalize: fin})
	if err := check(o.env, sys.Wrap(o.env, o.raw, id, sys.FinalizeTrampoline, id, nil)); err != nil {
		handles.remove(id)
		return err
	}
	return nil
}

// Unwrap returns the Go value associated with o via Wrap.
func (o Object) Unwrap() (any, error) {
	var p unsafe.Pointer
	if err := check(o.env, sys.Unwrap(o.env, o.raw, &p)); err != nil {
		if ne, ok := err.(*Error); ok && ne.Status == StatusInvalidArg {
			return nil, ErrNotWrapped
		}
		return nil, err
	}
	h := handles.get(uintptr(p))
	if h == nil {
		return nil, ErrNotWrapped
	}
	return h.value, nil
}

// RemoveWrap removes the association on o and returns the wrapped value. The finalizer registered
// with Wrap will not be called.
func (o Object) RemoveWrap() (any, error) {
	var p unsafe.Pointer
	if err := check(o.env, sys.RemoveWrap(o.env, o.raw, &p)); err != nil {
		if ne, ok := err.(*Error); ok && ne.Status == StatusInvalidArg {
			return nil, ErrNotWrapped
		}
		return nil, err
	}
	h := handles.remove(uintptr(p))
	if h == nil {
		return nil, ErrNotWrapped
	}
	return h.value, nil
}

// UnwrapAs returns the value associated with o via Wrap asserted to type T; a type mismatch
// returns a TypeError.
func UnwrapAs[T any](o Object) (T, error) {
	var zero T
	v, err := o.Unwrap()
	if err != nil {
		return zero, err
	}
	t, ok := v.(T)
	if !ok {
		return zero, TypeError("", "wrapped Go value has a different type")
	}
	return t, nil
}

// TypeTag is a 128-bit tag that can be attached to an object (napi_type_tag) to safely verify
// that it was created by this addon before calling Unwrap.
type TypeTag = sys.TypeTag

// SetTypeTag attaches tag to o; an object can be tagged at most once (Node-API 8).
func (o Object) SetTypeTag(tag TypeTag) error {
	return check(o.env, sys.TypeTagObject(o.env, o.raw, &tag))
}

// HasTypeTag reports whether o carries the given tag.
func (o Object) HasTypeTag(tag TypeTag) (bool, error) {
	var b bool
	st := sys.CheckObjectTypeTag(o.env, o.raw, &tag, &b)
	return b, check(o.env, st)
}

// ---- external ----

// External represents an opaque JS value carrying an arbitrary Go value (napi_create_external).
type External struct{ Value }

// NewExternal creates an external value holding v. fin, if non-nil, is called when the external
// is garbage-collected.
func (e Env) NewExternal(v any, fin FinalizeFunc) (External, error) {
	id := handles.add(&handle{value: v, onFinalize: fin})
	var r sys.Value
	st := sys.CreateExternal(e.raw, id, sys.FinalizeTrampoline, id, &r)
	val, err := e.value(r, st)
	if err != nil {
		handles.remove(id)
		return External{}, err
	}
	return External{val}, nil
}

// Get returns the Go value held by the external.
func (x External) Get() (any, error) {
	var p unsafe.Pointer
	if err := check(x.env, sys.GetValueExternal(x.env, x.raw, &p)); err != nil {
		return nil, err
	}
	h := handles.get(uintptr(p))
	if h == nil {
		return nil, TypeError("", "external was not created by napi-go")
	}
	return h.value, nil
}

// SetTypeTag attaches tag to the external; like objects, an external can be tagged at most once
// (napi_type_tag_object accepts externals).
func (x External) SetTypeTag(tag TypeTag) error { return Object{x.Value}.SetTypeTag(tag) }

// HasTypeTag reports whether the external carries the given tag.
func (x External) HasTypeTag(tag TypeTag) (bool, error) { return Object{x.Value}.HasTypeTag(tag) }

// ---- reference ----

// Ref holds a reference to a JS value across handle scopes (napi_ref). With a reference count > 0,
// the value is prevented from being garbage-collected; with a count of 0, the reference is weak
// and Value may return an empty Value. A Ref may only be used on its env's JS thread and must be
// released with Delete.
type Ref struct {
	env sys.Env
	raw sys.Ref
}

// NewRef creates a reference to v with the given initial reference count. In Node-API ≤ 9, only
// objects, functions, externals, and symbols may be referenced; starting in Node-API 10, any
// value is allowed.
func (v Value) NewRef(count uint32) (Ref, error) {
	var r sys.Ref
	if err := check(v.env, sys.CreateReference(v.env, v.raw, count, &r)); err != nil {
		return Ref{}, err
	}
	return Ref{env: v.env, raw: r}, nil
}

// IsEmpty reports whether r is the zero Ref.
func (r Ref) IsEmpty() bool { return r.raw == nil }

// Value returns the referenced JS value, or an empty Value if a weak reference has been collected.
func (r Ref) Value() (Value, error) {
	var v sys.Value
	if err := check(r.env, sys.GetReferenceValue(r.env, r.raw, &v)); err != nil {
		return Value{}, err
	}
	return Value{env: r.env, raw: v}, nil
}

// Ref increments the reference count and returns the new count.
func (r Ref) Ref() (uint32, error) {
	var n uint32
	st := sys.ReferenceRef(r.env, r.raw, &n)
	return n, check(r.env, st)
}

// Unref decrements the reference count and returns the new count; at 0 the reference becomes weak.
func (r Ref) Unref() (uint32, error) {
	var n uint32
	st := sys.ReferenceUnref(r.env, r.raw, &n)
	return n, check(r.env, st)
}

// Delete releases the reference. The Ref must not be used after calling Delete.
func (r Ref) Delete() error { return check(r.env, sys.DeleteReference(r.env, r.raw)) }

// ---- handle scope ----

// WithScope executes fn inside a new handle scope (napi_open_handle_scope / napi_close_handle_scope):
// any Values created within fn are released when fn returns. Use it in loops that create many
// values. Values created inside fn must not be used after WithScope returns.
//
// If fn returns an error carrying a Value (napi.Throw), the value is thrown before the scope
// closes and WithScope returns ErrPendingException: after closing, the handle would be dangling.
// If fn panics, the scope is closed and the panic goes on.
func (e Env) WithScope(fn func() error) error {
	var s sys.HandleScope
	if err := check(e.raw, sys.OpenHandleScope(e.raw, &s)); err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			// fn panicked: close the scope before the panic leaves it, or Node aborts when the
			// callback returns with a scope still open. The status is dropped: the panic carries
			// on, and closing fails only for an invalid env or scope.
			sys.CloseHandleScope(e.raw, s)
		}
	}()
	ferr := e.throwScopedValue(fn())
	closed = true
	if err := check(e.raw, sys.CloseHandleScope(e.raw, s)); err != nil && ferr == nil {
		return err
	}
	return ferr
}

// WithEscapableScope executes fn inside an escapable handle scope from which the single Value
// returned by fn is promoted to the outer scope (napi_escape_handle). Errors carrying a Value are
// handled as in WithScope.
func (e Env) WithEscapableScope(fn func() (Value, error)) (Value, error) {
	var s sys.EscapableHandleScope
	if err := check(e.raw, sys.OpenEscapableHandleScope(e.raw, &s)); err != nil {
		return Value{}, err
	}
	closed := false
	defer func() {
		if !closed {
			sys.CloseEscapableHandleScope(e.raw, s) // fn panicked: see WithScope (status dropped)
		}
	}()
	v, ferr := fn()
	ferr = e.throwScopedValue(ferr)
	var escaped sys.Value
	if ferr == nil && v.raw != nil {
		ferr = check(e.raw, sys.EscapeHandle(e.raw, s, v.raw, &escaped))
	}
	closed = true
	if err := check(e.raw, sys.CloseEscapableHandleScope(e.raw, s)); err != nil && ferr == nil {
		return Value{}, err
	}
	if ferr != nil {
		return Value{}, ferr
	}
	return Value{env: e.raw, raw: escaped}, nil
}

// throwScopedValue throws err now if it carries a Value (a *Thrown), which belongs to the scope
// about to close, and returns ErrPendingException in its place; other errors hold no handles and
// are returned unchanged.
func (e Env) throwScopedValue(err error) error {
	var thrown *Thrown
	if err == nil || !errors.As(err, &thrown) {
		return err
	}
	throwError(e.raw, err)
	return ErrPendingException
}

// ---- cleanup hook ----

// CleanupHook represents a hook registered with AddCleanupHook.
type CleanupHook struct {
	env sys.Env
	id  uintptr
	h   *handle // used to detect if the ID was already freed and reassigned
}

// AddCleanupHook registers fn to be called when the env is destroyed (on process exit or worker
// termination), before finalizers run. Hooks run in reverse registration order (LIFO).
func (e Env) AddCleanupHook(fn func()) (CleanupHook, error) {
	if fn == nil {
		return CleanupHook{}, TypeError("", "AddCleanupHook: nil function")
	}
	h := &handle{value: fn}
	id := handles.add(h)
	if err := check(e.raw, sys.AddEnvCleanupHook(e.raw, sys.CleanupHookTrampoline, id)); err != nil {
		handles.remove(id)
		return CleanupHook{}, err
	}
	return CleanupHook{env: e.raw, id: id, h: h}, nil
}

// Remove unregisters the hook if it has not yet run.
func (h CleanupHook) Remove() error {
	if h.h == nil || handles.get(h.id) != h.h {
		return nil // already executed or already removed
	}
	if err := check(h.env, sys.RemoveEnvCleanupHook(h.env, sys.CleanupHookTrampoline, h.id)); err != nil {
		return err
	}
	handles.remove(h.id)
	return nil
}

func dispatchCleanupHook(arg uintptr) {
	h := handles.remove(arg)
	if h == nil {
		return
	}
	if fn, ok := h.value.(func()); ok {
		fn()
	}
}
