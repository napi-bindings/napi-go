package napi

// conv.go — Layer 2: conversions between Go values and JS values.
//
// Each Go type gets a codec, built once with reflection and cached. A codec works on an
// unsafe.Pointer to the Go value, so a conversion does no reflection for primitives, strings,
// structs and arrays (slices and maps use reflect only to allocate). Primitive kinds are decoded
// and encoded by direct functions (decodePrim, encodePrim) that the generic wrappers call without
// an indirect call: their pointer argument does not escape, so a primitive conversion does not
// allocate.

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// FromJS is implemented by types (with a pointer receiver) that decode themselves from a JS value.
// It takes precedence over the built-in conversion of the type.
type FromJS interface {
	FromJS(v Value) error
}

// ToJS is implemented by types that encode themselves as a JS value. It takes precedence over the
// built-in conversion of the type.
type ToJS interface {
	ToJS(env Env) (Value, error)
}

// Error codes of conversion failures, the same Node.js uses for its own argument checks.
const (
	// CodeInvalidArgType: the JS value has the wrong type (TypeError).
	CodeInvalidArgType = "ERR_INVALID_ARG_TYPE"
	// CodeOutOfRange: a number does not fit the Go type, or is not an integer (RangeError).
	CodeOutOfRange = "ERR_OUT_OF_RANGE"
	// CodeInvalidArgValue: the value has the right type but cannot be used, e.g. an array of
	// the wrong length or an invalid Date (TypeError or RangeError).
	CodeInvalidArgValue = "ERR_INVALID_ARG_VALUE"
)

// ToValue converts a Go value into a JS value with the conversion of its type (see the package
// documentation for the type mapping). It must be called on the JS thread.
func ToValue[T any](env Env, v T) (Value, error) {
	r, err := codecFor(reflect.TypeFor[T]()).enc(env, unsafe.Pointer(&v))
	return r, finishPath("", err)
}

// FromValue converts a JS value into a Go value of type T. Values that borrow JS memory (a []byte
// from a Buffer, napi.Value and its specialized types) are valid only until the current callback
// returns. It must be called on the JS thread.
func FromValue[T any](v Value) (T, error) {
	var r T
	err := codecFor(reflect.TypeFor[T]()).dec(v, unsafe.Pointer(&r), modeBorrow)
	return r, finishPath("", err)
}

// mode selects how decoded values may reference JS memory.
type mode uint8

const (
	// modeBorrow: a []byte may alias the Buffer (valid during the callback).
	modeBorrow mode = iota
	// modeOwn: decoded values must outlive the callback (Async arguments): []byte are copied,
	// and types that borrow JS values cannot be decoded.
	modeOwn
)

// primKind identifies the types converted by decodePrim and encodePrim.
type primKind uint8

const (
	primNone primKind = iota
	primBool
	primInt
	primInt8
	primInt16
	primInt32
	primInt64
	primUint
	primUint8
	primUint16
	primUint32
	primUint64
	primUintptr
	primFloat32
	primFloat64
	primString
	// primBytes is []byte: a view of a Buffer (copied in modeOwn), encoded as a new Buffer.
	primBytes
)

// codec converts one Go type. decode or encode is nil when the direction is not supported, and
// decodeErr or encodeErr then says why.
type codec struct {
	t    reflect.Type
	prim primKind
	// jsName describes the expected JS value in errors ("number", "an array", ...).
	jsName string

	decode    func(v Value, p unsafe.Pointer, m mode) error
	encode    func(env Env, p unsafe.Pointer) (Value, error)
	decodeErr error
	encodeErr error

	// optional: undefined (a missing argument or property) is accepted.
	optional bool
	// borrows: decoded values reference JS values and cannot cross goroutines.
	borrows bool
	// class is set for *T of a registered class: converted by direct calls (decodeInstance,
	// instanceFor) on the pointer value.
	class *classMeta
	// done is set once the codec is filled in (recursive types see it unfinished).
	done bool
}

// requireElem makes c unsupported in the directions its element or field ec does not support.
// An unfinished ec belongs to a recursive type: its own check covers it.
func (c *codec) requireElem(ec *codec, where string) {
	if !ec.done {
		return
	}
	if ec.decode == nil && c.decode != nil {
		c.decode, c.decodeErr = nil, elemError(where, ec.decodeErr)
	}
	if ec.encode == nil && c.encode != nil {
		c.encode, c.encodeErr = nil, elemError(where, ec.encodeErr)
	}
}

func elemError(where string, err error) error {
	if ne, ok := err.(*Error); ok {
		return &Error{Kind: ne.Kind, Code: ne.Code, Message: where + ": " + ne.Message}
	}
	return err
}

func (c *codec) dec(v Value, p unsafe.Pointer, m mode) error {
	if c.decode == nil {
		return c.decodeErr
	}
	return c.decode(v, p, m)
}

func (c *codec) enc(env Env, p unsafe.Pointer) (Value, error) {
	if c.encode == nil {
		return Value{}, c.encodeErr
	}
	return c.encode(env, p)
}

// ---- errors ----

