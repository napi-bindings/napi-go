// Porting di test/node-api/test_worker_buffer_callback/test_worker_buffer_callback.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// bufferSize è sizeof(char).
const bufferSize = 1

// freeCallCount è condiviso da tutti gli env del processo, come la variabile globale del C.
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

// initModule equivale al corpo di NAPI_MODULE_INIT(): gira per ogni env, anche nei worker.
func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("getFreeCallCount", getFreeCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	// Variante rispetto al test senza Node-API: si crea un ArrayBuffer invece di un Buffer di
	// Node.js, perché quest'ultimo eserciterebbe gli stessi percorsi e non quelli specifici di
	// Node-API.
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
