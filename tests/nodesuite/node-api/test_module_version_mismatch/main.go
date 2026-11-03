// Port of test/node-api/test_module_version_mismatch/test_module_version_mismatch.c:
// binding.gyp declares NAPI_VERSION=2147483646, which the runner translates into the
// napi_go_test_version_mismatch build tag for sys. Node.js must reject the module before calling
// init, with an error that require() can catch.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// initModule corresponds to the body of NAPI_MODULE_INIT(): Node.js should never call it.
func initModule(_ sys.Env, exports sys.Value) sys.Value {
	return exports
}
