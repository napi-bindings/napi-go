package sys

/*
#include "internal.h"
*/
import "C"

import (
	"sync/atomic"
	"unsafe"
)

// Typed callbacks: functions whose primitive arguments and result are converted in C by
// TypedCallbackTrampoline (see typed.c). Layer 2 (napi) uses them for its generic wrappers.

// TypedKind is the kind of a typed argument or result.
type TypedKind int32

const (
	// TypedValue: a napi_value, converted in Go.
	TypedValue TypedKind = C.GN_TK_VALUE
	// TypedF64: a number, read or created as a double.
	TypedF64 TypedKind = C.GN_TK_F64
	// TypedBool: a boolean.
	TypedBool TypedKind = C.GN_TK_BOOL
	// TypedStr: a string, read in UTF-8 into a C buffer.
	TypedStr TypedKind = C.GN_TK_STR
	// TypedI32 and TypedU32: results created with napi_create_int32 and napi_create_uint32.
	TypedI32 TypedKind = C.GN_TK_I32
	TypedU32 TypedKind = C.GN_TK_U32
)

// TypedStrBuf is the size of the C buffers for input strings (shared by all arguments) and for
// the result string.
const TypedStrBuf = C.GN_TYPED_STRBUF

// MaxTypedArgs is the number of arguments a typed signature describes (GN_INLINE_ARGS).
const MaxTypedArgs = C.GN_INLINE_ARGS

// TypedCallbackTrampoline is the napi_callback of typed functions; their data is a TypedSig.
var TypedCallbackTrampoline = Callback(C.napi_callback(C.gn_typed_callback))

// TypedSig is a typed signature allocated in C memory: it is passed as the data of a typed
// function and must stay alive until the function is finalized.
type TypedSig uintptr

// TypedUnwrapThis makes the trampoline read the native object wrapped by this (napi_unwrap) and
// pass it to the handler as wrapped, for methods of wrapped objects.
const TypedUnwrapThis = C.GN_TF_UNWRAP_THIS

// NewTypedSig allocates the signature of a typed function: id is passed to the TypedCallbackHandler,
// flags is 0 or TypedUnwrapThis, and kinds[i] is the kind of argument i (at most MaxTypedArgs;
// further arguments are passed as values). It returns 0 if the allocation fails.
func NewTypedSig(id uintptr, flags int32, kinds []TypedKind) TypedSig {
	n := min(len(kinds), MaxTypedArgs)
	var ks [MaxTypedArgs]C.int32_t
	for i := 0; i < n; i++ {
		ks[i] = C.int32_t(kinds[i])
	}
	return TypedSig(C.gn_typed_sig_new(C.uintptr_t(id), C.int32_t(flags), C.int32_t(n), &ks[0]))
}

// Free releases the signature. The function using it must be finalized.
func (s TypedSig) Free() { C.gn_typed_sig_free(C.uintptr_t(s)) }

// TypedArg is an argument as prepared by the typed trampoline.
type TypedArg C.gn_typed_arg

// Kind returns the kind the argument was converted to (TypedValue if C did not convert it).
func (a *TypedArg) Kind() TypedKind { return TypedKind(a.kind) }

// F64 returns a TypedF64 argument.
func (a *TypedArg) F64() float64 { return float64(a.f64) }

// Bool returns a TypedBool argument.
func (a *TypedArg) Bool() bool { return a.b != 0 }

// String copies a TypedStr argument into a Go string (the C buffer lives only during the call).
func (a *TypedArg) String() string {
	return string(unsafe.Slice((*byte)(unsafe.Pointer(a.sp)), int(a.sn)))
}

// TypedOut receives the result of a typed callback.
type TypedOut C.gn_typed_out

// SetF64 returns a number created from f.
func (o *TypedOut) SetF64(f float64) { o.kind, o.f64 = C.GN_TK_F64, C.double(f) }

// SetInt32 returns a number created from i (napi_create_int32).
func (o *TypedOut) SetInt32(i int32) { o.kind, o.i32 = C.GN_TK_I32, C.int32_t(i) }

// SetUint32 returns a number created from u (napi_create_uint32).
func (o *TypedOut) SetUint32(u uint32) { o.kind, o.u32 = C.GN_TK_U32, C.uint32_t(u) }

// SetBool returns a boolean.
func (o *TypedOut) SetBool(b bool) {
	o.kind, o.b = C.GN_TK_BOOL, 0
	if b {
		o.b = 1
	}
}

// SetString returns a string created in C from a copy of s, if it fits in the C buffer; it
// reports false otherwise (create the string from Go and return it instead).
func (o *TypedOut) SetString(s string) bool {
	if len(s) > int(o.cap) {
		return false
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(o.buf)), len(s)), s)
	o.kind, o.sn = C.GN_TK_STR, C.size_t(len(s))
	return true
}

// TypedCallbackHandler handles every call to a typed function. args are all the arguments (as
// for CallbackHandler); slots are the first min(len(args), len(kinds)) arguments as converted
// in C; wrapped is the native object of this for a TypedUnwrapThis signature (0 if this wraps
// nothing). The handler either sets a result in out or returns a value (nil: undefined). slots
// and out are valid only during the call.
type TypedCallbackHandler func(env Env, info CallbackInfo, id uintptr, this Value, wrapped uintptr, args []Value, slots []TypedArg, out *TypedOut) Value

var typedCallbackHandler atomic.Pointer[TypedCallbackHandler]

// SetTypedCallbackHandler registers the TypedCallbackHandler.
func SetTypedCallbackHandler(h TypedCallbackHandler) {
	if h == nil {
		typedCallbackHandler.Store(nil)
		return
	}
	typedCallbackHandler.Store(&h)
}

//export goNapiTypedCallback
func goNapiTypedCallback(env C.napi_env, info C.napi_callback_info, id C.uintptr_t, argc C.size_t,
	argv *C.napi_value, this C.napi_value, wrapped C.uintptr_t, slots *C.gn_typed_arg, nslots C.size_t, out *C.gn_typed_out) (ret C.napi_value) {
	e := Env(env)
	o := (*TypedOut)(unsafe.Pointer(out))
	defer func() {
		if r := recover(); r != nil {
			o.kind = C.GN_TK_VALUE
			throwError(e, CodePanic, panicMessage("callback", r))
			ret = nil
		}
	}()
	h := typedCallbackHandler.Load()
	if h == nil {
		throwError(e, CodeNoHandler, "napi-go: no typed callback handler registered (sys.SetTypedCallbackHandler)")
		return nil
	}
	n := uint(argc)
	var args []Value
	if n <= inlineArgs {
		args = unsafe.Slice((*Value)(unsafe.Pointer(argv)), n)
	} else {
		args = make([]Value, n)
		if st := GetCbInfo(e, CallbackInfo(info), &n, &args[0], nil, nil); st != StatusOK {
			throwError(e, CodeInternal, "napi-go: napi_get_cb_info failed: "+st.String())
			return nil
		}
		args = args[:n]
	}
	var s []TypedArg
	if nslots > 0 {
		s = unsafe.Slice((*TypedArg)(unsafe.Pointer(slots)), int(nslots))
	}
	return C.napi_value((*h)(e, CallbackInfo(info), uintptr(id), Value(this), uintptr(wrapped), args, s, o))
}