// convTypeError reports a JS value of the wrong type.
func convTypeError(want string, got Value) error {
	return &Error{Kind: KindTypeError, Code: CodeInvalidArgType, Message: "expected " + want + ", got " + describe(got)}
}

// describe names the type of a JS value for error messages.
func describe(v Value) string {
	if v.raw == nil {
		return "nothing"
	}
	t, err := v.Type()
	if err != nil {
		return "an unreadable value"
	}
	if t == TypeObject {
		switch {
		case v.IsArray():
			return "an array"
		case v.IsDate():
			return "a Date"
		case v.IsTypedArray():
			return "a TypedArray"
		}
		return "an object"
	}
	return typeName(t)
}

// pathError carries the location of a conversion failure inside the value (a.b[2]), outermost
// segment first once complete.
type pathError struct {
	segs []string // innermost first
	err  *Error
}

func (e *pathError) Error() string { return e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

// withPath records that err happened at seg (".field", "[3]", `["key"]`). Errors other than
// *Error (from FromJS implementations) and pending exceptions pass through unchanged.
func withPath(err error, seg string) error {
	switch e := err.(type) {
	case *pathError:
		e.segs = append(e.segs, seg)
		return e
	case *Error:
		if e.Status == StatusPendingException {
			return err
		}
		return &pathError{segs: []string{seg}, err: e}
	}
	return err
}

// finishPath turns a pathError into an *Error whose message starts with prefix and the path:
// "argument 1: user.tags[2]: expected string, got number".
func finishPath(prefix string, err error) error {
	pe, ok := err.(*pathError)
	if !ok {
		if ne, isErr := err.(*Error); isErr && prefix != "" && ne.Status != StatusPendingException {
			return &Error{Status: ne.Status, Kind: ne.Kind, Code: ne.Code, Message: prefix + ": " + ne.Message}
		}
		return err
	}
	var b strings.Builder
	if prefix != "" {
		b.WriteString(prefix)
		b.WriteString(": ")
	}
	for i := len(pe.segs) - 1; i >= 0; i-- {
		s := pe.segs[i]
		if i == len(pe.segs)-1 {
			s = strings.TrimPrefix(s, ".")
		}
		b.WriteString(s)
	}
	b.WriteString(": ")
	b.WriteString(pe.err.Message)
	return &Error{Status: pe.err.Status, Kind: pe.err.Kind, Code: pe.err.Code, Message: b.String()}
}

// ---- primitives ----

// decodePrim decodes v into the primitive at p. p does not escape.
func decodePrim(v Value, k primKind, p unsafe.Pointer, m mode) error {
	switch k {
	case primBytes:
		b, err := viewBytes(v)
		if err != nil {
			return err
		}
		if m == modeOwn {
			b = bytes.Clone(b)
			if b == nil {
				b = []byte{}
			}
		}
		*(*[]byte)(p) = b
	case primString:
		s, err := v.GoString()
		if err != nil {
			return expectedError(err, "string", v)
		}
		*(*string)(p) = s
	case primBool:
		b, err := v.Bool()
		if err != nil {
			return expectedError(err, "boolean", v)
		}
		*(*bool)(p) = b
	case primFloat64:
		f, err := v.Float64()
		if err != nil {
			return expectedError(err, "number", v)
		}
		*(*float64)(p) = f
	case primFloat32:
		f, err := v.Float64()
		if err != nil {
			return expectedError(err, "number", v)
		}
		*(*float32)(p) = float32(f)
	default:
		f, err := v.Float64()
		if err != nil {
			if is64Bit(k) && v.IsBigInt() {
				return decodeBigIntPrim(v, k, p)
			}
			return expectedError(err, "number", v)
		}
		return storeInt(f, k, p)
	}
	return nil
}

// expectedError replaces a failed Node-API type check (napi_*_expected) with a conversion error;
// other failures (a pending exception, an env shutting down) pass through.
func expectedError(err error, want string, v Value) error {
	if ne, ok := err.(*Error); ok && ne.Kind == KindTypeError {
		return convTypeError(want, v)
	}
	return err
}

func is64Bit(k primKind) bool {
	switch k {
	case primInt, primInt64, primUint, primUint64, primUintptr:
		return true
	}
	return false
}

// intBounds returns the range of an integer kind as [lo, hiExclusive): powers of two, exactly
// representable as float64.
func intBounds(k primKind) (lo, hiEx float64) {
	switch k {
	case primInt8:
		return math.MinInt8, math.MaxInt8 + 1
	case primInt16:
		return math.MinInt16, math.MaxInt16 + 1
	case primInt32:
		return math.MinInt32, math.MaxInt32 + 1
	case primInt, primInt64:
		if k == primInt && strconv.IntSize == 32 {
			return math.MinInt32, math.MaxInt32 + 1
		}
		return math.MinInt64, -float64(math.MinInt64)
	case primUint8:
		return 0, math.MaxUint8 + 1
	case primUint16:
		return 0, math.MaxUint16 + 1
	case primUint32:
		return 0, math.MaxUint32 + 1
	}
	// primUint, primUint64, primUintptr
	if strconv.IntSize == 32 && k != primUint64 {
		return 0, math.MaxUint32 + 1
	}
	return 0, 2 * -float64(math.MinInt64)
}

// storeInt stores f as an integer of kind k, refusing fractions, NaN, infinities and values out
// of range instead of truncating them.
func storeInt(f float64, k primKind, p unsafe.Pointer) error {
	lo, hiEx := intBounds(k)
	if !(f >= lo && f < hiEx) || f != math.Trunc(f) {
		return rangeError(f, lo, hiEx)
	}
	switch k {
	case primInt8:
		*(*int8)(p) = int8(f)
	case primInt16:
		*(*int16)(p) = int16(f)
	case primInt32:
		*(*int32)(p) = int32(f)
	case primInt:
		*(*int)(p) = int(f)
	case primInt64:
		*(*int64)(p) = int64(f)
	case primUint8:
		*(*uint8)(p) = uint8(f)
	case primUint16:
		*(*uint16)(p) = uint16(f)
	case primUint32:
		*(*uint32)(p) = uint32(f)
	case primUint:
		*(*uint)(p) = uint(f)
	case primUint64:
		*(*uint64)(p) = uint64(f)
	case primUintptr:
		*(*uintptr)(p) = uintptr(f)
	}
	return nil
}

// rangeError reports a number that does not fit the integer kind. The bounds are printed from
// lo and hiEx exactly: hiEx is a power of two, and hiEx-1 is computed in integer arithmetic since
// float64 cannot represent it above 2^53.
func rangeError(f, lo, hiEx float64) error {
	maxText := strconv.FormatUint(uint64(hiEx/2)-1+uint64(hiEx/2), 10)
	return &Error{Kind: KindRangeError, Code: CodeOutOfRange, Message: "expected an integer from " +
		strconv.FormatInt(int64(lo), 10) + " to " + maxText + ", got " + formatNumber(f)}
}

// maxPlainNumber is where JS switches Number#toString to exponent notation.
const maxPlainNumber = 1e21

// formatNumber prints f as JS would: without an exponent below 1e21.
func formatNumber(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	if math.Abs(f) < maxPlainNumber {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// decodeBigIntPrim accepts a BigInt for the 64-bit integer kinds, if it fits.
func decodeBigIntPrim(v Value, k primKind, p unsafe.Pointer) error {
	b := BigInt{v}
	signed := k == primInt || k == primInt64
	if signed {
		i, lossless, err := b.Int64()
		if err != nil {
			return err
		}
		if lossless && (k == primInt64 || strconv.IntSize == 64 || (i >= math.MinInt32 && i <= math.MaxInt32)) {
			if k == primInt {
				*(*int)(p) = int(i)
			} else {
				*(*int64)(p) = i
			}
			return nil
		}
	} else {
		u, lossless, err := b.Uint64()
		if err != nil {
			return err
		}
		if lossless && (k == primUint64 || strconv.IntSize == 64 || u <= math.MaxUint32) {
			switch k {
			case primUint:
				*(*uint)(p) = uint(u)
			case primUintptr:
				*(*uintptr)(p) = uintptr(u)
			default:
				*(*uint64)(p) = u
			}
			return nil
		}
	}
	return &Error{Kind: KindRangeError, Code: CodeOutOfRange, Message: "BigInt out of range for " + primName(k)}
}

// encodePrim encodes the primitive at p. p does not escape.
func encodePrim(env Env, k primKind, p unsafe.Pointer) (Value, error) {
	switch k {
	case primBytes:
		b, err := env.NewBufferCopy(*(*[]byte)(p))
		return b.Value, err
	case primString:
		s, err := env.NewString(*(*string)(p))
		return s.Value, err
	case primBool:
		return env.NewBoolean(*(*bool)(p))
	case primFloat64:
		return env.NewFloat64(*(*float64)(p))
	case primFloat32:
		return env.NewFloat64(float64(*(*float32)(p)))
	case primInt8:
		return env.NewInt32(int32(*(*int8)(p)))
	case primInt16:
		return env.NewInt32(int32(*(*int16)(p)))
	case primInt32:
		return env.NewInt32(*(*int32)(p))
	case primInt:
		return env.NewInt64(int64(*(*int)(p)))
	case primInt64:
		return env.NewInt64(*(*int64)(p))
	case primUint8:
		return env.NewUint32(uint32(*(*uint8)(p)))
	case primUint16:
		return env.NewUint32(uint32(*(*uint16)(p)))
	case primUint32:
		return env.NewUint32(*(*uint32)(p))
	case primUint:
		return env.NewFloat64(float64(*(*uint)(p)))
	case primUint64:
		return env.NewFloat64(float64(*(*uint64)(p)))
	case primUintptr:
		return env.NewFloat64(float64(*(*uintptr)(p)))
	}
	return Value{}, &Error{Code: sys.CodeInternal, Message: "napi-go: unknown primitive kind"}
}

func primOf(k reflect.Kind) primKind {
	switch k {
	case reflect.Bool:
		return primBool
	case reflect.Int:
		return primInt
	case reflect.Int8:
		return primInt8
	case reflect.Int16:
		return primInt16
	case reflect.Int32:
		return primInt32
	case reflect.Int64:
		return primInt64
	case reflect.Uint:
		return primUint
	case reflect.Uint8:
		return primUint8
	case reflect.Uint16:
		return primUint16
	case reflect.Uint32:
		return primUint32
	case reflect.Uint64:
		return primUint64
	case reflect.Uintptr:
		return primUintptr
	case reflect.Float32:
		return primFloat32
	case reflect.Float64:
		return primFloat64
	case reflect.String:
		return primString
	}
	return primNone
}

func primName(k primKind) string {
	switch k {
	case primBool:
		return "boolean"
	case primString:
		return "string"
	case primBytes:
		return "a Buffer, Uint8Array or ArrayBuffer"
	case primInt64, primInt:
		return "int64"
	case primUint64, primUint, primUintptr:
		return "uint64"
	}
	return "number"
}

// ---- codec cache ----

var codecCache sync.Map // reflect.Type → *codec

var codecBuild struct {
	mu       sync.Mutex
	building map[reflect.Type]*codec
}

// codecFor returns the codec of t, building and caching it on first use. A type that cannot be
// converted still gets a codec: its decode or encode is nil and decodeErr or encodeErr says why
// (reported at conversion time, and by FuncDef when the module loads).
func codecFor(t reflect.Type) *codec {
	if c, ok := codecCache.Load(t); ok {
		return c.(*codec)
	}
	codecBuild.mu.Lock()
	defer codecBuild.mu.Unlock()
	if c, ok := codecCache.Load(t); ok {
		return c.(*codec)
	}
	codecBuild.building = map[reflect.Type]*codec{}
	c := buildCodec(t)
	for bt, bc := range codecBuild.building {
		codecCache.Store(bt, bc)
	}
	codecBuild.building = nil
	return c
}

// buildCodec builds t's codec; called with codecBuild.mu held. Recursive types get the codec
// under construction (filled in before any conversion can use it).
func buildCodec(t reflect.Type) *codec {
	if c, ok := codecCache.Load(t); ok {
		return c.(*codec)
	}
	if c, ok := codecBuild.building[t]; ok {
		return c
	}
	c := &codec{t: t}
	codecBuild.building[t] = c
	fillCodec(c, t)
	c.done = true
	return c
}

func unsupported(t reflect.Type, why string) error {
	return &Error{Kind: KindTypeError, Code: CodeInvalidArgType, Message: "type " + t.String() + " " + why}
}

var (
	fromJSType = reflect.TypeFor[FromJS]()
	toJSType   = reflect.TypeFor[ToJS]()
	bigIntPtr  = reflect.TypeFor[*big.Int]()
	bigIntType = reflect.TypeFor[big.Int]()
	timeType   = reflect.TypeFor[time.Time]()
	bytesType  = reflect.TypeFor[[]byte]()
	anyType    = reflect.TypeFor[any]()
)

func fillCodec(c *codec, t reflect.Type) {
	if fillValueCodec(c, t) {
		return
	}
	custom := false
	if reflect.PointerTo(t).Implements(fromJSType) {
		c.decode = func(v Value, p unsafe.Pointer, _ mode) error {
			return reflect.NewAt(t, p).Interface().(FromJS).FromJS(v)
		}
		c.jsName = "a value for " + t.String()
		custom = true
	}
	if reflect.PointerTo(t).Implements(toJSType) {
		c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
			return reflect.NewAt(t, p).Interface().(ToJS).ToJS(env)
		}
		custom = true
	}
	if custom {
		// The other direction, if any, falls back to the built-in conversion below.
		builtin := &codec{t: t}
		fillBuiltin(builtin, t)
		if c.decode == nil {
			c.decode, c.decodeErr, c.optional, c.jsName = builtin.decode, builtin.decodeErr, builtin.optional, builtin.jsName
		}
		if c.encode == nil {
			c.encode, c.encodeErr = builtin.encode, builtin.encodeErr
		}
		return
	}
	fillBuiltin(c, t)
}

func fillBuiltin(c *codec, t reflect.Type) {
	if t.Implements(eitherMarkerType) {
		fillEither(c, t)
		return
	}
	switch t {
	case bigIntPtr:
		fillBigInt(c, true)
		return
	case bigIntType:
		fillBigInt(c, false)
		return
	case timeType:
		fillTime(c)
		return
	case bytesType:
		fillBytes(c)
		return
	}
	if t.Kind() == reflect.Pointer && fillClassPointer(c, t) {
		return
	}
	if k := primOf(t.Kind()); k != primNone {
		c.prim = k
		c.jsName = primName(k)
		c.decode = func(v Value, p unsafe.Pointer, m mode) error { return decodePrim(v, k, p, m) }
		c.encode = func(env Env, p unsafe.Pointer) (Value, error) { return encodePrim(env, k, p) }
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		fillPointer(c, t)
	case reflect.Slice:
		fillSlice(c, t)
	case reflect.Array:
		fillArray(c, t)
	case reflect.Map:
		fillMap(c, t)
	case reflect.Struct:
		fillStruct(c, t)
	case reflect.Interface:
		fillInterface(c, t)
	default:
		err := unsupported(t, "has no JS conversion (kind "+t.Kind().String()+")")
		c.decodeErr, c.encodeErr = err, err
	}
}

// ---- napi values (passed through) ----

// valueTypes are the Layer 1 value types: they are passed as they are, after a type check.
var valueTypes = map[reflect.Type]struct {
	want  string
	check func(Value) bool
}{
	reflect.TypeFor[Value]():       {"any value", nil},
	reflect.TypeFor[Object]():      {"an object", Value.IsObject},
	reflect.TypeFor[Function]():    {"a function", Value.IsFunction},
	reflect.TypeFor[Array]():       {"an array", Value.IsArray},
	reflect.TypeFor[String]():      {"a string", Value.IsString},
	reflect.TypeFor[Symbol]():      {"a symbol", Value.IsSymbol},
	reflect.TypeFor[BigInt]():      {"a bigint", Value.IsBigInt},
	reflect.TypeFor[Date]():        {"a Date", Value.IsDate},
	reflect.TypeFor[Promise]():     {"a Promise", Value.IsPromise},
	reflect.TypeFor[External]():    {"an external", Value.IsExternal},
	reflect.TypeFor[Buffer]():      {"a Buffer", Value.IsBuffer},
	reflect.TypeFor[ArrayBuffer](): {"an ArrayBuffer", Value.IsArrayBuffer},
	reflect.TypeFor[TypedArray]():  {"a TypedArray", Value.IsTypedArray},
	reflect.TypeFor[DataView]():    {"a DataView", Value.IsDataView},
}

// fillValueCodec handles the Layer 1 value types; they all start with a Value (two words), so the
// Go value at p is read and written as a Value.
func fillValueCodec(c *codec, t reflect.Type) bool {
	vt, ok := valueTypes[t]
	if !ok {
		return false
	}
	c.jsName, c.borrows = vt.want, true
	c.optional = vt.check == nil
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if m == modeOwn {
			return unsupported(t, "cannot be used outside the JS thread (Async)")
		}
		if vt.check != nil && !vt.check(v) {
			return convTypeError(vt.want, v)
		}
		*(*Value)(p) = v
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		v := *(*Value)(p)
		if v.raw == nil {
			return env.Undefined(), nil
		}
		return v, nil
	}
	return true
}

// ---- special types ----

func fillBigInt(c *codec, pointer bool) {
	c.jsName = "a bigint"
	c.optional = pointer
	c.decode = func(v Value, p unsafe.Pointer, _ mode) error {
		if pointer && v.IsNullish() {
			*(**big.Int)(p) = nil
			return nil
		}
		var b *big.Int
		switch {
		case v.IsBigInt():
			var err error
			if b, err = (BigInt{v}).Big(); err != nil {
				return err
			}
		case v.IsNumber():
			f, err := v.Float64()
			if err != nil {
				return err
			}
			if math.IsInf(f, 0) || math.IsNaN(f) || f != math.Trunc(f) {
				return &Error{Kind: KindRangeError, Code: CodeOutOfRange, Message: "expected an integer, got " + formatNumber(f)}
			}
			b, _ = new(big.Float).SetFloat64(f).Int(nil)
		default:
			return convTypeError("a bigint", v)
		}
		if pointer {
			*(**big.Int)(p) = b
		} else {
			(*big.Int)(p).Set(b)
		}
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		b := (*big.Int)(p)
		if pointer {
			if b = *(**big.Int)(p); b == nil {
				return env.Null(), nil
			}
		}
		r, err := env.NewBigInt(b)
		return r.Value, err
	}
}

func fillTime(c *codec) {
	c.jsName = "a Date"
	c.decode = func(v Value, p unsafe.Pointer, _ mode) error {
		if !v.IsDate() {
			return convTypeError("a Date", v)
		}
		tm, err := (Date{Object{v}}).Time()
		if err != nil {
			if errors.Is(err, ErrInvalidDate) {
				return &Error{Kind: KindRangeError, Code: CodeInvalidArgValue, Message: "invalid Date"}
			}
			return err
		}
		*(*time.Time)(p) = tm
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		d, err := env.NewDate(*(*time.Time)(p))
		return d.Value, err
	}
}

// fillBytes maps []byte to Buffer: it accepts a Buffer, Uint8Array, Uint8ClampedArray, Int8Array,
// ArrayBuffer or DataView, aliasing its memory (modeBorrow) or copying it (modeOwn); it encodes
// as a new Buffer holding a copy. The conversion is a primitive kind (decodePrim, encodePrim).
func fillBytes(c *codec) {
	c.prim = primBytes
	c.jsName = "a Buffer, Uint8Array or ArrayBuffer"
	c.decode = func(v Value, p unsafe.Pointer, m mode) error { return decodePrim(v, primBytes, p, m) }
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) { return encodePrim(env, primBytes, p) }
}

