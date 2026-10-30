package napi

import (
	"github.com/napi-bindings/napi-go/sys"
)

// Value è un valore JavaScript (napi_value + env, come Napi::Value). Occupa due word e si passa per
// valore senza allocazioni. È valido solo sul thread JS e solo nello handle scope in cui è stato
// ottenuto: per conservarlo oltre la callback si usa una Ref.
//
// Il Value zero (IsEmpty) non è un valore JS: lo restituiscono le funzioni che falliscono.
type Value struct {
	env sys.Env
	raw sys.Value
}

// ValueFromRaw avvolge un napi_value di sys.
func ValueFromRaw(env sys.Env, raw sys.Value) Value { return Value{env: env, raw: raw} }

// Raw restituisce il napi_value di sys.
func (v Value) Raw() sys.Value { return v.raw }

// Env restituisce l'env del valore.
func (v Value) Env() Env { return Env{raw: v.env} }

// IsEmpty dice se v è il Value zero (nessun valore JS).
func (v Value) IsEmpty() bool { return v.raw == nil }

// ValueType è il risultato di typeof secondo Node-API (napi_valuetype).
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

// Type restituisce il tipo del valore (napi_typeof). A differenza di typeof in JS, null ha
// TypeNull.
func (v Value) Type() (ValueType, error) {
	var t sys.ValueType
	st := sys.Typeof(v.env, v.raw, &t)
	return t, check(v.env, st)
}

func (v Value) is(t ValueType) bool {
	got, err := v.Type()
	return err == nil && got == t
}

// IsUndefined, IsNull, ... controllano il tipo con napi_typeof. Un errore conta come false.
func (v Value) IsUndefined() bool { return v.is(TypeUndefined) }
func (v Value) IsNull() bool      { return v.is(TypeNull) }
func (v Value) IsBoolean() bool   { return v.is(TypeBoolean) }
func (v Value) IsNumber() bool    { return v.is(TypeNumber) }
func (v Value) IsString() bool    { return v.is(TypeString) }
func (v Value) IsSymbol() bool    { return v.is(TypeSymbol) }
func (v Value) IsFunction() bool  { return v.is(TypeFunction) }
func (v Value) IsExternal() bool  { return v.is(TypeExternal) }
func (v Value) IsBigInt() bool    { return v.is(TypeBigInt) }

// IsObject dice se v è un oggetto (funzioni comprese, null escluso).
func (v Value) IsObject() bool {
	t, err := v.Type()
	return err == nil && (t == TypeObject || t == TypeFunction)
}

// IsNullish dice se v è null o undefined.
func (v Value) IsNullish() bool {
	t, err := v.Type()
	return err == nil && (t == TypeNull || t == TypeUndefined)
}

// isKind seleziona la funzione napi_is_* da usare. Un indice e una switch al posto di un valore
// funzione: con una chiamata indiretta &b finirebbe sullo heap.
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

// IsArray, IsArrayBuffer, ... usano le funzioni napi_is_* corrispondenti. Un errore conta come
// false.
func (v Value) IsArray() bool       { return v.isA(isArray) }
func (v Value) IsArrayBuffer() bool { return v.isA(isArrayBuffer) }
func (v Value) IsBuffer() bool      { return v.isA(isBuffer) }
func (v Value) IsTypedArray() bool  { return v.isA(isTypedArray) }
func (v Value) IsDataView() bool    { return v.isA(isDataView) }
func (v Value) IsDate() bool        { return v.isA(isDate) }
func (v Value) IsPromise() bool     { return v.isA(isPromise) }
func (v Value) IsError() bool       { return v.isA(isError) }

// StrictEquals confronta con === (napi_strict_equals).
func (v Value) StrictEquals(w Value) (bool, error) {
	var b bool
	st := sys.StrictEquals(v.env, v.raw, w.raw, &b)
	return b, check(v.env, st)
}

// InstanceOf valuta v instanceof constructor. Può eseguire JS (Symbol.hasInstance).
func (v Value) InstanceOf(constructor Function) (bool, error) {
	var b bool
	st := sys.Instanceof(v.env, v.raw, constructor.raw, &b)
	return b, check(v.env, st)
}

// typeError costruisce l'errore di un controllo di tipo fallito.
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

// AsObject restituisce v come Object se è un oggetto o una funzione; altrimenti un *Error
// (TypeError, StatusObjectExpected).
func (v Value) AsObject() (Object, error) {
	if !v.IsObject() {
		return Object{}, typeError(StatusObjectExpected, "an object", v)
	}
	return Object{v}, nil
}

// AsArray restituisce v come Array se è un array.
func (v Value) AsArray() (Array, error) {
	if !v.IsArray() {
		return Array{}, typeError(StatusArrayExpected, "an array", v)
	}
	return Array{Object{v}}, nil
}

// AsFunction restituisce v come Function se è una funzione.
func (v Value) AsFunction() (Function, error) {
	if !v.is(TypeFunction) {
		return Function{}, typeError(StatusFunctionExpected, "a function", v)
	}
	return Function{Object{v}}, nil
}

// AsString restituisce v come String se è una stringa.
func (v Value) AsString() (String, error) {
	if !v.is(TypeString) {
		return String{}, typeError(StatusStringExpected, "a string", v)
	}
	return String{v}, nil
}

