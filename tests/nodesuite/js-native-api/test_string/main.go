// Porting di test/js-native-api/test_string/test_string.c.
//
// Le funzioni one-byte (char) e two-byte (char16_t) del C condividono qui un'unica
// implementazione generica su byte | uint16.
package main

import (
	"math"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

type lengthType int

const (
	actualLength lengthType = iota
	autoLength
)

// Dimensioni dei buffer del test C.
const (
	stringBufferSize       = 128
	insufficientBufferSize = 4
	corruptionBufferSize   = 10
)

// char è il tipo di carattere: byte per latin1/utf8, uint16 per utf16 (char16_t).
type char interface{ byte | uint16 }

type createAPI[T char] func(env sys.Env, str *T, length uint, result *sys.Value) sys.Status

type getAPI[T char] func(env sys.Env, value sys.Value, buf *T, bufsize uint, result *uint) sys.Status

// validateAndRetrieveSingleStringArg equivale alla funzione C omonima: NODE_API_CHECK_STATUS
// restituisce lo status, NODE_API_ASSERT_STATUS lancia e restituisce napi_generic_failure.
func validateAndRetrieveSingleStringArg(env sys.Env, info sys.CallbackInfo, arg *sys.Value) sys.Status {
	argc := uint(1)
	if st := sys.GetCbInfo(env, info, &argc, arg, nil, nil); st != sys.StatusOK {
		return st
	}

	if !nt.Assert(env, argc >= 1, "argc >= 1", "Wrong number of arguments") {
		return sys.StatusGenericFailure
	}

	var valuetype sys.ValueType
	if st := sys.Typeof(env, *arg, &valuetype); st != sys.StatusOK {
		return st
	}

	if !nt.Assert(env, valuetype == sys.TypeString, "valuetype == napi_string",
		"Wrong type of argument. Expects a string.") {
		return sys.StatusGenericFailure
	}

	return sys.StatusOK
}

// testImpl equivale a TestOneByteImpl e TestTwoByteImpl: rilegge la stringa ricevuta da JS
// con get e la ricrea con create.
func testImpl[T char](get getAPI[T], create createAPI[T], mode lengthType) nt.Func {
	return func(env sys.Env, info sys.CallbackInfo) sys.Value {
		var args [1]sys.Value
		if !nt.Call(env, validateAndRetrieveSingleStringArg(env, info, &args[0])) {
			return nil
		}

		var buffer [stringBufferSize]T
		bufferSize := uint(stringBufferSize)
		var copied uint

		if !nt.Call(env, get(env, args[0], &buffer[0], bufferSize, &copied)) {
			return nil
		}

		var output sys.Value
		if mode == autoLength {
			copied = sys.AutoLength
		}
		if !nt.Call(env, create(env, &buffer[0], copied, &output)) {
			return nil
		}

		return output
	}
}

// strlen conta i caratteri prima dello 0 finale (strlen e strlen16 del test C).
func strlen[T char](s *T) uint {
	n := uint(0)
	for p := s; *p != 0; p = (*T)(unsafe.Add(unsafe.Pointer(p), unsafe.Sizeof(*p))) {
		n++
	}
	return n
}

// freeString libera i dati di una stringa esterna, come free_string nel C.
func freeString(_ sys.Env, data, _ uintptr) {
	nt.FreeAddr(data)
}

// mallocCopy copia length caratteri di str più lo 0 finale in memoria C.
func mallocCopy[T char](str *T, length uint) *T {
	var zero T
	size := unsafe.Sizeof(zero)
	p := (*T)(nt.Malloc((length + 1) * uint(size)))
	if p == nil {
		return nil
	}
	dst := unsafe.Slice(p, length+1)
	copy(dst, unsafe.Slice(str, length+1))
	dst[length] = 0
	return p
}

func createExternalLatin1(env sys.Env, str *byte, length uint, result *sys.Value) sys.Status {
	// Inizializzato a true, il valore che non si vuole.
	copied := true
	actual := length
	if length == sys.AutoLength {
		actual = strlen(str)
	}
	stringCopy := mallocCopy(str, actual)
	if stringCopy == nil {
		return sys.StatusGenericFailure
	}

	cb, hint := nt.Finalizer(freeString, 0)
	status := sys.CreateExternalStringLatin1(env, stringCopy, length, cb, hint, result, &copied)
	// La stringa non deve essere copiata.
	if copied {
		return sys.StatusGenericFailure
	}
	if status != sys.StatusOK {
		nt.DiscardFinalizer(hint)
		nt.Free(unsafe.Pointer(stringCopy))
		return status
	}
	return sys.StatusOK
}

func createExternalUTF16(env sys.Env, str *uint16, length uint, result *sys.Value) sys.Status {
	// Inizializzato a true, il valore che non si vuole.
	copied := true
	actual := length
	if length == sys.AutoLength {
		actual = strlen(str)
	}
	stringCopy := mallocCopy(str, actual)
	if stringCopy == nil {
		return sys.StatusGenericFailure
	}

	cb, hint := nt.Finalizer(freeString, 0)
	status := sys.CreateExternalStringUTF16(env, stringCopy, length, cb, hint, result, &copied)
	if status != sys.StatusOK {
		nt.DiscardFinalizer(hint)
		nt.Free(unsafe.Pointer(stringCopy))
		return status
	}

	return sys.StatusOK
}

// testInsufficient equivale a TestLatin1Insufficient, TestUtf8Insufficient e
// TestUtf16Insufficient: buffer di 4 caratteri.
func testInsufficient[T char](get getAPI[T], create createAPI[T]) nt.Func {
	return func(env sys.Env, info sys.CallbackInfo) sys.Value {
		var args [1]sys.Value
		if !nt.Call(env, validateAndRetrieveSingleStringArg(env, info, &args[0])) {
			return nil
		}

		var buffer [insufficientBufferSize]T
		bufferSize := uint(insufficientBufferSize)
		var copied uint

		if !nt.Call(env, get(env, args[0], &buffer[0], bufferSize, &copied)) {
			return nil
		}

		var output sys.Value
		if !nt.Call(env, create(env, &buffer[0], copied, &output)) {
			return nil
		}

		return output
	}
}

// lengthOf equivale a Latin1Length, Utf16Length e Utf8Length: buffer NULL, solo la lunghezza.
func lengthOf[T char](get getAPI[T]) nt.Func {
	return func(env sys.Env, info sys.CallbackInfo) sys.Value {
		var args [1]sys.Value
		if !nt.Call(env, validateAndRetrieveSingleStringArg(env, info, &args[0])) {
			return nil
		}

		var length uint
		if !nt.Call(env, get(env, args[0], nil, 0, &length)) {
			return nil
		}

		var output sys.Value
		if !nt.Call(env, sys.CreateUint32(env, uint32(length), &output)) {
			return nil
		}

		return output
	}
}

// testLarge equivale a TestLargeUtf8, TestLargeLatin1 e TestLargeUtf16: lunghezza INT_MAX + 1.
func testLarge[T char](create createAPI[T], empty *T) nt.Func {
	return func(env sys.Env, _ sys.CallbackInfo) sys.Value {
		var output sys.Value
		if uint64(math.MaxUint) > math.MaxInt32 {
			if !nt.Call(env, create(env, empty, uint(math.MaxInt32)+1, &output)) {
				return nil
			}
		} else {
			// Con size_t a 32 bit non si può superare INT_MAX: si lancia l'errore atteso.
			if !nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Invalid argument"))) {
				return nil
			}
		}

		return output
	}
}