func viewBytes(v Value) ([]byte, error) {
	switch {
	case v.IsTypedArray():
		info, err := (TypedArray{Object{v}}).Info()
		if err != nil {
			return nil, err
		}
		if ElementSize(info.Type) != 1 {
			return nil, convTypeError("a Buffer, Uint8Array or ArrayBuffer", v)
		}
		return info.Data, nil
	case v.IsArrayBuffer():
		return (ArrayBuffer{Object{v}}).Bytes()
	case v.IsDataView():
		info, err := (DataView{Object{v}}).Info()
		if err != nil {
			return nil, err
		}
		return info.Data, nil
	}
	return nil, convTypeError("a Buffer, Uint8Array or ArrayBuffer", v)
}

// ---- composite types ----

func fillPointer(c *codec, t reflect.Type) {
	ec := buildCodec(t.Elem())
	c.optional = true
	c.jsName = ec.jsName + " or null"
	c.borrows = ec.borrows
	elem := t.Elem()
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if v.IsNullish() {
			*(*unsafe.Pointer)(p) = nil
			return nil
		}
		np := reflect.New(elem).UnsafePointer()
		if err := ec.dec(v, np, m); err != nil {
			return err
		}
		*(*unsafe.Pointer)(p) = np
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		ep := *(*unsafe.Pointer)(p)
		if ep == nil {
			return env.Null(), nil
		}
		return ec.enc(env, ep)
	}
	c.requireElem(ec, "pointed value")
}

