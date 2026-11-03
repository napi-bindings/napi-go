/*
 * gonapi.h — Layer 0 of the napi-go spike.
 *
 * Node-API symbols are NOT linked at build time: they are resolved at runtime from the
 * host process (node.exe, electron.exe, libnode...) and stored in a function pointer
 * table (same approach as napi-sys with the `dyn-symbols` feature).
 *
 * This header is included in the cgo preamble of files that use //export,
 * so it also ends up in _cgo_export.h: it must contain ONLY declarations and
 * `static inline` functions (never non-static definitions).
 *
 * In Phase 1 this X-macro list will be generated from the official headers.
 */
#ifndef GONAPI_H
#define GONAPI_H

#include <stdint.h>
#include <stdlib.h>
#include <stdbool.h>
#include <node_api.h>

/* X(return_type, name, (parameters), (arguments)) — REQUIRED functions (Node-API <= 8) */
#define GN_NAPI_REQUIRED(X)                                                                         \
  X(napi_status, napi_get_last_error_info, (napi_env env, const napi_extended_error_info** r), (env, r)) \
  X(napi_status, napi_get_undefined, (napi_env env, napi_value* r), (env, r))                       \
  X(napi_status, napi_get_null, (napi_env env, napi_value* r), (env, r))                            \
  X(napi_status, napi_get_global, (napi_env env, napi_value* r), (env, r))                          \
  X(napi_status, napi_get_boolean, (napi_env env, bool v, napi_value* r), (env, v, r))              \
  X(napi_status, napi_create_object, (napi_env env, napi_value* r), (env, r))                       \
  X(napi_status, napi_create_array_with_length, (napi_env env, size_t n, napi_value* r), (env, n, r)) \
  X(napi_status, napi_create_double, (napi_env env, double v, napi_value* r), (env, v, r))          \
  X(napi_status, napi_create_int32, (napi_env env, int32_t v, napi_value* r), (env, v, r))          \
  X(napi_status, napi_create_int64, (napi_env env, int64_t v, napi_value* r), (env, v, r))          \
  X(napi_status, napi_create_string_utf8, (napi_env env, const char* s, size_t n, napi_value* r), (env, s, n, r)) \
  X(napi_status, napi_create_function, (napi_env env, const char* name, size_t n, napi_callback cb, void* data, napi_value* r), (env, name, n, cb, data, r)) \
  X(napi_status, napi_create_error, (napi_env env, napi_value code, napi_value msg, napi_value* r), (env, code, msg, r)) \
  X(napi_status, napi_typeof, (napi_env env, napi_value v, napi_valuetype* r), (env, v, r))         \
  X(napi_status, napi_get_value_double, (napi_env env, napi_value v, double* r), (env, v, r))       \
  X(napi_status, napi_get_value_int32, (napi_env env, napi_value v, int32_t* r), (env, v, r))       \
  X(napi_status, napi_get_value_int64, (napi_env env, napi_value v, int64_t* r), (env, v, r))       \
  X(napi_status, napi_get_value_bool, (napi_env env, napi_value v, bool* r), (env, v, r))           \
  X(napi_status, napi_get_value_string_utf8, (napi_env env, napi_value v, char* buf, size_t n, size_t* r), (env, v, buf, n, r)) \
  X(napi_status, napi_set_property, (napi_env env, napi_value o, napi_value k, napi_value v), (env, o, k, v)) \
  X(napi_status, napi_get_property, (napi_env env, napi_value o, napi_value k, napi_value* r), (env, o, k, r)) \
  X(napi_status, napi_set_element, (napi_env env, napi_value o, uint32_t i, napi_value v), (env, o, i, v)) \
  X(napi_status, napi_set_named_property, (napi_env env, napi_value o, const char* k, napi_value v), (env, o, k, v)) \
  X(napi_status, napi_get_cb_info, (napi_env env, napi_callback_info info, size_t* argc, napi_value* argv, napi_value* self, void** data), (env, info, argc, argv, self, data)) \
  X(napi_status, napi_call_function, (napi_env env, napi_value recv, napi_value fn, size_t argc, const napi_value* argv, napi_value* r), (env, recv, fn, argc, argv, r)) \
  X(napi_status, napi_throw, (napi_env env, napi_value err), (env, err))                            \
  X(napi_status, napi_throw_type_error, (napi_env env, const char* code, const char* msg), (env, code, msg)) \
  X(napi_status, napi_is_exception_pending, (napi_env env, bool* r), (env, r))                      \
  X(napi_status, napi_open_handle_scope, (napi_env env, napi_handle_scope* r), (env, r))            \
  X(napi_status, napi_close_handle_scope, (napi_env env, napi_handle_scope s), (env, s))            \
  X(napi_status, napi_create_promise, (napi_env env, napi_deferred* d, napi_value* p), (env, d, p)) \
  X(napi_status, napi_resolve_deferred, (napi_env env, napi_deferred d, napi_value v), (env, d, v)) \
  X(napi_status, napi_reject_deferred, (napi_env env, napi_deferred d, napi_value v), (env, d, v))  \
  X(napi_status, napi_create_threadsafe_function, (napi_env env, napi_value fn, napi_value res, napi_value name, size_t maxq, size_t nthreads, void* fdata, napi_finalize fcb, void* ctx, napi_threadsafe_function_call_js cjs, napi_threadsafe_function* r), (env, fn, res, name, maxq, nthreads, fdata, fcb, ctx, cjs, r)) \
  X(napi_status, napi_call_threadsafe_function, (napi_threadsafe_function f, void* data, napi_threadsafe_function_call_mode m), (f, data, m)) \
  X(napi_status, napi_release_threadsafe_function, (napi_threadsafe_function f, napi_threadsafe_function_release_mode m), (f, m)) \
  X(napi_status, napi_ref_threadsafe_function, (napi_env env, napi_threadsafe_function f), (env, f)) \
  X(napi_status, napi_unref_threadsafe_function, (napi_env env, napi_threadsafe_function f), (env, f)) \
  X(napi_status, napi_set_instance_data, (napi_env env, void* data, napi_finalize cb, void* hint), (env, data, cb, hint)) \
  X(napi_status, napi_get_instance_data, (napi_env env, void** data), (env, data))                  \
  X(napi_status, napi_add_env_cleanup_hook, (napi_env env, void (*fn)(void*), void* arg), (env, fn, arg)) \
  X(napi_status, napi_get_version, (napi_env env, uint32_t* r), (env, r))                           \
  X(napi_status, napi_get_node_version, (napi_env env, const napi_node_version** r), (env, r))

