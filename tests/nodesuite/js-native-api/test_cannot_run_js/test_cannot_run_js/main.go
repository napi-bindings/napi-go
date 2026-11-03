// Port of the test_cannot_run_js target in test/js-native-api/test_cannot_run_js/binding.gyp
// (test_cannot_run_js.c with NAPI_VERSION=10).
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/js-native-api/test_cannot_run_js/internal/cannotrunjs"
)

func main() {}

func init() { nt.ModuleInit(cannotrunjs.Init) }