func fillSlice(c *codec, t reflect.Type) {
	ec := buildCodec(t.Elem())
	size := t.Elem().Size()
	c.jsName = "an array"
	c.borrows = ec.borrows
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if !v.IsArray() {
			return convTypeError("an array", v)
		}
		a := Array{Object{v}}
		n, err := a.Len()
		if err != nil {
			return err
		}
		s := reflect.MakeSlice(t, int(n), int(n))
		base := s.UnsafePointer()
		for i := uint32(0); i < n; i++ {
			ev, err := a.GetIndex(i)
			if err != nil {
				return err
			}
			if err := ec.dec(ev, unsafe.Add(base, uintptr(i)*size), m); err != nil {
				return withPath(err, "["+strconv.FormatUint(uint64(i), 10)+"]")
			}
		}
		reflect.NewAt(t, p).Elem().Set(s)
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		s := reflect.NewAt(t, p).Elem()
		return encodeElems(env, ec, s.UnsafePointer(), s.Len(), size)
	}
	c.requireElem(ec, "element")
}

func fillArray(c *codec, t reflect.Type) {
	ec := buildCodec(t.Elem())
	size := t.Elem().Size()
	n := t.Len()
	c.jsName = "an array of length " + strconv.Itoa(n)
	c.borrows = ec.borrows
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if !v.IsArray() {
			return convTypeError(c.jsName, v)
		}
		a := Array{Object{v}}
		got, err := a.Len()
		if err != nil {
			return err
		}
		if int(got) != n {
			return &Error{Kind: KindRangeError, Code: CodeInvalidArgValue,
				Message: "expected an array of length " + strconv.Itoa(n) + ", got length " + strconv.FormatUint(uint64(got), 10)}
		}
		for i := 0; i < n; i++ {
			ev, err := a.GetIndex(uint32(i))
			if err != nil {
				return err
			}
			if err := ec.dec(ev, unsafe.Add(p, uintptr(i)*size), m); err != nil {
				return withPath(err, "["+strconv.Itoa(i)+"]")
			}
		}
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) { return encodeElems(env, ec, p, n, size) }
	c.requireElem(ec, "element")
}

