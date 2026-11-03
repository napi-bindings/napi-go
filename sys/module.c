/*
 * module.c — Node-API module entry point.
 *
 * NAPI_MODULE_INIT() defines and exports:
 *   - node_api_module_get_api_version_v1() → NAPI_VERSION, set by the active version build tag
 *     (version_*.go);
 *   - napi_register_module_v1(env, exports) → called by Node once per env (main thread and each
 *     worker_thread).
 *
 * The Go runtime of a c-shared library starts on dlopen; goNapiRegisterModule automatically waits
 * for runtime initialization to complete.
 */
#include <stdio.h>
#include "internal.h"
#include "_cgo_export.h"

#define GN_MESSAGE_MAX 256

NAPI_MODULE_INIT() {
  /* First of all: the Go runtime has already started and must never be unloaded (pin.c). */
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
    /* If napi_throw_error itself is missing, at least the stderr message above remains. */
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
  /* The Go runtime's initsig has completed: if setsigstack stripped SA_RESETHAND from Node's
     signal handlers (macOS), restore it. Run this even when the module is rejected, because its
     Go runtime has started regardless. */
  gn_repair_node_signal_handlers();
  return result;
}

int32_t gn_module_api_version(void) { return NODE_API_MODULE_GET_API_VERSION(); }
