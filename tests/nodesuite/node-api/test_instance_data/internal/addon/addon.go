// Package addon is the port of test/node-api/test_instance_data/addon.c, shared by the
// test_set_then_ref and test_ref_then_set targets in binding.gyp.
package addon

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

const (
	refSize = uint(unsafe.Sizeof(sys.Ref(nil)))
	// freeMessage is the text expected by the parent script, by itself, on stderr.
	freeMessage = "addon_free"
)

func addonFree(env sys.Env, data, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(data))
	// Status is not checked, matching C.
	sys.DeleteReference(env, *ref)
	nt.FreeAddr(data)
	// Matches C's fprintf: a write error has no other channel to report to.
	fmt.Fprint(os.Stderr, freeMessage)
}

// New corresponds to addon_new: status values are not checked, matching C.
func New(env sys.Env, exports sys.Value, refFirst bool) sys.Value {
	ref := (*sys.Ref)(nt.Malloc(refSize))
	cb, hint := nt.Finalizer(addonFree, 0)
	if refFirst {
		sys.CreateReference(env, exports, 1, ref)
		sys.SetInstanceData(env, uintptr(unsafe.Pointer(ref)), cb, hint)
	} else {
		sys.SetInstanceData(env, uintptr(unsafe.Pointer(ref)), cb, hint)
		sys.CreateReference(env, exports, 1, ref)
	}
	return exports
}
