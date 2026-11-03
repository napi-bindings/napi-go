// Port of test/js-native-api/test_object/test_object.c.
//
// node_api_create_object_with_properties is experimental and omitted from sys headers: it is
// called via napitest.CreateObjectWithProperties (see the function comment).
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

// testCreateObjectWithPropertiesData is registered only once: the function created in
// TestCreateObjectWithCustomPrototype is created on every call.
var testCreateObjectWithPropertiesData uintptr

func init() {
	testCreateObjectWithPropertiesData = nt.Register(testCreateObjectWithProperties)
	nt.ModuleInit(initModule)
}

// testValue is the data for napi_wrap; as in C, its address is passed (a global variable is not
// moved by the GC).
var testValue int32 = 3

const expectedTestValue = 3

// Sizes of char key[256] and the key length limit in the C test.
const (
	keyBufferSize = 256
	maxKeyLength  = 255
)

const (
	errObjectExpected       = "Wrong type of arguments. Expects an object as first argument."
	errStringOrSymbolSecond = "Wrong type of arguments. Expects a string or symbol as second."
	errStringSecond         = "Wrong type of arguments. Expects a string as second."
	errKeyTooLong           = "Cannot accommodate keys longer than 255 bytes"
	errWrongArgs            = "Wrong number of arguments"
)

// getArgs reads up to len(buf) arguments, matching napi_get_cb_info in the C test.
func getArgs(env sys.Env, info sys.CallbackInfo, buf []sys.Value) (uint, bool) {
	argc := uint(len(buf))
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &buf[0], nil, nil)) {
		return 0, false
	}
	return argc, true
}

// expectObject checks that v is an object, with the C test's error message. expr is the C
// condition text (the variable name varies across functions).
func expectObject(env sys.Env, v sys.Value, expr string) bool {
	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, v, &t)) {
		return false
	}
	return nt.Assert(env, t == sys.TypeObject, expr+" == napi_object", errObjectExpected)
}

// expectKey checks that v is a string or, if allowSymbol is true, a symbol.
func expectKey(env sys.Env, v sys.Value, name string, allowSymbol bool, message string) bool {
	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, v, &t)) {
		return false
	}
	if allowSymbol {
		return nt.Assert(env, t == sys.TypeString || t == sys.TypeSymbol,
			name+" == napi_string || "+name+" == napi_symbol", message)
	}
	return nt.Assert(env, t == sys.TypeString, name+" == napi_string", message)
}

// readKey copies string v into key, matching napi_get_value_string_utf8(..., key, 255, &key_length)
// followed by key[255] = 0 and the length check.
func readKey(env sys.Env, v sys.Value, key *[keyBufferSize]byte) bool {
	var keyLength uint
	if !nt.Call(env, sys.GetValueStringUTF8(env, v, &key[0], maxKeyLength, &keyLength)) {
		return false
	}
	key[maxKeyLength] = 0
	return nt.Assert(env, keyLength <= maxKeyLength, "key_length <= 255", errKeyTooLong)
}

func get(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 2, "argc >= 2", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "valuetype0") ||
		!expectKey(env, args[1], "valuetype1", true, errStringOrSymbolSecond) {
		return nil
	}

	object := args[0]
	var output sys.Value
	if !nt.Call(env, sys.GetProperty(env, object, args[1], &output)) {
		return nil
	}
	return output
}

func getNamed(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	var key [keyBufferSize]byte
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 2, "argc >= 2", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "value_type0") ||
		!expectKey(env, args[1], "value_type1", false, errStringSecond) {
		return nil
	}

	object := args[0]
	if !readKey(env, args[1], &key) {
		return nil
	}

	var output sys.Value
	if !nt.Call(env, sys.GetNamedProperty(env, object, &key[0], &output)) {
		return nil
	}
	return output
}

// objectArg reads the first argument and checks that it is an object (value_type0 in C).
func objectArg(env sys.Env, info sys.CallbackInfo) (sys.Value, bool) {
	var args [1]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 1, "argc >= 1", errWrongArgs) {
		return nil, false
	}
	if !expectObject(env, args[0], "value_type0") {
		return nil, false
	}
	return args[0], true
}

func getPropertyNames(env sys.Env, info sys.CallbackInfo) sys.Value {
	obj, ok := objectArg(env, info)
	if !ok {
		return nil
	}
	var output sys.Value
	if !nt.Call(env, sys.GetPropertyNames(env, obj, &output)) {
		return nil
	}
	return output
}

