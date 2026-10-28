// Porting di test/node-api/test_instance_data/test_ref_then_set.c (target test_ref_then_set, con addon.c).
package main

import (
	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/node-api/test_instance_data/internal/addon"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// initModule equivale al corpo di NAPI_MODULE_INIT().
func initModule(env sys.Env, exports sys.Value) sys.Value {
	return addon.New(env, exports, true)
}
