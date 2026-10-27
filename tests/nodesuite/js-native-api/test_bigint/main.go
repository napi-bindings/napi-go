// Porting di test/js-native-api/test_bigint/test_bigint.c.
package main

import (
	"math"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// maxWords è la capacità del buffer uint64_t words[10] del test C.
const maxWords = 10

func isLossless(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	var isSigned bool
	if !nt.Call(env, sys.GetValueBool(env, args[1], &isSigned)) {
		return nil
	}

	var lossless bool

	if isSigned {
		var input int64
		if !nt.Call(env, sys.GetValueBigIntInt64(env, args[0], &input, &lossless)) {
			return nil
		}
	} else {
		var input uint64
		if !nt.Call(env, sys.GetValueBigIntUint64(env, args[0], &input, &lossless)) {
			return nil
		}
	}

	var output sys.Value
	if !nt.Call(env, sys.GetBoolean(env, lossless, &output)) {
		return nil
	}

	return output
}

// bigintArg legge il primo argomento con le verifiche comuni a TestInt64, TestUint64 e TestWords.
func bigintArg(env sys.Env, info sys.CallbackInfo) (sys.Value, bool) {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil, false
	}

	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return nil, false
	}

	var valuetype0 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[0], &valuetype0)) {
		return nil, false
	}

	if !nt.Assert(env, valuetype0 == sys.TypeBigInt, "valuetype0 == napi_bigint",
		"Wrong type of arguments. Expects a bigint as first argument.") {
		return nil, false
	}
	return args[0], true
}

func testInt64(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := bigintArg(env, info)
	if !ok {
		return nil
	}

	var input int64
	var lossless bool
	if !nt.Call(env, sys.GetValueBigIntInt64(env, arg, &input, &lossless)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateBigIntInt64(env, input, &output)) {
		return nil
	}

	return output
}

func testUint64(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := bigintArg(env, info)
	if !ok {
		return nil
	}

	var input uint64
	var lossless bool
	if !nt.Call(env, sys.GetValueBigIntUint64(env, arg, &input, &lossless)) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateBigIntUint64(env, input, &output)) {
		return nil
	}

	return output
}

func testWords(env sys.Env, info sys.CallbackInfo) sys.Value {
	arg, ok := bigintArg(env, info)
	if !ok {
		return nil
	}

	var expectedWordCount uint
	if !nt.Call(env, sys.GetValueBigIntWords(env, arg, nil, &expectedWordCount, nil)) {
		return nil
	}

	var signBit int32
	wordCount := uint(maxWords)
	var words [maxWords]uint64

	if !nt.Call(env, sys.GetValueBigIntWords(env, arg, &signBit, &wordCount, &words[0])) {
		return nil
	}

	if !nt.Assert(env, wordCount == expectedWordCount, "word_count == expected_word_count",
		"word counts do not match") {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.CreateBigIntWords(env, signBit, wordCount, &words[0], &output)) {
		return nil
	}

	return output
}

// createTooBigBigInt lancia un RangeError: word_count = SIZE_MAX.
func createTooBigBigInt(env sys.Env, _ sys.CallbackInfo) sys.Value {
	signBit := int32(0)
	wordCount := uint(math.MaxUint)
	var words [maxWords]uint64
	var output sys.Value

	if !nt.Call(env, sys.CreateBigIntWords(env, signBit, wordCount, &words[0], &output)) {
		return nil
	}

	return output
}

// makeBigIntWordsThrow verifica che le eccezioni del motore arrivino come napi_pending_exception.
func makeBigIntWordsThrow(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var words [maxWords]uint64
	var output sys.Value

	status := sys.CreateBigIntWords(env, 0, math.MaxInt32, &words[0], &output)
	if status != sys.StatusPendingException {
		nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Expected status `napi_pending_exception`")))
	}

	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("IsLossless", isLossless),
		nt.DeclareProperty("TestInt64", testInt64),
		nt.DeclareProperty("TestUint64", testUint64),
		nt.DeclareProperty("TestWords", testWords),
		nt.DeclareProperty("CreateTooBigBigInt", createTooBigBigInt),
		nt.DeclareProperty("MakeBigIntWordsThrow", makeBigIntWordsThrow),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
