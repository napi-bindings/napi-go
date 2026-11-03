package napi

import (
	"math"
	"math/big"
	"time"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// ---- number and boolean ----

// NewFloat64 creates a JS number from a float64.
func (e Env) NewFloat64(f float64) (Value, error) {
	var r sys.Value
	st := sys.CreateDouble(e.raw, f, &r)
	return e.value(r, st)
}

// NewInt32 creates a JS number from an int32.
func (e Env) NewInt32(i int32) (Value, error) {
	var r sys.Value
	st := sys.CreateInt32(e.raw, i, &r)
	return e.value(r, st)
}

// NewUint32 creates a JS number from a uint32.
func (e Env) NewUint32(u uint32) (Value, error) {
	var r sys.Value
	st := sys.CreateUint32(e.raw, u, &r)
	return e.value(r, st)
}

// NewInt64 creates a JS number from an int64. Values outside ±2^53 lose precision (as in JS); use
// NewBigIntInt64 for large integers.
func (e Env) NewInt64(i int64) (Value, error) {
	var r sys.Value
	st := sys.CreateInt64(e.raw, i, &r)
	return e.value(r, st)
}

// NewBoolean returns the JS boolean true or false.
func (e Env) NewBoolean(b bool) (Value, error) {
	var r sys.Value
	st := sys.GetBoolean(e.raw, b, &r)
	return e.value(r, st)
}

// ---- string ----

// String represents a JS string value.
type String struct{ Value }

// NewString creates a JS string from UTF-8. The bytes of s are passed to V8 without copying on
// the Go side.
func (e Env) NewString(s string) (String, error) {
	var r sys.Value
	st := sys.CreateStringUTF8(e.raw, unsafe.StringData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// NewStringUTF16 creates a JS string from UTF-16 code units.
func (e Env) NewStringUTF16(s []uint16) (String, error) {
	var r sys.Value
	st := sys.CreateStringUTF16(e.raw, unsafe.SliceData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// NewStringLatin1 creates a JS string from ISO-8859-1 bytes (one byte per character).
func (e Env) NewStringLatin1(s []byte) (String, error) {
	var r sys.Value
	st := sys.CreateStringLatin1(e.raw, unsafe.SliceData(s), uint(len(s)), &r)
	v, err := e.value(r, st)
	return String{v}, err
}

// propertyKey creates a property key: an internalized V8 string (node_api_create_property_key_utf8,
// Node-API 10) when available, or a regular string otherwise.
func (e Env) propertyKey(key string) (sys.Value, error) {
	var r sys.Value
	st := sys.CreatePropertyKeyUTF8(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	if st == sys.StatusUnavailable {
		st = sys.CreateStringUTF8(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	}
	if st != sys.StatusOK {
		return nil, statusError(e.raw, st)
	}
	return r, nil
}

const (
	// stackStringBytes is the capacity of GoString's stack buffer: shorter strings are read in a
	// single cgo crossing.
	stackStringBytes = 256
	// utf8MaxCharBytes is the maximum byte length of a UTF-8 code point.
	utf8MaxCharBytes = 4
)

// GoString reads a JS string as UTF-8. Strings up to stackStringBytes-utf8MaxCharBytes-1 bytes
// require a single Node-API call, and the only allocation is the returned Go string. A non-string
// value returns an *Error with StatusStringExpected (without coercion; use ToString to coerce).
func (v Value) GoString() (string, error) {
	var buf [stackStringBytes]byte
	var n uint
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, &buf[0], uint(len(buf)), &n)); err != nil {
		return "", err
	}
	// Node writes at most len(buf)-1 bytes plus a trailing NUL and never splits a multi-byte code
	// point when truncating: a truncated string therefore occupies more than
	// len(buf)-1-utf8MaxCharBytes bytes. Below that threshold the string is guaranteed complete;
	// otherwise, query its exact length and read again.
	if n < uint(len(buf))-1-utf8MaxCharBytes {
		return string(buf[:n]), nil
	}
	var total uint
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, nil, 0, &total)); err != nil {
		return "", err
	}
	b := make([]byte, total+1)
	if err := check(v.env, sys.GetValueStringUTF8(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return "", err
	}
	return unsafe.String(&b[0], int(n)), nil
}

// UTF16 reads a JS string as UTF-16 code units.
func (v Value) UTF16() ([]uint16, error) {
	var total uint
	if err := check(v.env, sys.GetValueStringUTF16(v.env, v.raw, nil, 0, &total)); err != nil {
		return nil, err
	}
	b := make([]uint16, total+1)
	var n uint
	if err := check(v.env, sys.GetValueStringUTF16(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return nil, err
	}
	return b[:n], nil
}

// Latin1 reads a JS string as ISO-8859-1: characters above U+00FF are truncated to their low byte,
// matching Node-API behavior.
func (v Value) Latin1() ([]byte, error) {
	var total uint
	if err := check(v.env, sys.GetValueStringLatin1(v.env, v.raw, nil, 0, &total)); err != nil {
		return nil, err
	}
	b := make([]byte, total+1)
	var n uint
	if err := check(v.env, sys.GetValueStringLatin1(v.env, v.raw, &b[0], uint(len(b)), &n)); err != nil {
		return nil, err
	}
	return b[:n], nil
}

// ---- symbol ----

// Symbol represents a JS symbol value.
type Symbol struct{ Value }

// NewSymbol creates a new symbol with the given description (Symbol(description)).
func (e Env) NewSymbol(description string) (Symbol, error) {
	d, err := e.NewString(description)
	if err != nil {
		return Symbol{}, err
	}
	var r sys.Value
	st := sys.CreateSymbol(e.raw, d.raw, &r)
	v, err := e.value(r, st)
	return Symbol{v}, err
}

// SymbolFor looks up or creates a symbol in the global symbol registry (Symbol.for(key), Node-API 9).
func (e Env) SymbolFor(key string) (Symbol, error) {
	var r sys.Value
	st := sys.SymbolFor(e.raw, unsafe.StringData(key), uint(len(key)), &r)
	v, err := e.value(r, st)
	return Symbol{v}, err
}

// ---- bigint ----

// BigInt represents a JS bigint value.
type BigInt struct{ Value }

// NewBigIntInt64 creates a bigint from an int64.
func (e Env) NewBigIntInt64(i int64) (BigInt, error) {
	var r sys.Value
	st := sys.CreateBigIntInt64(e.raw, i, &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigIntUint64 creates a bigint from a uint64.
func (e Env) NewBigIntUint64(u uint64) (BigInt, error) {
	var r sys.Value
	st := sys.CreateBigIntUint64(e.raw, u, &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigIntWords creates a bigint from 64-bit words in little-endian order (least significant word
// first) and a sign flag.
func (e Env) NewBigIntWords(negative bool, words []uint64) (BigInt, error) {
	sign := int32(0)
	if negative {
		sign = 1
	}
	var r sys.Value
	st := sys.CreateBigIntWords(e.raw, sign, uint(len(words)), unsafe.SliceData(words), &r)
	v, err := e.value(r, st)
	return BigInt{v}, err
}

// NewBigInt creates a bigint from a *big.Int.
func (e Env) NewBigInt(b *big.Int) (BigInt, error) {
	if b == nil {
		return BigInt{}, TypeError("", "NewBigInt: nil *big.Int")
	}
	return e.NewBigIntWords(b.Sign() < 0, bigToWords(b))
}

// Int64 reads the bigint as an int64; lossless is false if the value did not fit and was truncated.
func (b BigInt) Int64() (v int64, lossless bool, err error) {
	st := sys.GetValueBigIntInt64(b.env, b.raw, &v, &lossless)
	return v, lossless, check(b.env, st)
}

// Uint64 reads the bigint as a uint64; lossless is false if the value did not fit (negative or too
// large).
func (b BigInt) Uint64() (v uint64, lossless bool, err error) {
	st := sys.GetValueBigIntUint64(b.env, b.raw, &v, &lossless)
	return v, lossless, check(b.env, st)
}

// Words returns the sign and the 64-bit little-endian words of the bigint.
func (b BigInt) Words() (negative bool, words []uint64, err error) {
	var count uint
	if err := check(b.env, sys.GetValueBigIntWords(b.env, b.raw, nil, &count, nil)); err != nil {
		return false, nil, err
	}
	if count == 0 {
		return false, nil, nil
	}
	words = make([]uint64, count)
	var sign int32
	if err := check(b.env, sys.GetValueBigIntWords(b.env, b.raw, &sign, &count, &words[0])); err != nil {
		return false, nil, err
	}
	return sign != 0, words[:count], nil
}

// Big reads the bigint as a *big.Int.
func (b BigInt) Big() (*big.Int, error) {
	negative, words, err := b.Words()
	if err != nil {
		return nil, err
	}
	r := wordsToBig(words)
	if negative {
		r.Neg(r)
	}
	return r, nil
}

const wordBytes = 8

func bigToWords(b *big.Int) []uint64 {
	be := new(big.Int).Abs(b).Bytes() // big-endian
	words := make([]uint64, (len(be)+wordBytes-1)/wordBytes)
	for i := range be {
		// i-th byte from the end → word i/8, byte position i%8.
		byteFromEnd := be[len(be)-1-i]
		words[i/wordBytes] |= uint64(byteFromEnd) << (wordBytes * uint(i%wordBytes))
	}
	return words
}

func wordsToBig(words []uint64) *big.Int {
	be := make([]byte, len(words)*wordBytes)
	for i, w := range words {
		for j := 0; j < wordBytes; j++ {
			be[len(be)-1-(i*wordBytes+j)] = byte(w >> (wordBytes * uint(j)))
		}
	}
	return new(big.Int).SetBytes(be)
}

// ---- date ----

// Date represents a JS Date object.
type Date struct{ Object }

const nanosPerMilli = float64(time.Millisecond)

// NewDate creates a Date from t. A JS Date stores an integer number of milliseconds: any fractional
// millisecond component is truncated by JS.
func (e Env) NewDate(t time.Time) (Date, error) {
	// Use UnixMilli rather than UnixNano: UnixNano overflows outside the years 1678-2262.
	subMilli := float64(t.Nanosecond()%int(time.Millisecond)) / nanosPerMilli
	return e.NewDateMillis(float64(t.UnixMilli()) + subMilli)
}

// NewDateMillis creates a Date from milliseconds since the Unix epoch (like new Date(ms)).
func (e Env) NewDateMillis(ms float64) (Date, error) {
	var r sys.Value
	st := sys.CreateDate(e.raw, ms, &r)
	v, err := e.value(r, st)
	return Date{Object{v}}, err
}

// Millis returns the Date value in milliseconds since the Unix epoch (NaN for an invalid Date).
func (d Date) Millis() (float64, error) {
	var ms float64
	st := sys.GetDateValue(d.env, d.raw, &ms)
	return ms, check(d.env, st)
}

// ErrInvalidDate is returned by Date.Time for an invalid Date (new Date(NaN)).
var ErrInvalidDate = &Error{Kind: KindRangeError, Message: "invalid Date"}

// Time returns the Date as a time.Time.
func (d Date) Time() (time.Time, error) {
	ms, err := d.Millis()
	if err != nil {
		return time.Time{}, err
	}
	if math.IsNaN(ms) {
		return time.Time{}, ErrInvalidDate
	}
	whole := math.Trunc(ms)
	frac := ms - whole
	return time.UnixMilli(int64(whole)).Add(time.Duration(frac * nanosPerMilli)), nil
}
