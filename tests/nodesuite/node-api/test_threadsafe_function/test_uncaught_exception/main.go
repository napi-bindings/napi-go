// Porting del target test_uncaught_exception di test/node-api/test_threadsafe_function/binding.gyp
// (test_uncaught_exception.c con NAPI_EXPERIMENTAL).
package main

import (
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
	"github.com/napi-bindings/napi-go/tests/nodesuite/node-api/test_threadsafe_function/internal/uncaught"
)

func main() {}

func init() { nt.ModuleInit(uncaught.Init) }
