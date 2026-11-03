/*
 * sigfix.c — restores SA_RESETHAND on Node's signal handlers after Go runtime startup.
 *
 * Problem (observed on macOS, Go 1.24–1.27, Node 22/24): when a c-shared Go runtime starts,
 * it runs initsig → setsigstack(i) for every pre-existing signal handler it does not own: it
 * reads the current handler via sigaction and reinstalls it with flags | SA_ONSTACK. The macOS
 * kernel enforces SA_RESETHAND when set, but clears it when reporting the current sigaction
 * (verified in CI with experiments/sigflags), so reinstalling the handler loses SA_RESETHAND.
 * Node installs node::SignalExit for SIGINT and SIGTERM with SA_RESETHAND, resets the terminal,
 * and then calls raise(signo) expecting the signal disposition to have reset to SIG_DFL:
 * without SA_RESETHAND the handler re-invokes itself infinitely and the process spins at 100%
 * CPU instead of terminating (when SIGTERM/SIGINT has no JS listener).
 *
 * Fix: for SIGINT and SIGTERM, if the installed handler is the main executable's
 * node::SignalExit and the queried flags lack SA_RESETHAND, reinstall it with SA_RESETHAND
 * (preserving SA_ONSTACK, which Go requires for signals delivered on its threads). We only touch
 * the handler whose original flags we know (node::RegisterSignalHandler(..., reset_handler = true)).
 * On Linux sigaction reports SA_RESETHAND so this is a no-op; on macOS sigaction never reports it,
 * so the reinstallation always runs, which is idempotent and harmless.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dladdr/Dl_info on glibc: must be defined before any include */
#endif
#include "internal.h"

#if !defined(_WIN32)
#include <dlfcn.h>
#include <signal.h>
#include <string.h>

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
  (void)sigaction(signo, &sa, NULL); /* if this fails, keep the existing disposition */
}

void gn_repair_node_signal_handlers(void) {
  repair_one(SIGINT);
  repair_one(SIGTERM);
}

#else /* _WIN32 */
void gn_repair_node_signal_handlers(void) {}
#endif
