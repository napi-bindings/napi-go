/*
 * module.c — entry point del modulo Node-API.
 *
 * NAPI_MODULE_INIT() definisce ed esporta:
 *   - node_api_module_get_api_version_v1()  → NAPI_VERSION (8)
 *   - napi_register_module_v1(env, exports) → chiamata da Node una volta per env
 *     (main thread e ogni worker_thread).
 *
 * Il Go runtime di un c-shared si inizializza al dlopen; goNapiRegisterModule
 * (esportata da Go) attende automaticamente la fine dell'inizializzazione.
 */
#include <stdio.h>
#include "gonapi.h"
#include "_cgo_export.h"

NAPI_MODULE_INIT() {
  const char* first_missing = NULL;
  int missing = gn_load_symbols_once(&first_missing);
  if (missing != 0) {
    /* Senza i simboli non possiamo nemmeno lanciare un'eccezione JS. */
    fprintf(stderr, "napi-go: %d simboli Node-API mancanti nel processo host (primo: %s)\n",
            missing, first_missing ? first_missing : "?");
    return NULL;
  }
  napi_value result = goNapiRegisterModule(env, exports);
  if (result != NULL && gn_register_diag(env, result) != napi_ok) {
    fprintf(stderr, "napi-go: registrazione di __sigInfo fallita\n");
  }
  return result;
}
