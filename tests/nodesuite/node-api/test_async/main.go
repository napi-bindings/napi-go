// Port of test/node-api/test_async/test_async.c.
//
// uv_sleep becomes time.Sleep: in the C test it only serves to keep the thread pool thread busy.
// The assert() in Execute becomes a panic, which sys converts into napi_fatal_error (abort) just like
// C's assert.
package main

import (
	"time"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// maxCancelThreads must exceed the libuv thread pool size (4 by default).
const maxCancelThreads = 6

// executeDelay is the duration of uv_sleep(1000) in the C test.
const executeDelay = 1000 * time.Millisecond

const resourceName = "TestResource"

type carrier struct {
	input    int32
	output   int32
	callback sys.Ref
	request  sys.AsyncWork
}

var (
	theCarrier   carrier
	asyncCarrier [maxCancelThreads]carrier
)

func execute(_ sys.Env, data any) {
	time.Sleep(executeDelay)
	c := data.(*carrier)

	if c != &theCarrier {
		panic("assertion failed: c == &the_carrier")
	}

	c.output = c.input * 2
}

func complete(env sys.Env, status sys.Status, data any) {
	c := data.(*carrier)

	if c != &theCarrier {
		nt.ThrowTypeError(env, "Wrong data parameter to Complete.")
		return
	}

	if status != sys.StatusOK {
		nt.ThrowTypeError(env, "Execute callback failed.")
		return
	}

	var argv [2]sys.Value

	if !nt.Call(env, sys.GetNull(env, &argv[0])) {
		return
	}
	if !nt.Call(env, sys.CreateInt32(env, c.output, &argv[1])) {
		return
	}
	var callback sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, c.callback, &callback)) {
		return
	}
	var global sys.Value
	if !nt.Call(env, sys.GetGlobal(env, &global)) {
		return
	}

	var result sys.Value
	if !nt.Call(env, sys.CallFunction(env, global, callback, uint(len(argv)), &argv[0], &result)) {
		return
	}

	if !nt.Call(env, sys.DeleteReference(env, c.callback)) {
		return
	}
	if !nt.Call(env, sys.DeleteAsyncWork(env, c.request)) {
		return
	}
}

func test(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(3)
	var argv [3]sys.Value
	var this sys.Value
	var name sys.Value
	var data unsafe.Pointer
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], &this, &data)) {
		return nil
	}
	if !nt.Assert(env, argc >= 3, "argc >= 3", "Not enough arguments, expected 3.") {
		return nil
	}

	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, argv[0], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeNumber, "t == napi_number", "Wrong first argument, integer expected.") {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, argv[1], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeObject, "t == napi_object", "Wrong second argument, object expected.") {
		return nil
	}
	if !nt.Call(env, sys.Typeof(env, argv[2], &t)) {
		return nil
	}
	if !nt.Assert(env, t == sys.TypeFunction, "t == napi_function", "Wrong third argument, function expected.") {
		return nil
	}

	theCarrier.output = 0

	if !nt.Call(env, sys.GetValueInt32(env, argv[0], &theCarrier.input)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[2], 1, &theCarrier.callback)) {
		return nil
	}

	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(resourceName), sys.AutoLength, &name)) {
		return nil
	}
	if !nt.Call(env, sys.CreateAsyncWork(env, argv[1], name, sys.AsyncExecuteTrampoline,
		sys.AsyncCompleteTrampoline, nt.AsyncWorkData(execute, complete, &theCarrier), &theCarrier.request)) {
		return nil
	}
	if !nt.Call(env, sys.QueueAsyncWork(env, theCarrier.request)) {
		return nil
	}

	return nil
}

func busyCancelComplete(env sys.Env, _ sys.Status, data any) {
	c := data.(*carrier)
	// If it fails, nt.Call has already thrown the error in JS (NODE_API_CALL_RETURN_VOID).
	if !nt.Call(env, sys.DeleteAsyncWork(env, c.request)) {
		return
	}
}

func cancelComplete(env sys.Env, status sys.Status, data any) {
	c := data.(*carrier)

	if status == sys.StatusCancelled {
		// Expected status: the callback signals that cancellation succeeded.
		var callback sys.Value
		if !nt.Call(env, sys.GetReferenceValue(env, c.callback, &callback)) {
			return
		}
		var global sys.Value
		if !nt.Call(env, sys.GetGlobal(env, &global)) {
			return
		}
		var result sys.Value
		if !nt.Call(env, sys.CallFunction(env, global, callback, 0, nil, &result)) {
			return
		}
	}

	if !nt.Call(env, sys.DeleteAsyncWork(env, c.request)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, c.callback)) {
		return
	}
}

