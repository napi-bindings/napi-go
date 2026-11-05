package main

// Port of test/basic_types/{array,boolean,number,value}.cc.
//
// C++ constructs without a Go counterpart:
//   - an empty handle (Number(), Boolean(), Value()) is the zero napi.Value;
//   - conversion operators (static_cast<int32_t>(number), static_cast<bool>(boolean)) are the
//     typed getters, so the "operator" exports compare a getter with a second read of it;
//   - Value == / != are StrictEquals.

import (
	"math"

	"github.com/napi-bindings/napi-go/napi"
)

// <cfloat> limits: FLT_MIN and DBL_MIN are the smallest normalized values (Go's
// math.SmallestNonzeroFloat* are the smallest subnormal ones).
const (
	fltMin = 0x1p-126
	dblMin = 0x1p-1022
)

func initBasicTypesArray(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"createArray", func(c napi.CallContext) (napi.Value, error) {
			if c.Len() > 0 {
				n, err := argUint32(c, 0)
				if err != nil {
					return napi.Value{}, err
				}
				a, err := c.Env().NewArrayWithLength(int(n))
				return a.Value, err
			}
			a, err := c.Env().NewArray()
			return a.Value, err
		}},
		fn{"getLength", func(c napi.CallContext) (napi.Value, error) {
			a, err := c.Arg(0).AsArray()
			if err != nil {
				return napi.Value{}, err
			}
			n, err := a.Len()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewUint32(n)
		}},
		fn{"get", func(c napi.CallContext) (napi.Value, error) {
			a, err := c.Arg(0).AsArray()
			if err != nil {
				return napi.Value{}, err
			}
			i, err := argUint32(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			return a.GetIndex(i)
		}},
		fn{"set", func(c napi.CallContext) (napi.Value, error) {
			a, err := c.Arg(0).AsArray()
			if err != nil {
				return napi.Value{}, err
			}
			i, err := argUint32(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, a.SetIndex(i, c.Arg(2))
		}},
	)
}

func initBasicTypesBoolean(env napi.Env) (napi.Object, error) {
	// All exports except createEmptyBoolean read a boolean argument and return it.
	echo := func(c napi.CallContext) (napi.Value, error) {
		b, err := argBool(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(b)
	}
	return newExports(env,
		fn{"createBoolean", echo},
		fn{"createEmptyBoolean", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(napi.Value{}.IsEmpty())
		}},
		fn{"createBooleanFromExistingValue", echo},
		fn{"createBooleanFromPrimitive", echo},
		fn{"operatorBool", echo},
	)
}

func initBasicTypesNumber(env napi.Env) (napi.Object, error) {
	// convert returns a callback reading its first argument with get and returning it as a number.
	convert := func(get func(napi.Value) (float64, error)) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			f, err := get(c.Arg(0))
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewFloat64(f)
		}
	}
	// operator returns a callback checking that two reads of the argument agree.
	operator := func(get func(napi.Value) (float64, error)) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			a, err := get(c.Arg(0))
			if err != nil {
				return napi.Value{}, err
			}
			b, err := get(c.Arg(0))
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(a == b || (math.IsNaN(a) && math.IsNaN(b)))
		}
	}
	constant := func(f float64) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) { return c.Env().NewFloat64(f) }
	}
	int32Of := func(v napi.Value) (float64, error) { i, err := v.Int32(); return float64(i), err }
	uint32Of := func(v napi.Value) (float64, error) { u, err := v.Uint32(); return float64(u), err }
	int64Of := func(v napi.Value) (float64, error) { i, err := v.Int64(); return float64(i), err }
	floatOf := func(v napi.Value) (float64, error) { f, err := v.Float64(); return float64(float32(f)), err }
	doubleOf := napi.Value.Float64
	return newExports(env,
		fn{"toInt32", convert(int32Of)},
		fn{"toUint32", convert(uint32Of)},
		fn{"toInt64", convert(int64Of)},
		fn{"toFloat", convert(floatOf)},
		fn{"toDouble", convert(doubleOf)},
		fn{"minFloat", constant(fltMin)},
		fn{"maxFloat", constant(math.MaxFloat32)},
		fn{"minDouble", constant(dblMin)},
		fn{"maxDouble", constant(math.MaxFloat64)},
		fn{"operatorInt32", operator(int32Of)},
		fn{"operatorUint32", operator(uint32Of)},
		fn{"operatorInt64", operator(int64Of)},
		fn{"operatorFloat", operator(floatOf)},
		fn{"operatorDouble", operator(doubleOf)},
		fn{"createEmptyNumber", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(napi.Value{}.IsEmpty())
		}},
		fn{"createNumberFromExistingValue", func(c napi.CallContext) (napi.Value, error) {
			if _, err := c.Arg(0).Float64(); err != nil {
				return napi.Value{}, err
			}
			return c.Arg(0), nil
		}},
	)
}

