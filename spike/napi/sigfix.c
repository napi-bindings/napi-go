/*
 * sigfix.c — restores SA_RESETHAND on Node signal handlers after Go runtime startup.
 *
 * Problem (observed on macOS, Go 1.24–1.27, Node 22/24): when a c-shared library starts up, the
 * Go runtime runs initsig → setsigstack(i) for every pre-existing handler it does not own: it
 * reads back the handler with sigaction and reinstalls it with flags | SA_ONSTACK. The macOS
 * kernel enforces SA_RESETHAND when set, but does not report it back when queried (verified in
 * CI with experiments/sigflags), so reinstalling drops it. Node installs node::SignalExit for
 * SIGINT and SIGTERM specifically with SA_RESETHAND, resets the terminal, and then calls
 * raise(signo) relying on the reset to SIG_DFL: without SA_RESETHAND the handler re-invokes
 * itself endlessly and the process spins at 100% CPU instead of terminating (SIGTERM/SIGINT
 * without a JS listener).
 *
 * Fix: for SIGINT and SIGTERM, if the installed handler is node::SignalExit from the main
 * executable and read-back does not show SA_RESETHAND, reinstall it with SA_RESETHAND (keeping
 * SA_ONSTACK, which Go needs for signals delivered on its own threads). We only touch the
 * handler whose original flags we know (node::RegisterSignalHandler(..., reset_handler = true)).
 * On Linux read-back reports SA_RESETHAND so nothing changes; on macOS read-back never reports
 * it, so reinstallation always runs: it is idempotent and harmless.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dladdr/Dl_info on glibc: must be defined before any include */
#endif
#include "gonapi.h"

#if !defined(_WIN32)
#include <dlfcn.h>
#include <signal.h>
#include <string.h>

static volatile int gn_repaired_mask = 0; /* bit (1 << signo) of repaired signals */

static int is_node_signal_exit(void* fn) {
  Dl_info di;
  memset(&di, 0, sizeof di);
  if (dladdr(fn, &di) == 0 || di.dli_sname == NULL) return 0;
  /* Itanium mangling: _ZN4node10SignalExitEi... (siginfo_t on Linux, __siginfo on macOS). */
  return strncmp(di.dli_sname, "_ZN4node10SignalExitE", 21) == 0;
}

static void repair_one(int signo) {
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  if (sigaction(signo, NULL, &sa) != 0) return;
  void* fn = (sa.sa_flags & SA_SIGINFO) ? (void*)sa.sa_sigaction : (void*)sa.sa_handler;
  if (fn == (void*)SIG_DFL || fn == (void*)SIG_IGN) return;
  if (sa.sa_flags & SA_RESETHAND) return;
  if (!is_node_signal_exit(fn)) return;
  sa.sa_flags |= SA_RESETHAND | SA_ONSTACK;
  if (sigaction(signo, &sa, NULL) == 0) gn_repaired_mask |= (1 << signo);
}

void gn_repair_node_signal_handlers(void) {
  repair_one(SIGINT);
  repair_one(SIGTERM);
}

int gn_signal_repaired(int signo) {
  return signo > 0 && signo < 31 && (gn_repaired_mask & (1 << signo)) != 0;
}

#else /* _WIN32 */
void gn_repair_node_signal_handlers(void) {}
int gn_signal_repaired(int signo) { (void)signo; return 0; }
#endif