/* OPTIONAL functions (Node-API >= 9): only probed at runtime, never called if absent. */
#define GN_NAPI_OPTIONAL(X)                                                                         \
  X(napi_status, node_api_symbol_for, (napi_env env, const char* s, size_t n, napi_value* r), (env, s, n, r)) \
  X(napi_status, node_api_get_module_file_name, (napi_env env, const char** r), (env, r))           \
  X(napi_status, node_api_create_property_key_utf8, (napi_env env, const char* s, size_t n, napi_value* r), (env, s, n, r))

/* ---- function pointer table ---- */
#define GN_FIELD(ret, name, params, args) ret (NAPI_CDECL *name) params;
typedef struct gn_napi_table {
  GN_NAPI_REQUIRED(GN_FIELD)
  GN_NAPI_OPTIONAL(GN_FIELD)
} gn_napi_table;
#undef GN_FIELD

extern gn_napi_table gn_napi;

/* ---- static inline wrappers: gn_<name>(...) callable from cgo ---- */
#define GN_WRAP(ret, name, params, args) \
  static inline ret gn_##name params { return gn_napi.name args; }
GN_NAPI_REQUIRED(GN_WRAP)
GN_NAPI_OPTIONAL(GN_WRAP)
#undef GN_WRAP

/* ---- loader (loader.c) ---- */
/* Returns 0 if all required symbols were resolved, otherwise the number of
   missing symbols (the first missing symbol name is stored in *first_missing). */
int gn_load_symbols_once(const char** first_missing);
int gn_symbols_required_count(void);
int gn_symbols_optional_loaded(void);
int gn_has_symbol(const char* name);
uint64_t gn_thread_id(void);

