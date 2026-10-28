/*
 * pin.c — impedisce che il .node venga scaricato mentre il runtime Go è vivo.
 *
 * Problema (osservato su macOS arm64, Go 1.24–1.27, Node 22): quando un worker_thread termina,
 * Node chiude con dlclose gli addon che quel worker ha caricato. Se nessun altro env ha caricato
 * l'addon, il contatore di dlopen arriva a zero e dyld scarica la libreria, ma il runtime Go
 * non può essere fermato: i suoi thread, gli handler di segnale e il distruttore TSD del thread
 * del worker (cgo bindm) restano puntati su codice non più registrato. Risultato: il thread del
 * worker resta in _pthread_tsd_cleanup → _sigtramp → "<unknown binary>" a CPU piena e il processo
 * non termina (Worker::JoinThread → pthread_join). Su Linux non succede perché Go marca i
 * c-shared con DF_1_NODELETE; su macOS i Mach-O non hanno un flag equivalente.
 *
 * Correzione: al primo napi_register_module_v1 il modulo riapre se stesso con
 * RTLD_NOLOAD | RTLD_NODELETE (Unix) o si fissa con GET_MODULE_HANDLE_EX_FLAG_PIN (Windows):
 * da quel momento resta in memoria fino alla fine del processo, come su Linux.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dladdr/Dl_info su glibc: va definito prima di qualsiasi include */
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
  /* RTLD_NOLOAD: la libreria è già caricata, la riapertura aggiunge solo RTLD_NODELETE. Il
     riferimento in più non viene mai rilasciato, di proposito. */
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
