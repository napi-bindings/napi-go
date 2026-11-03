/*
 * trampoline.c — sole C → Go entry points besides the module entry point.
 *
 * Node-API always receives these C function pointers; associated payloads (data, hint, context, arg)
 * arrive in Go as uintptr and are never Go pointers. Every trampoline passes through the
 * gn_enter/gn_leave guard (active only on macOS x64).
 */
#include <stdio.h>
#include "internal.h"
#include "_cgo_export.h"

static const char gn_foreign_code[] = "ERR_NAPI_GO_FOREIGN_RUNTIME";
static const char gn_foreign_message[] =
    "napi-go: another Go runtime owns this thread; on macOS x64 only one Go addon can be loaded "
    "per process";

/* Where throwing a JS exception is not possible, log to stderr once per process. */
static void gn_report_foreign_runtime(const char* where) {
  static volatile int reported = 0;
  if (reported) return;
  reported = 1;
  fprintf(stderr, "%s (%s skipped)\n", gn_foreign_message, where);
}

void gn_throw_foreign_runtime(napi_env env) {
  /* If throwing fails (e.g. an exception is already pending), at least log the message to stderr. */
  if (gn_napi_throw_error(env, gn_foreign_code, gn_foreign_message) != napi_ok) {
    gn_report_foreign_runtime("throw");
  }
}

/* Callback for every JS function created by napi-go: reads arguments, this, and data in C so that
   entering Go requires only a single cgo transition. Node fills argv slots beyond argc with undefined. */
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

/* env and js_callback are NULL when the TSFN is being torn down with items still queued. */
void gn_call_js(napi_env env, napi_value js_callback, void* context, void* data) {
  if (!gn_enter()) {
    if (env != NULL) gn_throw_foreign_runtime(env);
    return;
  }
  goNapiCallJS(env, js_callback, (uintptr_t)context, (uintptr_t)data);
  gn_leave();
}

/* Runs on a libuv worker pool thread: env must not be used for Node-API calls. */
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
