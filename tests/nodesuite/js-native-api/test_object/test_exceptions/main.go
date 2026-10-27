// Porting di test/js-native-api/test_object/test_exceptions.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const expectPending = "expect exception pending"

func testExceptions(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	target := args[0]
	var exception, key, value sys.Value
	var isExceptionPending, boolResult bool
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("key"), sys.AutoLength, &key)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("value"), sys.AutoLength, &value)) {
		return nil
	}

	// procedure equivale alla macro PROCEDURE: la chiamata deve fallire con un'eccezione
	// pendente, che poi viene rimossa.
	procedure := func(status sys.Status) bool {
		if !nt.Assert(env, status == sys.StatusPendingException, "status == napi_pending_exception", expectPending) {
			return false
		}
		if !nt.Call(env, sys.IsExceptionPending(env, &isExceptionPending)) {
			return false
		}
		if !nt.Assert(env, isExceptionPending, "is_exception_pending", expectPending) {
			return false
		}
		return nt.Call(env, sys.GetAndClearLastException(env, &exception))
	}

	descriptors := []sys.PropertyDescriptor{nt.DeclarePropertyValue("key", value)}
	keyName := nt.CStr("key")

	calls := []func() sys.Status{
		// Proprietà.
		func() sys.Status { return sys.SetProperty(env, target, key, value) },
		func() sys.Status { return sys.SetNamedProperty(env, target, keyName, value) },
		func() sys.Status { return sys.HasProperty(env, target, key, &boolResult) },
		func() sys.Status { return sys.HasOwnProperty(env, target, key, &boolResult) },
		func() sys.Status { return sys.HasNamedProperty(env, target, keyName, &boolResult) },
		func() sys.Status { return sys.GetProperty(env, target, key, &value) },
		func() sys.Status { return sys.GetNamedProperty(env, target, keyName, &value) },
		func() sys.Status { return sys.DeleteProperty(env, target, key, &boolResult) },
		// Elementi.
		func() sys.Status { return sys.SetElement(env, target, 0, value) },
		func() sys.Status { return sys.HasElement(env, target, 0, &boolResult) },
		func() sys.Status { return sys.GetElement(env, target, 0, &value) },
		func() sys.Status { return sys.DeleteElement(env, target, 0, &boolResult) },
		func() sys.Status { return nt.DefineProperties(env, target, descriptors) },
		func() sys.Status {
			return sys.GetAllPropertyNames(env, target, sys.KeyOwnOnly, sys.KeyEnumerable, sys.KeyKeepNumbers, &value)
		},
		func() sys.Status { return sys.GetPropertyNames(env, target, &value) },
	}
	for _, call := range calls {
		if !procedure(call()) {
			return nil
		}
	}

	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("testExceptions", testExceptions),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