// allNames corresponds to C's Get*Names functions, which differ only in the parameters passed
// to napi_get_all_property_names.
func allNames(mode sys.KeyCollectionMode, filter sys.KeyFilter) nt.Func {
	return func(env sys.Env, info sys.CallbackInfo) sys.Value {
		obj, ok := objectArg(env, info)
		if !ok {
			return nil
		}
		var output sys.Value
		if !nt.Call(env, sys.GetAllPropertyNames(env, obj, mode, filter, sys.KeyNumbersToStrings, &output)) {
			return nil
		}
		return output
	}
}

func set(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [3]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 3, "argc >= 3", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "valuetype0") ||
		!expectKey(env, args[1], "valuetype1", true, errStringOrSymbolSecond) {
		return nil
	}

	if !nt.Call(env, sys.SetProperty(env, args[0], args[1], args[2])) {
		return nil
	}

	var valueTrue sys.Value
	if !nt.Call(env, sys.GetBoolean(env, true, &valueTrue)) {
		return nil
	}
	return valueTrue
}

func setNamed(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [3]sys.Value
	var key [keyBufferSize]byte
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 3, "argc >= 3", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "value_type0") ||
		!expectKey(env, args[1], "value_type1", false, errStringSecond) {
		return nil
	}
	if !readKey(env, args[1], &key) {
		return nil
	}

	if !nt.Call(env, sys.SetNamedProperty(env, args[0], &key[0], args[2])) {
		return nil
	}

	var valueTrue sys.Value
	if !nt.Call(env, sys.GetBoolean(env, true, &valueTrue)) {
		return nil
	}
	return valueTrue
}

func has(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 2, "argc >= 2", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "valuetype0") ||
		!expectKey(env, args[1], "valuetype1", true, errStringOrSymbolSecond) {
		return nil
	}

	var hasProperty bool
	if !nt.Call(env, sys.HasProperty(env, args[0], args[1], &hasProperty)) {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, hasProperty, &ret)) {
		return nil
	}
	return ret
}

func hasNamed(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	var key [keyBufferSize]byte
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 2, "argc >= 2", errWrongArgs) {
		return nil
	}
	// C also accepts a symbol here, though it uses the "string as second" message.
	if !expectObject(env, args[0], "value_type0") ||
		!expectKey(env, args[1], "value_type1", true, errStringSecond) {
		return nil
	}
	if !readKey(env, args[1], &key) {
		return nil
	}

	var hasProperty bool
	if !nt.Call(env, sys.HasNamedProperty(env, args[0], &key[0], &hasProperty)) {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, hasProperty, &ret)) {
		return nil
	}
	return ret
}

func hasOwn(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc == 2, "argc == 2", errWrongArgs) {
		return nil
	}
	// C does not check the type of the second argument (the check is commented out).
	if !expectObject(env, args[0], "valuetype0") {
		return nil
	}

	var hasProperty bool
	if !nt.Call(env, sys.HasOwnProperty(env, args[0], args[1], &hasProperty)) {
		return nil
	}

	var ret sys.Value
	if !nt.Call(env, sys.GetBoolean(env, hasProperty, &ret)) {
		return nil
	}
	return ret
}

func deleteProp(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [2]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc == 2, "argc == 2", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "valuetype0") ||
		!expectKey(env, args[1], "valuetype1", true, errStringOrSymbolSecond) {
		return nil
	}

	var result bool
	var ret sys.Value
	if !nt.Call(env, sys.DeleteProperty(env, args[0], args[1], &result)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, result, &ret)) {
		return nil
	}
	return ret
}

// testNumber is the value 987654321 of the test_number property created by New.
const testNumber = 987654321

func newObject(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var ret sys.Value
	if !nt.Call(env, sys.CreateObject(env, &ret)) {
		return nil
	}

	var num sys.Value
	if !nt.Call(env, sys.CreateInt32(env, testNumber, &num)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, ret, nt.CStr("test_number"), num)) {
		return nil
	}

	var str sys.Value
	const strVal = "test string"
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(strVal), uint(len(strVal)), &str)) {
		return nil
	}
	if !nt.Call(env, sys.SetNamedProperty(env, ret, nt.CStr("test_string"), str)) {
		return nil
	}

	return ret
}

