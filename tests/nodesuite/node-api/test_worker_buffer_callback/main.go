// Port of test/node-api/test_worker_buffer_callback/test_worker_buffer_callback.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// bufferSize is sizeof(char).
const bufferSize = 1

// freeCallCount is shared across all envs in the process, matching the global variable in C.
var freeCallCount uint32

func getFreeCallCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var value sys.Value
	if !nt.Call(env, sys.CreateUint32(env, freeCallCount, &value)) {
		return nil
	}
	return value
}

func finalizeCb(_ sys.Env, finalizeData, _ uintptr) {
	nt.FreeAddr(finalizeData)
	freeCallCount++
}

// initModule corresponds to the body of NAPI_MODULE_INIT(): it runs for every env, including workers.
func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("getFreeCallCount", getFreeCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	// Difference from the non-Node-API test: creates an ArrayBuffer instead of a Node.js Buffer,
	// because the latter would exercise the same non-Node-API code paths rather than the
	// Node-API-specific ones.
	var buffer sys.Value
	data := nt.Malloc(bufferSize)
	cb, hint := nt.Finalizer(finalizeCb, 0)
	if !nt.Call(env, sys.CreateExternalArrayBuffer(env, uintptr(data), bufferSize, cb, hint, &buffer)) {
		nt.DiscardFinalizer(hint)
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("buffer"), buffer)) {
		return nil
	}
	return exports
}
