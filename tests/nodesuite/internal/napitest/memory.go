package napitest

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static void gn_test_free_addr(uintptr_t addr) { free((void*)addr); }
*/
import "C"

import (
	"fmt"
	"os"
	"runtime/cgo"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Malloc corresponds to malloc(size): C memory, so it remains usable by Node-API after the call
// returns (external strings and ArrayBuffers). The C tests use the result of malloc without
// checking it; here a failed allocation terminates the process with a message (outOfMemory)
// instead of writing to NULL, so callers do not need to check for nil. When size is 0 the result
// may be nil, as with malloc(0).
func Malloc(size uint) unsafe.Pointer {
	p := C.malloc(C.size_t(size))
	if p == nil && size > 0 {
		// Write error intentionally ignored: the process terminates immediately afterward.
		fmt.Fprintln(os.Stderr, outOfMemory)
		Abort()
	}
	return p
}

// outOfMemory is the message printed by Malloc when allocation fails.
const outOfMemory = "napitest: out of memory"

// Free corresponds to free(p).
func Free(p unsafe.Pointer) {
	C.free(p)
}

// FreeAddr corresponds to free((void*)addr): used in finalizers, which receive data as a
// uintptr, without converting a uintptr to unsafe.Pointer on the Go side.
func FreeAddr(addr uintptr) {
	C.gn_test_free_addr(C.uintptr_t(addr))
}

var utf16Strings sync.Map // string → *uint16 in C memory, never freed

// UTF16 returns a NUL-terminated UTF-16 copy of s in C memory, shared and never freed like CStr
// strings: corresponds to a char16_t[] constant in the C test.
func UTF16(s string) *uint16 {
	if p, ok := utf16Strings.Load(s); ok {
		return p.(*uint16)
	}
	units := utf16.Encode([]rune(s))
	const unitSize = unsafe.Sizeof(uint16(0))
	p := (*uint16)(Malloc(uint(len(units)+1) * uint(unitSize)))
	if p == nil {
		panic(outOfMemory)
	}
	buf := unsafe.Slice(p, len(units)+1)
	copy(buf, units)
	buf[len(units)] = 0
	if prev, loaded := utf16Strings.LoadOrStore(s, p); loaded {
		Free(unsafe.Pointer(p))
		return prev.(*uint16)
	}
	return p
}

// FinalizeFunc corresponds to napi_finalize.
type FinalizeFunc func(env sys.Env, data, hint uintptr)

type finalizer struct {
	fn   FinalizeFunc
	hint uintptr
}

// Finalizer returns the callback/hint pair to pass to Node-API in place of the C test's
// napi_finalize function and finalize_hint. fn receives Node-API's data and hint unchanged.
//
// The handle to fn is released after the call, which Node-API makes at most once. If the
// Node-API function that was supposed to register the finalizer fails, it must be released with
// DiscardFinalizer.
func Finalizer(fn FinalizeFunc, hint uintptr) (sys.Finalize, uintptr) {
	if fn == nil {
		panic("napitest: Finalizer(nil)")
	}
	return sys.FinalizeTrampoline, uintptr(cgo.NewHandle(&finalizer{fn: fn, hint: hint}))
}

// DiscardFinalizer releases a finalizer created with Finalizer that Node-API will not call.
func DiscardFinalizer(token uintptr) {
	cgo.Handle(token).Delete()
}

// dispatchFinalize receives as its hint either a Finalizer token or, for a threadsafe function's
// thread_finalize_cb, the context created by ThreadsafeFunction (Node-API passes the context as
// the hint to the TSFN finalizer).
func dispatchFinalize(env sys.Env, data, token uintptr) {
	if token == 0 {
		panic("napitest: sys.FinalizeTrampoline used without napitest.Finalizer")
	}
	h := cgo.Handle(token)
	switch f := h.Value().(type) {
	case *finalizer:
		h.Delete()
		f.fn(env, data, f.hint)
	case *threadsafeFunction:
		// The token is not released: after thread_finalize_cb Node-API may still call
		// call_js_cb (with a NULL env) for items remaining in the queue, and never signals when it
		// is done (see ThreadsafeFunction).
		if f.finalize != nil {
			f.finalize(env, data, f.context)
		}
	default:
		panic("napitest: unexpected finalizer token")
	}
}