func inflate(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [1]sys.Value
	argc, ok := getArgs(env, info, args[:])
	if !ok || !nt.Assert(env, argc >= 1, "argc >= 1", errWrongArgs) {
		return nil
	}
	if !expectObject(env, args[0], "valuetype0") {
		return nil
	}

	obj := args[0]
	var propertynames sys.Value
	if !nt.Call(env, sys.GetPropertyNames(env, obj, &propertynames)) {
		return nil
	}

	var length uint32
	if !nt.Call(env, sys.GetArrayLength(env, propertynames, &length)) {
		return nil
	}

	for i := uint32(0); i < length; i++ {
		var propertyStr, value sys.Value
		if !nt.Call(env, sys.GetElement(env, propertynames, i, &propertyStr)) {
			return nil
		}
		if !nt.Call(env, sys.GetProperty(env, obj, propertyStr, &value)) {
			return nil
		}
		var doubleVal float64
		if !nt.Call(env, sys.GetValueDouble(env, value, &doubleVal)) {
			return nil
		}
		if !nt.Call(env, sys.CreateDouble(env, doubleVal+1, &value)) {
			return nil
		}
		if !nt.Call(env, sys.SetProperty(env, obj, propertyStr, value)) {
			return nil
		}
	}

	return obj
}

func wrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var arg sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &arg, nil, nil)) {
		return nil
	}
	if !nt.Call(env, sys.Wrap(env, arg, uintptr(unsafe.Pointer(&testValue)), nil, 0, nil)) {
		return nil
	}
	return nil
}

func unwrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var arg sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &arg, nil, nil)) {
		return nil
	}

	var data unsafe.Pointer
	if !nt.Call(env, sys.Unwrap(env, arg, &data)) {
		return nil
	}

	isExpected := data != nil && *(*int32)(data) == expectedTestValue
	var result sys.Value
	if !nt.Call(env, sys.GetBoolean(env, isExpected, &result)) {
		return nil
	}
	return result
}

// nullCheckSetup creates object and key as in TestSetProperty, TestHasProperty, and TestGetProperty.
func nullCheckSetup(env sys.Env) (object, key sys.Value, ok bool) {
	if !nt.Call(env, sys.CreateObject(env, &object)) {
		return nil, nil, false
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(""), sys.AutoLength, &key)) {
		return nil, nil, false
	}
	return object, key, true
}

// The status of unchecked calls in the next three functions is read immediately after via
// AddLastStatus, matching C.

func testSetProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	object, key, ok := nullCheckSetup(env)
	if !ok {
		return nil
	}
	var value sys.Value
	if !nt.Call(env, sys.CreateObject(env, &value)) {
		return nil
	}

	status := sys.SetProperty(nil, object, key, value)
	nt.AddReturnedStatus(env, "envIsNull", object, "Invalid argument", sys.StatusInvalidArg, status)

	sys.SetProperty(env, nil, key, value)
	nt.AddLastStatus(env, "objectIsNull", object)

	sys.SetProperty(env, object, nil, value)
	nt.AddLastStatus(env, "keyIsNull", object)

	sys.SetProperty(env, object, key, nil)
	nt.AddLastStatus(env, "valueIsNull", object)

	return object
}

func testHasProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	object, key, ok := nullCheckSetup(env)
	if !ok {
		return nil
	}
	var result bool

	status := sys.HasProperty(nil, object, key, &result)
	nt.AddReturnedStatus(env, "envIsNull", object, "Invalid argument", sys.StatusInvalidArg, status)

	sys.HasProperty(env, nil, key, &result)
	nt.AddLastStatus(env, "objectIsNull", object)

	sys.HasProperty(env, object, nil, &result)
	nt.AddLastStatus(env, "keyIsNull", object)

	sys.HasProperty(env, object, key, nil)
	nt.AddLastStatus(env, "resultIsNull", object)

	return object
}

func testGetProperty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	object, key, ok := nullCheckSetup(env)
	if !ok {
		return nil
	}
	var result sys.Value
	if !nt.Call(env, sys.CreateObject(env, &result)) {
		return nil
	}

	status := sys.GetProperty(nil, object, key, &result)
	nt.AddReturnedStatus(env, "envIsNull", object, "Invalid argument", sys.StatusInvalidArg, status)

	sys.GetProperty(env, nil, key, &result)
	nt.AddLastStatus(env, "objectIsNull", object)

	sys.GetProperty(env, object, nil, &result)
	nt.AddLastStatus(env, "keyIsNull", object)

	sys.GetProperty(env, object, key, nil)
	nt.AddLastStatus(env, "resultIsNull", object)

	return object
}

