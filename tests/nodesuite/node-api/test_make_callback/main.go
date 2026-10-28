// Porting di test/node-api/test_make_callback/binding.c. Il define V8_DEPRECATION_WARNINGS del
// binding.gyp riguarda solo gli header di V8 e non ha un equivalente.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	maxArguments = 10
	reservedArgs = 3
)

var makeCallbackData = nt.Register(makeCallback)

func makeCallback(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(maxArguments)
	var n uint
	var args [maxArguments]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc > 0, "argc > 0", "Wrong number of arguments") {
		return nil
	}

	resource := args[0]
	recv := args[1]
	fn := args[2]

	var argv [maxArguments - reservedArgs]sys.Value
	for n = reservedArgs; n < argc; n++ {
		argv[n-reservedArgs] = args[n]
	}

	var funcType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, fn, &funcType)) {
		return nil
	}

	var resourceName sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("test"), sys.AutoLength, &resourceName)) {
		return nil
	}

	var context sys.AsyncContext
	if !nt.Call(env, sys.AsyncInit(env, resource, resourceName, &context)) {
		return nil
	}

	var result sys.Value
	if funcType == sys.TypeFunction {
		if !nt.Call(env, sys.MakeCallback(env, context, recv, fn, argc-reservedArgs, &argv[0], &result)) {
			return nil
		}
	} else {
		if !nt.Assert(env, false, "false", "Unexpected argument type") {
			return nil
		}
	}

	if !nt.Call(env, sys.AsyncDestroy(env, context)) {
		return nil
	}

	return result
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
