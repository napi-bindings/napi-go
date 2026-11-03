// Port of test/js-native-api/test_new_target/test_new_target.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func baseClass(env sys.Env, info sys.CallbackInfo) sys.Value {
	var newTargetArg sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTargetArg)) {
		return nil
	}
	var thisArg sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &thisArg, nil)) {
		return nil
	}
	var undefined sys.Value
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return nil
	}

	// this !== new.target because we are being invoked through super().
	var result bool
	if !nt.Call(env, sys.StrictEquals(env, newTargetArg, thisArg, &result)) {
		return nil
	}
	if !nt.Assert(env, !result, "!result", "this !== new.target") {
		return nil
	}

	// new.target !== undefined because we should be called as a new expression.
	if !nt.Assert(env, newTargetArg != nil, "newTargetArg != NULL", "newTargetArg != NULL") {
		return nil
	}
	if !nt.Call(env, sys.StrictEquals(env, newTargetArg, undefined, &result)) {
		return nil
	}
	if !nt.Assert(env, !result, "!result", "new.target !== undefined") {
		return nil
	}

	return thisArg
}

func constructor(env sys.Env, info sys.CallbackInfo) sys.Value {
	var result bool
	var newTargetArg sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTargetArg)) {
		return nil
	}
	argc := uint(1)
	var argv, thisArg sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv, &thisArg, nil)) {
		return nil
	}
	var undefined sys.Value
	if !nt.Call(env, sys.GetUndefined(env, &undefined)) {
		return nil
	}

	// new.target !== undefined because we should be called as a new expression.
	if !nt.Assert(env, newTargetArg != nil, "newTargetArg != NULL", "newTargetArg != NULL") {
		return nil
	}
	if !nt.Call(env, sys.StrictEquals(env, newTargetArg, undefined, &result)) {
		return nil
	}
	if !nt.Assert(env, !result, "!result", "new.target !== undefined") {
		return nil
	}

	// arguments[0] is Constructor itself (passed by the test script).
	if !nt.Call(env, sys.StrictEquals(env, newTargetArg, argv, &result)) {
		return nil
	}
	if !nt.Assert(env, result, "result", "new.target === Constructor") {
		return nil
	}

	return thisArg
}

func ordinaryFunction(env sys.Env, info sys.CallbackInfo) sys.Value {
	var newTargetArg sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTargetArg)) {
		return nil
	}
	if !nt.Assert(env, newTargetArg == nil, "newTargetArg == NULL", "newTargetArg == NULL") {
		return nil
	}
	var trueValue sys.Value
	if !nt.Call(env, sys.GetBoolean(env, true, &trueValue)) {
		return nil
	}
	return trueValue
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	desc := []sys.PropertyDescriptor{
		nt.DeclareProperty("BaseClass", baseClass),
		nt.DeclareProperty("OrdinaryFunction", ordinaryFunction),
		nt.DeclareProperty("Constructor", constructor),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, desc)) {
		return nil
	}
	return exports
}