/* ---- trampolines (trampoline.c) ---- */
napi_value gn_callback(napi_env env, napi_callback_info info);
void gn_tsfn_call_js(napi_env env, napi_value js_cb, void* ctx, void* data);
void gn_env_cleanup(void* arg);

/* ---- diagnostics (diag.c): __sigInfo(signo) in pure C, without entering Go ---- */
napi_status gn_register_diag(napi_env env, napi_value exports);

/* ---- sigfix.c: restores SA_RESETHAND on node::SignalExit after Go runtime initsig ---- */
void gn_repair_node_signal_handlers(void);
int gn_signal_repaired(int signo);

/* Number of arguments read "inline" on the C stack before entering Go. */
#define GN_INLINE_ARGS 8

/* ---- typed path: primitive conversion entirely in C ----
 * For functions with a known signature (e.g., func(float64, float64) float64), the typed
 * trampoline reads and converts arguments in C, enters Go ONCE with primitive values,
 * and converts the result in C: zero Go→C calls on the happy path. */
enum { GN_K_VALUE = 0, GN_K_F64 = 1, GN_K_I32 = 2, GN_K_BOOL = 3, GN_K_STR = 4, GN_K_UNDEF = 5 };
#define GN_STRBUF 512
typedef struct { const char* p; size_t n; } gn_str;
typedef struct {
  int32_t kind;
  union { double f64; int32_t i32; bool b; napi_value v; gn_str s; } u;
} gn_arg;
typedef struct {
  uintptr_t id;
  int32_t nargs;
  int32_t ret;
  int32_t kinds[GN_INLINE_ARGS];
} gn_sig;
napi_value gn_typed_callback(napi_env env, napi_callback_info info);
static inline napi_status gn_new_typed_function(napi_env env, const char* name, size_t n,
                                                const gn_sig* sig, napi_value* r) {
  return gn_napi.napi_create_function(env, name, n, gn_typed_callback, (void*)sig, r);
}
static inline gn_sig* gn_alloc_sig(void) { return (gn_sig*)calloc(1, sizeof(gn_sig)); }

/* ---- single-crossing cgo "batch" helpers ---- */

/* Result returned by value: no Go pointer passed to C → no escape → zero allocations. */
typedef struct { napi_value v; napi_status s; } gn_value_result;
typedef struct { double v; napi_status s; } gn_double_result;
typedef struct { int32_t v; napi_status s; } gn_int32_result;

static inline gn_value_result gn_new_double(napi_env env, double d) {
  gn_value_result r; r.s = gn_napi.napi_create_double(env, d, &r.v); return r;
}
static inline gn_value_result gn_new_int32(napi_env env, int32_t i) {
  gn_value_result r; r.s = gn_napi.napi_create_int32(env, i, &r.v); return r;
}
static inline gn_double_result gn_get_double(napi_env env, napi_value v) {
  gn_double_result r; r.s = gn_napi.napi_get_value_double(env, v, &r.v); return r;
}
static inline gn_int32_result gn_get_int32(napi_env env, napi_value v) {
  gn_int32_result r; r.s = gn_napi.napi_get_value_int32(env, v, &r.v); return r;
}
static inline gn_value_result gn_undefined(napi_env env) {
  gn_value_result r; r.s = gn_napi.napi_get_undefined(env, &r.v); return r;
}
/* The Go string is passed as ptr+len for the duration of the call only (allowed:
   the bytes contain no Go pointers); V8 copies it. */
