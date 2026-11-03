package napitest

/*
#include <stdint.h>
*/
import "C"

import "runtime/cgo"

// NoEnvFinalizeFunc corresponds to node_api_noenv_finalize: the env-less finalizer for the
// backing store of an external SharedArrayBuffer, which V8 may call from any thread.
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