func cancelExecute(sys.Env, any) {
	time.Sleep(executeDelay)
}

func testCancel(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var argv [1]sys.Value
	var this sys.Value
	var name sys.Value
	var data unsafe.Pointer

	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(resourceName), sys.AutoLength, &name)) {
		return nil
	}

	// The work to cancel must not be able to start: occupy all thread pool threads first.
	for i := 1; i < maxCancelThreads; i++ {
		c := &asyncCarrier[i]
		if !nt.Call(env, sys.CreateAsyncWork(env, nil, name, sys.AsyncExecuteTrampoline,
			sys.AsyncCompleteTrampoline, nt.AsyncWorkData(cancelExecute, busyCancelComplete, c), &c.request)) {
			return nil
		}
		if !nt.Call(env, sys.QueueAsyncWork(env, c.request)) {
			return nil
		}
	}

	// Queue the work and cancel it immediately: cancellation would fail if it had already started,
	// but all threads are busy with the preceding work items.
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], &this, &data)) {
		return nil
	}
	c := &asyncCarrier[0]
	if !nt.Call(env, sys.CreateAsyncWork(env, nil, name, sys.AsyncExecuteTrampoline,
		sys.AsyncCompleteTrampoline, nt.AsyncWorkData(cancelExecute, cancelComplete, c), &c.request)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, argv[0], 1, &c.callback)) {
		return nil
	}
	if !nt.Call(env, sys.QueueAsyncWork(env, c.request)) {
		return nil
	}
	if !nt.Call(env, sys.CancelAsyncWork(env, c.request)) {
		return nil
	}
	return nil
}

type repeatedWork struct {
	ref  sys.Ref
	work sys.AsyncWork
}

var repeatedWorkInfo repeatedWork

func repeatedWorkerThread(sys.Env, any) {}

func repeatedWorkComplete(env sys.Env, status sys.Status, _ any) {
	var cb, jsStatus sys.Value
	if !nt.Call(env, sys.GetReferenceValue(env, repeatedWorkInfo.ref, &cb)) {
		return
	}
	if !nt.Call(env, sys.DeleteAsyncWork(env, repeatedWorkInfo.work)) {
		return
	}
	if !nt.Call(env, sys.DeleteReference(env, repeatedWorkInfo.ref)) {
		return
	}
	repeatedWorkInfo.work = nil
	repeatedWorkInfo.ref = nil
	if !nt.Call(env, sys.CreateUint32(env, uint32(status), &jsStatus)) {
		return
	}
	// If it fails, nt.Call has already thrown the error in JS (NODE_API_CALL_RETURN_VOID).
	if !nt.Call(env, sys.CallFunction(env, cb, cb, 1, &jsStatus, nil)) {
		return
	}
}

func doRepeatedWork(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var cb, name sys.Value
	if !nt.Assert(env, repeatedWorkInfo.ref == nil, "repeated_work_info.ref == NULL",
		"Reference left over from previous work") {
		return nil
	}
	if !nt.Assert(env, repeatedWorkInfo.work == nil, "repeated_work_info.work == NULL",
		"Work pointer left over from previous work") {
		return nil
	}
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &cb, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.CreateReference(env, cb, 1, &repeatedWorkInfo.ref)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("Repeated Work"), sys.AutoLength, &name)) {
		return nil
	}
	if !nt.Call(env, sys.CreateAsyncWork(env, nil, name, sys.AsyncExecuteTrampoline,
		sys.AsyncCompleteTrampoline, nt.AsyncWorkData(repeatedWorkerThread, repeatedWorkComplete, &repeatedWorkInfo),
		&repeatedWorkInfo.work)) {
		return nil
	}
	if !nt.Call(env, sys.QueueAsyncWork(env, repeatedWorkInfo.work)) {
		return nil
	}
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("Test", test),
		nt.DeclareProperty("TestCancel", testCancel),
		nt.DeclareProperty("DoRepeatedWork", doRepeatedWork),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	return exports
}
