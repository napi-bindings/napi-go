package napi

import (
	"github.com/napi-bindings/napi-go/sys"
)

// Value represents a JavaScript value (napi_value + env, like Napi::Value). It occupies two words
// and is passed by value without allocations. It is valid only on the JS thread and within the
// handle scope in which it was obtained: use a Ref to retain it beyond a callback.
//
// The zero Value (IsEmpty) does not represent a JS value; it is returned by functions that fail.
type Value struct {
	env sys.Env
	raw sys.Value
}

// ValueFromRaw wraps a raw sys.Value.
func ValueFromRaw(env sys.Env, raw sys.Value) Value { return Value{env: env, raw: raw} }

// Raw returns the underlying sys.Value.
func (v Value) Raw() sys.Value { return v.raw }

// Env returns the value's environment.
func (v Value) Env() Env { return Env{raw: v.env} }

// IsEmpty reports whether v is the zero Value (no underlying JS value).
func (v Value) IsEmpty() bool { return v.raw == nil }

// ValueType represents the result of typeof according to Node-API (napi_valuetype).
type ValueType = sys.ValueType

const (
	TypeUndefined = sys.TypeUndefined
	TypeNull      = sys.TypeNull
	TypeBoolean   = sys.TypeBoolean
	TypeNumber    = sys.TypeNumber
	TypeString    = sys.TypeString
	TypeSymbol    = sys.TypeSymbol
	TypeObject    = sys.TypeObject
	TypeFunction  = sys.TypeFunction
	TypeExternal  = sys.TypeExternal
	TypeBigInt    = sys.TypeBigInt
)

// Type returns the type of the value (napi_typeof). Unlike JS typeof, null has TypeNull.
func (v Value) Type() (ValueType, error) {
	var t sys.ValueType
	st := sys.Typeof(v.env, v.raw, &t)
	return t, check(v.env, st)
}

func (v Value) is(t ValueType) bool {
	got, err := v.Type()
	return err == nil && got == t
}

// IsUndefined, IsNull, ... check the value's type via napi_typeof. Any error is treated as false.
func (v Value) IsUndefined() bool { return v.is(TypeUndefined) }
func (v Value) IsNull() bool      { return v.is(TypeNull) }
func (v Value) IsBoolean() bool   { return v.is(TypeBoolean) }
func (v Value) IsNumber() bool    { return v.is(TypeNumber) }
func (v Value) IsString() bool    { return v.is(TypeString) }
func (v Value) IsSymbol() bool    { return v.is(TypeSymbol) }
func (v Value) IsFunction() bool  { return v.is(TypeFunction) }
func (v Value) IsExternal() bool  { return v.is(TypeExternal) }
func (v Value) IsBigInt() bool    { return v.is(TypeBigInt) }

// IsObject reports whether v is an object (including functions, excluding null).
func (v Value) IsObject() bool {
	t, err := v.Type()
	return err == nil && (t == TypeObject || t == TypeFunction)
}

// IsNullish reports whether v is null or undefined.
func (v Value) IsNullish() bool {
	t, err := v.Type()
	return err == nil && (t == TypeNull || t == TypeUndefined)
}

// isKind selects which napi_is_* function to invoke. Using an enum and a switch instead of a
// function value avoids an indirect call that would cause &b to escape to the heap.
type isKind uint8

const (
	isArray isKind = iota
	isArrayBuffer
	isBuffer
	isTypedArray
	isDataView
	isDate
	isPromise
	isError
)

func (v Value) isA(k isKind) bool {
	var b bool
	var st sys.Status
	switch k {
	case isArray:
		st = sys.IsArray(v.env, v.raw, &b)
	case isArrayBuffer:
		st = sys.IsArrayBuffer(v.env, v.raw, &b)
	case isBuffer:
		st = sys.IsBuffer(v.env, v.raw, &b)
	case isTypedArray:
		st = sys.IsTypedArray(v.env, v.raw, &b)
	case isDataView:
		st = sys.IsDataView(v.env, v.raw, &b)
	case isDate:
		st = sys.IsDate(v.env, v.raw, &b)
	case isPromise:
		st = sys.IsPromise(v.env, v.raw, &b)
	case isError:
		st = sys.IsError(v.env, v.raw, &b)
	}
	return st == sys.StatusOK && b
}