func testFreeze(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [1]sys.Value
	if _, ok := getArgs(env, info, args[:]); !ok {
		return nil
	}
	object := args[0]
	if !nt.Call(env, sys.ObjectFreeze(env, object)) {
		return nil
	}
	return object
}

func testSeal(env sys.Env, info sys.CallbackInfo) sys.Value {
	var args [1]sys.Value
	if _, ok := getArgs(env, info, args[:]); !ok {
		return nil
	}
	object := args[0]
	if !nt.Call(env, sys.ObjectSeal(env, object)) {
		return nil
	}
	return object
}

// typeTags are the 128-bit tags from the C test.
var typeTags = [...]sys.TypeTag{
	{Lower: 0xdaf987b3cc62481a, Upper: 0xb745b0497f299531},
	{Lower: 0xbb7936c374084d9b, Upper: 0xa9548d0762eeedb9},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0},
	{Lower: 0, Upper: 0},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0xdaf987b3cc62481a},
}

// validateTypeIndex is equivalent to VALIDATE_TYPE_INDEX. The C macro throws the RangeError and
// continues (with an out-of-bounds access) until the next Node-API call fails due to the pending
// exception; here we return immediately with the same observable behavior.
func validateTypeIndex(env sys.Env, typeIndex uint32) bool {
	if typeIndex >= uint32(len(typeTags)) {
		nt.Call(env, sys.ThrowRangeError(env, nt.CStr("NODE_API_TEST_INVALID_TYPE_INDEX"), nt.CStr("Invalid type index")))
		return false
	}
	return true
}

// inLieuOfNull is the External data: V8 does not accept an External with NULL data.
const inLieuOfNull = 1

// typeIndexArg reads the tag index from the first argument and validates it.
func typeIndexArg(env sys.Env, info sys.CallbackInfo) (uint32, bool) {
	argc := uint(1)
	var whichType sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &whichType, nil, nil)) {
		return 0, false
	}
	var typeIndex uint32
	if !nt.Call(env, sys.GetValueUint32(env, whichType, &typeIndex)) {
		return 0, false
	}
	return typeIndex, validateTypeIndex(env, typeIndex)
}

// tagObject applies a copy of the tag and zeroes the copy afterwards: an implementation that
// stored the pointer instead of the value would fail the subsequent check.
func tagObject(env sys.Env, instance sys.Value, typeIndex uint32) bool {
	tag := typeTags[typeIndex]
	if !nt.Call(env, sys.TypeTagObject(env, instance, &tag)) {
		return false
	}
	tag = sys.TypeTag{}
	return true
}

func typeTaggedInstance(env sys.Env, info sys.CallbackInfo) sys.Value {
	typeIndex, ok := typeIndexArg(env, info)
	if !ok {
		return nil
	}
	var instance sys.Value
	if !nt.Call(env, sys.CreateObject(env, &instance)) {
		return nil
	}
	if !tagObject(env, instance, typeIndex) {
		return nil
	}
	return instance
}

func plainExternal(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var instance sys.Value
	if !nt.Call(env, sys.CreateExternal(env, inLieuOfNull, nil, 0, &instance)) {
		return nil
	}
	return instance
}

func typeTaggedExternal(env sys.Env, info sys.CallbackInfo) sys.Value {
	typeIndex, ok := typeIndexArg(env, info)
	if !ok {
		return nil
	}
	var instance sys.Value
	if !nt.Call(env, sys.CreateExternal(env, inLieuOfNull, nil, 0, &instance)) {
		return nil
	}
	if !tagObject(env, instance, typeIndex) {
		return nil
	}
	return instance
}

func checkTypeTag(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var argv [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &argv[0], nil, nil)) {
		return nil
	}
	var typeIndex uint32
	if !nt.Call(env, sys.GetValueUint32(env, argv[0], &typeIndex)) {
		return nil
	}
	if !validateTypeIndex(env, typeIndex) {
		return nil
	}
	var result bool
	if !nt.Call(env, sys.CheckObjectTypeTag(env, argv[1], &typeTags[typeIndex], &result)) {
		return nil
	}
	var jsResult sys.Value
	if !nt.Call(env, sys.GetBoolean(env, result, &jsResult)) {
		return nil
	}
	return jsResult
}

// testAnswer is the value 42 of properties created by the create_object_with_properties tests.
const testAnswer = 42

