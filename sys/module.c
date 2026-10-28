/*
 * module.c — entry point del modulo Node-API.
 *
 * NAPI_MODULE_INIT() definisce ed esporta:
 *   - node_api_module_get_api_version_v1() → NAPI_VERSION, impostata dal build tag di versione
 *     (version_*.go);
 *   - napi_register_module_v1(env, exports) → chiamata da Node una volta per env (main thread e
 *     ogni worker_thread).
 *
 * Il runtime Go di un c-shared si avvia al dlopen; goNapiRegisterModule attende da sola la fine
 * della sua inizializzazione.
 */
#include <stdio.h>
#include "internal.h"
#include "_cgo_export.h"

#define GN_MESSAGE_MAX 256

NAPI_MODULE_INIT() {
  /* Prima di tutto: il runtime Go è già partito e non deve essere scaricato (pin.c). */
  gn_pin_module();
  const char* first_missing = NULL;
  int missing = gn_api_load_once(&first_missing);
  if (missing != 0) {
    char msg[GN_MESSAGE_MAX];
    snprintf(msg, sizeof msg,
             "napi-go: %d Node-API functions required by Node-API %d are missing from the host "
             "process (first: %s)",
             missing, (int)NAPI_VERSION, first_missing ? first_missing : "?");
    fprintf(stderr, "%s\n", msg);
    /* Se manca anche napi_throw_error resta solo il messaggio su stderr. */
    gn_napi_throw_error(env, "ERR_NAPI_GO_MISSING_FUNCTIONS", msg);
    return NULL;
  }
  napi_value result = NULL;
  if (gn_enter()) {
    result = goNapiRegisterModule(env, exports);
    gn_leave();
  } else {
    gn_throw_foreign_runtime(env);
  }
  /* L'initsig del runtime Go è concluso: se setsigstack ha perso SA_RESETHAND sugli handler di
     Node (macOS) lo ripristiniamo. Anche quando il modulo viene rifiutato, perché il suo runtime
     è comunque partito. */
  gn_repair_node_signal_handlers();
  return result;
}

int32_t gn_module_api_version(void) { return NODE_API_MODULE_GET_API_VERSION(); }
