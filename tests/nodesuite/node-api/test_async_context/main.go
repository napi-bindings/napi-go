// Porting di test/node-api/test_async_context/binding.c.
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const (
	maxArguments                = 10
	reservedArgs                = 3
	createAsyncResourceArgc     = 2
	destroyAsyncResourceArgc    = 1
	destroyOnFinalizerArgsCount = 2
)

var (
	makeCallbackData         = nt.Register(makeCallback)
	createAsyncResourceData  = nt.Register(createAsyncResource)
	destroyAsyncResourceData = nt.Register(destroyAsyncResource)
)

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

	asyncContextWrap := args[0]
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

	var context unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, asyncContextWrap, &context)) {
		return nil
	}

	var result sys.Value
	if funcType == sys.TypeFunction {
		if !nt.Call(env, sys.MakeCallback(env, sys.AsyncContext(context), recv, fn, argc-reservedArgs, &argv[0],
			&result)) {
			return nil
		}
	} else {
		if !nt.Assert(env, false, "false", "Unexpected argument type") {
			return nil
		}
	}

	return result
}

func asyncDestroyCb(env sys.Env, data, _ uintptr) {
	status := sys.AsyncDestroy(env, sys.AsyncContext(nt.Pointer(data)))
	// Non si può usare NODE_API_ASSERT_RETURN_VOID: le eccezioni richiedono uno stack JS sotto.
	nt.CAssert(status == sys.StatusOK, "status == napi_ok")
}

func createAsyncResource(env sys.Env, info sys.CallbackInfo) sys.Value {
	var asyncContextWrap sys.Value
	if !nt.Call(env, sys.CreateObject(env, &asyncContextWrap)) {
		return nil
	}

	argc := uint(createAsyncResourceArgc)
	var args [createAsyncResourceArgc]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	resource := args[0]
	jsDestroyOnFinalizer := args[1]
	var resourceType sys.ValueType
	if !nt.Call(env, sys.Typeof(env, resource, &resourceType)) {
		return nil
	}
	if resourceType != sys.TypeObject {
		resource = nil
	}

	var resourceName sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("test_async"), sys.AutoLength, &resourceName)) {
		return nil
	}

	var context sys.AsyncContext
	if !nt.Call(env, sys.AsyncInit(env, resource, resourceName, &context)) {
		return nil
	}

	destroyOnFinalizer := true
	if argc == destroyOnFinalizerArgsCount {
		if !nt.Call(env, sys.GetValueBool(env, jsDestroyOnFinalizer, &destroyOnFinalizer)) {
			return nil
		}
	}
	if resourceType == sys.TypeObject && destroyOnFinalizer {
		cb, hint := nt.Finalizer(asyncDestroyCb, 0)
		if !nt.Call(env, sys.AddFinalizer(env, resource, uintptr(unsafe.Pointer(context)), cb, hint, nil)) {
			nt.DiscardFinalizer(hint)
			return nil
		}
	}
	if !nt.Call(env, sys.Wrap(env, asyncContextWrap, uintptr(unsafe.Pointer(context)), nil, 0, nil)) {
		return nil
	}
	return asyncContextWrap
}

func destroyAsyncResource(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(destroyAsyncResourceArgc)
	var args [destroyAsyncResourceArgc]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}

	asyncContextWrap := args[0]

	var asyncContext unsafe.Pointer
	if !nt.Call(env, sys.RemoveWrap(env, asyncContextWrap, &asyncContext)) {
		return nil
	}
	if !nt.Call(env, sys.AsyncDestroy(env, sys.AsyncContext(asyncContext))) {
		return nil
	}

	return asyncContextWrap
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var fn sys.Value
	for _, f := range []struct {
		name string
		data uintptr
	}{
		{"makeCallback", makeCallbackData},
		{"createAsyncResource", createAsyncResourceData},
		{"destroyAsyncResource", destroyAsyncResourceData},
	} {
		if !nt.Call(env, sys.CreateFunction(env, nil, sys.AutoLength, sys.CallbackTrampoline, f.data, &fn)) {
			return nil
		}
		if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr(f.name), fn)) {
			return nil
		}
	}
	return exports
}
