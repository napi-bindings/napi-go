/*
 * loader.c — dynamic resolution of Node-API functions from the host process.
 *
 *  - Unix:    dlsym(RTLD_DEFAULT, ...): symbols are exported by the node executable.
 *  - Windows: GetProcAddress(GetModuleHandle(NULL), ...), falling back to libnode.dll / node.dll
 *             for hosts that embed Node as a shared library.
 *
 * No import library and no undefined symbols at link time: the .node binary has no declared
 * link dependency on node, and functions from newer Node-API versions are detected at runtime
 * (sys.Available).
 */
#if defined(_WIN32)
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#else
#ifndef _GNU_SOURCE
#define _GNU_SOURCE /* RTLD_DEFAULT on glibc */
#endif
#include <dlfcn.h>
#include <pthread.h>
#endif
#include "internal.h"

static void* gn_sym(const char* name) {
#if defined(_WIN32)
  void* p = (void*)GetProcAddress(GetModuleHandleW(NULL), name);
  if (p == NULL) {
    HMODULE m = GetModuleHandleW(L"libnode.dll");
    if (m == NULL) m = GetModuleHandleW(L"node.dll");
    if (m != NULL) p = (void*)GetProcAddress(m, name);
  }
  return p;
#else
  return dlsym(RTLD_DEFAULT, name);
#endif
}

static int gn_missing = 0;
static const char* gn_first_missing = NULL;

static void gn_api_load(void) {
  for (int i = 0; i < GN_API_COUNT; i++) {
    void* p = gn_sym(gn_api_entries[i].name);
    gn_api[i] = p;
    /* Functions newer than the declared version are optional. */
    if (p == NULL && gn_api_entries[i].version <= NAPI_VERSION) {
      if (gn_first_missing == NULL) gn_first_missing = gn_api_entries[i].name;
      gn_missing++;
    }
  }
}

#if defined(_WIN32)
static INIT_ONCE gn_once = INIT_ONCE_STATIC_INIT;
static BOOL CALLBACK gn_once_cb(PINIT_ONCE once, PVOID param, PVOID* ctx) {
  (void)once;
  (void)param;
  (void)ctx;
  gn_api_load();
  return TRUE;
}
#else
static pthread_once_t gn_once = PTHREAD_ONCE_INIT;
#endif

int gn_api_load_once(const char** first_missing) {
#if defined(_WIN32)
  int ok = InitOnceExecuteOnce(&gn_once, gn_once_cb, NULL, NULL) != 0;
#else
  int ok = pthread_once(&gn_once, gn_api_load) == 0;
#endif
  if (!ok) {
    /* Table initialization did not run: fail module load rather than running with NULL pointers. */
    if (first_missing != NULL) *first_missing = "(table load did not run)";
    return GN_API_COUNT;
  }
  if (first_missing != NULL) *first_missing = gn_first_missing;
  return gn_missing;
}
