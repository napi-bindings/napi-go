// Port of test/node-api/test_async_cleanup_hook/binding.c. uv_async_t and uv_close go through
// napitest's libuv shim; struct AsyncData becomes a Go struct with the uv_async_t handle in
// C memory (whose data field points to the struct's cgo.Handle, matching handle->data in C).
package main

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// cleanupHooksExpected: AsyncCleanupHook and the two subsequent callbacks each run twice.
const cleanupHooksExpected = 6

const refSize = uint(unsafe.Sizeof(sys.Ref(nil)))

var cleanupHookCount int32

func mustNotCall(sys.AsyncCleanupHookHandle, uintptr) {
	nt.CAssert(false, "0")
}

type asyncData struct {
	async  unsafe.Pointer // uv_async_t
	env    sys.Env
	handle sys.AsyncCleanupHookHandle
}

func createAsyncData() *asyncData {
	return &asyncData{async: nt.NewUVHandle(nt.UVAsync)}
}

func afterCleanupHookTwo(handle unsafe.Pointer) {
	cleanupHookCount++
	h := cgo.Handle(nt.UVData(handle))
	data := h.Value().(*asyncData)
	status := sys.RemoveAsyncCleanupHook(data.handle)
	nt.CAssert(status == sys.StatusOK, "status == napi_ok")
	h.Delete()
	nt.Free(data.async)
}

func afterCleanupHookOne(async unsafe.Pointer) {
	cleanupHookCount++
	nt.UVClose(async, afterCleanupHookTwo)
}

func asyncCleanupHook(handle sys.AsyncCleanupHookHandle, arg uintptr) {
	cleanupHookCount++
	h := cgo.Handle(arg)
	data := h.Value().(*asyncData)
	var loop unsafe.Pointer
	status := sys.GetUVEventLoop(data.env, &loop)
	nt.CAssert(status == sys.StatusOK, "status == napi_ok")
	err := nt.UVAsyncInit(loop, data.async, afterCleanupHookOne)
	nt.CAssert(err == 0, "err == 0")

	nt.SetUVData(data.async, arg)
	data.handle = handle
	nt.UVAsyncSend(data.async)
}

func objectFinalizer(env sys.Env, data, _ uintptr) {
	// AsyncCleanupHook and the subsequent callbacks are called twice.
	nt.CAssert(cleanupHookCount == cleanupHooksExpected, "cleanup_hook_count == 6")
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

var (
	asyncCleanupHookID = nt.RegisterAsyncCleanupHook(asyncCleanupHook)
	mustNotCallID      = nt.RegisterAsyncCleanupHook(mustNotCall)
)

func initModule(env sys.Env, _ sys.Value) sys.Value {
	// In C the static variable is zeroed for musl compatibility.
	cleanupHookCount = 0
	// Wrap created before the cleanup hooks.
	createObjectWrap(env)

	// Status values are not checked, matching C.
	{
		data := createAsyncData()
		data.env = env
		sys.AddAsyncCleanupHook(env, sys.AsyncCleanupHookTrampoline,
			asyncCleanupHookID.Arg(uintptr(cgo.NewHandle(data))), &data.handle)
	}

	{
		data := createAsyncData()
		data.env = env
		sys.AddAsyncCleanupHook(env, sys.AsyncCleanupHookTrampoline,
			asyncCleanupHookID.Arg(uintptr(cgo.NewHandle(data))), nil)
	}

	{
		var mustNotCallHandle sys.AsyncCleanupHookHandle
		sys.AddAsyncCleanupHook(env, sys.AsyncCleanupHookTrampoline, mustNotCallID.Arg(0), &mustNotCallHandle)
		sys.RemoveAsyncCleanupHook(mustNotCallHandle)
	}

	// Wrap created after the cleanup hooks.
	createObjectWrap(env)
	return nil
}
