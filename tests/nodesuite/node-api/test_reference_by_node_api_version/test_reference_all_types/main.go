// Porting del target test_reference_all_types di test/node-api/test_reference_by_node_api_version/binding.gyp
// (test_reference_by_node_api_version.c con NAPI_VERSION=10).
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/node-api/test_reference_by_node_api_version/internal/refbyversion"
)

func main() {}

func init() { nt.ModuleInit(refbyversion.Init) }
