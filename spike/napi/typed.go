package napi

/*
#include "gonapi.h"
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// Prim is the set of types converted entirely in C by the typed trampoline.
type Prim interface {
	~float64 | ~int32 | ~bool | ~string
}

func kindOf[T Prim]() C.int32_t {
	var z T
	switch any(z).(type) {
	case float64:
		return C.GN_K_F64
	case int32:
		return C.GN_K_I32
	case bool:
		return C.GN_K_BOOL
	case string:
		return C.GN_K_STR
	}
	// defined types (~float64 etc.): fall back to the underlying type size
	switch unsafe.Sizeof(z) {
	case 1:
		return C.GN_K_BOOL
	case 4:
		return C.GN_K_I32
	case 8:
		return C.GN_K_F64
	}
	return C.GN_K_STR
}

type typedFn func(env C.napi_env, args []C.gn_arg, out *C.gn_arg) error

// Same scheme as the callback registry: lock-free reads, rare writes.
var typedRegistry struct {
	mu  sync.Mutex
	fns atomic.Pointer[[]typedFn]
}

// TypedDef describes a function with a known primitive signature.
type TypedDef struct {
	kinds []C.int32_t
	ret   C.int32_t
	fn    typedFn
}

func argOf[T Prim](env C.napi_env, a *C.gn_arg) (T, error) {
	var out T
	p := unsafe.Pointer(&a.u)
	switch a.kind {
	case C.GN_K_F64, C.GN_K_I32, C.GN_K_BOOL:
		out = *(*T)(p) // identical representation: no conversion, no allocation
	case C.GN_K_STR:
		s := (*C.gn_str)(p)
		str := C.GoStringN(s.p, C.int(s.n)) // copy: C memory is valid only during the call
		out = *(*T)(unsafe.Pointer(&str))
	case C.GN_K_VALUE: // string too long for the C buffer: slow path
		v := Value{env, *(*C.napi_value)(p)}
		str, err := v.GoString()
		if err != nil {
			return out, err
		}
		out = *(*T)(unsafe.Pointer(&str))
	}
	return out, nil
}

func setRet[T Prim](env C.napi_env, out *C.gn_arg, r T) error {
	p := unsafe.Pointer(&out.u)
	switch out.kind {
	case C.GN_K_F64:
		*(*float64)(p) = *(*float64)(unsafe.Pointer(&r))
	case C.GN_K_I32:
		*(*int32)(p) = *(*int32)(unsafe.Pointer(&r))
	case C.GN_K_BOOL:
		*(*bool)(p) = *(*bool)(unsafe.Pointer(&r))
	case C.GN_K_STR:
		str := *(*string)(unsafe.Pointer(&r))
		buf := (*C.gn_str)(p)
		if len(str) <= int(buf.n) { // fits in the C buffer: copy without crossing cgo
			copy(unsafe.Slice((*byte)(unsafe.Pointer(buf.p)), len(str)), str)
			buf.n = C.size_t(len(str))
			return nil
		}
		v, err := Env{env}.String(str) // slow path: create the string from Go
		if err != nil {
			return err
		}
		out.kind = C.GN_K_VALUE
		*(*C.napi_value)(p) = v.raw
	}
	return nil
}

// Fn1 and Fn2 construct functions with typed signatures (preview of Layer 2).
func Fn1[A, R Prim](f func(A) (R, error)) TypedDef {
	return TypedDef{
		kinds: []C.int32_t{kindOf[A]()},
		ret:   kindOf[R](),
		fn: func(env C.napi_env, args []C.gn_arg, out *C.gn_arg) error {
			a, err := argOf[A](env, &args[0])
			if err != nil {
				return err
			}
			r, err := f(a)
			if err != nil {
				return err
			}
			return setRet(env, out, r)
		},
	}
}

func Fn2[A, B, R Prim](f func(A, B) (R, error)) TypedDef {
	return TypedDef{
		kinds: []C.int32_t{kindOf[A](), kindOf[B]()},
		ret:   kindOf[R](),
		fn: func(env C.napi_env, args []C.gn_arg, out *C.gn_arg) error {
			a, err := argOf[A](env, &args[0])
			if err != nil {
				return err
			}
			b, err := argOf[B](env, &args[1])
			if err != nil {
				return err
			}
			r, err := f(a, b)
			if err != nil {
				return err
			}
			return setRet(env, out, r)
		},
	}
}

type typedExport struct {
	name string
	sig  *C.gn_sig // C memory: pointer remains valid for the lifetime of the process
}

var typedExports []typedExport

// ExportTyped registers a typed-signature function on the exports object.
func ExportTyped(name string, d TypedDef) {
	typedRegistry.mu.Lock()
	var cur []typedFn
	if p := typedRegistry.fns.Load(); p != nil {
		cur = *p
	}
	next := append(append(make([]typedFn, 0, len(cur)+1), cur...), d.fn)
	typedRegistry.fns.Store(&next)
	id := len(next)
	typedRegistry.mu.Unlock()

	sig := C.gn_alloc_sig()
	sig.id = C.uintptr_t(id)
	sig.nargs = C.int32_t(len(d.kinds))
	sig.ret = d.ret
	for i, k := range d.kinds {
		sig.kinds[i] = k
	}
	typedExports = append(typedExports, typedExport{name: name, sig: sig})
}

//export goNapiTypedCallback
func goNapiTypedCallback(env C.napi_env, id C.uintptr_t, args *C.gn_arg, nargs C.int32_t, out *C.gn_arg) (rc C.int32_t) {
	defer func() {
		if r := recover(); r != nil {
			throwPanic(env, r)
			rc = 1
		}
	}()
	fn := (*typedRegistry.fns.Load())[id-1]
	if err := fn(env, unsafe.Slice(args, int(nargs)), out); err != nil {
		Env{env}.Throw(err)
		return 1
	}
	return 0
}

func materializeTyped(env C.napi_env, exports Value) error {
	for _, d := range typedExports {
		var fn C.napi_value
		s := C.gn_new_typed_function(env, (*C.char)(unsafe.Pointer(unsafe.StringData(d.name))), C.size_t(len(d.name)), d.sig, &fn)
		if err := check(env, s); err != nil {
			return err
		}
		if err := exports.Set(d.name, Value{env, fn}); err != nil {
			return err
		}
	}
	return nil
}
