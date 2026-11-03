// Port of test/node-api/test_null_init/test_null_init.c. C registers the module using the legacy
// style (napi_module_register from a constructor) with nm_register_func set to NULL. In napi-go
// the equivalent is an addon that registers no init function (neither nt.ModuleInit nor
// sys.SetModuleInitHandler): sys then throws the same error as Node.js.
package main

import (
	// Link sys into the addon as in other tests, without registering an init handler.
	_ "github.com/napi-bindings/napi-go/sys"
)

func main() {}
