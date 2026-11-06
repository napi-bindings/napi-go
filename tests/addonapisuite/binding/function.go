package main

// Port of test/function.cc.
//
// C++ constructs without a Go counterpart:
//   - Function::New(env, cb) and the templated Function::New<cb>(env) create the same function,
//     so the "plain" and "templated" objects hold the same exports;
//   - the initializer-list, std::vector, C array and operator() forms of Call/New are one
//     variadic Call/New;
//   - the data pointer of a function (Function::New(env, cb, name, &data)) is a variable captured
//     by the Go closure, and lambdas with move-only captures are ordinary closures.
//
// napi does not wrap async contexts yet (Phase 3): MakeCallback goes through sys
// (napi_async_init, napi_make_callback, napi_async_destroy), as Napi::AsyncContext does.

import (
	"errors"

	"github.com/napi-bindings/napi-go/napi"
	"github.com/napi-bindings/napi-go/sys"
)

// functionTestData is the data pointer of the "WithData" callbacks.
var functionTestData = 1

// lambdaCapture is the value captured by the lambda exports.
const lambdaCapture = 42

// callArgs are the arguments after the function: (fn, a, b, c) → a, b, c.
func callArgs(c napi.CallContext, first int) []napi.Value {
	return []napi.Value{c.Arg(first), c.Arg(first + 1), c.Arg(first + 2)}
}

func initFunction(env napi.Env) (napi.Object, error) {
	setFoo := func(env napi.Env, o napi.Object, withData bool) error {
		if err := o.Set("foo", must(str(env, "bar"))); err != nil {
			return err
		}
		if withData {
			return o.Set("data", must(env.NewInt32(int32(functionTestData))))
		}
		return nil
	}
	voidCallback := func(withData bool) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			o, err := argObject(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, setFoo(c.Env(), o, withData)
		}
	}
	valueCallback := func(withData bool) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			o, err := c.Env().NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			return o.Value, setFoo(c.Env(), o, withData)
		}
	}
	call := func(c napi.CallContext) (napi.Value, error) {
		f, err := argFunction(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		return f.Call(napi.Value{}, callArgs(c, 1)...)
	}
	callWithReceiver := func(c napi.CallContext) (napi.Value, error) {
		f, err := argFunction(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		return f.Call(c.Arg(1), callArgs(c, 2)...)
	}
	construct := func(c napi.CallContext) (napi.Value, error) {
		f, err := argFunction(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		o, err := f.New(callArgs(c, 1)...)
		return o.Value, err
	}
	fns := []fn{
		{"emptyConstructor", func(c napi.CallContext) (napi.Value, error) {
			isEmpty, err := argBool(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			f := napi.Function{}
			if !isEmpty {
				// Function(env, Object::New(env)): a non-empty handle, whatever it holds.
				o, err := c.Env().NewObject()
				if err != nil {
					return napi.Value{}, err
				}
				f = napi.Function{Object: o}
			}
			return c.Env().NewBoolean(f.IsEmpty())
		}},
		{"voidCallback", voidCallback(false)},
		{"valueCallback", valueCallback(false)},
		{"voidCallbackWithData", voidCallback(true)},
		{"valueCallbackWithData", valueCallback(true)},
		{"newTargetCallback", func(c napi.CallContext) (napi.Value, error) { return c.NewTarget() }},
		{"callWithArgs", call},
		{"callWithVector", call},
		{"callWithVectorUsingCppWrapper", call},
		{"callWithCStyleArray", call},
		{"callWithReceiverAndCStyleArray", callWithReceiver},
		{"callWithReceiverAndArgs", callWithReceiver},
		{"callWithReceiverAndVector", callWithReceiver},
		{"callWithReceiverAndVectorUsingCppWrapper", callWithReceiver},
		{"callWithInvalidReceiver", func(c napi.CallContext) (napi.Value, error) {
			f, err := argFunction(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			// Function.Call treats an empty receiver as undefined: pass the NULL receiver of
			// Call(Value(), {}) to Node-API through sys.
			var r sys.Value
			st := sys.CallFunction(c.Env().Raw(), nil, f.Raw(), 0, nil, &r)
			return napi.ValueFromRaw(c.Env().Raw(), r), c.Env().Check(st)
		}},
		{"callConstructorWithArgs", construct},
		{"callConstructorWithVector", construct},
		{"callConstructorWithCStyleArray", construct},
		{"isConstructCall", func(c napi.CallContext) (napi.Value, error) {
			f, err := argFunction(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			b, err := c.Env().NewBoolean(c.IsConstructCall())
			if err != nil {
				return napi.Value{}, err
			}
			_, err = f.Call(napi.Value{}, b)
			return napi.Value{}, err
		}},
		{"makeCallbackWithArgs", makeCallback},
		{"makeCallbackWithVector", makeCallback},
		{"makeCallbackWithCStyleArray", makeCallback},
		{"makeCallbackWithInvalidReceiver", func(c napi.CallContext) (napi.Value, error) {
			f, err := argFunction(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			var r sys.Value
			st := sys.MakeCallback(c.Env().Raw(), nil, nil, f.Raw(), 0, nil, &r)
			return napi.Value{}, c.Env().Check(st)
		}},
		{"callWithFunctionOperator", call},
	}
	result, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	for _, name := range []string{"plain", "templated"} {
		o, err := newExports(env, fns...)
		if err != nil {
			return napi.Object{}, err
		}
		if err := result.Set(name, o.Value); err != nil {
			return napi.Object{}, err
		}
	}
	capture := lambdaCapture
	moveOnly := &capture
	lambda, err := newExports(env,
		fn{"lambdaWithNoCapture", func(c napi.CallContext) (napi.Value, error) { return c.Env().NewBoolean(true) }},
		fn{"lambdaWithCapture", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(capture == lambdaCapture)
		}},
		fn{"lambdaWithMoveOnlyCapture", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(*moveOnly == lambdaCapture)
		}},
	)
	if err != nil {
		return napi.Object{}, err
	}
	return result, result.Set("lambda", lambda.Value)
}

// makeCallback is callback.MakeCallback(resource, {a, b, c}, AsyncContext(env,
// "function_test_context", resource)).
func makeCallback(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	f, err := argFunction(c, 0)
	if err != nil {
		return napi.Value{}, err
	}
	resource, err := argObject(c, 1)
	if err != nil {
		return napi.Value{}, err
	}
	name, err := env.NewString("function_test_context")
	if err != nil {
		return napi.Value{}, err
	}
	var ctx sys.AsyncContext
	if err := env.Check(sys.AsyncInit(env.Raw(), resource.Raw(), name.Raw(), &ctx)); err != nil {
		return napi.Value{}, err
	}
	args := callArgs(c, 2)
	raw := make([]sys.Value, len(args))
	for i, a := range args {
		raw[i] = a.Raw()
	}
	var r sys.Value
	st := sys.MakeCallback(env.Raw(), ctx, resource.Raw(), f.Raw(), uint(len(raw)), &raw[0], &r)
	callErr := env.Check(st)
	if err := env.Check(sys.AsyncDestroy(env.Raw(), ctx)); err != nil {
		return napi.Value{}, errors.Join(callErr, err)
	}
	return napi.Value{}, callErr
}
