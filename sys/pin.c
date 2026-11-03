/*
 * pin.c — prevents the .node shared library from being unloaded while the Go runtime is alive.
 *
 * Problem (observed on macOS arm64, Go 1.24–1.27, Node 22): when a worker_thread exits,
 * Node calls dlclose on addons loaded by that worker. If no other env has loaded the addon,
 * the dlopen reference count drops to zero and dyld unloads the image, even though the Go
 * runtime cannot be stopped: its background threads, signal handlers, and the worker thread's
 * pthread TSD destructor (cgo bindm) remain pointing at unmapped code. As a result, the worker
 * thread spins in _pthread_tsd_cleanup → _sigtramp → "<unknown binary>" at 100% CPU and the
 * process never exits (Worker::JoinThread → pthread_join). On Linux this does not happen
 * because Go links c-shared objects with DF_1_NODELETE; macOS Mach-O binaries have no
 * equivalent linker flag.
 *
 * Fix: on the first napi_register_module_v1 call, the module reopens itself with
 * RTLD_NOLOAD | RTLD_NODELETE (Unix) or pins itself with GET_MODULE_HANDLE_EX_FLAG_PIN (Windows),
 * keeping the image mapped until process exit, just like on Linux.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dladdr/Dl_info on glibc: must be defined before any include */
#endif
#include <stdio.h>
#include "internal.h"

#if !defined(_WIN32)
#include <dlfcn.h>
#include <string.h>

void gn_pin_module(void) {
  static int pinned = 0;
  if (pinned) return;
  Dl_info di;
  memset(&di, 0, sizeof di);
  if (dladdr((void*)&gn_pin_module, &di) == 0 || di.dli_fname == NULL) {
    fprintf(stderr, "napi-go: cannot pin the addon in memory: dladdr failed\n");
    return;
  }
  /* RTLD_NOLOAD: the library is already loaded; reopening only adds RTLD_NODELETE.
     The extra reference is intentionally never closed. */
  if (dlopen(di.dli_fname, RTLD_LAZY | RTLD_NOLOAD | RTLD_NODELETE) == NULL) {
    const char* err = dlerror();
    fprintf(stderr, "napi-go: cannot pin %s in memory: %s\n", di.dli_fname, err ? err : "dlopen failed");
    return;
  }
  pinned = 1;
}

#else /* _WIN32 */
#include <windows.h>

void gn_pin_module(void) {
  static int pinned = 0;
  if (pinned) return;
  HMODULE self = NULL;
  if (!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_PIN,
                          (LPCWSTR)(void*)&gn_pin_module, &self)) {
    fprintf(stderr, "napi-go: cannot pin the addon in memory: GetModuleHandleExW error %lu\n",
            (unsigned long)GetLastError());
    return;
  }
  pinned = 1;
}
#endif
