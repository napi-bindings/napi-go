// Porting di test/node-api/test_null_init/test_null_init.c. Il C registra il modulo alla vecchia
// maniera (napi_module_register da un costruttore) con nm_register_func NULL. In napi-go
// l'equivalente è un addon che non registra nessuna funzione di init (né nt.ModuleInit né
// sys.SetModuleInitHandler): sys lancia allora lo stesso errore di Node.js.
package main

import (
	// Collega sys all'addon come negli altri test, senza registrare l'init.
	_ "github.com/napi-bindings/napi-go/sys"
)

func main() {}
