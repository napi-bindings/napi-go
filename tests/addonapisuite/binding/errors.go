package main

// Port of test/error.cc and test/error_handling_for_primitives.cc. error.cc is ported in its build without C++ exceptions (the variant whose control flow Go
// shares: errors are values, and a JS exception is a pending state checked after each call).
//
// C++ constructs without a Go counterpart:
//   - the copy/move semantics checks of Napi::Error become a check of the message of a new Error;
//   - Function(env, nullptr).Call() is a call on a napi.Function holding the env and no value;
//   - the "CStr" and std::string factories take the same Go string.
//
// Not ported: the promise handshake (resetPromises, waitForWorkerThread, releaseWorkerThread and
// the optional second argument of the throw* functions), used only by
// error_terminating_environment.js, which terminates worker threads mid-call.

import (
	"errors"
	"fmt"

	"github.com/napi-bindings/napi-go/napi"
)

func initError(env napi.Env) (napi.Object, error) {
	messageError := func(newErr func(message string) error) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			msg, err := argString(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, newErr(msg)
		}
	}
	plainError := func(msg string) error { return errors.New(msg) }
	typeError := func(msg string) error { return napi.TypeError("", msg) }
	rangeError := func(msg string) error { return napi.RangeError("", msg) }
	syntaxError := func(msg string) error { return napi.SyntaxError("", msg) }
	// rethrow is TypeError(env, value).ThrowAsJavaScriptException(): the error object received
	// from JS is thrown as is.
	rethrow := func(c napi.CallContext) (napi.Value, error) { return napi.Value{}, napi.Throw(c.Arg(0)) }
	return newExports(env,
		fn{"throwApiError", func(c napi.CallContext) (napi.Value, error) {
			// Function(info.Env(), nullptr): an empty function bound to the env, so that
			// Node-API can report the error.
			empty := napi.Function{Object: napi.Object{Value: napi.ValueFromRaw(c.Env().Raw(), nil)}}
			return empty.Call(napi.Value{})
		}},
		fn{"testErrorCopySemantics", checkErrorMessage("errorCopyCtor")},
		fn{"testErrorMoveSemantics", checkErrorMessage("errorMoveCtor")},
		fn{"lastExceptionErrorCode", func(c napi.CallContext) (napi.Value, error) {
			s, err := str(c.Env(), "asd")
			if err != nil {
				return napi.Value{}, err
			}
			_, err = s.Bool()
			return napi.Value{}, err
		}},
		fn{"throwJSError", messageError(plainError)},
		fn{"throwTypeError", messageError(typeError)},
		fn{"throwTypeErrorCtor", rethrow},
		fn{"throwTypeErrorCStr", messageError(typeError)},
		fn{"throwRangeError", messageError(rangeError)},
		fn{"throwRangeErrorCtor", rethrow},
		fn{"throwRangeErrorCStr", messageError(rangeError)},
		fn{"throwEmptyRangeError", func(c napi.CallContext) (napi.Value, error) {
			return napi.Value{}, napi.RangeError("", "")
		}},
		fn{"throwSyntaxError", messageError(syntaxError)},
		fn{"throwSyntaxErrorCtor", rethrow},
		fn{"throwSyntaxErrorCStr", messageError(syntaxError)},
		fn{"catchError", func(c napi.CallContext) (napi.Value, error) {
			e, caught, err := callAndCatch(c)
			if err != nil || caught {
				return e, err
			}
			return c.Env().Null(), nil
		}},
		fn{"catchErrorMessage", func(c napi.CallContext) (napi.Value, error) {
			e, caught, err := callAndCatch(c)
			if err != nil {
				return napi.Value{}, err
			}
			if !caught {
				return c.Env().Null(), nil
			}
			msg, err := unsafeObject(e).Get("message")
			if err != nil {
				return napi.Value{}, err
			}
			s, err := msg.GoString()
			if err != nil {
				return napi.Value{}, err
			}
			return str(c.Env(), s)
		}},
		fn{"doNotCatch", func(c napi.CallContext) (napi.Value, error) {
			f, err := argFunction(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			_, err = f.Call(napi.Value{})
			return napi.Value{}, err
		}},
		fn{"catchAndRethrowError", func(c napi.CallContext) (napi.Value, error) {
			e, caught, err := callAndCatch(c)
			if err != nil || !caught {
				return napi.Value{}, err
			}
			return napi.Value{}, markCaughtAndRethrow(c.Env(), e)
		}},
		fn{"throwErrorThatEscapesScope", throwErrorThatEscapesScope},
		fn{"catchAndRethrowErrorThatEscapesScope", func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			return napi.Value{}, env.WithScope(func() error {
				if _, err := throwErrorThatEscapesScope(c); !errors.Is(err, napi.ErrPendingException) {
					return err
				}
				e, err := env.GetAndClearLastException()
				if err != nil {
					return err
				}
				return markCaughtAndRethrow(env, e)
			})
		}},
		fn{"throwFatalError", func(c napi.CallContext) (napi.Value, error) {
			napi.FatalError("Error::ThrowFatalError", "This is a fatal error")
			return napi.Value{}, nil
		}},
		fn{"throwDefaultError", throwDefaultError},
	)
}

