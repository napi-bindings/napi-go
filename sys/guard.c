/*
 * guard.c — guard against a second Go runtime on the same thread (macOS x64 only).
 *
 * On darwin/amd64 the Go runtime stores g in the fixed TLS slot 0x30(GS), reserved by Apple for Go
 * (golang/go#23617; cmd/link computeTLSOffset, runtime/sys_darwin_amd64.s). The slot is process-wide,
 * so two Go addons contend for it: after a call into the first addon, its g remains bound to the
 * thread (x_cgo_bindm) and the second addon's cgocallback would treat that foreign g as its own
 * (in CI: "fatal error: bad sweepgen in refill").
 *
 * For each thread we record which g our runtime left in TLS on the outermost exit. Before entering
 * Go at depth zero, a non-NULL slot that differs from gn_own_g means another Go runtime owns the
 * thread, so entry is refused. Re-entrant calls (Go → JS → Go) are tracked via gn_depth.
 *
 * Known limitation: the rejected runtime has already started and its signal handlers remain
 * installed. See the sys README.
 */
#include "internal.h"

#if defined(GN_GUARD_FOREIGN_RUNTIME)

static __thread void* gn_own_g = NULL;
static __thread int gn_depth = 0;

static inline void* gn_tls_g(void) {
  void* g;
  /* Same offset as runtime/sys_darwin_amd64.s. */
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
