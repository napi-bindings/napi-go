package main

// Port of test/bigint.cc, test/date.cc, test/name.cc and test/symbol.cc.
//
// C++ constructs without a Go counterpart:
//   - the many Symbol::New / Symbol::For overloads (C string, std::string, std::string_view,
//     string-like classes, Napi::String) all take a Go string;
//   - String::New(env, nullptr) has no Go equivalent (a Go string cannot be null): the
//     nullStringShouldThrow assertion is removed from test/name.js;
//   - Symbol::For(env, napi_value) calls the JS Symbol.for, like node-addon-api does.

import (
	"errors"
	"math"
	"unicode/utf16"

	"github.com/napi-bindings/napi-go/napi"
	"github.com/napi-bindings/napi-go/sys"
)

// bigintTooManyWords is a word count above the Node-API limit (INT_MAX), like SIZE_MAX in the C++
// test.
const bigintTooManyWords = math.MaxUint32

func initBigInt(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"IsLossless", func(c napi.CallContext) (napi.Value, error) {
			b, err := c.Arg(0).AsBigInt()
			if err != nil {
				return napi.Value{}, err
			}
			signed, err := c.Arg(1).ToBool()
			if err != nil {
				return napi.Value{}, err
			}
			isSigned, err := signed.Bool()
			if err != nil {
				return napi.Value{}, err
			}
			var lossless bool
			if isSigned {
				_, lossless, err = b.Int64()
			} else {
				_, lossless, err = b.Uint64()
			}
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(lossless)
		}},
		fn{"IsBigInt", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(c.Arg(0).IsBigInt())
		}},
		fn{"TestInt64", func(c napi.CallContext) (napi.Value, error) {
			b, err := c.Arg(0).AsBigInt()
			if err != nil {
				return napi.Value{}, err
			}
			i, _, err := b.Int64()
			if err != nil {
				return napi.Value{}, err
			}
			r, err := c.Env().NewBigIntInt64(i)
			return r.Value, err
		}},
		fn{"TestUint64", func(c napi.CallContext) (napi.Value, error) {
			b, err := c.Arg(0).AsBigInt()
			if err != nil {
				return napi.Value{}, err
			}
			u, _, err := b.Uint64()
			if err != nil {
				return napi.Value{}, err
			}
			r, err := c.Env().NewBigIntUint64(u)
			return r.Value, err
		}},
		fn{"TestWords", func(c napi.CallContext) (napi.Value, error) {
			b, err := c.Arg(0).AsBigInt()
			if err != nil {
				return napi.Value{}, err
			}
			negative, words, err := b.Words()
			if err != nil {
				return napi.Value{}, err
			}
			r, err := c.Env().NewBigIntWords(negative, words)
			return r.Value, err
		}},
		fn{"TestTooBigBigInt", func(c napi.CallContext) (napi.Value, error) {
			// NewBigIntWords takes a slice, whose length cannot lie: go through sys, as the C++
			// test lies about the length of its 10-word array.
			var words [10]uint64
			var r sys.Value
			st := sys.CreateBigIntWords(c.Env().Raw(), 0, bigintTooManyWords, &words[0], &r)
			return napi.ValueFromRaw(c.Env().Raw(), r), c.Env().Check(st)
		}},
	)
}

func initDate(env napi.Env) (napi.Object, error) {
	argDate := func(c napi.CallContext) (napi.Date, error) { return c.Arg(0).AsDate() }
	return newExports(env,
		fn{"CreateDate", func(c napi.CallContext) (napi.Value, error) {
			ms, err := argFloat(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			d, err := c.Env().NewDateMillis(ms)
			return d.Value, err
		}},
		fn{"CreateDateFromTimePoint", func(c napi.CallContext) (napi.Value, error) {
			// std::chrono::system_clock::time_point{} is the Unix epoch.
			d, err := c.Env().NewDateMillis(0)
			return d.Value, err
		}},
		fn{"IsDate", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(c.Arg(0).IsDate())
		}},
		fn{"ValueOf", func(c napi.CallContext) (napi.Value, error) {
			d, err := argDate(c)
			if err != nil {
				return napi.Value{}, err
			}
			ms, err := d.Millis()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewFloat64(ms)
		}},
		fn{"OperatorValue", func(c napi.CallContext) (napi.Value, error) {
			d, err := argDate(c)
			if err != nil {
				return napi.Value{}, err
			}
			ms, err := d.Millis()
			if err != nil {
				return napi.Value{}, err
			}
			t, err := d.Time()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(float64(t.UnixMilli()) == ms)
		}},
	)
}

const (
	nameTestValue = "123456789"
	encodingUTF8  = "utf8"
	encodingUTF16 = "utf16"
)

var errInvalidEncoding = errors.New("Invalid encoding.")

