// Porting del target test_pending_exception di test/js-native-api/test_cannot_run_js/binding.gyp
// (test_cannot_run_js.c con NAPI_VERSION=9).
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/js-native-api/test_cannot_run_js/internal/cannotrunjs"
)

func main() {}

func init() { nt.ModuleInit(cannotrunjs.Init) }
