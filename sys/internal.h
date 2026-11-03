/*
 * internal.h — internal C declarations for sys (loader, trampolines, runtime guard, signals).
 *
 * Also included by the cgo preamble of handlers.go, so it ends up in _cgo_export.h: keep to
 * declarations and static inline functions only.
 */
#ifndef NAPI_GO_INTERNAL_H
#define NAPI_GO_INTERNAL_H

#include "zz_api.h"

/* Number of arguments read onto the stack by the callback trampoline; beyond this, Go re-fetches them. */
#define GN_INLINE_ARGS 8

/* loader.c: resolves the gn_api table once per process. Returns the number of missing functions
   among those required by the declared version (NAPI_VERSION). */
int gn_api_load_once(const char** first_missing);

/* module.c */
int32_t gn_module_api_version(void);

/* trampoline.c: C function pointers passed to Node-API that enter Go. */
napi_value gn_callback(napi_env env, napi_callback_info info);
void gn_finalize(napi_env env, void* data, void* hint);
void gn_call_js(napi_env env, napi_value js_callback, void* context, void* data);
void gn_async_execute(napi_env env, void* data);
void gn_async_complete(napi_env env, napi_status status, void* data);
void gn_cleanup_hook(void* arg);
void gn_async_cleanup_hook(napi_async_cleanup_hook_handle handle, void* arg);
void gn_throw_foreign_runtime(napi_env env);

/*
 * Guard against a second Go runtime on the same thread (macOS x64 only).
 * On darwin/amd64 all Go runtimes in the process store g in the same fixed TLS slot
 * (0x30(GS), golang/go#23617): entering Go while the slot holds another runtime's g
 * corrupts memory. gn_enter() returns 0 in that case. On all other platforms each runtime
 * has its own TLS slot and the guard is a no-op.
 */
#if defined(__APPLE__) && defined(__x86_64__)
#define GN_GUARD_FOREIGN_RUNTIME 1
int gn_enter(void);
void gn_leave(void);
#else
static inline int gn_enter(void) { return 1; }
static inline void gn_leave(void) {}
#endif

/* sigfix.c: restores SA_RESETHAND on Node's signal handlers (macOS). */
void gn_repair_node_signal_handlers(void);

/* pin.c: keeps the module mapped in memory until process exit (worker dlclose on macOS). */
void gn_pin_module(void);

#endif /* NAPI_GO_INTERNAL_H */
