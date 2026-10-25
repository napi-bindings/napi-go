/*
 * loader.c — risoluzione dinamica dei simboli Node-API dal processo host.
 *
 *  - Unix:    dlsym(RTLD_DEFAULT, ...)  (i simboli sono esportati dall'eseguibile node)
 *  - Windows: GetProcAddress(GetModuleHandle(NULL), ...)  (node.exe / electron.exe)
 *
 * Nessuna import library e nessuno stub a link-time: il .node non ha dipendenze
 * dichiarate verso node.exe e funziona con qualsiasi host che esporti Node-API.
 */
#if defined(_WIN32)
#  define WIN32_LEAN_AND_MEAN
#  include <windows.h>
#else
#  ifndef _GNU_SOURCE
#    define _GNU_SOURCE /* RTLD_DEFAULT su glibc */
#  endif
#  include <dlfcn.h>
#  include <pthread.h>
#  if defined(__linux__)
#    include <unistd.h>
#    include <sys/syscall.h>
#  endif
#endif
#include <string.h>
#include "gonapi.h"

gn_napi_table gn_napi; /* zero-inizializzata */

static void* gn_sym(const char* name) {
#if defined(_WIN32)
  void* p = (void*)GetProcAddress(GetModuleHandleW(NULL), name);
  if (p == NULL) {
    /* Host che incorporano Node come DLL (libnode.dll / node.dll). */
    HMODULE m = GetModuleHandleW(L"libnode.dll");
    if (m == NULL) m = GetModuleHandleW(L"node.dll");
    if (m != NULL) p = (void*)GetProcAddress(m, name);
  }
  return p;
#else
  return dlsym(RTLD_DEFAULT, name);
#endif
}

static int g_missing = -1;
static const char* g_first_missing = NULL;
static int g_optional_loaded = 0;

static void gn_do_load(void) {
  int missing = 0, opt = 0;
  void* p;
#define GN_LOAD_REQ(ret, name, params, args)                                \
  p = gn_sym(#name);                                                        \
  if (p) { memcpy(&gn_napi.name, &p, sizeof p); }                           \
  else { if (!missing) g_first_missing = #name; missing++; }
#define GN_LOAD_OPT(ret, name, params, args)                                \
  p = gn_sym(#name);                                                        \
  if (p) { memcpy(&gn_napi.name, &p, sizeof p); opt++; }
  GN_NAPI_REQUIRED(GN_LOAD_REQ)
  GN_NAPI_OPTIONAL(GN_LOAD_OPT)
#undef GN_LOAD_REQ
#undef GN_LOAD_OPT
  g_optional_loaded = opt;
  g_missing = missing;
}

#if defined(_WIN32)
static INIT_ONCE g_once = INIT_ONCE_STATIC_INIT;
static BOOL CALLBACK gn_once_cb(PINIT_ONCE o, PVOID p, PVOID* c) {
  (void)o; (void)p; (void)c;
  gn_do_load();
  return TRUE;
}
#else
static pthread_once_t g_once = PTHREAD_ONCE_INIT;
#endif

int gn_load_symbols_once(const char** first_missing) {
#if defined(_WIN32)
  InitOnceExecuteOnce(&g_once, gn_once_cb, NULL, NULL);
#else
  pthread_once(&g_once, gn_do_load);
#endif
  if (first_missing) *first_missing = g_first_missing;
  return g_missing;
}

int gn_symbols_required_count(void) {
  int n = 0;
#define GN_COUNT(ret, name, params, args) n++;
  GN_NAPI_REQUIRED(GN_COUNT)
#undef GN_COUNT
  return n;
}

int gn_symbols_optional_loaded(void) { return g_optional_loaded; }

int gn_has_symbol(const char* name) {
#define GN_HAS(ret, n, params, args) \
  if (strcmp(name, #n) == 0) return gn_napi.n != NULL;
  GN_NAPI_REQUIRED(GN_HAS)
  GN_NAPI_OPTIONAL(GN_HAS)
#undef GN_HAS
  return 0;
}

uint64_t gn_thread_id(void) {
#if defined(_WIN32)
  return (uint64_t)GetCurrentThreadId();
#elif defined(__APPLE__)
  uint64_t tid = 0;
  pthread_threadid_np(NULL, &tid);
  return tid;
#elif defined(__linux__)
  return (uint64_t)syscall(SYS_gettid);
#else
  return (uint64_t)(uintptr_t)pthread_self();
#endif
}
