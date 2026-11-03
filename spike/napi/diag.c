/*
 * diag.c — pure C signal diagnostics (never enters Go).
 *
 * Exports __sigInfo(signo) on the exports object, returning for the calling thread
 * (the JS thread):
 *   - blocked:     is the signal in the thread mask?
 *   - pending:     is the signal pending for the process/thread?
 *   - disposition: "SIG_DFL" | "SIG_IGN" | "handler"
 *   - owner/symbol: library and symbol of the installed handler (dladdr), e.g., node or basic.node
 *   - flags:       SA_ONSTACK, SA_RESETHAND, SA_SIGINFO, SA_RESTART
 * Used to inspect whether the Go runtime alters signal handling in the Node process (macOS:
 * SIGTERM with default action fails to terminate the process when the addon is loaded).
 * Note: on macOS the resethand field is always false because sigaction() never reports
 * SA_RESETHAND when queried (see experiments/sigflags).
 *
 * darwin/amd64 only: __goTlsSlot() returns the contents of TLS slot 0x30(GS), where
 * EVERY Go runtime in the process stores its g pointer (fixed slot reserved by Apple for Go,
 * golang/go#23617). Used to verify that two Go addons contend over it.
 */
#if !defined(_WIN32)
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include <dlfcn.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include "gonapi.h"

static napi_status set_str(napi_env env, napi_value obj, const char* key, const char* val) {
  napi_value v;
  napi_status s = gn_napi.napi_create_string_utf8(env, val ? val : "", NAPI_AUTO_LENGTH, &v);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, obj, key, v);
}

static napi_status set_bool(napi_env env, napi_value obj, const char* key, int val) {
  napi_value v;
  napi_status s = gn_napi.napi_get_boolean(env, val != 0, &v);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, obj, key, v);
}

static napi_value gn_diag_siginfo(napi_env env, napi_callback_info info) {
  size_t argc = 1;
  napi_value argv[1];
  int32_t signo = 0;
  if (gn_napi.napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok ||
      argc < 1 || gn_napi.napi_get_value_int32(env, argv[0], &signo) != napi_ok ||
      signo <= 0 || signo >= NSIG) {
    gn_napi.napi_throw_type_error(env, NULL, "__sigInfo(signo): invalid signal number");
    return NULL;
  }

  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  sigset_t blocked, pending;
  sigemptyset(&blocked);
  sigemptyset(&pending);
  int sa_ok = sigaction(signo, NULL, &sa) == 0;
  int mask_ok = pthread_sigmask(SIG_BLOCK, NULL, &blocked) == 0;
  int pend_ok = sigpending(&pending) == 0;

  const char* disposition = "unknown";
  const char* owner = "";
  const char* symbol = "";
  char addr[32] = "";
  void* fn = NULL;
  if (sa_ok) {
    if (sa.sa_flags & SA_SIGINFO) fn = (void*)sa.sa_sigaction;
    else fn = (void*)sa.sa_handler;
    if (fn == (void*)SIG_DFL) disposition = "SIG_DFL";
    else if (fn == (void*)SIG_IGN) disposition = "SIG_IGN";
    else {
      disposition = "handler";
      snprintf(addr, sizeof addr, "%p", fn);
      Dl_info di;
      if (dladdr(fn, &di) != 0) {
        owner = di.dli_fname;
        symbol = di.dli_sname;
      }
    }
  }

  napi_value obj;
  if (gn_napi.napi_create_object(env, &obj) != napi_ok) return NULL;
  set_str(env, obj, "disposition", disposition);
  set_str(env, obj, "handler", addr);
  set_str(env, obj, "owner", owner);
  set_str(env, obj, "symbol", symbol);
  set_bool(env, obj, "blocked", mask_ok && sigismember(&blocked, signo) == 1);
  set_bool(env, obj, "pending", pend_ok && sigismember(&pending, signo) == 1);
  set_bool(env, obj, "onstack", sa_ok && (sa.sa_flags & SA_ONSTACK));
  set_bool(env, obj, "resethand", sa_ok && (sa.sa_flags & SA_RESETHAND));
  set_bool(env, obj, "siginfo", sa_ok && (sa.sa_flags & SA_SIGINFO));
  set_bool(env, obj, "restart", sa_ok && (sa.sa_flags & SA_RESTART));
  set_bool(env, obj, "repairedByNapiGo", gn_signal_repaired(signo)); /* sigfix.c */
  return obj;
}

