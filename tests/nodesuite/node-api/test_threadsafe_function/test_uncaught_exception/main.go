// Port of the test_uncaught_exception target from test/node-api/test_threadsafe_function/binding.gyp
// (test_uncaught_exception.c with NAPI_EXPERIMENTAL).
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/node-api/test_threadsafe_function/internal/uncaught"
)

func main() {}

func init() { nt.ModuleInit(uncaught.Init) }
