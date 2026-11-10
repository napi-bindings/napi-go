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

/* typed.c: typed callback trampoline (Layer 2). Argument and result kinds: */
enum {
  GN_TK_VALUE = 0, /* a napi_value converted in Go */
  GN_TK_F64 = 1,   /* number (double) */
  GN_TK_BOOL = 2,  /* boolean */
  GN_TK_STR = 3,   /* UTF-8 string in a C buffer */
  GN_TK_I32 = 4,   /* result only: napi_create_int32 */
  GN_TK_U32 = 5    /* result only: napi_create_uint32 */
};
/* Size of the stack buffers holding the input strings (shared) and the result string. */
#define GN_TYPED_STRBUF 512
/* gn_typed_sig flags */
#define GN_TF_UNWRAP_THIS 1 /* methods: C reads the native object wrapped by this (napi_unwrap) */
typedef struct {
  uintptr_t id; /* entry in the Go table of typed functions */
  int32_t nargs;
  int32_t flags;
  int32_t kinds[GN_INLINE_ARGS];
} gn_typed_sig;
/* One argument; the double comes first so that Go and C agree on the layout on every ABI. */
typedef struct {
  double f64;
  const char* sp;
  size_t sn;
  napi_value v;
  int32_t kind;
  int32_t b;
} gn_typed_arg;
/* The result: Go sets kind and the matching field, or copies a string into buf (cap bytes). */
typedef struct {
  double f64;
  char* buf;
  size_t cap;
  size_t sn;
  int32_t kind;
  int32_t b;
  int32_t i32;
  uint32_t u32;
} gn_typed_out;
napi_value gn_typed_callback(napi_env env, napi_callback_info info);
uintptr_t gn_typed_sig_new(uintptr_t id, int32_t flags, int32_t nargs, const int32_t* kinds);
void gn_typed_sig_free(uintptr_t s);

/* sigfix.c: restores SA_RESETHAND on Node's signal handlers (macOS). */
void gn_repair_node_signal_handlers(void);

/* onstack.c: adds SA_ONSTACK to the signal handlers installed after the module loads (Linux). */
void gn_install_onstack_hook(void);

/* pin.c: keeps the module mapped in memory until process exit (worker dlclose on macOS). */
void gn_pin_module(void);

#endif /* NAPI_GO_INTERNAL_H */
