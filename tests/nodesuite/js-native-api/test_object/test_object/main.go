// Porting di test/js-native-api/test_object/test_object.c.
//
// node_api_create_object_with_properties è sperimentale e manca dagli header di sys: si chiama
// tramite napitest.CreateObjectWithProperties (vedi il commento della funzione).
package main

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

// testCreateObjectWithPropertiesData si registra una volta sola: la funzione creata in
// TestCreateObjectWithCustomPrototype si crea a ogni chiamata.
var testCreateObjectWithPropertiesData uintptr

func init() {
	testCreateObjectWithPropertiesData = nt.Register(testCreateObjectWithProperties)
	nt.ModuleInit(initModule)
}

// testValue è il dato di napi_wrap; come nel C se ne passa l'indirizzo (una variabile globale non
// viene spostata dal GC).
var testValue int32 = 3

const expectedTestValue = 3

// Dimensioni di char key[256] e limite della lunghezza dei nomi nel test C.
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

// args legge fino a len(buf) argomenti, come napi_get_cb_info nel test C.
func getArgs(env sys.Env, info sys.CallbackInfo, buf []sys.Value) (uint, bool) {
	argc := uint(len(buf))
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &buf[0], nil, nil)) {
		return 0, false
	}
	return argc, true
}

// expectObject verifica che v sia un oggetto, con il messaggio del test C. expr è il testo C della
// condizione (il nome della variabile cambia tra le funzioni).
func expectObject(env sys.Env, v sys.Value, expr string) bool {
	var t sys.ValueType
	if !nt.Call(env, sys.Typeof(env, v, &t)) {
		return false
	}
	return nt.Assert(env, t == sys.TypeObject, expr+" == napi_object", errObjectExpected)
}

// expectKey verifica che v sia una stringa o, se allowSymbol, un simbolo.
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

// readKey copia la stringa v in key, come napi_get_value_string_utf8(…, key, 255, &key_length)
// seguito da key[255] = 0 e dal controllo sulla lunghezza.
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

// objectArg legge il primo argomento e verifica che sia un oggetto (value_type0 nel C).
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

// allNames equivale alle funzioni Get*Names del C, che differiscono solo per i parametri di
// napi_get_all_property_names.
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
	// Il C accetta anche un simbolo ma usa il messaggio "string as second".
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
	// Il C non controlla il tipo del secondo argomento (verifica commentata).
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

// testNumber è il valore 987654321 della proprietà test_number creata da New.
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

// nullCheck crea object e key come TestSetProperty, TestHasProperty e TestGetProperty.
func nullCheckSetup(env sys.Env) (object, key sys.Value, ok bool) {
	if !nt.Call(env, sys.CreateObject(env, &object)) {
		return nil, nil, false
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(""), sys.AutoLength, &key)) {
		return nil, nil, false
	}
	return object, key, true
}

// Lo status delle chiamate senza controllo nelle tre funzioni seguenti è letto subito dopo da
// AddLastStatus, come nel C.

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

// typeTags sono i tag a 128 bit del test C.
var typeTags = [...]sys.TypeTag{
	{Lower: 0xdaf987b3cc62481a, Upper: 0xb745b0497f299531},
	{Lower: 0xbb7936c374084d9b, Upper: 0xa9548d0762eeedb9},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0},
	{Lower: 0, Upper: 0},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0xdaf987b3cc62481a},
}

// validateTypeIndex equivale a VALIDATE_TYPE_INDEX. La macro C lancia il RangeError e prosegue
// (con un accesso fuori dai limiti) fino alla prima chiamata Node-API che fallisce per l'eccezione
// pendente; qui si esce subito, con lo stesso effetto osservabile.
func validateTypeIndex(env sys.Env, typeIndex uint32) bool {
	if typeIndex >= uint32(len(typeTags)) {
		nt.Call(env, sys.ThrowRangeError(env, nt.CStr("NODE_API_TEST_INVALID_TYPE_INDEX"), nt.CStr("Invalid type index")))
		return false
	}
	return true
}

// inLieuOfNull è il data dell'External: V8 non accetta un External con data NULL.
const inLieuOfNull = 1

// typeIndexArg legge l'indice del tag dal primo argomento e lo valida.
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

// tagObject applica una copia del tag e poi la azzera: un'implementazione che memorizzasse il
// puntatore invece del valore fallirebbe il controllo successivo.
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

// testAnswer è il valore 42 delle proprietà create dai test di create_object_with_properties.
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