static inline gn_value_result gn_new_string_utf8(napi_env env, const char* s, size_t n) {
  gn_value_result r;
  if (s == NULL) { s = ""; n = 0; } /* empty Go string: unsafe.StringData may be nil */
  r.s = gn_napi.napi_create_string_utf8(env, s, n, &r.v);
  return r;
}
static inline napi_status gn_set_instance_id(napi_env env, uintptr_t id) {
  return gn_napi.napi_set_instance_data(env, (void*)id, NULL, NULL);
}
static inline uintptr_t gn_get_instance_id(napi_env env) {
  void* d = NULL;
  if (gn_napi.napi_get_instance_data(env, &d) != napi_ok) return 0;
  return (uintptr_t)d;
}
static inline napi_status gn_call_tsfn(napi_threadsafe_function f) {
  return gn_napi.napi_call_threadsafe_function(f, NULL, napi_tsfn_nonblocking);
}
static inline napi_status gn_abort_tsfn(napi_threadsafe_function f) {
  return gn_napi.napi_release_threadsafe_function(f, napi_tsfn_abort);
}
static inline const char* gn_last_error_message(napi_env env) {
  const napi_extended_error_info* info = NULL;
  if (gn_napi.napi_get_last_error_info(env, &info) != napi_ok || info == NULL) return NULL;
  return info->error_message;
}
/* Single-crossing string read: queries the length and, if it fits into `buf`
   (Go memory, typically stack-allocated), copies it immediately.
   Returns the byte length; *copied = 1 if the content is already in buf. */
static inline napi_status gn_get_string_utf8(napi_env env, napi_value v, char* buf, size_t cap,
                                             size_t* len, int* copied) {
  napi_status s = gn_napi.napi_get_value_string_utf8(env, v, NULL, 0, len);
  *copied = 0;
  if (s != napi_ok) return s;
  if (*len < cap) {
    size_t w = 0;
    s = gn_napi.napi_get_value_string_utf8(env, v, buf, cap, &w);
    *copied = (s == napi_ok);
  }
  return s;
}
/* Creates a JS function pointing to the single trampoline; `id` is an index into the Go
   registry (not a Go pointer), so cgo pointer rules are satisfied. */
static inline napi_status gn_new_function(napi_env env, const char* name, size_t n, uintptr_t id,
                                          napi_value* r) {
  return gn_napi.napi_create_function(env, name, n, gn_callback, (void*)id, r);
}
/* TSFN for the env dispatcher: no JS function, ctx = env ID. */
static inline napi_status gn_new_dispatcher_tsfn(napi_env env, uintptr_t env_id,
                                                 napi_threadsafe_function* r) {
  napi_value name;
  napi_status s = gn_napi.napi_create_string_utf8(env, "napi-go:dispatcher", NAPI_AUTO_LENGTH, &name);
  if (s != napi_ok) return s;
  return gn_napi.napi_create_threadsafe_function(env, NULL, NULL, name, 0, 1, NULL, NULL,
                                                 (void*)env_id, gn_tsfn_call_js, r);
}
static inline napi_status gn_add_cleanup_hook(napi_env env, uintptr_t env_id) {
  return gn_napi.napi_add_env_cleanup_hook(env, gn_env_cleanup, (void*)env_id);
}
/* Throws a JS Error with message and code (ptr+len, without malloc). */
static inline napi_status gn_throw_error(napi_env env, const char* msg, size_t msg_len,
                                         const char* code, size_t code_len) {
  napi_value m, c = NULL, e;
  napi_status s = gn_napi.napi_create_string_utf8(env, msg, msg_len, &m);
  if (s != napi_ok) return s;
  if (code_len > 0) {
    s = gn_napi.napi_create_string_utf8(env, code, code_len, &c);
    if (s != napi_ok) return s;
  }
  s = gn_napi.napi_create_error(env, c, m, &e);
  if (s != napi_ok) return s;
  return gn_napi.napi_throw(env, e);
}
static inline napi_status gn_new_error(napi_env env, const char* msg, size_t msg_len,
                                       const char* code, size_t code_len, napi_value* r) {
  napi_value m, c = NULL;
  napi_status s = gn_napi.napi_create_string_utf8(env, msg, msg_len, &m);
  if (s != napi_ok) return s;
  if (code_len > 0) {
    s = gn_napi.napi_create_string_utf8(env, code, code_len, &c);
    if (s != napi_ok) return s;
  }
  return gn_napi.napi_create_error(env, c, m, r);
}
static inline bool gn_exception_pending(napi_env env) {
  bool b = false;
  gn_napi.napi_is_exception_pending(env, &b);
  return b;
}

#endif /* GONAPI_H */
