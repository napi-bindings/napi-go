package napitest

/*
#include <stdint.h>
*/
import "C"

import "runtime/cgo"

// NoEnvFinalizeFunc equivale a node_api_noenv_finalize: il finalizer senza env del backing store
// di uno SharedArrayBuffer esterno, che V8 può chiamare da qualsiasi thread.
type NoEnvFinalizeFunc func(data, hint uintptr)

type noEnvFinalizer struct {
	fn   NoEnvFinalizeFunc
	hint uintptr
}

//export goNapitestNoEnvFinalize
func goNapitestNoEnvFinalize(data, token C.uintptr_t) {
	h := cgo.Handle(token)
	f := h.Value().(*noEnvFinalizer)
	h.Delete()
	f.fn(uintptr(data), f.hint)
}
