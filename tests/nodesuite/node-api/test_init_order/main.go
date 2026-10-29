// Porting di test/node-api/test_init_order/test_init_order.cc. L'inizializzazione dinamica delle
// variabili statiche C++ diventa l'inizializzazione delle variabili di pacchetto Go, che il
// runtime completa prima che Node.js chiami il modulo: lo script verifica che i valori siano già
// pronti quando gira Init.
package main

import (
	"strconv"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// initialValue e initialString sono i valori di MyClass::valueHolder e MyClass::testString.
const (
	initialValue  = 42
	initialString = 123
)

// Come i membri statici di MyClass: inizializzati da codice eseguito a runtime (non costanti),
// così l'ordine di inizializzazione conta davvero.
var (
	valueHolder = newInt(initialValue)
	testString  = strconv.Itoa(initialString)
)

func newInt(v int32) *int32 { return &v }

func initModule(env sys.Env, exports sys.Value) sys.Value {
	var cppIntValue, cppStringValue sys.Value
	if !nt.Call(env, sys.CreateInt32(env, *valueHolder, &cppIntValue)) {
		return nil
	}
	if !nt.Call(env, sys.CreateStringUTF8(env, nt.CStr(testString), sys.AutoLength, &cppStringValue)) {
		return nil
	}
	descriptors := []sys.PropertyDescriptor{
		nt.DeclarePropertyValue("cppIntValue", cppIntValue),
		nt.DeclarePropertyValue("cppStringValue", cppStringValue),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}
	return exports
}