// checkErrorMessage returns an export that creates an Error with message and checks that the
// message property reads back unchanged.
func checkErrorMessage(message string) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		e, err := newError(c.Env(), message)
		if err != nil {
			return napi.Value{}, err
		}
		v, err := e.Get("message")
		if err != nil {
			return napi.Value{}, err
		}
		got, err := v.GoString()
		if err != nil {
			return napi.Value{}, err
		}
		if got != message {
			return napi.Value{}, fmt.Errorf("Error message %q, want %q", got, message)
		}
		return napi.Value{}, nil
	}
}

// callAndCatch calls the function passed as the first argument and catches the exception it
// throws, if any.
func callAndCatch(c napi.CallContext) (exception napi.Value, caught bool, err error) {
	f, err := argFunction(c, 0)
	if err != nil {
		return napi.Value{}, false, err
	}
	_, err = f.Call(napi.Value{})
	if e, ok := c.Env().Catch(err); ok {
		return e, true, nil
	}
	return napi.Value{}, false, err
}

// markCaughtAndRethrow sets e.caught = true and throws e again.
func markCaughtAndRethrow(env napi.Env, e napi.Value) error {
	t, err := env.NewBoolean(true)
	if err != nil {
		return err
	}
	if err := unsafeObject(e).Set("caught", t); err != nil {
		return err
	}
	return napi.Throw(e)
}

// throwErrorThatEscapesScope throws an Error created inside a handle scope: the exception must
// outlive the scope.
func throwErrorThatEscapesScope(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	return napi.Value{}, env.WithScope(func() error {
		msg, err := argString(c, 0)
		if err != nil {
			return err
		}
		env.Throw(errors.New(msg))
		return napi.ErrPendingException
	})
}

// throwDefaultError mirrors Napi::Error::New(env) built from the last Node-API error: with a true
// argument, reading undefined as a number fails and is thrown; the second failure (reading a
// property of undefined) then re-throws the first exception instead of replacing it.
func throwDefaultError(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	undefinedValue := env.Undefined()
	first, err := argBool(c, 0)
	if err != nil {
		return napi.Value{}, err
	}
	if first {
		_, err := undefinedValue.Uint32()
		if err == nil {
			napi.FatalError("ThrowDefaultError", "napi_get_value_uint32")
		}
		env.Throw(err)
	}
	_, err = unsafeObject(undefinedValue).Get("xyzzy")
	if err == nil {
		napi.FatalError("ThrowDefaultError", "napi_get_named_property")
	}
	return napi.Value{}, err
}

// initErrorHandlingPrim calls the function it receives: a primitive thrown by the function must
// reach the caller unchanged.
func initErrorHandlingPrim(env napi.Env) (napi.Object, error) {
	return newExports(env, fn{"errorHandlingPrim", func(c napi.CallContext) (napi.Value, error) {
		f, err := argFunction(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		_, err = f.Call(napi.Value{})
		return napi.Value{}, err
	}})
}