func encodeElems(env Env, ec *codec, base unsafe.Pointer, n int, size uintptr) (Value, error) {
	a, err := env.NewArrayWithLength(n)
	if err != nil {
		return Value{}, err
	}
	for i := 0; i < n; i++ {
		ev, err := ec.enc(env, unsafe.Add(base, uintptr(i)*size))
		if err != nil {
			return Value{}, withPath(err, "["+strconv.Itoa(i)+"]")
		}
		if err := a.SetIndex(uint32(i), ev); err != nil {
			return Value{}, err
		}
	}
	return a.Value, nil
}

func fillMap(c *codec, t reflect.Type) {
	kt := t.Key()
	switch kt.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
	default:
		err := unsupported(t, "has no JS conversion: map keys must be strings or integers")
		c.decodeErr, c.encodeErr = err, err
		return
	}
	ec := buildCodec(t.Elem())
	c.jsName = "an object"
	c.borrows = ec.borrows
	elem := t.Elem()
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if !v.IsObject() || v.IsArray() {
			return convTypeError("an object", v)
		}
		o := Object{v}
		keys, err := o.Keys()
		if err != nil {
			return err
		}
		n, err := keys.Len()
		if err != nil {
			return err
		}
		mv := reflect.MakeMapWithSize(t, int(n))
		for i := uint32(0); i < n; i++ {
			kv, err := keys.GetIndex(i)
			if err != nil {
				return err
			}
			ks, err := kv.GoString()
			if err != nil {
				return err
			}
			key, err := mapKey(kt, ks)
			if err != nil {
				return withPath(err, "["+strconv.Quote(ks)+"]")
			}
			ev, err := o.GetProperty(kv)
			if err != nil {
				return err
			}
			ep := reflect.New(elem)
			if err := ec.dec(ev, ep.UnsafePointer(), m); err != nil {
				return withPath(err, "["+strconv.Quote(ks)+"]")
			}
			mv.SetMapIndex(key, ep.Elem())
		}
		reflect.NewAt(t, p).Elem().Set(mv)
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		mv := reflect.NewAt(t, p).Elem()
		o, err := env.NewObject()
		if err != nil {
			return Value{}, err
		}
		type entry struct {
			key string
			val reflect.Value
		}
		entries := make([]entry, 0, mv.Len())
		it := mv.MapRange()
		for it.Next() {
			entries = append(entries, entry{mapKeyString(it.Key()), it.Value()})
		}
		// Sorted for a deterministic property order (Go maps have none).
		sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })
		ep := reflect.New(elem)
		for _, e := range entries {
			ep.Elem().Set(e.val)
			ev, err := ec.enc(env, ep.UnsafePointer())
			if err != nil {
				return Value{}, withPath(err, "["+strconv.Quote(e.key)+"]")
			}
			if err := o.Set(e.key, ev); err != nil {
				return Value{}, err
			}
		}
		return o.Value, nil
	}
	c.requireElem(ec, "value")
}

