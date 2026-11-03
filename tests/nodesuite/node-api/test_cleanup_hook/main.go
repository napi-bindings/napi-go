// Port of test/node-api/test_cleanup_hook/binding.c.
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
	// secret and wrongSecret are global variables matching C: their addresses serve as arg.
	secret      int32 = 42
	wrongSecret int32 = 17
	cleanupHook       = nt.RegisterCleanupHook(cleanup)
)

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

func cleanup(arg uintptr) {
	cleanupHookCount++
	// Matches C's printf: the parent script reads stdout.
	if _, err := fmt.Fprintf(os.Stdout, "cleanup(%d)\n", *(*int32)(nt.Pointer(arg))); err != nil {
		fmt.Fprintf(os.Stderr, "napitest: cannot write to stdout: %v\n", err)
	}
}

func objectFinalizer(env sys.Env, data, _ uintptr) {
	// cleanup is called only once.
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
	// Strong reference so the finalizer is called at shutdown.
	if !nt.Call(env, sys.ReferenceRef(env, *ref, nil)) {
		return
	}
}

func initModule(env sys.Env, _ sys.Value) sys.Value {
	// Wrap created before the cleanup hooks.
	createObjectWrap(env)
	// Status values are not checked, matching C.
	sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&wrongSecret))))
	sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&secret))))
	sys.RemoveEnvCleanupHook(env, sys.CleanupHookTrampoline, cleanupHook.Arg(uintptr(unsafe.Pointer(&wrongSecret))))
	// Wrap created after the cleanup hooks.
	createObjectWrap(env)
	return nil
}
