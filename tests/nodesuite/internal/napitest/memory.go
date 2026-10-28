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

// Malloc equivale a malloc(size): memoria C, quindi utilizzabile da Node-API anche dopo la
// chiamata (stringhe e ArrayBuffer esterni). I test C usano il risultato di malloc senza
// controllarlo; qui un'allocazione fallita termina il processo con un messaggio (outOfMemory)
// invece di scrivere su NULL, quindi i chiamanti non devono controllare nil. Con size 0 il
// risultato può essere nil, come per malloc(0).
func Malloc(size uint) unsafe.Pointer {
	p := C.malloc(C.size_t(size))
	if p == nil && size > 0 {
		// L'errore della scrittura non è controllato: il processo termina subito dopo.
		fmt.Fprintln(os.Stderr, outOfMemory)
		Abort()
	}
	return p
}

// outOfMemory è il messaggio di Malloc quando l'allocazione fallisce.
const outOfMemory = "napitest: out of memory"

// Free equivale a free(p).
func Free(p unsafe.Pointer) {
	C.free(p)
}

// FreeAddr equivale a free((void*)addr): serve nei finalizer, che ricevono il data come
// uintptr, senza convertire un uintptr in unsafe.Pointer lato Go.
func FreeAddr(addr uintptr) {
	C.gn_test_free_addr(C.uintptr_t(addr))
}

var utf16Strings sync.Map // string → *uint16 in memoria C, mai liberata

// UTF16 restituisce s in UTF-16 terminata da 0 in memoria C, condivisa e mai liberata come le
// stringhe di CStr: equivale a una costante char16_t[] del test C.
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

// FinalizeFunc equivale a napi_finalize.
type FinalizeFunc func(env sys.Env, data, hint uintptr)

type finalizer struct {
	fn   FinalizeFunc
	hint uintptr
}

// Finalizer restituisce la coppia callback/hint da passare a Node-API al posto della funzione
// napi_finalize e del finalize_hint del test C. fn riceve il data di Node-API e hint invariati.
//
// Il riferimento a fn viene rilasciato dopo la chiamata, che Node-API fa una sola volta. Se la
// funzione Node-API che doveva registrare il finalizer fallisce, va rilasciato con
// DiscardFinalizer.
func Finalizer(fn FinalizeFunc, hint uintptr) (sys.Finalize, uintptr) {
	if fn == nil {
		panic("napitest: Finalizer(nil)")
	}
	return sys.FinalizeTrampoline, uintptr(cgo.NewHandle(&finalizer{fn: fn, hint: hint}))
}

// DiscardFinalizer rilascia un finalizer creato con Finalizer che Node-API non chiamerà.
func DiscardFinalizer(token uintptr) {
	cgo.Handle(token).Delete()
}

// dispatchFinalize riceve come hint il token di Finalizer oppure, per il thread_finalize_cb di una
// threadsafe function, il context creato da ThreadsafeFunction (Node-API passa il context come
// hint del finalizer della TSFN).
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
		// Il token non si rilascia: dopo il thread_finalize_cb Node-API può ancora chiamare
		// call_js_cb (con env NULL) per gli elementi rimasti in coda, e non segnala quando ha
		// finito (vedi ThreadsafeFunction).
		if f.finalize != nil {
			f.finalize(env, data, f.context)
		}
	default:
		panic("napitest: unexpected finalizer token")
	}
}