func mapKey(kt reflect.Type, s string) (reflect.Value, error) {
	k := reflect.New(kt).Elem()
	switch kt.Kind() {
	case reflect.String:
		k.SetString(s)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 10, kt.Bits())
		if err != nil {
			return k, &Error{Kind: KindTypeError, Code: CodeInvalidArgValue, Message: "key is not an integer of type " + kt.String()}
		}
		k.SetInt(i)
	default:
		u, err := strconv.ParseUint(s, 10, kt.Bits())
		if err != nil {
			return k, &Error{Kind: KindTypeError, Code: CodeInvalidArgValue, Message: "key is not an integer of type " + kt.String()}
		}
		k.SetUint(u)
	}
	return k, nil
}

func mapKeyString(k reflect.Value) string {
	switch k.Kind() {
	case reflect.String:
		return k.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(k.Int(), 10)
	}
	return strconv.FormatUint(k.Uint(), 10)
}

// ---- interfaces ----

// fillInterface: an interface encodes its dynamic value with that value's codec. Only the empty
// interface decodes, into the natural Go type of the JS value (see decodeAny).
func fillInterface(c *codec, t reflect.Type) {
	c.optional = true
	c.jsName = "any value"
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		iv := reflect.NewAt(t, p).Elem()
		if iv.IsNil() {
			return env.Null(), nil
		}
		dyn := iv.Elem()
		dc := codecFor(dyn.Type())
		cp := reflect.New(dyn.Type())
		cp.Elem().Set(dyn)
		return dc.enc(env, cp.UnsafePointer())
	}
	if t != anyType {
		c.decodeErr = unsupported(t, "cannot be decoded from JS (only the empty interface can)")
		return
	}
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		x, err := decodeAny(v, m)
		if err != nil {
			return err
		}
		*(*any)(p) = x
		return nil
	}
}

