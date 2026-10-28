// Porting di test/js-native-api/8_passing_wrapped/8_passing_wrapped.cc.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func createObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var instance sys.Value
	if !nt.Call(env, newInstance(env, args[0], &instance)) {
		return nil
	}

	return instance
}

func add(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	obj1, ok := unwrap(env, args[0])
	if !ok {
		return nil
	}

	obj2, ok := unwrap(env, args[1])
	if !ok {
		return nil
	}

	var sum sys.Value
	if !nt.Call(env, sys.CreateDouble(env, obj1.val()+obj2.val(), &sum)) {
		return nil
	}

	return sum
}

func finalizeCountFn(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var returnValue sys.Value
	if !nt.Call(env, sys.CreateUint32(env, finalizeCount, &returnValue)) {
		return nil
	}

	return returnValue
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	// Lo status non è controllato, come nel C.
	initClass(env)

	desc := []sys.PropertyDescriptor{
		nt.DeclareProperty("createObject", createObject),
		nt.DeclareProperty("add", add),
		nt.DeclareProperty("finalizeCount", finalizeCountFn),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}

	return exports
}