/* Test-only: strips SA_RESETHAND from signo's handler, reproducing what setsigstack
   in the Go runtime does on macOS. Allows verifying the bug and fix on Linux too. */
static napi_value gn_diag_emulate_darwin_loss(napi_env env, napi_callback_info info) {
  size_t argc = 1;
  napi_value argv[1];
  int32_t signo = 0;
  if (gn_napi.napi_get_cb_info(env, info, &argc, argv, NULL, NULL) != napi_ok ||
      argc < 1 || gn_napi.napi_get_value_int32(env, argv[0], &signo) != napi_ok ||
      signo <= 0 || signo >= NSIG) {
    gn_napi.napi_throw_type_error(env, NULL, "__sigEmulateDarwinLoss(signo): invalid signal number");
    return NULL;
  }
  struct sigaction sa;
  memset(&sa, 0, sizeof sa);
  int ok = sigaction(signo, NULL, &sa) == 0;
  if (ok) {
    sa.sa_flags &= ~SA_RESETHAND;
    sa.sa_flags |= SA_ONSTACK;
    ok = sigaction(signo, &sa, NULL) == 0;
  }
  napi_value r;
  gn_napi.napi_get_boolean(env, ok, &r);
  return r;
}

/* Test-only: re-runs the sigfix.c repair. */
static napi_value gn_diag_repair(napi_env env, napi_callback_info info) {
  (void)info;
  gn_repair_node_signal_handlers();
  napi_value r;
  gn_napi.napi_get_undefined(env, &r);
  return r;
}

#if defined(__APPLE__) && defined(__x86_64__)
#define GN_HAS_GO_TLS_SLOT 1
#define GN_PTR_TEXT_MAX 32
static napi_value gn_diag_go_tls_slot(napi_env env, napi_callback_info info) {
  (void)info;
  void* g;
  /* Same offset as runtime/sys_darwin_amd64.s and cmd/link computeTLSOffset. */
  __asm__ volatile("movq %%gs:0x30, %0" : "=r"(g));
  char text[GN_PTR_TEXT_MAX];
  snprintf(text, sizeof text, "%p", g);
  napi_value r;
  if (gn_napi.napi_create_string_utf8(env, text, NAPI_AUTO_LENGTH, &r) != napi_ok) {
    static const char msg[] = "__goTlsSlot(): string creation failed";
    static const char code[] = "GO_DIAG";
    gn_throw_error(env, msg, sizeof msg - 1, code, sizeof code - 1);
    return NULL;
  }
  return r;
}
#endif

static napi_status export_fn(napi_env env, napi_value exports, const char* name, napi_callback cb) {
  napi_value fn;
  napi_status s = gn_napi.napi_create_function(env, name, NAPI_AUTO_LENGTH, cb, NULL, &fn);
  if (s != napi_ok) return s;
  return gn_napi.napi_set_named_property(env, exports, name, fn);
}

napi_status gn_register_diag(napi_env env, napi_value exports) {
  napi_status s = export_fn(env, exports, "__sigInfo", gn_diag_siginfo);
  if (s != napi_ok) return s;
  s = export_fn(env, exports, "__sigEmulateDarwinLoss", gn_diag_emulate_darwin_loss);
  if (s != napi_ok) return s;
#ifdef GN_HAS_GO_TLS_SLOT
  s = export_fn(env, exports, "__goTlsSlot", gn_diag_go_tls_slot);
  if (s != napi_ok) return s;
#endif
  return export_fn(env, exports, "__sigRepair", gn_diag_repair);
}

#else /* _WIN32: no POSIX signals */
#include "gonapi.h"
napi_status gn_register_diag(napi_env env, napi_value exports) {
  (void)env; (void)exports;
  return napi_ok;
}
#endif