// AsBigInt restituisce v come BigInt se è un bigint.
func (v Value) AsBigInt() (BigInt, error) {
	if !v.is(TypeBigInt) {
		return BigInt{}, typeError(StatusBigIntExpected, "a bigint", v)
	}
	return BigInt{v}, nil
}

// AsSymbol restituisce v come Symbol se è un symbol.
func (v Value) AsSymbol() (Symbol, error) {
	if !v.is(TypeSymbol) {
		return Symbol{}, typeError(StatusInvalidArg, "a symbol", v)
	}
	return Symbol{v}, nil
}

// AsExternal restituisce v come External se è un external.
func (v Value) AsExternal() (External, error) {
	if !v.is(TypeExternal) {
		return External{}, typeError(StatusInvalidArg, "an external", v)
	}
	return External{v}, nil
}

// AsDate restituisce v come Date se è una Date.
func (v Value) AsDate() (Date, error) {
	if !v.IsDate() {
		return Date{}, typeError(StatusDateExpected, "a Date", v)
	}
	return Date{Object{v}}, nil
}

// AsPromise restituisce v come Promise se è una Promise.
func (v Value) AsPromise() (Promise, error) {
	if !v.IsPromise() {
		return Promise{}, typeError(StatusInvalidArg, "a Promise", v)
	}
	return Promise{Object{v}}, nil
}

// AsArrayBuffer restituisce v come ArrayBuffer se è un ArrayBuffer.
func (v Value) AsArrayBuffer() (ArrayBuffer, error) {
	if !v.IsArrayBuffer() {
		return ArrayBuffer{}, typeError(sys.StatusArrayBufferExpected, "an ArrayBuffer", v)
	}
	return ArrayBuffer{Object{v}}, nil
}

// AsBuffer restituisce v come Buffer se è un Buffer di Node.js (o un Uint8Array, che Node-API
// accetta come Buffer).
func (v Value) AsBuffer() (Buffer, error) {
	if !v.IsBuffer() {
		return Buffer{}, typeError(StatusInvalidArg, "a Buffer", v)
	}
	return Buffer{Object{v}}, nil
}

// AsTypedArray restituisce v come TypedArray se è un TypedArray.
func (v Value) AsTypedArray() (TypedArray, error) {
	if !v.IsTypedArray() {
		return TypedArray{}, typeError(StatusInvalidArg, "a TypedArray", v)
	}
	return TypedArray{Object{v}}, nil
}

// AsDataView restituisce v come DataView se è un DataView.
func (v Value) AsDataView() (DataView, error) {
	if !v.IsDataView() {
		return DataView{}, typeError(StatusInvalidArg, "a DataView", v)
	}
	return DataView{Object{v}}, nil
}

// ---- lettura dei primitivi ----
//
// Node-API controlla il tipo: un valore del tipo sbagliato dà un *Error con lo status
// corrispondente (es. StatusNumberExpected).

// Float64 legge un number.
func (v Value) Float64() (float64, error) {
	var f float64
	st := sys.GetValueDouble(v.env, v.raw, &f)
	return f, check(v.env, st)
}

// Int32 legge un number convertito a int32 come ToInt32 di JS (troncamento, modulo 2^32; NaN e
// ±Infinity danno 0).
func (v Value) Int32() (int32, error) {
	var i int32
	st := sys.GetValueInt32(v.env, v.raw, &i)
	return i, check(v.env, st)
}

// Uint32 legge un number convertito a uint32 come ToUint32 di JS.
func (v Value) Uint32() (uint32, error) {
	var u uint32
	st := sys.GetValueUint32(v.env, v.raw, &u)
	return u, check(v.env, st)
}

// Int64 legge un number troncato a int64; fuori intervallo satura, NaN dà 0.
func (v Value) Int64() (int64, error) {
	var i int64
	st := sys.GetValueInt64(v.env, v.raw, &i)
	return i, check(v.env, st)
}

// Bool legge un boolean.
func (v Value) Bool() (bool, error) {
	var b bool
	st := sys.GetValueBool(v.env, v.raw, &b)
	return b, check(v.env, st)
}

// ---- coercizione (possono eseguire JS: valueOf, toString, Symbol.toPrimitive) ----

// ToBool applica Boolean(v).
func (v Value) ToBool() (Value, error) {
	var r sys.Value
	st := sys.CoerceToBool(v.env, v.raw, &r)
	return Env{raw: v.env}.value(r, st)
}

// ToNumber applica Number(v); lancia per i symbol e i bigint.
func (v Value) ToNumber() (Value, error) {
	var r sys.Value
	st := sys.CoerceToNumber(v.env, v.raw, &r)
	return Env{raw: v.env}.value(r, st)
}

// ToObject applica Object(v); lancia per null e undefined.
func (v Value) ToObject() (Object, error) {
	var r sys.Value
	st := sys.CoerceToObject(v.env, v.raw, &r)
	o, err := Env{raw: v.env}.value(r, st)
	return Object{o}, err
}

// ToString applica String(v); lancia per i symbol.
func (v Value) ToString() (String, error) {
	var r sys.Value
	st := sys.CoerceToString(v.env, v.raw, &r)
	o, err := Env{raw: v.env}.value(r, st)
	return String{o}, err
}