func testCreateObjectWithProperties(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var names, values [3]sys.Value
	var result sys.Value

	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("name"), sys.AutoLength, &names[0])) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("Foo"), sys.AutoLength, &values[0])) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("age"), sys.AutoLength, &names[1])) {
		return nil
	}
	if !nt.Call(env, sys.CreateInt32(env, testAnswer, &values[1])) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("active"), sys.AutoLength, &names[2])) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, true, &values[2])) {
		return nil
	}

	var nullPrototype sys.Value
	if !nt.Call(env, sys.GetNull(env, &nullPrototype)) {
		return nil
	}

	if !nt.Call(env, nt.CreateObjectWithProperties(env, nullPrototype, names[:], values[:], &result)) {
		return nil
	}
	return result
}

func testCreateObjectWithPropertiesEmpty(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, nt.CreateObjectWithProperties(env, nil, nil, nil, &result)) {
		return nil
	}
	return result
}

func testCreateObjectWithCustomPrototype(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var prototype, methodName, methodFunc sys.Value
	var names, values [1]sys.Value
	var result sys.Value

	if !nt.Call(env, sys.CreateObject(env, &prototype)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("test"), sys.AutoLength, &methodName)) {
		return nil
	}
	if !nt.Call(env, sys.CreateFunction(env, nt.CStr("test"), sys.AutoLength, sys.CallbackTrampoline,
		testCreateObjectWithPropertiesData, &methodFunc)) {
		return nil
	}
	if !nt.Call(env, sys.SetProperty(env, prototype, methodName, methodFunc)) {
		return nil
	}

	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("value"), sys.AutoLength, &names[0])) {
		return nil
	}
	if !nt.Call(env, sys.CreateInt32(env, testAnswer, &values[0])) {
		return nil
	}

	if !nt.Call(env, nt.CreateObjectWithProperties(env, prototype, names[:], values[:], &result)) {
		return nil
	}
	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("Get", get),
		nt.DeclareProperty("GetNamed", getNamed),
		nt.DeclareProperty("GetPropertyNames", getPropertyNames),
		nt.DeclareProperty("GetSymbolNames", allNames(sys.KeyIncludePrototypes, sys.KeySkipStrings)),
		nt.DeclareProperty("GetEnumerableWritableNames",
			allNames(sys.KeyIncludePrototypes, sys.KeyEnumerable|sys.KeyWritable)),
		nt.DeclareProperty("GetOwnWritableNames", allNames(sys.KeyOwnOnly, sys.KeyWritable)),
		nt.DeclareProperty("GetEnumerableConfigurableNames",
			allNames(sys.KeyIncludePrototypes, sys.KeyEnumerable|sys.KeyConfigurable)),
		nt.DeclareProperty("GetOwnConfigurableNames", allNames(sys.KeyOwnOnly, sys.KeyConfigurable)),
		nt.DeclareProperty("Set", set),
		nt.DeclareProperty("SetNamed", setNamed),
		nt.DeclareProperty("Has", has),
		nt.DeclareProperty("HasNamed", hasNamed),
		nt.DeclareProperty("HasOwn", hasOwn),
		nt.DeclareProperty("Delete", deleteProp),
		nt.DeclareProperty("New", newObject),
		nt.DeclareProperty("Inflate", inflate),
		nt.DeclareProperty("Wrap", wrap),
		nt.DeclareProperty("Unwrap", unwrap),
		nt.DeclareProperty("TestSetProperty", testSetProperty),
		nt.DeclareProperty("TestHasProperty", testHasProperty),
		nt.DeclareProperty("TypeTaggedInstance", typeTaggedInstance),
		nt.DeclareProperty("TypeTaggedExternal", typeTaggedExternal),
		nt.DeclareProperty("PlainExternal", plainExternal),
		nt.DeclareProperty("CheckTypeTag", checkTypeTag),
		nt.DeclareProperty("TestGetProperty", testGetProperty),
		nt.DeclareProperty("TestFreeze", testFreeze),
		nt.DeclareProperty("TestSeal", testSeal),
		{
			Utf8Name:   nt.CStr("TestCreateObjectWithProperties"),
			Method:     sys.CallbackTrampoline,
			Attributes: sys.PropertyDefault,
			Data:       testCreateObjectWithPropertiesData,
		},
		nt.DeclareProperty("TestCreateObjectWithPropertiesEmpty", testCreateObjectWithPropertiesEmpty),
		nt.DeclareProperty("TestCreateObjectWithCustomPrototype", testCreateObjectWithCustomPrototype),
	}

	initTestNull(env, exports)

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
