/*
 * sigflags.c — does SA_RESETHAND survive "read back and reinstall with SA_ONSTACK"?
 *
 * This is the sequence the Go runtime executes in initsig/setsigstack for every pre-existing
 * handler when a c-shared library is loaded. Node installs node::SignalExit for
 * SIGINT/SIGTERM with SA_RESETHAND and then calls raise(): if SA_RESETHAND is lost, the handler
 * re-invokes itself endlessly (observed on macOS with a napi-go addon).
 *
 *   cc -O2 -o sigflags sigflags.c && ./sigflags
 *
 * Results (CI, October 2026):
 *   Linux (glibc):  SA_RESETHAND reported on read-back and preserved → outcome "ok".
 *   macOS 26 arm64: SA_RESETHAND applied (step 2) but NOT reported on read-back (steps 1 and 3),
 *                   so reinstalling with the read-back flags drops it (step 4).
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
  sa.sa_flags = SA_RESETHAND; /* like node::RegisterSignalHandler(..., reset_handler=true) */
  if (sigaction(sig, &sa, NULL) != 0) perror("sigaction");
}

int main(void) {
  const int sig = SIGUSR1;
  int failures = 0;

  install(sig);
  int r1 = flag_set(sig, SA_RESETHAND);
  printf("1. read-back after sigaction(SA_RESETHAND):       SA_RESETHAND %s\n", r1 ? "present" : "MISSING");
  raise(sig);
  int d1 = is_default(sig);
  printf("2. after a signal, handler resets to SIG_DFL:     %s\n", d1 ? "yes" : "NO");

  /* Emulate Go runtime setsigstack: read back and reinstall with flags | SA_ONSTACK. */
  install(sig);
  struct sigaction o;
  memset(&o, 0, sizeof o);
  if (sigaction(sig, NULL, &o) != 0) { perror("sigaction (read-back)"); return 2; }
  o.sa_flags |= SA_ONSTACK;
  if (sigaction(sig, &o, NULL) != 0) { perror("sigaction (reinstall)"); return 2; }
  int r2 = flag_set(sig, SA_RESETHAND);
  printf("3. after read-back + reinstall (setsigstack):     SA_RESETHAND %s\n", r2 ? "present" : "MISSING");
  raise(sig);
  int d2 = is_default(sig);
  printf("4. ... and after a signal, resets to SIG_DFL:     %s\n", d2 ? "yes" : "NO: SA_RESETHAND lost");

  failures += !d1 + !d2;
  printf("handler executed %d times; outcome: %s\n", (int)calls, failures ? "SA_RESETHAND LOST" : "ok");
  return failures ? 1 : 0;
}
