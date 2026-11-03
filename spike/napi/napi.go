// Package napi is the prototype (Phase 0) of the Go binding for Node-API.
//
// It contains in minimal form the three layers planned for the architecture:
//   - Layer 0: runtime-resolved Node-API symbol table (gonapi.h, loader.c)
//   - Layer 1: Env, Value, errors, callbacks, scopes
//   - concurrency: per-env TSFN-based dispatcher and Promises resolved from goroutines
//
// This code validates the architectural choices; it is not the final API.
package napi

/*
#cgo CFLAGS: -I${SRCDIR}/include -DNAPI_VERSION=8 -O2
#cgo linux LDFLAGS: -ldl

// "Leaf" functions: they do not retain pointers (noescape) and cannot execute JS
// or re-enter Go (nocallback). They allow keeping buffers and out-parameters passed
// to C on the Go stack. They must NEVER be applied to functions that may execute JS
// (call_function, get/set_property, coerce_to_*, resolve_deferred, ...).
#cgo noescape gn_get_string_utf8
#cgo nocallback gn_get_string_utf8
#cgo noescape gn_new_string_utf8
#cgo nocallback gn_new_string_utf8
#cgo noescape gn_napi_typeof
#cgo nocallback gn_napi_typeof
#cgo noescape gn_napi_create_object
#cgo nocallback gn_napi_create_object
#cgo noescape gn_napi_get_value_bool
#cgo nocallback gn_napi_get_value_bool
#cgo noescape gn_napi_get_boolean
#cgo nocallback gn_napi_get_boolean
#cgo noescape gn_napi_create_int64
#cgo nocallback gn_napi_create_int64
#cgo noescape gn_napi_get_value_int64
#cgo nocallback gn_napi_get_value_int64
#cgo nocallback gn_new_double
#cgo nocallback gn_get_double
#cgo nocallback gn_new_int32
#cgo nocallback gn_get_int32
#cgo nocallback gn_undefined

#include "gonapi.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"unsafe"
)

// Env is the Node-API context for a single invocation. It is valid ONLY on the
// JavaScript thread that provided it and only for the duration of the callback: it must
// not be stored or passed to goroutines (equivalent of `napi_env` / `Napi::Env`).
type Env struct{ raw C.napi_env }

// Value is a handle to a JavaScript value, valid within the current handle scope.
// It carries its env (like Napi::Value in node-addon-api): 2 words, zero allocations.
type Value struct {
	env C.napi_env
	raw C.napi_value
}

// ValueType mirrors napi_valuetype.
type ValueType int

const (
	Undefined ValueType = C.napi_undefined
	Null      ValueType = C.napi_null
	Boolean   ValueType = C.napi_boolean
	Number    ValueType = C.napi_number
	String    ValueType = C.napi_string
	Symbol    ValueType = C.napi_symbol
	Object    ValueType = C.napi_object
	Function  ValueType = C.napi_function
	External  ValueType = C.napi_external
	BigInt    ValueType = C.napi_bigint
)

// Status mirrors napi_status.
type Status int

const (
	StatusOK               Status = C.napi_ok
	StatusPendingException Status = C.napi_pending_exception
	StatusClosing          Status = C.napi_closing
)

// Error is a Node-API error (status != napi_ok) or an application error with a code.
type Error struct {
	Status  Status
	Code    string
	Message string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return "napi: " + e.Message + " (status " + strconv.Itoa(int(e.Status)) + ")"
}

// ErrPendingException indicates that a JS exception is already pending: it must be propagated, not re-thrown.
var ErrPendingException = &Error{Status: StatusPendingException, Message: "JavaScript exception pending"}

// Coded is implemented by errors that want to set `error.code` on the JS side.
type Coded interface{ JSCode() string }

// check converts a status into an error. The happy path does not allocate.
func check(env C.napi_env, s C.napi_status) error {
	if s == C.napi_ok {
		return nil
	}
	if Status(s) == StatusPendingException {
		return ErrPendingException
	}
	msg := "unknown error"
	if m := C.gn_last_error_message(env); m != nil {
		msg = C.GoString(m)
	}
	return &Error{Status: Status(s), Message: msg}
}

func (e Env) value(r C.gn_value_result) (Value, error) {
	if r.s != C.napi_ok {
		return Value{}, check(e.raw, r.s)
	}
	return Value{e.raw, r.v}, nil
}

// ---- value constructors ----

func (e Env) Undefined() Value {
	r := C.gn_undefined(e.raw)
	return Value{e.raw, r.v}
}

func (e Env) Float64(f float64) (Value, error) { return e.value(C.gn_new_double(e.raw, C.double(f))) }
func (e Env) Int32(i int32) (Value, error)     { return e.value(C.gn_new_int32(e.raw, C.int32_t(i))) }

func (e Env) Int64(i int64) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_int64(e.raw, C.int64_t(i), &r))
}

func (e Env) Bool(b bool) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_get_boolean(e.raw, C.bool(b), &r))
}

// String creates a JS string with zero copies on the Go side: the bytes are passed
// as ptr+len and copied by V8 (equivalent of napi_create_string_utf8 with explicit length).
func (e Env) String(s string) (Value, error) {
	p := (*C.char)(unsafe.Pointer(unsafe.StringData(s)))
	return e.value(C.gn_new_string_utf8(e.raw, p, C.size_t(len(s))))
}

func (e Env) Object() (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_object(e.raw, &r))
}

func (e Env) Array(n int) (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_create_array_with_length(e.raw, C.size_t(n), &r))
}

func (e Env) Global() (Value, error) {
	var r C.napi_value
	return Value{e.raw, r}, check(e.raw, C.gn_napi_get_global(e.raw, &r))
}

// NewError creates a JS Error object from a Go error (code extracted from Coded or *Error).
func (e Env) NewError(err error) (Value, error) {
	msg, code := errorParts(err)
	var r C.napi_value
	s := C.gn_new_error(e.raw,
		(*C.char)(unsafe.Pointer(unsafe.StringData(msg))), C.size_t(len(msg)),
		(*C.char)(unsafe.Pointer(unsafe.StringData(code))), C.size_t(len(code)), &r)
	return Value{e.raw, r}, check(e.raw, s)
}

// Throw throws err as a JS exception, unless an exception is already pending.
func (e Env) Throw(err error) {
	if errors.Is(err, ErrPendingException) || bool(C.gn_exception_pending(e.raw)) {
		return
	}
	msg, code := errorParts(err)
	C.gn_throw_error(e.raw,
		(*C.char)(unsafe.Pointer(unsafe.StringData(msg))), C.size_t(len(msg)),
		(*C.char)(unsafe.Pointer(unsafe.StringData(code))), C.size_t(len(code)))
}

func errorParts(err error) (msg, code string) {
	var ne *Error
	var c Coded
	switch {
	case errors.As(err, &ne):
		return ne.Message, ne.Code
	case errors.As(err, &c):
		return err.Error(), c.JSCode()
	default:
		return err.Error(), ""
	}
}

// ---- reading values ----

func (v Value) Env() Env     { return Env{v.env} }
func (v Value) IsZero() bool { return v.raw == nil }

func (v Value) Type() (ValueType, error) {
	var t C.napi_valuetype
	err := check(v.env, C.gn_napi_typeof(v.env, v.raw, &t))
	return ValueType(t), err
}

func (v Value) Float64() (float64, error) {
	r := C.gn_get_double(v.env, v.raw)
	return float64(r.v), check(v.env, r.s)
}

func (v Value) Int32() (int32, error) {
	r := C.gn_get_int32(v.env, v.raw)
	return int32(r.v), check(v.env, r.s)
}

func (v Value) Int64() (int64, error) {
	var r C.int64_t
	return int64(r), check(v.env, C.gn_napi_get_value_int64(v.env, v.raw, &r))
}

func (v Value) Bool() (bool, error) {
	var r C.bool
	return bool(r), check(v.env, C.gn_napi_get_value_bool(v.env, v.raw, &r))
}

// GoString copies a JS string into a Go string. Up to 255 bytes it uses a stack
// buffer and a single cgo crossing; the only allocation is the resulting Go string.
func (v Value) GoString() (string, error) {
	var buf [256]byte
	var n C.size_t
	var copied C.int
	s := C.gn_get_string_utf8(v.env, v.raw, (*C.char)(unsafe.Pointer(&buf[0])), C.size_t(len(buf)), &n, &copied)
	if s != C.napi_ok {
		return "", check(v.env, s)
	}
	if copied != 0 {
		return string(buf[:n]), nil
	}
	b := make([]byte, int(n)+1)
	var w C.size_t
	s = C.gn_napi_get_value_string_utf8(v.env, v.raw, (*C.char)(unsafe.Pointer(&b[0])), C.size_t(len(b)), &w)
	if s != C.napi_ok {
		return "", check(v.env, s)
	}
	return unsafe.String(&b[0], int(w)), nil
}

// Set sets o[key] = val. May execute JS setters.
func (v Value) Set(key string, val Value) error {
	k := C.gn_new_string_utf8(v.env, (*C.char)(unsafe.Pointer(unsafe.StringData(key))), C.size_t(len(key)))
	if k.s != C.napi_ok {
		return check(v.env, k.s)
	}
	return check(v.env, C.gn_napi_set_property(v.env, v.raw, k.v, val.raw))
}

// Get reads o[key]. May execute JS getters.
func (v Value) Get(key string) (Value, error) {
	k := C.gn_new_string_utf8(v.env, (*C.char)(unsafe.Pointer(unsafe.StringData(key))), C.size_t(len(key)))
	if k.s != C.napi_ok {
		return Value{}, check(v.env, k.s)
	}
	var r C.napi_value
	return Value{v.env, r}, check(v.env, C.gn_napi_get_property(v.env, v.raw, k.v, &r))
}

func (v Value) SetIndex(i uint32, val Value) error {
	return check(v.env, C.gn_napi_set_element(v.env, v.raw, C.uint32_t(i), val.raw))
}

// Call invokes v as a JS function (synchronously, on the JS thread).
func (v Value) Call(this Value, args ...Value) (Value, error) {
	var argv [8]C.napi_value
	raw := argv[:0]
	if len(args) > len(argv) {
		raw = make([]C.napi_value, 0, len(args))
	}
	for _, a := range args {
		raw = append(raw, a.raw)
	}
	var argp *C.napi_value
	if len(raw) > 0 {
		argp = &raw[0]
	}
	var r C.napi_value
	err := check(v.env, C.gn_napi_call_function(v.env, this.raw, v.raw, C.size_t(len(raw)), argp, &r))
	return Value{v.env, r}, err
}

// Scope runs fn inside a new handle scope (equivalent of Napi::HandleScope).
func (e Env) Scope(fn func() error) error {
	var s C.napi_handle_scope
	if err := check(e.raw, C.gn_napi_open_handle_scope(e.raw, &s)); err != nil {
		return err
	}
	defer C.gn_napi_close_handle_scope(e.raw, s)
	return fn()
}

// ---- spike diagnostics ----

// NapiVersion returns the highest Node-API version supported by the host process.
func (e Env) NapiVersion() (uint32, error) {
	var r C.uint32_t
	return uint32(r), check(e.raw, C.gn_napi_get_version(e.raw, &r))
}

// NodeVersion returns the version of Node.js hosting the addon.
func (e Env) NodeVersion() (string, error) {
	var r *C.napi_node_version
	if err := check(e.raw, C.gn_napi_get_node_version(e.raw, &r)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", r.major, r.minor, r.patch), nil
}

// HasSymbol reports whether the host process exports a given Node-API function (runtime detection).
func HasSymbol(name string) bool {
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	return C.gn_has_symbol(cs) != 0
}

// SymbolStats returns (resolved required symbols, resolved optional symbols).
func SymbolStats() (required, optional int) {
	return int(C.gn_symbols_required_count()), int(C.gn_symbols_optional_loaded())
}

// ThreadID returns the current OS thread ID.
func ThreadID() uint64 { return uint64(C.gn_thread_id()) }
