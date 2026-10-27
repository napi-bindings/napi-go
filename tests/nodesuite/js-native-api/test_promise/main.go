// Porting di test/js-native-api/test_promise/test_promise.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

var deferred sys.Deferred

func createPromise(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var promise sys.Value

	// Un deferred già esistente non si sovrascrive.
	if deferred != nil {
		return nil
	}

	if !nt.Call(env, sys.CreatePromise(env, &deferred, &promise)) {
		return nil
	}

	return promise
}

func concludeCurrentPromise(env sys.Env, info sys.CallbackInfo) sys.Value {
	var argv [2]sys.Value
	argc := uint(2)
	var resolution bool

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.GetValueBool(env, argv[1], &resolution)) {
		return nil
	}
	if resolution {
		if !nt.Call(env, sys.ResolveDeferred(env, deferred, argv[0])) {
			return nil
		}
	} else {
		if !nt.Call(env, sys.RejectDeferred(env, deferred, argv[0])) {
			return nil
		}
	}

	deferred = nil

	return nil
}

func isPromise(env sys.Env, info sys.CallbackInfo) sys.Value {
	var promise, result sys.Value
	argc := uint(1)
	var isPromise bool

	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &promise, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.IsPromise(env, promise, &isPromise)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, isPromise, &result)) {
		return nil
	}

	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("createPromise", createPromise),
		nt.DeclareProperty("concludeCurrentPromise", concludeCurrentPromise),
		nt.DeclareProperty("isPromise", isPromise),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
