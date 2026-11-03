// Port of the myobject_basic_finalizer target in test/js-native-api/6_object_wrap/binding.gyp:
// myobject.cc compiled with NAPI_EXPERIMENTAL, so Destructor runs during GC.
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/js-native-api/6_object_wrap/internal/myobject"
)

func main() {}

func init() { nt.ModuleInit(myobject.Init) }