func testMemoryCorruption(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}

	var buf [corruptionBufferSize]byte
	if !nt.Call(env, sys.GetValueStringUTF8(env, args[0], &buf[0], 0, nil)) {
		return nil
	}

	var zero [corruptionBufferSize]byte
	if buf != zero {
		if !nt.Call(env, sys.ThrowError(env, nil, nt.CStr("Buffer overwritten"))) {
			return nil
		}
	}

	return nil
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	emptyUTF8 := nt.CStr("")
	emptyUTF16 := nt.UTF16("")
	properties := []sys.PropertyDescriptor{
		nt.DeclareProperty("TestLatin1", testImpl(sys.GetValueStringLatin1, sys.CreateStringLatin1, actualLength)),
		nt.DeclareProperty("TestLatin1AutoLength", testImpl(sys.GetValueStringLatin1, sys.CreateStringLatin1, autoLength)),
		nt.DeclareProperty("TestLatin1External", testImpl(sys.GetValueStringLatin1, createExternalLatin1, actualLength)),
		nt.DeclareProperty("TestLatin1ExternalAutoLength", testImpl(sys.GetValueStringLatin1, createExternalLatin1, autoLength)),
		nt.DeclareProperty("TestLatin1Insufficient", testInsufficient(sys.GetValueStringLatin1, sys.CreateStringLatin1)),
		nt.DeclareProperty("TestUtf8", testImpl(sys.GetValueStringUTF8, sys.CreateStringUTF8, actualLength)),
		nt.DeclareProperty("TestUtf8AutoLength", testImpl(sys.GetValueStringUTF8, sys.CreateStringUTF8, autoLength)),
		nt.DeclareProperty("TestUtf8Insufficient", testInsufficient(sys.GetValueStringUTF8, sys.CreateStringUTF8)),
		nt.DeclareProperty("TestUtf16", testImpl(sys.GetValueStringUTF16, sys.CreateStringUTF16, actualLength)),
		nt.DeclareProperty("TestUtf16AutoLength", testImpl(sys.GetValueStringUTF16, sys.CreateStringUTF16, autoLength)),
		nt.DeclareProperty("TestUtf16External", testImpl(sys.GetValueStringUTF16, createExternalUTF16, actualLength)),
		nt.DeclareProperty("TestUtf16ExternalAutoLength", testImpl(sys.GetValueStringUTF16, createExternalUTF16, autoLength)),
		nt.DeclareProperty("TestUtf16Insufficient", testInsufficient(sys.GetValueStringUTF16, sys.CreateStringUTF16)),
		nt.DeclareProperty("Latin1Length", lengthOf(sys.GetValueStringLatin1)),
		nt.DeclareProperty("Utf16Length", lengthOf(sys.GetValueStringUTF16)),
		nt.DeclareProperty("Utf8Length", lengthOf(sys.GetValueStringUTF8)),
		nt.DeclareProperty("TestLargeUtf8", testLarge(sys.CreateStringUTF8, emptyUTF8)),
		nt.DeclareProperty("TestLargeLatin1", testLarge(sys.CreateStringLatin1, emptyUTF8)),
		nt.DeclareProperty("TestLargeUtf16", testLarge(sys.CreateStringUTF16, emptyUTF16)),
		nt.DeclareProperty("TestMemoryCorruption", testMemoryCorruption),
		nt.DeclareProperty("TestPropertyKeyLatin1", testImpl(sys.GetValueStringLatin1, sys.CreatePropertyKeyLatin1, actualLength)),
		nt.DeclareProperty("TestPropertyKeyLatin1AutoLength", testImpl(sys.GetValueStringLatin1, sys.CreatePropertyKeyLatin1, autoLength)),
		nt.DeclareProperty("TestPropertyKeyUtf8", testImpl(sys.GetValueStringUTF8, sys.CreatePropertyKeyUTF8, actualLength)),
		nt.DeclareProperty("TestPropertyKeyUtf8AutoLength", testImpl(sys.GetValueStringUTF8, sys.CreatePropertyKeyUTF8, autoLength)),
		nt.DeclareProperty("TestPropertyKeyUtf16", testImpl(sys.GetValueStringUTF16, sys.CreatePropertyKeyUTF16, actualLength)),
		nt.DeclareProperty("TestPropertyKeyUtf16AutoLength", testImpl(sys.GetValueStringUTF16, sys.CreatePropertyKeyUTF16, autoLength)),
	}

	initTestNull(env, exports)

	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	return exports
}
