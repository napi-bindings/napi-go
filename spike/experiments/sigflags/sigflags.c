/*
 * sigflags.c — SA_RESETHAND sopravvive a "rileggi e reinstalla con SA_ONSTACK"?
 *
 * È la sequenza che il runtime Go esegue in initsig/setsigstack per ogni handler
 * preesistente quando viene caricato un c-shared. Node installa node::SignalExit per
 * SIGINT/SIGTERM con SA_RESETHAND e poi fa raise(): se SA_RESETHAND si perde, l'handler
 * si richiama all'infinito (osservato su macOS con un addon napi-go).
 *
 *   cc -O2 -o sigflags sigflags.c && ./sigflags
 */
#include <signal.h>
#include <stdio.h>
#include <string.h>

static volatile sig_atomic_t calls = 0;
static void handler(int s) { (void)s; calls++; }

static int flag_set(int sig, int flag) {
  struct sigaction o;
  memset(&o, 0, sizeof o);
  if (sigaction(sig, NULL, &o) != 0) return -1;
  return (o.sa_flags & flag) != 0;
}

static int is_default(int sig) {
  struct sigaction o;
  memset(&o, 0, sizeof o);
  if (sigaction(sig, NULL, &o) != 0) return -1;
  return o.sa_handler == SIG_DFL;
}

static void install(int sig) {
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  sa.sa_handler = handler;
  sigfillset(&sa.sa_mask);
  sa.sa_flags = SA_RESETHAND; /* come node::RegisterSignalHandler(..., reset_handler=true) */
  if (sigaction(sig, &sa, NULL) != 0) perror("sigaction");
}

int main(void) {
  const int sig = SIGUSR1;
  int failures = 0;

  install(sig);
  int r1 = flag_set(sig, SA_RESETHAND);
  printf("1. rilettura dopo sigaction(SA_RESETHAND):        SA_RESETHAND %s\n", r1 ? "presente" : "ASSENTE");
  raise(sig);
  int d1 = is_default(sig);
  printf("2. dopo un segnale l'handler torna a SIG_DFL:      %s\n", d1 ? "si" : "NO");

  /* Emula setsigstack del runtime Go: rileggi e reinstalla con flags | SA_ONSTACK. */
  install(sig);
  struct sigaction o;
  memset(&o, 0, sizeof o);
  sigaction(sig, NULL, &o);
  o.sa_flags |= SA_ONSTACK;
  sigaction(sig, &o, NULL);
  int r2 = flag_set(sig, SA_RESETHAND);
  printf("3. dopo rilettura + reinstallazione (setsigstack): SA_RESETHAND %s\n", r2 ? "presente" : "ASSENTE");
  raise(sig);
  int d2 = is_default(sig);
  printf("4. ... e dopo un segnale l'handler torna a SIG_DFL: %s\n", d2 ? "si" : "NO: SA_RESETHAND perso");

  failures += !d1 + !d2;
  printf("handler eseguito %d volte; esito: %s\n", (int)calls, failures ? "SA_RESETHAND PERSO" : "ok");
  return failures ? 1 : 0;
}
