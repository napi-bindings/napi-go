// Porting di test/js-native-api/test_constructor/test_constructor.c.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

var (
	value       = 1.0
	staticValue = 10.0
	// testDefineClassData si registra una volta sola (vedi init).
	testDefineClassData uintptr
)

func init() {
	testDefineClassData = nt.Register(testDefineClass)
	nt.ModuleInit(initModule)
}

const trackedFunction = "TrackedFunction"

func testDefineClass(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result, returnValue sys.Value

	propertyDescriptor := sys.PropertyDescriptor{
		Utf8Name:   nt.CStr("TestDefineClass"),
		Method:     sys.CallbackTrampoline,
		Attributes: sys.PropertyEnumerable | sys.PropertyStatic,
		Data:       testDefineClassData,
	}

	if !nt.Call(env, sys.CreateObject(env, &returnValue)) {
		return nil
	}

	// Lo status delle chiamate senza controllo è letto subito dopo da AddLastStatus, come nel C.
	// Il data del costruttore è NULL come nel C: queste classi non vengono mai istanziate.
	status := sys.DefineClass(nil, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		0, 1, &propertyDescriptor, &result)

	nt.AddReturnedStatus(env, "envIsNull", returnValue, "Invalid argument", sys.StatusInvalidArg, status)

	sys.DefineClass(env, nil, sys.AutoLength, sys.CallbackTrampoline, 0, 1,
		&propertyDescriptor, &result)

	nt.AddLastStatus(env, "nameIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(trackedFunction), sys.AutoLength, nil, 0, 1, &propertyDescriptor, &result)

	nt.AddLastStatus(env, "cbIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		0, 1, &propertyDescriptor, &result)

	nt.AddLastStatus(env, "cbDataIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		0, 1, nil, &result)

	nt.AddLastStatus(env, "propertiesIsNull", returnValue)

	sys.DefineClass(env, nt.CStr(trackedFunction), sys.AutoLength, sys.CallbackTrampoline,
		0, 1, &propertyDescriptor, nil)

	nt.AddLastStatus(env, "resultIsNull", returnValue)

	return returnValue
}

func getValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(0)
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, nil, nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 0, "argc == 0", "Wrong number of arguments") {
		return nil
	}

	var number sys.Value
	if !nt.Call(env, sys.CreateDouble(env, value, &number)) {
		return nil
	}
	return number
}

func setValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	if !nt.Call(env, sys.GetValueDouble(env, args[0], &value)) {
		return nil
	}
	return nil
}

func echo(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}
	return args[0]
}

func newObject(env sys.Env, info sys.CallbackInfo) sys.Value {
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}
	return this
}

func getStaticValue(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(0)
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, nil, nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 0, "argc == 0", "Wrong number of arguments") {
		return nil
	}

	var number sys.Value
	if !nt.Call(env, sys.CreateDouble(env, staticValue, &number)) {
		return nil
	}
	return number
}

func newExtra(env sys.Env, info sys.CallbackInfo) sys.Value {
	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}
	return this
}

func initModule(env sys.Env, _ sys.Value) sys.Value {
	var number, cons sys.Value
	if !nt.Call(env, sys.CreateDouble(env, value, &number)) {
		return nil
	}

	// Lunghezza 8: il nome della classe diventa "MyObject".
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("MyObject_Extra"), 8, sys.CallbackTrampoline,
		nt.Register(newExtra), 0, nil, &cons)) {
		return nil
	}

	properties := []sys.PropertyDescriptor{
		nt.Method("echo", echo, sys.PropertyEnumerable),
		nt.Value("readwriteValue", number, sys.PropertyEnumerable|sys.PropertyWritable),
		nt.Value("readonlyValue", number, sys.PropertyEnumerable),
		nt.Value("hiddenValue", number, sys.PropertyDefault),
		nt.Accessor("readwriteAccessor1", getValue, setValue, sys.PropertyDefault),
		nt.Accessor("readwriteAccessor2", getValue, setValue, sys.PropertyWritable),
		nt.Accessor("readonlyAccessor1", getValue, nil, sys.PropertyDefault),
		nt.Accessor("readonlyAccessor2", getValue, nil, sys.PropertyWritable),
		nt.Accessor("staticReadonlyAccessor1", getStaticValue, nil, sys.PropertyDefault|sys.PropertyStatic),
		nt.Value("constructorName", cons, sys.PropertyEnumerable|sys.PropertyStatic),
		{
			Utf8Name:   nt.CStr("TestDefineClass"),
			Method:     sys.CallbackTrampoline,
			Attributes: sys.PropertyEnumerable | sys.PropertyStatic,
			Data:       testDefineClassData,
		},
	}

	if !nt.Call(env, sys.DefineClass(env, nt.CStr("MyObject"), sys.AutoLength, sys.CallbackTrampoline,
		nt.Register(newObject), uint(len(properties)), &properties[0], &cons)) {
		return nil
	}

	initTestNull(env, cons)

	return cons
}
