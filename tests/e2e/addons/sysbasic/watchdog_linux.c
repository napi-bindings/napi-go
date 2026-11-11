/*
 * watchdog_linux.c — C watchdog of the test addon: after a delay it reports where every thread
 * of the process is executing. It is a plain pthread that uses only async-signal-safe system
 * calls and dprintf, so it works when the Go runtime is stuck (a stop-the-world that never
 * completes, a thread spinning with signals blocked), which a Go timer cannot report.
 *
 * Each thread is interrupted with a real-time signal whose handler records the program counter,
 * link register and stack pointer of the interrupted code. A thread that does not answer has the
 * signal blocked (it is inside another signal handler) or does not return to user mode. The
 * addresses are printed as module+offset: the test resolves those of the addon with
 * go tool addr2line.
 */
#define _GNU_SOURCE
#include <dirent.h>
#include <dlfcn.h>
#include <errno.h>
#include <pthread.h>
#include <signal.h>
#include <stdarg.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/syscall.h>
#include <time.h>
#include <ucontext.h>
#include <unistd.h>

/* Threads sampled per round; a Node process with the addon has about twenty. */
#define GN_MAX_THREADS 128
/* Rounds of samples, GN_ROUND_MS apart: a thread whose pc changes is looping, not parked. */
#define GN_ROUNDS 3
#define GN_ROUND_MS 100
/* Offset from SIGRTMIN of the sampling signal: glibc and musl keep the first ones for themselves. */
#define GN_SAMPLE_SIGNAL_OFFSET 6
#define GN_NSEC_PER_MSEC 1000000L
#define GN_MSEC_PER_SEC 1000

typedef struct {
  uintptr_t pc, lr, sp;
  _Atomic int tid; /* written last: a non-zero tid marks a complete sample */
} gn_sample;

static gn_sample gn_samples[GN_MAX_THREADS];
static atomic_int gn_nsamples;

static void gn_on_sample(int signo, siginfo_t* info, void* context) {
  (void)signo;
  (void)info;
  int i = atomic_fetch_add(&gn_nsamples, 1);
  if (i >= GN_MAX_THREADS) return;
  const ucontext_t* uc = (const ucontext_t*)context;
#if defined(__aarch64__)
  gn_samples[i].pc = (uintptr_t)uc->uc_mcontext.pc;
  gn_samples[i].lr = (uintptr_t)uc->uc_mcontext.regs[30];
  gn_samples[i].sp = (uintptr_t)uc->uc_mcontext.sp;
#elif defined(__x86_64__)
  gn_samples[i].pc = (uintptr_t)uc->uc_mcontext.gregs[REG_RIP];
  gn_samples[i].lr = 0; /* the return address is on the stack */
  gn_samples[i].sp = (uintptr_t)uc->uc_mcontext.gregs[REG_RSP];
#else
  (void)uc;
#endif
  /* gettid cannot fail; the handler makes no other call. */
  atomic_store(&gn_samples[i].tid, (int)syscall(SYS_gettid));
}

/* Set when writing the report fails: stderr is gone, so the watchdog stops sampling. */
static atomic_int gn_report_failed;

/* gn_report writes to stderr. A failure cannot be reported anywhere: it is recorded, and the
   watchdog gives up after the current round. */
static void gn_report(const char* format, ...) {
  va_list args;
  va_start(args, format);
  if (vdprintf(STDERR_FILENO, format, args) < 0) atomic_store(&gn_report_failed, 1);
  va_end(args);
}

static void gn_sleep_ms(unsigned ms) {
  struct timespec ts = {(time_t)(ms / GN_MSEC_PER_SEC), (long)(ms % GN_MSEC_PER_SEC) * GN_NSEC_PER_MSEC};
  while (nanosleep(&ts, &ts) != 0 && errno == EINTR) {
  }
}