// decodeAny converts a JS value into its natural Go type: nil (undefined, null), bool, float64,
// string, *big.Int, time.Time (Date), []byte (a copy of a Buffer or Uint8Array), []any (array),
// map[string]any (other objects), and Function, Symbol or External as they are (not in Async).
func decodeAny(v Value, m mode) (any, error) {
	t, err := v.Type()
	if err != nil {
		return nil, err
	}
	switch t {
	case TypeUndefined, TypeNull:
		return nil, nil
	case TypeBoolean:
		return v.Bool()
	case TypeNumber:
		return v.Float64()
	case TypeString:
		return v.GoString()
	case TypeBigInt:
		return BigInt{v}.Big()
	case TypeObject:
		switch {
		case v.IsArray():
			a := Array{Object{v}}
			n, err := a.Len()
			if err != nil {
				return nil, err
			}
			out := make([]any, n)
			for i := uint32(0); i < n; i++ {
				ev, err := a.GetIndex(i)
				if err != nil {
					return nil, err
				}
				if out[i], err = decodeAny(ev, m); err != nil {
					return nil, withPath(err, "["+strconv.FormatUint(uint64(i), 10)+"]")
				}
			}
			return out, nil
		case v.IsDate():
			return (Date{Object{v}}).Time()
		case v.IsTypedArray() || v.IsArrayBuffer() || v.IsDataView():
			if b, err := viewBytes(v); err == nil {
				return bytes.Clone(b), nil
			}
		}
		var out map[string]any
		mc := codecFor(reflect.TypeFor[map[string]any]())
		return out, mc.dec(v, unsafe.Pointer(&out), m)
	}
	// Function, Symbol, External: kept as JS values, valid during the callback.
	if m == modeOwn {
		return nil, &Error{Kind: KindTypeError, Code: CodeInvalidArgType, Message: "a " + typeName(t) + " cannot be used outside the JS thread (Async)"}
	}
	switch t {
	case TypeFunction:
		return Function{Object{v}}, nil
	case TypeSymbol:
		return Symbol{v}, nil
	}
	return External{v}, nil
}