// IsArray, IsArrayBuffer, ... call the corresponding napi_is_* functions. Any error is treated as
// false.
func (v Value) IsArray() bool       { return v.isA(isArray) }
func (v Value) IsArrayBuffer() bool { return v.isA(isArrayBuffer) }
func (v Value) IsBuffer() bool      { return v.isA(isBuffer) }
func (v Value) IsTypedArray() bool  { return v.isA(isTypedArray) }
func (v Value) IsDataView() bool    { return v.isA(isDataView) }
func (v Value) IsDate() bool        { return v.isA(isDate) }
func (v Value) IsPromise() bool     { return v.isA(isPromise) }
func (v Value) IsError() bool       { return v.isA(isError) }

// StrictEquals compares v and w with === (napi_strict_equals).
func (v Value) StrictEquals(w Value) (bool, error) {
	var b bool
	st := sys.StrictEquals(v.env, v.raw, w.raw, &b)
	return b, check(v.env, st)
}

// InstanceOf evaluates v instanceof constructor. It may execute JS (Symbol.hasInstance).
func (v Value) InstanceOf(constructor Function) (bool, error) {
	var b bool
	st := sys.Instanceof(v.env, v.raw, constructor.raw, &b)
	return b, check(v.env, st)
}

// typeError constructs the error for a failed type check.
func typeError(st Status, want string, got Value) error {
	t, err := got.Type()
	desc := "an unreadable value"
	if err == nil {
		desc = typeName(t)
	}
	return &Error{Status: st, Kind: KindTypeError, Message: "expected " + want + ", got " + desc}
}

func typeName(t ValueType) string {
	switch t {
	case TypeUndefined:
		return "undefined"
	case TypeNull:
		return "null"
	case TypeBoolean:
		return "boolean"
	case TypeNumber:
		return "number"
	case TypeString:
		return "string"
	case TypeSymbol:
		return "symbol"
	case TypeObject:
		return "object"
	case TypeFunction:
		return "function"
	case TypeExternal:
		return "external"
	case TypeBigInt:
		return "bigint"
	}
	return t.String()
}

// AsObject returns v as an Object if it is an object or function; otherwise it returns an *Error
// (TypeError, StatusObjectExpected).
func (v Value) AsObject() (Object, error) {
	if !v.IsObject() {
		return Object{}, typeError(StatusObjectExpected, "an object", v)
	}
	return Object{v}, nil
}

// AsArray returns v as an Array if it is an array.
func (v Value) AsArray() (Array, error) {
	if !v.IsArray() {
		return Array{}, typeError(StatusArrayExpected, "an array", v)
	}
	return Array{Object{v}}, nil
}

// AsFunction returns v as a Function if it is a function.
func (v Value) AsFunction() (Function, error) {
	if !v.is(TypeFunction) {
		return Function{}, typeError(StatusFunctionExpected, "a function", v)
	}
	return Function{Object{v}}, nil
}

// AsString returns v as a String if it is a string.
func (v Value) AsString() (String, error) {
	if !v.is(TypeString) {
		return String{}, typeError(StatusStringExpected, "a string", v)
	}
	return String{v}, nil
}

// AsBigInt returns v as a BigInt if it is a bigint.
func (v Value) AsBigInt() (BigInt, error) {
	if !v.is(TypeBigInt) {
		return BigInt{}, typeError(StatusBigIntExpected, "a bigint", v)
	}
	return BigInt{v}, nil
}

// AsSymbol returns v as a Symbol if it is a symbol.
func (v Value) AsSymbol() (Symbol, error) {
	if !v.is(TypeSymbol) {
		return Symbol{}, typeError(StatusInvalidArg, "a symbol", v)
	}
	return Symbol{v}, nil
}

// AsExternal returns v as an External if it is an external.
func (v Value) AsExternal() (External, error) {
	if !v.is(TypeExternal) {
		return External{}, typeError(StatusInvalidArg, "an external", v)
	}
	return External{v}, nil
}

// AsDate returns v as a Date if it is a Date.
func (v Value) AsDate() (Date, error) {
	if !v.IsDate() {
		return Date{}, typeError(StatusDateExpected, "a Date", v)
	}
	return Date{Object{v}}, nil
}