func initName(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"echoString", func(c napi.CallContext) (napi.Value, error) {
			encoding, err := argString(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			switch encoding {
			case encodingUTF8:
				s, err := argString(c, 0)
				if err != nil {
					return napi.Value{}, err
				}
				return str(c.Env(), s)
			case encodingUTF16:
				u, err := c.Arg(0).UTF16()
				if err != nil {
					return napi.Value{}, err
				}
				s, err := c.Env().NewStringUTF16(u)
				return s.Value, err
			}
			return napi.Value{}, errInvalidEncoding
		}},
		fn{"createString", func(c napi.CallContext) (napi.Value, error) {
			encoding, err := argString(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			value := nameTestValue
			if !c.Arg(1).IsUndefined() {
				n, err := argUint32(c, 1)
				if err != nil {
					return napi.Value{}, err
				}
				value = value[:n]
			}
			switch encoding {
			case encodingUTF8:
				return str(c.Env(), value)
			case encodingUTF16:
				s, err := c.Env().NewStringUTF16(utf16.Encode([]rune(value)))
				return s.Value, err
			}
			return napi.Value{}, errInvalidEncoding
		}},
		fn{"createStringFromStringView", func(c napi.CallContext) (napi.Value, error) { return str(c.Env(), "hello1") }},
		fn{"checkString", func(c napi.CallContext) (napi.Value, error) {
			encoding, err := argString(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			want := nameTestValue
			if !c.Arg(2).IsUndefined() {
				n, err := argUint32(c, 2)
				if err != nil {
					return napi.Value{}, err
				}
				want = want[:n]
			}
			switch encoding {
			case encodingUTF8:
				s, err := argString(c, 0)
				if err != nil {
					return napi.Value{}, err
				}
				return c.Env().NewBoolean(s == want)
			case encodingUTF16:
				u, err := c.Arg(0).UTF16()
				if err != nil {
					return napi.Value{}, err
				}
				return c.Env().NewBoolean(string(utf16.Decode(u)) == want)
			}
			return napi.Value{}, errInvalidEncoding
		}},
		fn{"createSymbol", func(c napi.CallContext) (napi.Value, error) {
			description := ""
			if !c.Arg(0).IsUndefined() {
				d, err := argString(c, 0)
				if err != nil {
					return napi.Value{}, err
				}
				description = d
			}
			s, err := c.Env().NewSymbol(description)
			return s.Value, err
		}},
		fn{"createSymbolFromStringView", func(c napi.CallContext) (napi.Value, error) {
			s, err := c.Env().NewSymbol("hello2")
			return s.Value, err
		}},
		fn{"checkSymbol", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewBoolean(c.Arg(0).IsSymbol())
		}},
	)
}

func initSymbol(env napi.Env) (napi.Object, error) {
	newSymbol := func(c napi.CallContext) (napi.Value, error) {
		d, err := argString(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		s, err := c.Env().NewSymbol(d)
		return s.Value, err
	}
	symbolFor := func(c napi.CallContext) (napi.Value, error) {
		k, err := argString(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		s, err := c.Env().SymbolFor(k)
		return s.Value, err
	}
	// jsSymbolFor calls the global Symbol.for with key, as Symbol::For(env, napi_value) does.
	jsSymbolFor := func(env napi.Env, key napi.Value) (napi.Value, error) {
		ctor, err := env.Global().Get("Symbol")
		if err != nil {
			return napi.Value{}, err
		}
		o, err := ctor.AsObject()
		if err != nil {
			return napi.Value{}, err
		}
		f, err := o.Get("for")
		if err != nil {
			return napi.Value{}, err
		}
		forFn, err := f.AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		return forFn.Call(ctor, key)
	}
	exports := []fn{
		{"createNewSymbolWithNoArgs", undefined},
		{"createNewSymbolWithCppStr", newSymbol},
		{"createNewSymbolWithCStr", newSymbol},
		{"createNewSymbolWithNapi", newSymbol},
		{"getWellKnownSymbol", func(c napi.CallContext) (napi.Value, error) {
			name, err := argString(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return wellKnownSymbol(c.Env(), name)
		}},
		{"getSymbolFromGlobalRegistry", func(c napi.CallContext) (napi.Value, error) {
			if _, err := c.Arg(0).AsString(); err != nil {
				return napi.Value{}, err
			}
			return jsSymbolFor(c.Env(), c.Arg(0))
		}},
		{"testUndefinedSymbolCanBeCreated", func(c napi.CallContext) (napi.Value, error) {
			return jsSymbolFor(c.Env(), c.Env().Undefined())
		}},
		{"testNullSymbolCanBeCreated", func(c napi.CallContext) (napi.Value, error) {
			return jsSymbolFor(c.Env(), c.Env().Null())
		}},
	}
	for _, name := range []string{
		"getSymbolFromGlobalRegistryWithCKey",
		"getSymbolFromGlobalRegistryWithCppKey",
		"getSymbolFromGlobalRegistryWithStringViewKey",
		"getSymbolFromGlobalRegistryWithStringLikeKey",
		"getSymbolFromGlobalRegistryWithRvalueStringLikeKey",
		"getSymbolFromGlobalRegistryWithStringOnlyLikeKey",
		"getSymbolFromGlobalRegistryWithBothBasesKey",
		"getSymbolFromGlobalRegistryWithViewAndNapiStringKey",
		"getSymbolFromGlobalRegistryWithStringReferenceKey",
		"getSymbolFromGlobalRegistryWithImplicitViewKey",
	} {
		exports = append(exports, fn{name, symbolFor})
	}
	return newExports(env, exports...)
}
