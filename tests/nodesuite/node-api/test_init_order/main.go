// Port of test/node-api/test_init_order/test_init_order.cc. Dynamic initialization of C++ static
// variables becomes initialization of Go package-level variables, which the runtime completes
// before Node.js invokes the module: the script verifies that the values are already initialized
// when Init runs.
package main

import (
	"strconv"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// initialValue and initialString are the values of MyClass::valueHolder and MyClass::testString.
const (
	initialValue  = 42
	initialString = 123
)

// Like the static members of MyClass: initialized by code executed at runtime (not constants),
// so initialization order actually matters.
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
