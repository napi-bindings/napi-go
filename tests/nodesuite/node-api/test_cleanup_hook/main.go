// Porting di test/node-api/test_cleanup_hook/binding.c.
package main

import (
	"fmt"
	"os"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

var (
	cleanupHookCount int32
	// secret e wrongSecret sono variabili globali come nel C: il loro indirizzo fa da arg.
	secret      int32 = 42
	wrongSecret int32 = 17
	cleanupHook       = nt.RegisterCleanupHook(cleanup)
)

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

func cleanup(arg uintptr) {
	cleanupHookCount++
	// Come il printf del C: lo script padre legge stdout.
	if _, err := fmt.Fprintf(os.Stdout, "cleanup(%d)\n", *(*int32)(nt.Pointer(arg))); err != nil {
		fmt.Fprintf(os.Stderr, "napitest: cannot write to stdout: %v\n", err)
	}
}

func objectFinalizer(env sys.Env, data, _ uintptr) {
	// La cleanup è chiamata una volta sola.
	nt.CAssert(cleanupHookCount == 1, "cleanup_hook_count == 1")
	ref := (*sys.Ref)(nt.Pointer(data))
	if !nt.Call(env, sys.DeleteReference(env, *ref)) {
		return
	}
	nt.FreeAddr(data)
}

func createObjectWrap(env sys.Env) {
	var jsObj sys.Value
	ref := (*sys.Ref)(nt.Malloc(refSize))
	if !nt.Call(env, sys.CreateObject(env, &jsObj)) {
		return
	}
	cb, hint := nt.Finalizer(objectFinalizer, 0)
	if !nt.Call(env, sys.Wrap(env, jsObj, uintptr(unsafe.Pointer(ref)), cb, hint, ref)) {
		nt.DiscardFinalizer(hint)
		return
	}
	// Un riferimento forte, così il finalizer viene chiamato allo spegnimento.
	if !nt.Call(env, sys.ReferenceRef(env, *ref, nil)) {
		return
	}
}

func initModule(env sys.Env, _ sys.Value) sys.Value {
	// Wrap creato prima dei cleanup hook.
	createObjectWrap(env)
	// Gli status non sono controllati, come nel C.
	sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&wrongSecret))))
	sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&secret))))
	sys.RemoveEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&wrongSecret))))
	// Wrap creato dopo i cleanup hook.
	createObjectWrap(env)
	return nil
}
