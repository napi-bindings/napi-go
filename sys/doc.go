// Package sys è il Layer 0 di napi-go: binding 1:1 delle funzioni Node-API (N-API) per scrivere
// moduli nativi Node.js (.node) in Go.
//
// La maggior parte del pacchetto è generata da internal/gen a partire dagli header ufficiali
// (include/, node-api-headers): ogni funzione C napi_xxx / node_api_xxx ha un wrapper Go con lo
// stesso ordine di parametri, es. napi_create_int32 → CreateInt32. Per un'API idiomatica si usa
// il Layer 1 (pacchetto napi); sys serve a chi ha bisogno dell'ABI così com'è.
//
// # Convenzioni dei wrapper
//
//   - Uscite: i parametri C `T*` di uscita restano puntatori in Go, ma il wrapper C le restituisce
//     per valore, quindi nessun puntatore Go attraversa cgo e non ci sono allocazioni. Un puntatore
//     nil diventa NULL (alcune funzioni cambiano comportamento, es. napi_wrap senza result).
//     Le uscite vengono scritte solo se lo status è StatusOK.
//   - Dati opachi: i `void*` che Node-API conserva (data, hint, context, arg, native_object) sono
//     uintptr. Per le regole di cgo non possono essere puntatori Go: si usa un indice, un
//     runtime/cgo.Handle o memoria C.
//   - Buffer e array in ingresso (`const char*`, `const napi_value*`, ...) sono puntatori Go
//     validi per la sola durata della chiamata. Le struct passate per puntatore
//     (PropertyDescriptor) non devono contenere puntatori Go non fissati con runtime.Pinner.
//   - Funzioni mancanti: tutte le funzioni sono risolte a runtime dal processo host. Se il Node
//     che carica il modulo non ne esporta una, il wrapper restituisce StatusUnavailable;
//     Available(Fn...) lo dice in anticipo.
//
// # Thread
//
// Env, Value e gli altri handle valgono solo sul thread JavaScript dell'env e, per i Value, solo
// dentro la callback o lo scope corrente. Da una goroutine si possono chiamare solo le funzioni
// thread-safe di Node-API: CallThreadsafeFunction, AcquireThreadsafeFunction,
// ReleaseThreadsafeFunction e GetThreadsafeFunctionContext.
//
// # Ingressi in Go
//
// Node-API riceve puntatori a funzione C (CallbackTrampoline, FinalizeTrampoline, ...) che entrano
// in Go attraverso gli handler registrati con SetCallbackHandler, SetFinalizeHandler, ecc. Il
// modulo viene inizializzato da SetModuleInitHandler. Gli handler vanno registrati in init().
// Un panic che arriva fino a sys diventa un Error JS con code "GO_PANIC" dove è possibile
// lanciarlo; altrove (finalizer, async work, cleanup hook) termina il processo, come un panic Go
// non recuperato.
//
// # Versione Node-API dichiarata
//
// Il modulo dichiara a Node.js la versione DeclaredVersion (node_api_module_get_api_version_v1).
// Il default è 8; i build tag napi_go_v9, napi_go_v10 e napi_go_experimental la alzano. Le
// funzioni fino alla versione dichiarata devono essere presenti, altrimenti il caricamento
// fallisce con ERR_NAPI_GO_MISSING_FUNCTIONS; quelle più recenti sono facoltative.
//
// # Limiti di piattaforma
//
//   - macOS x64: tutti i runtime Go del processo condividono lo slot TLS di g, quindi è
//     supportato un solo addon Go per processo. Un secondo addon viene rifiutato con
//     ERR_NAPI_GO_FOREIGN_RUNTIME invece di corrompere la memoria.
//   - macOS: dopo l'avvio del runtime Go, sys ripristina SA_RESETHAND sugli handler SIGINT/SIGTERM
//     di Node, che il runtime perde (altrimenti il processo non termina).
//   - musl (Alpine): non supportato dalle release attuali di Go per i c-shared.
package sys

//go:generate go run ./internal/gen
