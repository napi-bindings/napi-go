// Porting di test/node-api/test_fatal/test_fatal.c. uv_thread_create diventa nt.Thread.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// Lunghezze esplicite di TestStringLength: troncano "test_fatal::TestStringLength" a
// "test_fatal::Test" e "fatal message" resta intero.
const (
	testStringLengthLocationLen = 16
	testStringLengthMessageLen  = 13
)

var uvThread nt.Thread

func workThread() {
	sys.FatalError(nt.CStr("work_thread"), sys.AutoLength, nt.CStr("foobar"), sys.AutoLength)
}

func test(sys.Env, sys.CallbackInfo) sys.Value {
	sys.FatalError(nt.CStr("test_fatal::Test"), sys.AutoLength, nt.CStr("fatal message"), sys.AutoLength)
	return nil
}

func testThread(env sys.Env, _ sys.CallbackInfo) sys.Value {
	if !nt.Assert(env, uvThread.Create(workThread) == 0,
		"(uv_thread_create(&uv_thread, work_thread, NULL) == 0)", "Thread creation") {
		return nil
	}
	return nil
}

func testStringLength(sys.Env, sys.CallbackInfo) sys.Value {
	sys.FatalError(nt.CStr("test_fatal::TestStringLength"), testStringLengthLocationLen, nt.CStr("fatal message"),
		testStringLengthMessageLen)
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("Test", test),
		nt.DeclareProperty("TestStringLength", testStringLength),
		nt.DeclareProperty("TestThread", testThread),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}
	return exports
}
