// Package addon è il porting di test/node-api/test_instance_data/addon.c, condiviso dai target
// test_set_then_ref e test_ref_then_set del binding.gyp.
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
	// freeMessage è il testo che lo script padre si aspetta, da solo, su stderr.
	freeMessage = "addon_free"
)

func addonFree(env sys.Env, data, _ uintptr) {
	ref := (*sys.Ref)(nt.Pointer(data))
	// Lo status non è controllato, come nel C.
	sys.DeleteReference(env, *ref)
	nt.FreeAddr(data)
	// Come il fprintf del C: l'errore di scrittura non ha un altro canale a cui arrivare.
	fmt.Fprint(os.Stderr, freeMessage)
}

// New è addon_new: gli status non sono controllati, come nel C.
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
