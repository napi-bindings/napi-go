/*
 * sigfix.c — ripristina SA_RESETHAND sugli handler di Node dopo l'avvio del runtime Go.
 *
 * Problema (osservato su macOS, Go 1.24–1.27, Node 22/24): all'avvio di un c-shared il runtime
 * Go esegue initsig → setsigstack(i) per ogni handler preesistente che non gestisce: rilegge
 * l'handler con sigaction e lo reinstalla con flags | SA_ONSTACK. Il kernel di macOS applica
 * SA_RESETHAND ma non lo riporta in lettura (verificato in CI con experiments/sigflags), quindi
 * la reinstallazione lo perde. Node installa node::SignalExit per SIGINT e SIGTERM proprio con
 * SA_RESETHAND, ripristina il terminale e poi fa raise(signo) contando sul ritorno a SIG_DFL:
 * senza SA_RESETHAND l'handler si richiama all'infinito e il processo resta al 100% di CPU
 * invece di terminare (SIGTERM/SIGINT senza listener JS).
 *
 * Correzione: per SIGINT e SIGTERM, se l'handler installato è node::SignalExit dell'eseguibile
 * principale e la lettura non mostra SA_RESETHAND, lo reinstalliamo con SA_RESETHAND (mantenendo SA_ONSTACK,
 * che serve a Go per i segnali consegnati sui suoi thread). Tocchiamo solo l'handler di cui
 * conosciamo i flag originali (node::RegisterSignalHandler(..., reset_handler = true)).
 * Su Linux la lettura riporta SA_RESETHAND e non cambia nulla; su macOS la lettura non lo
 * riporta mai, quindi la reinstallazione avviene sempre: è idempotente e innocua.
 */
#if !defined(_WIN32) && !defined(_GNU_SOURCE)
#define _GNU_SOURCE /* dladdr/Dl_info su glibc: va definito prima di qualsiasi include */
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
  /* Itanium mangling: _ZN4node10SignalExitEi... (siginfo_t su Linux, __siginfo su macOS). */
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
  (void)sigaction(signo, &sa, NULL); /* se fallisce resta il comportamento di prima */
}

void gn_repair_node_signal_handlers(void) {
  repair_one(SIGINT);
  repair_one(SIGTERM);
}

#else /* _WIN32 */
void gn_repair_node_signal_handlers(void) {}
#endif