// AsPromise returns v as a Promise if it is a Promise.
func (v Value) AsPromise() (Promise, error) {
	if !v.IsPromise() {
		return Promise{}, typeError(StatusInvalidArg, "a Promise", v)
	}
	return Promise{Object{v}}, nil
}

// AsArrayBuffer returns v as an ArrayBuffer if it is an ArrayBuffer.
func (v Value) AsArrayBuffer() (ArrayBuffer, error) {
	if !v.IsArrayBuffer() {
		return ArrayBuffer{}, typeError(sys.StatusArrayBufferExpected, "an ArrayBuffer", v)
	}
	return ArrayBuffer{Object{v}}, nil
}

// AsBuffer returns v as a Buffer if it is a Node.js Buffer (or a Uint8Array, which Node-API
// accepts as a Buffer).
func (v Value) AsBuffer() (Buffer, error) {
	if !v.IsBuffer() {
		return Buffer{}, typeError(StatusInvalidArg, "a Buffer", v)
	}
	return Buffer{Object{v}}, nil
}

// AsTypedArray returns v as a TypedArray if it is a TypedArray.
func (v Value) AsTypedArray() (TypedArray, error) {
	if !v.IsTypedArray() {
		return TypedArray{}, typeError(StatusInvalidArg, "a TypedArray", v)
	}
	return TypedArray{Object{v}}, nil
}

// AsDataView returns v as a DataView if it is a DataView.
func (v Value) AsDataView() (DataView, error) {
	if !v.IsDataView() {
		return DataView{}, typeError(StatusInvalidArg, "a DataView", v)
	}
	return DataView{Object{v}}, nil
}

// ---- reading primitives ----
//
// Node-API checks the value's type: a value of the wrong type returns an *Error with the
// corresponding status (e.g. StatusNumberExpected).

// Float64 reads a JS number as a float64.
func (v Value) Float64() (float64, error) {
	var f float64
	st := sys.GetValueDouble(v.env, v.raw, &f)
	return f, check(v.env, st)
}

// Int32 reads a JS number converted to int32 using JS ToInt32 semantics (truncation modulo 2^32;
// NaN and ±Infinity yield 0).
func (v Value) Int32() (int32, error) {
	var i int32
	st := sys.GetValueInt32(v.env, v.raw, &i)
	return i, check(v.env, st)
}

// Uint32 reads a JS number converted to uint32 using JS ToUint32 semantics.
func (v Value) Uint32() (uint32, error) {
	var u uint32
	st := sys.GetValueUint32(v.env, v.raw, &u)
	return u, check(v.env, st)
}

// Int64 reads a JS number truncated to int64; out-of-range values saturate, and NaN yields 0.
func (v Value) Int64() (int64, error) {
	var i int64
	st := sys.GetValueInt64(v.env, v.raw, &i)
	return i, check(v.env, st)
}

// Bool reads a JS boolean.
func (v Value) Bool() (bool, error) {
	var b bool
	st := sys.GetValueBool(v.env, v.raw, &b)
	return b, check(v.env, st)
}

// ---- coercion (may execute JS: valueOf, toString, Symbol.toPrimitive) ----

// ToBool applies Boolean(v).
func (v Value) ToBool() (Value, error) {
	var r sys.Value
	st := sys.CoerceToBool(v.env, v.raw, &r)
	return Env{raw: v.env}.value(r, st)
}

// ToNumber applies Number(v); throws for symbols and bigints.
func (v Value) ToNumber() (Value, error) {
	var r sys.Value
	st := sys.CoerceToNumber(v.env, v.raw, &r)
	return Env{raw: v.env}.value(r, st)
}

// ToObject applies Object(v); throws for null and undefined.
func (v Value) ToObject() (Object, error) {
	var r sys.Value
	st := sys.CoerceToObject(v.env, v.raw, &r)
	o, err := Env{raw: v.env}.value(r, st)
	return Object{o}, err
}

// ToString applies String(v); throws for symbols.
func (v Value) ToString() (String, error) {
	var r sys.Value
	st := sys.CoerceToString(v.env, v.raw, &r)
	o, err := Env{raw: v.env}.value(r, st)
	return String{o}, err
}