// valueTestData is the int pointed to by the External created by createExternal.
var valueTestData = 1

func initBasicTypesValue(env napi.Env) (napi.Object, error) {
	predicate := func(is func(napi.Value) bool) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) { return c.Env().NewBoolean(is(c.Arg(0))) }
	}
	coerce := func(to func(napi.Value) (napi.Value, error)) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) { return to(c.Arg(0)) }
	}
	strictEquals := func(c napi.CallContext) (napi.Value, error) {
		eq, err := c.Arg(0).StrictEquals(c.Arg(1))
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(eq)
	}
	createExternal := func(c napi.CallContext) (napi.Value, error) {
		x, err := c.Env().NewExternal(&valueTestData, nil)
		return x.Value, err
	}
	return newExports(env,
		fn{"isEmpty", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(napi.Value{}.IsEmpty())
		}},
		fn{"isUndefined", predicate(napi.Value.IsUndefined)},
		fn{"isNull", predicate(napi.Value.IsNull)},
		fn{"isBoolean", predicate(napi.Value.IsBoolean)},
		fn{"isNumber", predicate(napi.Value.IsNumber)},
		fn{"isString", predicate(napi.Value.IsString)},
		fn{"isSymbol", predicate(napi.Value.IsSymbol)},
		fn{"isArray", predicate(napi.Value.IsArray)},
		fn{"isArrayBuffer", predicate(napi.Value.IsArrayBuffer)},
		fn{"isTypedArray", predicate(napi.Value.IsTypedArray)},
		fn{"isObject", predicate(napi.Value.IsObject)},
		fn{"isFunction", predicate(napi.Value.IsFunction)},
		fn{"isPromise", predicate(napi.Value.IsPromise)},
		fn{"isDataView", predicate(napi.Value.IsDataView)},
		fn{"isExternal", predicate(napi.Value.IsExternal)},
		fn{"toBoolean", coerce(napi.Value.ToBool)},
		fn{"toNumber", coerce(napi.Value.ToNumber)},
		fn{"toString", coerce(func(v napi.Value) (napi.Value, error) { s, err := v.ToString(); return s.Value, err })},
		fn{"toObject", coerce(func(v napi.Value) (napi.Value, error) { o, err := v.ToObject(); return o.Value, err })},
		fn{"accessProp", func(c napi.CallContext) (napi.Value, error) {
			o, err := c.Arg(0).ToObject()
			if err != nil {
				return napi.Value{}, err
			}
			return o.GetProperty(c.Arg(1))
		}},
		fn{"strictlyEquals", strictEquals},
		fn{"strictlyEqualsOverload", strictEquals},
		fn{"strictlyNotEqualsOverload", func(c napi.CallContext) (napi.Value, error) {
			eq, err := c.Arg(0).StrictEquals(c.Arg(1))
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(!eq)
		}},
		fn{"assertValueReturnsCorrectEnv", func(c napi.CallContext) (napi.Value, error) {
			x, err := createExternal(c)
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(x.Env() == c.Env())
		}},
		fn{"assertEmptyValReturnNullPtrOnCast", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(napi.Value{}.Raw() == nil)
		}},
		fn{"assertNonEmptyReturnValOnCast", func(c napi.CallContext) (napi.Value, error) {
			b, err := c.Env().NewBoolean(true)
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(b.Raw() != nil)
		}},
		fn{"createNonEmptyValue", func(c napi.CallContext) (napi.Value, error) { return str(c.Env(), "non_empty_val") }},
		fn{"createExternal", createExternal},
	)
}
