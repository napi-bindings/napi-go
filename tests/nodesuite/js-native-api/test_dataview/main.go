// Porting di test/js-native-api/test_dataview/test_dataview.c.
//
// Differenza: il test C, se il primo argomento non è un ArrayBuffer, verifica con
// node_api_is_sharedarraybuffer che sia uno SharedArrayBuffer. La funzione è sperimentale e
// manca dagli header di sys (node-api-headers 1.9.0): qui il controllo è lasciato a
// napi_create_dataview, che rifiuta da sé un buffer non valido. Gli script JS non passano mai
// un valore che non sia un ArrayBuffer o uno SharedArrayBuffer, quindi l'esito non cambia.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

func createDataView(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(3)
	var args [3]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 3, "argc == 3", "Wrong number of arguments") {
		return nil
	}

	var valuetype0 sys.ValueType
	arraybuffer := args[0]

	if !nt.Call(env, sys.Typeof(env, arraybuffer, &valuetype0)) {
		return nil
	}
	if !nt.Assert(env, valuetype0 == sys.TypeObject, "valuetype0 == napi_object",
		"Wrong type of arguments. Expects a ArrayBuffer as the first argument.") {
		return nil
	}

	var isArrayBuffer bool
	if !nt.Call(env, sys.IsArrayBuffer(env, arraybuffer, &isArrayBuffer)) {
		return nil
	}
	// Ramo !is_arraybuffer → node_api_is_sharedarraybuffer omesso: vedi il commento del file.

	var valuetype1 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[1], &valuetype1)) {
		return nil
	}

	if !nt.Assert(env, valuetype1 == sys.TypeNumber, "valuetype1 == napi_number",
		"Wrong type of arguments. Expects a number as second argument.") {
		return nil
	}

	var byteOffset uint32
	if !nt.Call(env, sys.GetValueUint32(env, args[1], &byteOffset)) {
		return nil
	}

	var valuetype2 sys.ValueType
	if !nt.Call(env, sys.Typeof(env, args[2], &valuetype2)) {
		return nil
	}

	if !nt.Assert(env, valuetype2 == sys.TypeNumber, "valuetype2 == napi_number",
		"Wrong type of arguments. Expects a number as third argument.") {
		return nil
	}

	var length uint32
	if !nt.Call(env, sys.GetValueUint32(env, args[2], &length)) {
		return nil
	}

	var outputDataView sys.Value
	if !nt.Call(env, sys.CreateDataView(env, uint(length), arraybuffer, uint(byteOffset), &outputDataView)) {
		return nil
	}

	return outputDataView
}

func createDataViewFromJSDataView(env sys.Env, info sys.CallbackInfo) sys.Value {
	argc := uint(1)
	var args [1]sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, &argc, &args[0], nil, nil)) {
		return nil
	}

	if !nt.Assert(env, argc == 1, "argc == 1", "Wrong number of arguments") {
		return nil
	}

	var valuetype sys.ValueType
	inputDataView := args[0]

	if !nt.Call(env, sys.Typeof(env, inputDataView, &valuetype)) {
		return nil
	}
	if !nt.Assert(env, valuetype == sys.TypeObject, "valuetype == napi_object",
		"Wrong type of arguments. Expects a DataView as the first argument.") {
		return nil
	}

	var isDataView bool
	if !nt.Call(env, sys.IsDataView(env, inputDataView, &isDataView)) {
		return nil
	}
	if !nt.Assert(env, isDataView, "is_dataview",
		"Wrong type of arguments. Expects a DataView as the first argument.") {
		return nil
	}
	var byteOffset, length uint
	var buffer sys.Value
	if !nt.Call(env, sys.GetDataViewInfo(env, inputDataView, &length, nil, &buffer, &byteOffset)) {
		return nil
	}

	var outputDataView sys.Value
	if !nt.Call(env, sys.CreateDataView(env, length, buffer, byteOffset, &outputDataView)) {
		return nil
	}

	return outputDataView
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("CreateDataView", createDataView),
		nt.DeclareProperty("CreateDataViewFromJSDataView", createDataViewFromJSDataView),
	}

	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
