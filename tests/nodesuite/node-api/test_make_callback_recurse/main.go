// Porting di test/node-api/test_make_callback_recurse/binding.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

var makeCallbackData = nt.Register(makeCallback)

func makeCallback(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	recv := args[0]
	fn := args[1]

	status := sys.MakeCallback(env, nil /* async_context */, recv, fn, 0 /* argc */, nil, /* argv */
		nil /* result */)

	var isExceptionPending bool
	if !nt.Call(env, sys.IsExceptionPending(env, &isExceptionPending)) {
		return nil
	}
	if isExceptionPending && !(status == sys.StatusPendingException) {
		// Con un'eccezione pendente non ci si aspetta nessun altro errore.
		var pendingError sys.Value
		// Lo status non è controllato, come nel C.
		sys.GetAndClearLastException(env, &pendingError)
		if !nt.Call(env, sys.ThrowError(env, nil, nt.CStr("error when only pending exception expected"))) {
			return nil
		}
	}

	return recv
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var fn sys.Value
	if !nt.Call(env, sys.CreateFunction(env, nil, sys.AutoLength, sys.CallbackTrampoline, makeCallbackData, &fn)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("makeCallback"), fn)) {
		return nil
	}
	return exports
}