// ---- structs ----

// structField is a field converted as a property of the JS object.
type structField struct {
	name      string
	goName    string
	offset    uintptr
	t         reflect.Type
	c         *codec
	omitEmpty bool
}

func fillStruct(c *codec, t reflect.Type) {
	fields, err := collectFields(t, 0, nil)
	if err != nil {
		c.decodeErr, c.encodeErr = err, err
		return
	}
	c.jsName = "an object"
	for _, f := range fields {
		if f.c.borrows {
			c.borrows = true
		}
	}
	c.decode = func(v Value, p unsafe.Pointer, m mode) error {
		if !v.IsObject() || v.IsArray() {
			return convTypeError("an object", v)
		}
		o := Object{v}
		for i := range fields {
			f := &fields[i]
			fv, err := o.Get(f.name)
			if err != nil {
				return err
			}
			if (f.omitEmpty || f.c.optional) && fv.IsUndefined() {
				continue
			}
			if err := f.c.dec(fv, unsafe.Add(p, f.offset), m); err != nil {
				return withPath(err, "."+f.name)
			}
		}
		return nil
	}
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		o, err := env.NewObject()
		if err != nil {
			return Value{}, err
		}
		for i := range fields {
			f := &fields[i]
			fp := unsafe.Add(p, f.offset)
			if f.omitEmpty && reflect.NewAt(f.t, fp).Elem().IsZero() {
				continue
			}
			fv, err := f.c.enc(env, fp)
			if err != nil {
				return Value{}, withPath(err, "."+f.name)
			}
			if err := o.Set(f.name, fv); err != nil {
				return Value{}, err
			}
		}
		return o.Value, nil
	}
	for i := range fields {
		c.requireElem(fields[i].c, "field "+fields[i].goName)
	}
}

// collectFields lists the converted fields of t: exported fields, with embedded structs flattened
// (their fields promoted, as in encoding/json). Names come from the napi tag, then the json tag,
// then the Go name in lowerCamelCase.
func collectFields(t reflect.Type, base uintptr, seen []reflect.Type) ([]structField, error) {
	for _, s := range seen {
		if s == t {
			return nil, nil // embedding cycle through pointers is not followed
		}
	}
	seen = append(seen, t)
	var out []structField
	names := map[string]string{}
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, opts, tagged := fieldTag(sf)
		if tagged && name == "-" && opts == "" {
			continue
		}
		if sf.Anonymous && !tagged && sf.Type.Kind() == reflect.Struct {
			inner, err := collectFields(sf.Type, base+sf.Offset, seen)
			if err != nil {
				return nil, err
			}
			for _, f := range inner {
				if _, dup := names[f.name]; !dup {
					names[f.name] = f.goName
					out = append(out, f)
				}
			}
			continue
		}
		if !sf.IsExported() {
			continue
		}
		if name == "" {
			name = lowerCamel(sf.Name)
		}
		if prev, dup := names[name]; dup {
			return nil, unsupported(t, "has fields "+prev+" and "+sf.Name+" with the same JS name "+strconv.Quote(name))
		}
		names[name] = sf.Name
		out = append(out, structField{
			name: name, goName: sf.Name, offset: base + sf.Offset, t: sf.Type,
			c: buildCodec(sf.Type), omitEmpty: strings.Contains(opts, "omitempty"),
		})
	}
	return out, nil
}

// fieldTag returns the name and options of the napi tag, or of the json tag if there is none. As
// in encoding/json, the tag "-" skips the field and "-," names it "-".
func fieldTag(sf reflect.StructField) (name, opts string, tagged bool) {
	tag, ok := sf.Tag.Lookup("napi")
	if !ok {
		tag, ok = sf.Tag.Lookup("json")
	}
	if !ok {
		return "", "", false
	}
	name, opts, hasOpts := strings.Cut(tag, ",")
	if hasOpts {
		opts = "," + opts
	}
	return name, opts, true
}

// lowerCamel converts a Go identifier to the JS convention: UserID → userID, ID → id,
// URLPath → urlPath, HTTPServer → httpServer.
func lowerCamel(s string) string {
	r := []rune(s)
	n := 0
	for n < len(r) && r[n] >= 'A' && r[n] <= 'Z' {
		n++
	}
	if n == 0 {
		return s
	}
	if n > 1 && n < len(r) && r[n] >= 'a' && r[n] <= 'z' {
		n-- // keep the capital that starts the next word (URLPath → urlPath)
	}
	for i := 0; i < n; i++ {
		r[i] += 'a' - 'A'
	}
	return string(r)
}

// definitionError reports a wrapper whose types cannot be converted (when its function is
// created, i.e. when the module loads for exports): a TypeError naming the wrapper and the type.
func definitionError(where string, err error) error {
	msg := err.Error()
	if ne, ok := err.(*Error); ok {
		msg = ne.Message
	}
	return &Error{Kind: KindTypeError, Code: CodeInvalidArgType, Message: fmt.Sprintf("napi-go: %s: %s", where, msg)}
}
