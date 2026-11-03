/*
 * module.c — Node-API module entry point.
 *
 * NAPI_MODULE_INIT() defines and exports:
 *   - node_api_module_get_api_version_v1()  → NAPI_VERSION (8)
 *   - napi_register_module_v1(env, exports) → called by Node once per env
 *     (main thread and each worker_thread).
 *
 * The Go runtime of a c-shared library initializes on dlopen; goNapiRegisterModule
 * (exported by Go) automatically waits for initialization to finish.
 */
#include <stdio.h>
#include "gonapi.h"
#include "_cgo_export.h"

NAPI_MODULE_INIT() {
  const char* first_missing = NULL;
  int missing = gn_load_symbols_once(&first_missing);
  if (missing != 0) {
    /* Without symbols we cannot even throw a JS exception. */
    fprintf(stderr, "napi-go: %d missing Node-API symbols in host process (first: %s)\n",
            missing, first_missing ? first_missing : "?");
    return NULL;
  }
  napi_value result = goNapiRegisterModule(env, exports);
  /* At this point Go runtime initsig has completed (goNapiRegisterModule waits for it):
     restore SA_RESETHAND on node::SignalExit if setsigstack dropped it (macOS). */
  gn_repair_node_signal_handlers();
  if (result != NULL && gn_register_diag(env, result) != napi_ok) {
    fprintf(stderr, "napi-go: failed to register __sigInfo\n");
  }
  return result;
}
