package napitest

import (
	"runtime/cgo"

	"github.com/napi-bindings/napi-go/sys"
)

// AsyncExecute corresponds to napi_async_execute_callback: it runs on a libuv threadpool worker.
type AsyncExecute func(env sys.Env, data any)

// AsyncComplete corresponds to napi_async_complete_callback: it runs on the JS thread.
type AsyncComplete func(env sys.Env, status sys.Status, data any)

type asyncWork struct {
	execute  AsyncExecute
	complete AsyncComplete
	data     any
}

// AsyncWorkData prepares the data to pass to sys.CreateAsyncWork along with
// sys.AsyncExecuteTrampoline and sys.AsyncCompleteTrampoline: it corresponds to the pair of C
// callbacks plus their void* data. The same data value is passed to execute and complete, so tests
// that compare pointers in C (c == &the_carrier) compare the Go value here.
//
// The handle is released after complete, which Node-API always calls, even for canceled work.
// Work created and deleted with napi_delete_async_work without ever being queued does not release
// it: that case does not occur in the tests.
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
