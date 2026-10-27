/*
 * guard.c — guardia contro un secondo runtime Go sullo stesso thread (solo macOS x64).
 *
 * Su darwin/amd64 il runtime Go tiene g nello slot TLS fisso 0x30(GS), riservato da Apple a Go
 * (golang/go#23617; cmd/link computeTLSOffset, runtime/sys_darwin_amd64.s). Lo slot è unico per
 * processo, quindi due addon Go se lo contendono: dopo una chiamata nel primo, il suo g resta
 * legato al thread (x_cgo_bindm) e il cgocallback del secondo lo userebbe come proprio
 * (in CI: "fatal error: bad sweepgen in refill").
 *
 * Per ogni thread ricordiamo quale g ha lasciato il nostro runtime all'ultima uscita. Prima di
 * entrare in Go a profondità zero, uno slot non vuoto e diverso significa che un altro runtime
 * possiede il thread: si rifiuta l'ingresso. Le rientranze (Go → JS → Go) passano dal contatore.
 *
 * Limite noto: il runtime rifiutato è comunque avviato e i suoi handler di segnale restano
 * installati. Vedi il README di sys.
 */
#include "internal.h"

#if defined(GN_GUARD_FOREIGN_RUNTIME)

static __thread void* gn_own_g = NULL;
static __thread int gn_depth = 0;

static inline void* gn_tls_g(void) {
  void* g;
  /* Stesso offset di runtime/sys_darwin_amd64.s. */
  __asm__ volatile("movq %%gs:0x30, %0" : "=r"(g));
  return g;
}

int gn_enter(void) {
  if (gn_depth == 0) {
    void* cur = gn_tls_g();
    if (cur != NULL && cur != gn_own_g) return 0;
  }
  gn_depth++;
  return 1;
}

void gn_leave(void) {
  if (--gn_depth == 0) gn_own_g = gn_tls_g();
}

#endif
