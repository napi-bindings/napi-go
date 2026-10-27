/*
 * loader.c — risoluzione dinamica delle funzioni Node-API dal processo host.
 *
 *  - Unix:    dlsym(RTLD_DEFAULT, ...): le funzioni sono esportate dall'eseguibile node.
 *  - Windows: GetProcAddress(GetModuleHandle(NULL), ...), poi libnode.dll / node.dll per gli host
 *             che incorporano Node come DLL.
 *
 * Nessuna import library e nessun simbolo indefinito a link-time: il .node non ha dipendenze
 * dichiarate verso node e le funzioni più nuove si rilevano a runtime (sys.Available).
 */
#if defined(_WIN32)
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#else
#ifndef _GNU_SOURCE
#define _GNU_SOURCE /* RTLD_DEFAULT su glibc */
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
    /* Le funzioni più recenti della versione dichiarata sono facoltative. */
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
    /* La tabella non è stata caricata: il modulo deve fallire invece di girare senza funzioni. */
    if (first_missing != NULL) *first_missing = "(caricamento della tabella non eseguito)";
    return GN_API_COUNT;
  }
  if (first_missing != NULL) *first_missing = gn_first_missing;
  return gn_missing;
}
