// Porting di test/node-api/test_module_version_mismatch/test_module_version_mismatch.c: il
// binding.gyp dichiara NAPI_VERSION=2147483646, che il runner traduce nel build tag
// napi_go_test_version_mismatch di sys. Node.js deve rifiutare il modulo prima di chiamare l'init,
// con un errore che require() può catturare.
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// initModule equivale al corpo di NAPI_MODULE_INIT(): Node.js non dovrebbe mai chiamarlo.
func initModule(_ sys.Env, exports sys.Value) sys.Value {
	return exports
}
