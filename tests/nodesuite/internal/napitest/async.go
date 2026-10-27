package napitest

import (
	"runtime/cgo"

	"github.com/napi-bindings/napi-go/sys"
)

// AsyncExecute equivale a napi_async_execute_callback: gira su un thread del pool di libuv.
type AsyncExecute func(env sys.Env, data any)

// AsyncComplete equivale a napi_async_complete_callback: gira sul thread JS.
type AsyncComplete func(env sys.Env, status sys.Status, data any)

type asyncWork struct {
	execute  AsyncExecute
	complete AsyncComplete
	data     any
}

// AsyncWorkData prepara il data da passare a sys.CreateAsyncWork insieme a
// sys.AsyncExecuteTrampoline e sys.AsyncCompleteTrampoline: equivale alla coppia di callback C
// più il loro void* data. Il data arriva identico a execute e complete, quindi i test che in C
// confrontano il puntatore (c == &the_carrier) confrontano qui il valore Go.
//
// Il riferimento viene rilasciato dopo complete, che Node-API chiama sempre, anche per un lavoro
// annullato. Un lavoro creato e cancellato con napi_delete_async_work senza essere accodato non
// lo rilascia: nei test è un caso che non si presenta.
func AsyncWorkData(execute AsyncExecute, complete AsyncComplete, data any) uintptr {
	if execute == nil {
		panic("napitest: AsyncWorkData with nil execute")
	}
	return uintptr(cgo.NewHandle(&asyncWork{execute: execute, complete: complete, data: data}))
}

func dispatchAsyncExecute(env sys.Env, data uintptr) {
	w := cgo.Handle(data).Value().(*asyncWork)
	w.execute(env, w.data)
}

func dispatchAsyncComplete(env sys.Env, status sys.Status, data uintptr) {
	h := cgo.Handle(data)
	w := h.Value().(*asyncWork)
	h.Delete()
	if w.complete != nil {
		w.complete(env, status, w.data)
	}
}
