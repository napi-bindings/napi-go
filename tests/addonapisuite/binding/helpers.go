package main

import (
	"errors"

	"github.com/napi-bindings/napi-go/napi"
)

// Error messages in this package are copied verbatim from the C++ tests (capitalized, often with a
// trailing period), since the scripts may compare them; Go's lowercase convention does not apply.

// fn is a named function of a module's exports object.
type fn struct {
	name string
	cb   napi.Callback
}

// newExports creates an object with one function property per entry, like the
// `exports["name"] = Function::New(env, Name)` lines of the C++ Init functions.
func newExports(env napi.Env, fns ...fn) (napi.Object, error) {
	o, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	return o, setFunctions(env, o, fns...)
}

func setFunctions(env napi.Env, o napi.Object, fns ...fn) error {
	for _, f := range fns {
		v, err := env.NewFunction(f.name, f.cb)
		if err != nil {
			return err
		}
		if err := o.Set(f.name, v.Value); err != nil {
			return err
		}
	}
	return nil
}

// ---- value constructors returning (napi.Value, error), the callback result type ----

func boolean(env napi.Env, b bool) (napi.Value, error) { return env.NewBoolean(b) }

func number(env napi.Env, f float64) (napi.Value, error) { return env.NewFloat64(f) }

func str(env napi.Env, s string) (napi.Value, error) {
	v, err := env.NewString(s)
	return v.Value, err
}

func undefined(c napi.CallContext) (napi.Value, error) { return napi.Value{}, nil }

// ---- argument readers: the Go counterpart of info[i].As<T>().XValue() ----

func argString(c napi.CallContext, i int) (string, error) { return c.Arg(i).GoString() }

func argFloat(c napi.CallContext, i int) (float64, error) { return c.Arg(i).Float64() }

func argUint32(c napi.CallContext, i int) (uint32, error) { return c.Arg(i).Uint32() }

func argBool(c napi.CallContext, i int) (bool, error) { return c.Arg(i).Bool() }

func argObject(c napi.CallContext, i int) (napi.Object, error) { return c.Arg(i).AsObject() }

func argFunction(c napi.CallContext, i int) (napi.Function, error) { return c.Arg(i).AsFunction() }

// toGoString is MaybeUnwrap(value.ToString()) followed by Utf8Value().
func toGoString(v napi.Value) (string, error) {
	s, err := v.ToString()
	if err != nil {
		return "", err
	}
	return s.GoString()
}

// newError is Napi::Error::New(env, message): an Error object, not thrown.
func newError(env napi.Env, message string) (napi.Object, error) {
	return env.NewError(errors.New(message))
}

// wellKnownSymbol is Napi::Symbol::WellKnown: Symbol[name] from the global Symbol constructor.
func wellKnownSymbol(env napi.Env, name string) (napi.Value, error) {
	ctor, err := env.Global().Get("Symbol")
	if err != nil {
		return napi.Value{}, err
	}
	o, err := ctor.AsObject()
	if err != nil {
		return napi.Value{}, err
	}
	return o.Get(name)
}

// setProps sets each string-keyed property of props on o (obj["key"] = value).
func setProps(o napi.Object, props map[string]napi.Value) error {
	for k, v := range props {
		if err := o.Set(k, v); err != nil {
			return err
		}
	}
	return nil
}

// must returns v, panicking if err is not nil. Only for creating primitive values, which fails
// only if the env is broken: the panic reaches JS as an Error with code GO_PANIC.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
