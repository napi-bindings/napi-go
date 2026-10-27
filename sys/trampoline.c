/*
 * trampoline.c — unici punti di ingresso C → Go oltre all'entry point del modulo.
 *
 * Node-API riceve sempre questi puntatori a funzione; i dati associati (data, hint, context, arg)
 * arrivano a Go come uintptr: non sono mai puntatori Go. Ogni trampolino passa per la guardia
 * gn_enter/gn_leave (attiva solo su macOS x64).
 */
#include <stdio.h>
#include "internal.h"
#include "_cgo_export.h"

static const char gn_foreign_code[] = "ERR_NAPI_GO_FOREIGN_RUNTIME";
static const char gn_foreign_message[] =
    "napi-go: another Go runtime owns this thread; on macOS x64 only one Go addon can be loaded "
    "per process";

/* Dove non si può lanciare un'eccezione si scrive su stderr, una volta sola per processo. */
static void gn_report_foreign_runtime(const char* where) {
  static volatile int reported = 0;
  if (reported) return;
  reported = 1;
  fprintf(stderr, "%s (%s skipped)\n", gn_foreign_message, where);
}

void gn_throw_foreign_runtime(napi_env env) {
  /* Se il lancio fallisce (es. eccezione già pendente) resta almeno il messaggio su stderr. */
  if (gn_napi_throw_error(env, gn_foreign_code, gn_foreign_message) != napi_ok) {
    gn_report_foreign_runtime("throw");
  }
}

/* Callback di ogni funzione JS creata da napi-go: legge argomenti, this e data in C, così che
   l'ingresso in Go sia uno solo. Gli slot argv oltre argc sono riempiti da Node con undefined. */
napi_value gn_callback(napi_env env, napi_callback_info info) {
  size_t argc = GN_INLINE_ARGS;
  napi_value argv[GN_INLINE_ARGS];
  gn_r_napi_get_cb_info r = gn_napi_get_cb_info(env, info, &argc, argv, 1u | 2u);
  if (r.status != napi_ok) return NULL;
  if (!gn_enter()) {
    gn_throw_foreign_runtime(env);
    return NULL;
  }
  napi_value ret = goNapiCallback(env, info, (uintptr_t)r.o1, argc, argv, r.o0);
  gn_leave();
  return ret;
}

void gn_finalize(napi_env env, void* data, void* hint) {
  if (!gn_enter()) {
    gn_report_foreign_runtime("finalizer");
    return;
  }
  goNapiFinalize(env, (uintptr_t)data, (uintptr_t)hint);
  gn_leave();
}

/* env e js_callback sono NULL quando la TSFN viene distrutta con elementi ancora in coda. */
void gn_call_js(napi_env env, napi_value js_callback, void* context, void* data) {
  if (!gn_enter()) {
    if (env != NULL) gn_throw_foreign_runtime(env);
    return;
  }
  goNapiCallJS(env, js_callback, (uintptr_t)context, (uintptr_t)data);
  gn_leave();
}

/* Gira su un thread del pool di libuv: env non va usato per chiamate Node-API. */
void gn_async_execute(napi_env env, void* data) {
  if (!gn_enter()) {
    gn_report_foreign_runtime("async work");
    return;
  }
  goNapiAsyncExecute(env, (uintptr_t)data);
  gn_leave();
}

void gn_async_complete(napi_env env, napi_status status, void* data) {
  if (!gn_enter()) {
    gn_throw_foreign_runtime(env);
    return;
  }
  goNapiAsyncComplete(env, status, (uintptr_t)data);
  gn_leave();
}

void gn_cleanup_hook(void* arg) {
  if (!gn_enter()) {
    gn_report_foreign_runtime("cleanup hook");
    return;
  }
  goNapiCleanupHook((uintptr_t)arg);
  gn_leave();
}

void gn_async_cleanup_hook(napi_async_cleanup_hook_handle handle, void* arg) {
  if (!gn_enter()) {
    gn_report_foreign_runtime("async cleanup hook");
    return;
  }
  goNapiAsyncCleanupHook(handle, (uintptr_t)arg);
  gn_leave();
}
