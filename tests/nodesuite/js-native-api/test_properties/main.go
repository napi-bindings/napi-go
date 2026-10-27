// Porting di test/js-native-api/test_properties/test_properties.c (#define NAPI_VERSION 9 →
// napi-go.json).
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

var value = 1.0

// nameBufferSize è la dimensione di char buffer[128] in HasNamedProperty.
const nameBufferSize = 128

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

func hasNamedProperty(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(2)
	var args [2]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}
	if !nt.Assert(env, argc == 2, "argc == 2", "Wrong number of arguments") {
		return nil
	}

	// Nome della proprietà da controllare.
	var buffer [nameBufferSize]byte
	var copied uint
	if !nt.Call(env, sys.GetValueStringUTF8(env, args[1], &buffer[0], uint(len(buffer)), &copied)) {
		return nil
	}

	var value bool
	var result sys.Value
	if !nt.Call(env, sys.HasNamedProperty(env, args[0], &buffer[0], &value)) {
		return nil
	}
	if !nt.Call(env, sys.GetBoolean(env, value, &result)) {
		return nil
	}
	return result
}

// keyed equivale a { NULL, name, 0, 0, 0, value, attributes, 0 }: proprietà con chiave napi_value.
func keyed(name, value sys.Value, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{Name: name, Value: value, Attributes: attributes}
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var number sys.Value
	if !nt.Call(env, sys.CreateDouble(env, value, &number)) {
		return nil
	}

	var nameValue sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("NameKeyValue"), sys.AutoLength, &nameValue)) {
		return nil
	}

	var symbolDescription, nameSymbol sys.Value
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr("NameKeySymbol"), sys.AutoLength, &symbolDescription)) {
		return nil
	}
	if !nt.Call(env, sys.CreateSymbol(env, symbolDescription, &nameSymbol)) {
		return nil
	}

	var nameSymbolDescriptionless sys.Value
	if !nt.Call(env, sys.CreateSymbol(env, nil, &nameSymbolDescriptionless)) {
		return nil
	}

	var nameSymbolFor sys.Value
	if !nt.Call(env, sys.SymbolFor(env, nt.CStr("NameKeySymbolFor"), sys.AutoLength, &nameSymbolFor)) {
		return nil
	}

	properties := []sys.PropertyDescriptor{
		nt.Method("echo", echo, sys.PropertyEnumerable),
		nt.Value("readwriteValue", number, sys.PropertyEnumerable|sys.PropertyWritable),
		nt.Value("readonlyValue", number, sys.PropertyEnumerable),
		nt.Value("hiddenValue", number, sys.PropertyDefault),
		keyed(nameValue, number, sys.PropertyEnumerable),
		keyed(nameSymbol, number, sys.PropertyEnumerable),
		keyed(nameSymbolDescriptionless, number, sys.PropertyEnumerable),
		keyed(nameSymbolFor, number, sys.PropertyEnumerable),
		nt.Accessor("readwriteAccessor1", getValue, setValue, sys.PropertyDefault),
		nt.Accessor("readwriteAccessor2", getValue, setValue, sys.PropertyWritable),
		nt.Accessor("readonlyAccessor1", getValue, nil, sys.PropertyDefault),
		nt.Accessor("readonlyAccessor2", getValue, nil, sys.PropertyWritable),
		nt.Method("hasNamedProperty", hasNamedProperty, sys.PropertyDefault),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, properties)) {
		return nil
	}

	return exports
}