/* Prints addr as module+offset, with the nearest exported symbol when there is one. */
static void gn_print_addr(const char* label, uintptr_t addr) {
  Dl_info di;
  if (addr != 0 && dladdr((void*)addr, &di) != 0 && di.dli_fname != NULL) {
    gn_report(" %s=%s+0x%lx", label, di.dli_fname, (unsigned long)(addr - (uintptr_t)di.dli_fbase));
    if (di.dli_sname != NULL) {
      gn_report("(%s+0x%lx)", di.dli_sname, (unsigned long)(addr - (uintptr_t)di.dli_saddr));
    }
    return;
  }
  gn_report(" %s=0x%lx", label, (unsigned long)addr);
}

static void gn_sample_round(int round, int signo) {
  int tids[GN_MAX_THREADS];
  int n = 0;
  int self = (int)syscall(SYS_gettid);
  DIR* dir = opendir("/proc/self/task");
  if (dir == NULL) {
    gn_report("watchdog: /proc/self/task: %s\n", strerror(errno));
    return;
  }
  for (struct dirent* e = readdir(dir); e != NULL && n < GN_MAX_THREADS; e = readdir(dir)) {
    int tid = atoi(e->d_name);
    if (tid > 0 && tid != self) tids[n++] = tid;
  }
  if (closedir(dir) != 0) gn_report("watchdog: closedir: %s\n", strerror(errno));
  for (int i = 0; i < GN_MAX_THREADS; i++) atomic_store(&gn_samples[i].tid, 0);
  atomic_store(&gn_nsamples, 0);
  for (int i = 0; i < n; i++) {
    if (syscall(SYS_tgkill, getpid(), tids[i], signo) != 0 && errno != ESRCH) {
      gn_report("watchdog: tgkill %d: %s\n", tids[i], strerror(errno));
    }
  }
  gn_sleep_ms(GN_ROUND_MS);
  for (int i = 0; i < n; i++) {
    const gn_sample* s = NULL;
    for (int j = 0; j < GN_MAX_THREADS && s == NULL; j++) {
      if (atomic_load(&gn_samples[j].tid) == tids[i]) s = &gn_samples[j];
    }
    gn_report("sample %d thread %d", round, tids[i]);
    if (s == NULL) {
      gn_report(" no reply (signal blocked: inside a signal handler, or not returning to user mode)\n");
      continue;
    }
    gn_print_addr("pc", s->pc);
    gn_print_addr("lr", s->lr);
    gn_report(" sp=0x%lx\n", (unsigned long)s->sp);
  }
}

static void* gn_watchdog_main(void* arg) {
  unsigned ms = (unsigned)(uintptr_t)arg;
  gn_sleep_ms(ms);
  int signo = SIGRTMIN + GN_SAMPLE_SIGNAL_OFFSET;
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  sa.sa_sigaction = gn_on_sample;
  /* SA_ONSTACK: the handler also runs on Go threads, whose goroutine stacks are small. */
  sa.sa_flags = SA_SIGINFO | SA_ONSTACK | SA_RESTART;
  sigfillset(&sa.sa_mask);
  if (sigaction(signo, &sa, NULL) != 0) {
    gn_report("watchdog: sigaction: %s\n", strerror(errno));
    return NULL;
  }
  gn_report("napi-go watchdog (C): process still running after %u ms\n", ms);
  for (int round = 0; round < GN_ROUNDS && !atomic_load(&gn_report_failed); round++) gn_sample_round(round, signo);
  return NULL;
}

/* Starts the watchdog thread; returns 0 or an errno value. */
int gn_start_watchdog(unsigned ms) {
  pthread_t thread;
  pthread_attr_t attr;
  int err = pthread_attr_init(&attr);
  if (err != 0) return err;
  err = pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED);
  if (err == 0) {
    /* The thread must not take the signals of the tests: its handlers run elsewhere. */
    sigset_t all, old;
    sigfillset(&all);
    err = pthread_sigmask(SIG_SETMASK, &all, &old);
    if (err == 0) {
      err = pthread_create(&thread, &attr, gn_watchdog_main, (void*)(uintptr_t)ms);
      int restore = pthread_sigmask(SIG_SETMASK, &old, NULL);
      if (err == 0) err = restore;
    }
  }
  int destroy = pthread_attr_destroy(&attr);
  return err != 0 ? err : destroy;
}
