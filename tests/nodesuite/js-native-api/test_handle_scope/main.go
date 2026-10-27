// Porting di test/js-native-api/test_handle_scope/test_handle_scope.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func newScope(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var scope sys.HandleScope
	var output sys.Value

	if !nt.Call(env, sys.OpenHandleScope(env, &scope)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &output)) {
		return nil
	}
	if !nt.Call(env, sys.CloseHandleScope(env, scope)) {
		return nil
	}
	return nil
}

func newScopeEscape(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var scope sys.EscapableHandleScope
	var output, escapee sys.Value

	if !nt.Call(env, sys.OpenEscapableHandleScope(env, &scope)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &output)) {
		return nil
	}
	if !nt.Call(env, sys.EscapeHandle(env, scope, output, &escapee)) {
		return nil
	}
	if !nt.Call(env, sys.CloseEscapableHandleScope(env, scope)) {
		return nil
	}
	return escapee
}

func newScopeEscapeTwice(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var scope sys.EscapableHandleScope
	var output, escapee sys.Value

	if !nt.Call(env, sys.OpenEscapableHandleScope(env, &scope)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &output)) {
		return nil
	}
	if !nt.Call(env, sys.EscapeHandle(env, scope, output, &escapee)) {
		return nil
	}
	status := sys.EscapeHandle(env, scope, output, &escapee)
	if !nt.Assert(env, status == sys.StatusEscapeCalledTwice, "status == napi_escape_called_twice",
		"Escaping twice fails") {
		return nil
	}
	if !nt.Call(env, sys.CloseEscapableHandleScope(env, scope)) {
		return nil
	}
	return nil
}

func newScopeWithException(env sys.Env, info sys.CallbackInfo) sys.Value {
	var scope sys.HandleScope
	var exceptionFunction, output sys.Value

	if !nt.Call(env, sys.OpenHandleScope(env, &scope)) {
		return nil
	}
	if !nt.Call(env, sys.CreateObject(env, &output)) {
		return nil
	}

	argc := uint(1)
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &exceptionFunction, nil, nil)) {
		return nil
	}

	status := sys.CallFunction(env, output, exceptionFunction, 0, nil, nil)
	if !nt.Assert(env, status == sys.StatusPendingException, "status == napi_pending_exception",
		"Function should have thrown.") {
		return nil
	}

	// La chiusura di uno scope deve riuscire anche con un'eccezione pendente.
	if !nt.Call(env, sys.CloseHandleScope(env, scope)) {
		return nil
	}
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("NewScope", newScope),
		nt.DeclareProperty("NewScopeEscape", newScopeEscape),
		nt.DeclareProperty("NewScopeEscapeTwice", newScopeEscapeTwice),
		nt.DeclareProperty("NewScopeWithException", newScopeWithException),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	return exports
}
