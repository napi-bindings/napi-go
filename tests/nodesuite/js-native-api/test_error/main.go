// Porting di test/js-native-api/test_error/test_error.c (#define NAPI_VERSION 9 → napi-go.json).
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

const testCode = "ERR_TEST_CODE"

func checkError(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var r bool
	if !nt.Call(env, sys.IsError(env, args[0], &r)) {
		return nil
	}

	var result sys.Value
	if !nt.Call(env, sys.GetBoolean(env, r, &result)) {
		return nil
	}

	return result
}

func throwExistingError(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var message, err sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("existing error"), sys.AutoLength, &message)) {
		return nil
	}
	if !nt.Call(env, sys.CreateError(env, nil, message, &err)) {
		return nil
	}
	nt.Call(env, sys.Throw(env, err))
	return nil
}

// throwFn equivale a napi_throw_error, napi_throw_range_error, … (code, msg C).
type throwFn func(env sys.Env, code, msg *byte) sys.Status

// thrower restituisce la funzione JS che chiama throw con code (nil = NULL) e msg.
func thrower(throw throwFn, code *byte, msg string) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		nt.Call(env, throw(env, code, nt.CStr(msg)))
		return nil
	}
}

// createFn equivale a napi_create_error, napi_create_range_error, … (code, msg napi_value).
type createFn func(env sys.Env, code, msg sys.Value, result *sys.Value) sys.Status

// creator restituisce la funzione JS che crea l'errore con il messaggio msg e, se withCode, il
// codice ERR_TEST_CODE.
func creator(create createFn, msg string, withCode bool) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var result, message, code sys.Value
		if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(msg), sys.AutoLength, &message)) {
			return nil
		}
		if withCode {
			if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(testCode), sys.AutoLength, &code)) {
				return nil
			}
		}
		if !nt.Call(env, create(env, code, message, &result)) {
			return nil
		}
		return result
	}
}

func throwArbitrary(env sys.Env, info sys.CallbackInfo) sys.Value {
	var arbitrary sys.Value
	argc := uint(1)
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &arbitrary, nil, nil)) {
		return nil
	}
	nt.Call(env, sys.Throw(env, arbitrary))
	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	code := nt.CStr(testCode)
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("checkError", checkError),
		nt.DeclareProperty("throwExistingError", throwExistingError),
		nt.DeclareProperty("throwError", thrower(sys.ThrowError, nil, "error")),
		nt.DeclareProperty("throwRangeError", thrower(sys.ThrowRangeError, nil, "range error")),
		nt.DeclareProperty("throwTypeError", thrower(sys.ThrowTypeError, nil, "type error")),
		nt.DeclareProperty("throwSyntaxError", thrower(sys.ThrowSyntaxError, nil, "syntax error")),
		nt.DeclareProperty("throwErrorCode", thrower(sys.ThrowError, code, "Error [error]")),
		nt.DeclareProperty("throwRangeErrorCode", thrower(sys.ThrowRangeError, code, "RangeError [range error]")),
		nt.DeclareProperty("throwTypeErrorCode", thrower(sys.ThrowTypeError, code, "TypeError [type error]")),
		nt.DeclareProperty("throwSyntaxErrorCode", thrower(sys.ThrowSyntaxError, code, "SyntaxError [syntax error]")),
		nt.DeclareProperty("throwArbitrary", throwArbitrary),
		nt.DeclareProperty("createError", creator(sys.CreateError, "error", false)),
		nt.DeclareProperty("createRangeError", creator(sys.CreateRangeError, "range error", false)),
		nt.DeclareProperty("createTypeError", creator(sys.CreateTypeError, "type error", false)),
		nt.DeclareProperty("createSyntaxError", creator(sys.CreateSyntaxError, "syntax error", false)),
		nt.DeclareProperty("createErrorCode", creator(sys.CreateError, "Error [error]", true)),
		nt.DeclareProperty("createRangeErrorCode", creator(sys.CreateRangeError, "RangeError [range error]", true)),
		nt.DeclareProperty("createTypeErrorCode", creator(sys.CreateTypeError, "TypeError [type error]", true)),
		nt.DeclareProperty("createSyntaxErrorCode", creator(sys.CreateSyntaxError, "SyntaxError [syntax error]", true)),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
