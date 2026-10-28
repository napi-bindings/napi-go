/*
 * internal.h — dichiarazioni C interne a sys (loader, trampolini, guardia, segnali).
 *
 * Incluso anche dal preambolo di handlers.go, quindi finisce in _cgo_export.h: solo
 * dichiarazioni e funzioni static inline.
 */
#ifndef NAPI_GO_INTERNAL_H
#define NAPI_GO_INTERNAL_H

#include "zz_api.h"

/* Argomenti letti sullo stack dal trampolino delle callback; oltre, Go li rilegge. */
#define GN_INLINE_ARGS 8

/* loader.c: risolve la tabella gn_api una volta per processo. Restituisce il numero di funzioni
   mancanti tra quelle richieste dalla versione dichiarata (NAPI_VERSION). */
int gn_api_load_once(const char** first_missing);

/* module.c */
int32_t gn_module_api_version(void);

/* trampoline.c: puntatori a funzione C passati a Node-API, che entrano in Go. */
napi_value gn_callback(napi_env env, napi_callback_info info);
void gn_finalize(napi_env env, void* data, void* hint);
void gn_call_js(napi_env env, napi_value js_callback, void* context, void* data);
void gn_async_execute(napi_env env, void* data);
void gn_async_complete(napi_env env, napi_status status, void* data);
void gn_cleanup_hook(void* arg);
void gn_async_cleanup_hook(napi_async_cleanup_hook_handle handle, void* arg);
void gn_throw_foreign_runtime(napi_env env);

/*
 * Guardia contro un secondo runtime Go sullo stesso thread (solo macOS x64).
 * Su darwin/amd64 tutti i runtime Go del processo tengono g nello stesso slot TLS fisso
 * (0x30(GS), golang/go#23617): entrare in Go mentre lo slot contiene il g di un altro runtime
 * corrompe la memoria. gn_enter() restituisce 0 in quel caso. Altrove ogni runtime ha il proprio
 * slot e la guardia non fa nulla.
 */
#if defined(__APPLE__) && defined(__x86_64__)
#define GN_GUARD_FOREIGN_RUNTIME 1
int gn_enter(void);
void gn_leave(void);
#else
static inline int gn_enter(void) { return 1; }
static inline void gn_leave(void) {}
#endif

/* sigfix.c: ripristina SA_RESETHAND sugli handler di Node (macOS). */
void gn_repair_node_signal_handlers(void);

/* pin.c: tiene il modulo in memoria fino alla fine del processo (dlclose dei worker, macOS). */
void gn_pin_module(void);

#endif /* NAPI_GO_INTERNAL_H */
